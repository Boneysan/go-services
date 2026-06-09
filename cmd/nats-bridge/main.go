package main

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	nats "github.com/nats-io/nats.go"
	"github.com/boneysan/ryzom/go-services/internal/config"
	"github.com/boneysan/ryzom/go-services/internal/health"
)

// EntityEvent published to NATS (from mirror DELTA)
type EntityEvent struct {
	EntityID    string         `json:"entity_id"`
	ZoneID      string         `json:"zone_id"`
	Tick        uint32         `json:"tick"`
	Seq         uint64         `json:"seq"`
	Position    *Position      `json:"position,omitempty"`
	HPPct       *int           `json:"hp_pct,omitempty"`
	StaPct      *int           `json:"sta_pct,omitempty"`
	Mode        string         `json:"mode,omitempty"`
	Properties  map[string]any `json:"properties,omitempty"`
	IsPlayer    bool           `json:"is_player"`
	IsFullState bool           `json:"is_full_state"`
}

type Position struct {
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Z       float64 `json:"z"`
	Heading float64 `json:"heading"`
}

func main() {
	natsURL := config.Env("NATS_URL", nats.DefaultURL)
	namingHost := config.Env("NEL_NAMING_HOST", "localhost")
	namingPort := config.Env("NEL_NAMING_PORT", "50000")
	myMirrorPort := config.Env("MIRROR_PORT", "47805") // example
	zoneID := config.Env("ZONE_ID", "demo")
	healthAddr := config.Env("BRIDGE_HEALTH_ADDR", ":47806") // observable like logger (Phase 1.3 polish)

	nc, err := nats.Connect(natsURL)
	if err != nil {
		slog.Error("failed to connect to NATS", "url", natsURL, "err", err)
		os.Exit(1)
	}
	defer nc.Close()

	js, err := nc.JetStream()
	if err != nil {
		slog.Error("failed to get JetStream context", "err", err)
		os.Exit(1)
	}

	_, err = js.AddStream(&nats.StreamConfig{
		Name:     "ENTITY_EVENTS",
		Subjects: []string{"entity.>", "zone.>", "server.tick"},
	})
	if err != nil {
		var apiErr *nats.APIError
		if !errors.As(err, &apiErr) {
			slog.Error("failed to create NATS stream", "err", err)
			os.Exit(1)
		}
		slog.Info("NATS stream already exists", "code", apiErr.ErrorCode)
	}

	slog.Info("nats-bridge starting",
		"nats", natsURL,
		"naming", namingHost+":"+namingPort,
		"health", healthAddr,
	)

	// Minimal health server (reuses internal/health; makes bridge observable in compose/healthchecks)
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /health", health.Handler(map[string]string{"nats": natsURL, "zone": zoneID}))
		hs := &http.Server{Addr: healthAddr, Handler: mux}
		slog.Info("nats-bridge health starting", "addr", healthAddr)
		if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("nats-bridge health exited", "err", err)
		}
	}()

	// Phase 1.3 Step 1: Do real(ish) naming lookup for mirror_service, then bridge DELTAs.
	go func() {
		mirrorAddr := lookupMirrorViaNaming(namingHost, namingPort)
		if mirrorAddr == "" {
			mirrorAddr = net.JoinHostPort(namingHost, myMirrorPort) // fallback
			slog.Warn("using fallback mirror addr", "addr", mirrorAddr)
		}
		connectToMirrorAndBridge(js, mirrorAddr, namingHost, namingPort, zoneID)
	}()

	// For testing the parser without full mirror (set TEST_DELTA=1):
	if os.Getenv("TEST_DELTA") != "" {
		go func() {
			// Example mock DELTA payload (after "DELTA" name): gamecycle + sheet + header + add section
			mock := make([]byte, 0)
			mock = binary.LittleEndian.AppendUint32(mock, 12345) // gamecycle
			mock = binary.LittleEndian.AppendUint32(mock, 42)    // sheetId
			mock = append(mock, 0x02)                            // header = adding
			// one add: row, 16-byte eid, spawner
			mock = binary.LittleEndian.AppendUint32(mock, 100)
			eid := make([]byte, 16)
			eid[0] = 0xAA
			mock = append(mock, eid...)
			mock = append(mock, 5) // spawner
			mock = binary.LittleEndian.AppendUint32(mock, 0xffffffff) // end
			parseAndPublishDelta(js, mock, 0, zoneID)
			slog.Info("TEST_DELTA: sent mock parse", "zone", zoneID)

			// also exercise TOCK path
			tockPayload, _ := json.Marshal(map[string]uint32{"tick": 12345})
			_, _ = js.Publish("server.tick", tockPayload)
			slog.Info("TEST_DELTA: sent mock TOCK", "zone", zoneID)

			// Demo: publish a sample entity event with position for testing consumers
			sampleEvt := EntityEvent{
				EntityID: "DEMO_ENTITY_001",
				ZoneID:   zoneID,
				Tick:     12345,
				Position: &Position{X: 100.5, Y: 50.0, Z: 0.0, Heading: 90.0},
				HPPct:    func() *int { i := 100; return &i }(),
				IsPlayer: true,
			}
			sampleB, _ := json.Marshal(sampleEvt)
			_, _ = js.Publish("entity."+zoneID+".DEMO_ENTITY_001", sampleB)
			slog.Info("TEST_DELTA: published sample entity event", "zone", zoneID)
		}()
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("nats-bridge shutting down")
}

