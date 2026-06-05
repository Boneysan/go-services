// NATS bridge — subscribes to mirror_service DELTA messages and re-publishes
// entity state as NATS JetStream events. (Phase 1.3)
//
// Wire format notes (from Code Investigation Task 7):
//   mirror_service uses NeL unified network (TCP callback array).
//   DELTA messages are binary: dataset header (sheetId + flags byte) followed
//   by conditional sections for entity adds/removes, property changes, and sync.
//   Entity IDs: TDataSetRow (uint32 local index) + CEntityId (128-bit global).
//   Property changes: TPropertyIndex (uint16) + typed value + TGameCycle timestamp.
//   Tick-synchronous: apply deltas only after TOCK message.
//
// Implementation sequence:
//   1. Connect to naming_service to discover mirror_service endpoint.
//   2. Implement NeL TCP handshake and DELTA message parser.
//   3. Maintain property schema from DATASETS message.
//   4. Publish parsed entity state to NATS on entity.{zone_id}.{entity_id}.
//   5. Verify NATS events are byte-equivalent to mirror_service output (packet capture).
package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	nats "github.com/nats-io/nats.go"
	"github.com/boneysan/ryzom/go-services/internal/config"
)

type EntityEvent struct {
	EntityID    string            `json:"entity_id"`
	ZoneID      string            `json:"zone_id"`
	Tick        uint32            `json:"tick"`
	Seq         uint64            `json:"seq"`
	Position    *Position         `json:"position,omitempty"`
	HPPct       *int              `json:"hp_pct,omitempty"`
	StaPct      *int              `json:"sta_pct,omitempty"`
	Mode        string            `json:"mode,omitempty"`
	Properties  map[string]any    `json:"properties,omitempty"`
	IsPlayer    bool              `json:"is_player"`
	IsFullState bool              `json:"is_full_state"`
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

	// Ensure the entity stream exists. Ignore "already exists" errors on restart.
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
		slog.Info("NATS stream already exists, continuing", "code", apiErr.ErrorCode)
	}

	slog.Info("nats-bridge starting",
		"nats", natsURL,
		"naming", namingHost+":"+namingPort,
	)

	// TODO: implement NeL naming service lookup + mirror_service TCP connection.
	// Connect to namingHost:namingPort, discover mirror_service endpoint,
	// then subscribe to DELTA messages. See Code Investigation Task 7 for binary format.
	// Stub: publish a test heartbeat to verify NATS connectivity.
	_ = namingHost
	_ = namingPort
	_ = publishHeartbeat(js)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("nats-bridge shutting down")
}

func publishHeartbeat(js nats.JetStreamContext) error {
	payload, _ := json.Marshal(map[string]string{"status": "bridge_starting"})
	_, err := js.Publish("server.tick", payload)
	return err
}