// lookupMirrorViaNaming does a minimal connect + lookup for "MS" (mirror_service).
// Real NeL naming uses specific binary messages; this is a practical starting point.
func lookupMirrorViaNaming(host, port string) string {
	addr := net.JoinHostPort(host, port)
	for attempt := 0; attempt < 5; attempt++ {
		conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
		if err != nil {
			slog.Warn("naming lookup connect failed, retrying", "addr", addr, "attempt", attempt, "err", err)
			time.Sleep(2 * time.Second)
			continue
		}
		defer conn.Close()

		// Send a simple lookup request (approximation of NeL naming query for service "MS")
		_, _ = conn.Write([]byte("LOOKUP MS\n"))
		// Read response (expect something like "MS <ip:port>" or raw addr)
		buf := make([]byte, 256)
		n, _ := conn.Read(buf)
		resp := string(buf[:n])
		slog.Info("naming lookup response", "resp", resp)

		// Crude parse: look for host:port in response
		if strings.Contains(resp, ":") {
			// take last token that looks like addr
			fields := strings.Fields(resp)
			for _, f := range fields {
				if strings.Contains(f, ":") {
					return f
				}
			}
		}
		return ""
	}
	slog.Error("naming lookup failed after retries", "addr", addr)
	return ""
}

// connectToMirrorAndBridge implements the core of the NATS bridge (Task 1.3 Step 1).
// Connects to mirror_service (via naming lookup with retries), reads DELTA/TOCK messages (per Investigation Task 7 + CDeltaToMS),
// and republishes entity state to NATS for consumers (Godot proxy, GM dashboard, etc.).
// To fully validate/retire mirror for new consumers: run with live C++ mirror (after local build), capture real DELTA packets, confirm no data loss.
func connectToMirrorAndBridge(js nats.JetStreamContext, mirrorAddr, namingHost, namingPort, zoneID string) {
	conn, err := net.Dial("tcp", mirrorAddr)
	if err != nil {
		slog.Error("mirror connect failed", "addr", mirrorAddr, "err", err)
		return
	}
	defer conn.Close()
	slog.Info("connected to mirror_service for DELTA bridge", "addr", mirrorAddr, "zone", zoneID)

	// Read loop for NeL CMessages containing "DELTA" and "TOCK".
	// DELTA format (from source + checklist): gamecycle, per-dataset (sheetId + header flags + sections:
	// adds (row + CEntityId + spawner), removes, prop changes (propIndex + (row, ts, value)...)).
	buf := make([]byte, 8192)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			slog.Warn("mirror read err, continuing for demo (real impl may reconnect)", "err", err)
			time.Sleep(1 * time.Second)
			continue
		}
		data := buf[:n]

		// Real-ish DELTA/TOCK parser (followup 2: hardened from heuristic)
		// Based on CDeltaToMS + Investigation #7: after CMessage name,
		// uint32 gamecycle, then per dataset: uint32 sheetId, uint8 header (flags),
		// conditional sections for adds (row+CEntityId+spawner), removes, props.
		name, off := extractCMessageName(data)
		if name == "DELTA" {
			// Hardened parser: real binary for CDeltaToMS / Investigation #7
			// After CMessage name: [optional gamecycle uint32], then per-dataset:
			// uint32 sheetId, uint8 header (flags: 1=bind,2=add,4=rem,8=prop,16=sync)
			// then conditional sections: adds (uint32 row + [16]eid + uint8 spawner ... until 0xffffffff),
			// removes, props (uint16 propIdx + (row, ts, value)...)
			parseAndPublishDelta(js, data, off, zoneID)
		}

		if name == "TOCK" {
			payload, _ := json.Marshal(map[string]uint32{"tick": uint32(time.Now().Unix())})
			_, _ = js.Publish("server.tick", payload)
			slog.Debug("published TOCK to NATS")
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) > 0 && (s[0:len(sub)] == sub || contains(s[1:], sub)))
}

// extractCMessageName finds a CMessage name (uint32 len + string after 4-byte header)
func extractCMessageName(data []byte) (string, int) {
	if len(data) < 8 {
		return "", 0
	}
	off := 4
	nameLen := binary.LittleEndian.Uint32(data[off : off+4])
	off += 4
	if len(data) < off+int(nameLen) {
		return "", 0
	}
	return string(data[off : off+int(nameLen)]), off + int(nameLen)
}

// parseAndPublishDelta does proper little-endian parsing of a DELTA payload (after CMessage name).
// Uses the layout from CDeltaToMS and mirror delta sections.
func parseAndPublishDelta(js nats.JetStreamContext, data []byte, start int, zoneID string) {
	pos := start
	if pos+4 > len(data) {
		return
	}
	// Try gamecycle (often present)
	gamecycle := binary.LittleEndian.Uint32(data[pos:])
	pos += 4
	// zoneID passed from caller for consistency with compose env

	for pos+5 <= len(data) {  // sheetId + header at minimum
		sheetId := binary.LittleEndian.Uint32(data[pos:])
		pos += 4
		header := data[pos]
		pos++

		evtBase := EntityEvent{
			Tick:       gamecycle,
			Properties: map[string]any{"sheetId": sheetId, "header": header},
		}

		// Parse sections based on flags (from CDeltaToMS and investigation)
		if header&0x02 != 0 { // AddingBit
			// adds: uint32 row, 16-byte CEntityId, uint8 spawner, ... until INVALID (0xffffffff)
			for pos+4+16+1 <= len(data) {
				row := binary.LittleEndian.Uint32(data[pos:])
				pos += 4
				if row == 0xffffffff {
					break
				}
				eid := data[pos : pos+16]
				pos += 16
				spawner := data[pos]
				pos++
				eidStr := hex.EncodeToString(eid)
				evt := evtBase
				evt.EntityID = eidStr
				evt.IsPlayer = true // heuristic
				evt.IsFullState = true
				// Demo: add sample position (real would come from prop changes or initial state in live mirror)
				evt.Position = &Position{X: 100.0 + float64(row%100), Y: 50.0, Z: 0.0, Heading: 0.0}
				evt.Properties = map[string]any{"sheetId": sheetId, "row": row, "spawner": spawner, "action": "add"}
				b, _ := json.Marshal(evt)
				subject := "entity." + zoneID + "." + eidStr
				_, _ = js.Publish(subject, b)
				slog.Info("published entity add from DELTA", "zone", zoneID, "sheet", sheetId, "eid", eidStr)
			}
		}

		if header&0x04 != 0 { // RemovingBit
			for pos+4 <= len(data) {
				row := binary.LittleEndian.Uint32(data[pos:])
				pos += 4
				if row == 0xffffffff {
					break
				}
				// For removes, we may not have eid easily; publish generic
				evt := evtBase
				evt.Properties = map[string]any{"sheetId": sheetId, "row": row, "action": "remove"}
				b, _ := json.Marshal(evt)
				subject := "entity." + zoneID + ".remove." + fmt.Sprintf("%d", row)
				_, _ = js.Publish(subject, b)
				slog.Debug("published entity remove from DELTA", "zone", zoneID, "sheet", sheetId, "row", row)
			}
		}

		if header&0x08 != 0 { // PropChangeBit
			for pos+2 <= len(data) {
				propIdx := binary.LittleEndian.Uint16(data[pos:])
				pos += 2
				if pos+4+4+4 > len(data) {
					break
				}
				row := binary.LittleEndian.Uint32(data[pos:])
				pos += 4
				ts := binary.LittleEndian.Uint32(data[pos:])
				pos += 4
				val := binary.LittleEndian.Uint32(data[pos:]) // simplistic, real depends on prop type
				pos += 4
				evt := evtBase
				evt.Properties = map[string]any{"sheetId": sheetId, "prop": propIdx, "row": row, "ts": ts, "val": val, "action": "prop"}
				b, _ := json.Marshal(evt)
				subject := "entity." + zoneID + "." + fmt.Sprintf("%d", row)
				_, _ = js.Publish(subject, b)
				slog.Debug("published prop change from DELTA", "zone", zoneID, "sheet", sheetId, "row", row, "prop", propIdx)
			}
		}
	}
}


