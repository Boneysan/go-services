package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/boneysan/ryzom/go-services/internal/config"
	"github.com/boneysan/ryzom/go-services/internal/health"
)

// LogEntry for HTTP /log from Go services
type LogEntry struct {
	Service   string            `json:"service"`
	Level     string            `json:"level"`
	Timestamp string            `json:"timestamp"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields,omitempty"`
}

// NeL types (subset, matching serial() in logger_service_itf.h)
type TSupportedParamType uint8

const (
	spt_uint32 TSupportedParamType = iota
	spt_uint64
	spt_sint32
	spt_float
	spt_string
	spt_entityId
	spt_sheetId
	spt_itemId
	spt_invalid
)

type TParamDesc struct {
	Name string
	Type TSupportedParamType
	List bool
}

type TParamValue struct {
	Type     TSupportedParamType
	UInt32   uint32
	UInt64   uint64
	SInt32   int32
	Float    float32
	String   string
	EntityId [16]byte
	SheetId  uint32
	ItemId   uint64
}

type TListParamValues struct {
	Params []TParamValue
}

type TLogDefinition struct {
	LogName    string
	Context    bool
	LogText    string
	Params     []TParamDesc
	ListParams []TParamDesc
}

type TLogInfo struct {
	LogName    string
	TimeStamp  uint32
	Params     []TParamValue
	ListParams []TListParamValues
}

var (
	currentSchema []TLogDefinition
	schemaMu      sync.Mutex
)

func main() {
	httpAddr := config.Env("LOGGER_ADDR", ":47803")
	logFile := config.Env("LOGGER_FILE", "")
	tcpAddr := config.Env("LOGGER_TCP_ADDR", ":47804")
	namingHost := config.Env("NEL_NAMING_HOST", "localhost")
	namingPort := config.Env("NEL_NAMING_PORT", "50000")

	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	stdoutHandler := slog.NewJSONHandler(os.Stdout, opts)
	var logger *slog.Logger

	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			slog.Error("failed to open log file", "path", logFile, "err", err)
			os.Exit(1)
		}
		defer f.Close()
		mw := io.MultiWriter(os.Stdout, f)
		fileHandler := slog.NewJSONHandler(mw, opts)
		logger = slog.New(fileHandler)
		slog.Info("logger also writing to file", "path", logFile)
	} else {
		logger = slog.New(stdoutHandler)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /log", func(w http.ResponseWriter, r *http.Request) {
		var entry LogEntry
		if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
			http.Error(w, `{"error":"invalid json","code":"bad_request"}`, http.StatusBadRequest)
			return
		}
		if entry.Timestamp == "" {
			entry.Timestamp = time.Now().UTC().Format(time.RFC3339)
		}
		logger.Info(entry.Message,
			"service", entry.Service,
			"level", entry.Level,
			"timestamp", entry.Timestamp,
			"fields", entry.Fields,
		)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /health", health.Handler(map[string]string{"log_file": logFile, "tcp": tcpAddr}))

	server := &http.Server{Addr: httpAddr, Handler: mux}

	go func() {
		slog.Info("logger HTTP starting", "addr", httpAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("logger HTTP exited", "err", err)
			os.Exit(1)
		}
	}()

	if tcpAddr != "" {
		go runTCPShim(logger, tcpAddr)
		// Register as LGS with naming so C++ services can discover us (Phase 1.2)
		// Use compose service name for internal addr
		fullAddr := "logger" + tcpAddr
		go registerWithNaming(logger, namingHost, namingPort, fullAddr)
		// For testing registration: set NEL_NAMING_HOST/PORT to a mock or use the compose nel-naming; logs will show "sent RG" + response.
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("logger shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
	slog.Info("logger stopped")
}

// === Full NeL parser for TCP shim (Task 1.2) ===

func parseTParamValue(data []byte, pos *int) (TParamValue, error) {
	if *pos >= len(data) {
		return TParamValue{}, io.ErrUnexpectedEOF
	}
	v := TParamValue{Type: TSupportedParamType(data[*pos])}
	*pos++
	switch v.Type {
	case spt_uint32:
		if *pos+4 > len(data) { return v, io.ErrUnexpectedEOF }
		v.UInt32 = binary.LittleEndian.Uint32(data[*pos:])
		*pos += 4
	case spt_uint64:
		if *pos+8 > len(data) { return v, io.ErrUnexpectedEOF }
		v.UInt64 = binary.LittleEndian.Uint64(data[*pos:])
		*pos += 8
	case spt_sint32:
		if *pos+4 > len(data) { return v, io.ErrUnexpectedEOF }
		v.SInt32 = int32(binary.LittleEndian.Uint32(data[*pos:]))
		*pos += 4
	case spt_float:
		if *pos+4 > len(data) { return v, io.ErrUnexpectedEOF }
		bits := binary.LittleEndian.Uint32(data[*pos:])
		v.Float = math.Float32frombits(bits)
		*pos += 4
	case spt_string:
		s, n, err := readNeLString(data, pos)
		if err != nil { return v, err }
		v.String = s
		*pos += n // consume content (readNeLString only advanced past length prefix)
	case spt_entityId:
		if *pos+16 > len(data) { return v, io.ErrUnexpectedEOF }
		copy(v.EntityId[:], data[*pos:*pos+16])
		*pos += 16
	case spt_sheetId:
		if *pos+4 > len(data) { return v, io.ErrUnexpectedEOF }
		v.SheetId = binary.LittleEndian.Uint32(data[*pos:])
		*pos += 4
	case spt_itemId:
		if *pos+8 > len(data) { return v, io.ErrUnexpectedEOF }
		v.ItemId = binary.LittleEndian.Uint64(data[*pos:])
		*pos += 8
	}
	return v, nil
}

func readNeLString(data []byte, pos *int) (string, int, error) {
	if *pos+4 > len(data) { return "", 0, io.ErrUnexpectedEOF }
	l := int(binary.LittleEndian.Uint32(data[*pos:]))
	*pos += 4
	if *pos+l > len(data) { return "", 0, io.ErrUnexpectedEOF }
	s := string(data[*pos : *pos+l])
	return s, l, nil
}

func parseTParamDesc(data []byte, pos *int) (TParamDesc, error) {
	name, _, err := readNeLString(data, pos)
	if err != nil { return TParamDesc{}, err }
	if *pos >= len(data) { return TParamDesc{}, io.ErrUnexpectedEOF }
	t := TSupportedParamType(data[*pos])
	*pos++
	if *pos >= len(data) { return TParamDesc{}, io.ErrUnexpectedEOF }
	list := data[*pos] != 0
	*pos++
	return TParamDesc{Name: name, Type: t, List: list}, nil
}

func parseVectorTParamDesc(data []byte, pos *int) ([]TParamDesc, error) {
	if *pos+4 > len(data) { return nil, io.ErrUnexpectedEOF }
	count := int(binary.LittleEndian.Uint32(data[*pos:]))
	*pos += 4
	res := make([]TParamDesc, count)
	for i := 0; i < count; i++ {
		d, err := parseTParamDesc(data, pos)
		if err != nil { return nil, err }
		res[i] = d
	}
	return res, nil
}

func parseTLogDefinition(data []byte, pos *int) (TLogDefinition, error) {
	name, nameLen, err := readNeLString(data, pos)
	if err != nil { return TLogDefinition{}, err }
	*pos += nameLen
	if *pos >= len(data) { return TLogDefinition{}, io.ErrUnexpectedEOF }
	ctx := data[*pos] != 0
	*pos++
	text, textLen, err := readNeLString(data, pos)
	if err != nil { return TLogDefinition{}, err }
	*pos += textLen
	params, err := parseVectorTParamDesc(data, pos)
	if err != nil { return TLogDefinition{}, err }
	listParams, err := parseVectorTParamDesc(data, pos)
	if err != nil { return TLogDefinition{}, err }
	return TLogDefinition{LogName: name, Context: ctx, LogText: text, Params: params, ListParams: listParams}, nil
}

func parseVectorTLogDefinition(data []byte, pos *int) ([]TLogDefinition, error) {
	if *pos+4 > len(data) { return nil, io.ErrUnexpectedEOF }
	count := int(binary.LittleEndian.Uint32(data[*pos:]))
	*pos += 4
	res := make([]TLogDefinition, count)
	for i := 0; i < count; i++ {
		d, err := parseTLogDefinition(data, pos)
		if err != nil { return nil, err }
		res[i] = d
	}
	return res, nil
}

func parseTParamValueList(data []byte, pos *int) ([]TParamValue, error) {
	if *pos+4 > len(data) { return nil, io.ErrUnexpectedEOF }
	count := int(binary.LittleEndian.Uint32(data[*pos:]))
	*pos += 4
	res := make([]TParamValue, count)
	for i := 0; i < count; i++ {
		v, err := parseTParamValue(data, pos)
		if err != nil { return nil, err }
		res[i] = v
	}
	return res, nil
}

func parseTListParamValues(data []byte, pos *int) (TListParamValues, error) {
	params, err := parseTParamValueList(data, pos)
	if err != nil { return TListParamValues{}, err }
	return TListParamValues{Params: params}, nil
}

func parseVectorTListParamValues(data []byte, pos *int) ([]TListParamValues, error) {
	if *pos+4 > len(data) { return nil, io.ErrUnexpectedEOF }
	count := int(binary.LittleEndian.Uint32(data[*pos:]))
	*pos += 4
	res := make([]TListParamValues, count)
	for i := 0; i < count; i++ {
		l, err := parseTListParamValues(data, pos)
		if err != nil { return nil, err }
		res[i] = l
	}
	return res, nil
}

func parseTLogInfo(data []byte, pos *int) (TLogInfo, error) {
	name, nameLen, err := readNeLString(data, pos)
	if err != nil { return TLogInfo{}, err }
	*pos += nameLen // consume the string content bytes (readNeLString only advanced past the length prefix)
	if *pos+4 > len(data) { return TLogInfo{}, io.ErrUnexpectedEOF }
	ts := binary.LittleEndian.Uint32(data[*pos:])
	*pos += 4
	params, err := parseTParamValueList(data, pos)
	if err != nil { return TLogInfo{}, err }
	listParams, err := parseVectorTListParamValues(data, pos)
	if err != nil { return TLogInfo{}, err }
	return TLogInfo{LogName: name, TimeStamp: ts, Params: params, ListParams: listParams}, nil
}

func parseVectorTLogInfo(data []byte, pos *int) ([]TLogInfo, error) {
	if *pos+4 > len(data) { return nil, io.ErrUnexpectedEOF }
	count := int(binary.LittleEndian.Uint32(data[*pos:]))
	*pos += 4
	res := make([]TLogInfo, count)
	for i := 0; i < count; i++ {
		li, err := parseTLogInfo(data, pos)
		if err != nil { return nil, err }
		res[i] = li
	}
	return res, nil
}

func runTCPShim(logger *slog.Logger, addr string) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Error("failed to listen on TCP shim port", "addr", addr, "err", err)
		return
	}
	defer ln.Close()
	logger.Info("logger TCP shim listening (NeL compat mode)", "addr", addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			logger.Error("TCP shim accept error", "err", err)
			continue
		}
		go handleNeLClient(logger, conn)
	}
}

func handleNeLClient(logger *slog.Logger, c net.Conn) {
	defer c.Close()
	peer := c.RemoteAddr().String()
	logger.Info("C++ client connected to logger TCP shim (LGS)", "peer", peer)

	buf := make([]byte, 0, 8192)
	tmp := make([]byte, 4096)

	for {
		n, err := c.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			for len(buf) > 0 {
				consumed, name, ok := tryParseCMessage(buf)
				if !ok {
					break
				}
				payload := buf[consumed:]
				buf = buf[consumed:]

				logger.Info("received NeL module message on LGS shim", "peer", peer, "name", name)

				switch name {
				case "RC", "registerClient":
					pos := 0
					if len(payload) >= 4 {
						shard := binary.LittleEndian.Uint32(payload[pos:])
						pos += 4
						defs, err := parseVectorTLogDefinition(payload, &pos)
						if err == nil && len(defs) > 0 {
							schemaMu.Lock()
							currentSchema = defs
							schemaMu.Unlock()
							logger.Info("C++ client registered log schema (RC)", "peer", peer, "shard", shard, "defs", len(defs))
						}
					}
				case "LG", "reportLog":
					pos := 0
					infos, err := parseVectorTLogInfo(payload, &pos)
					if err == nil {
						schemaMu.Lock()
						schema := currentSchema
						schemaMu.Unlock()
						for _, li := range infos {
							fields := map[string]any{"timestamp": li.TimeStamp}
							for j, p := range li.Params {
								key := fmt.Sprintf("param%d", j)
								if len(schema) > 0 && len(schema[0].Params) > j {
									key = schema[0].Params[j].Name
								}
								fields[key] = paramValueToAny(p)
							}
							logger.Info(li.LogName, "service", "C++", "fields", fields)
						}
					}
				}
			}
			if len(buf) > 8192 {
				buf = buf[len(buf)-4096:]
			}
		}
		if err != nil {
			if err != io.EOF {
				logger.Debug("TCP shim read error", "peer", peer, "err", err)
			}
			return
		}
	}
}

func tryParseCMessage(data []byte) (consumed int, name string, ok bool) {
	if len(data) < 8 {
		return 0, "", false
	}
	off := 4
	if len(data) < off+4 {
		return 0, "", false
	}
	nameLen := binary.LittleEndian.Uint32(data[off : off+4])
	off += 4
	if len(data) < off+int(nameLen) {
		return 0, "", false
	}
	name = string(data[off : off+int(nameLen)])
	consumed = off + int(nameLen)
	if name == "registerClient" {
		name = "RC"
	}
	if name == "reportLog" {
		name = "LG"
	}
	return consumed, name, true
}

func paramValueToAny(p TParamValue) any {
	switch p.Type {
	case spt_uint32:
		return p.UInt32
	case spt_uint64:
		return p.UInt64
	case spt_sint32:
		return p.SInt32
	case spt_float:
		return p.Float
	case spt_string:
		return p.String
	case spt_entityId:
		return hex.EncodeToString(p.EntityId[:])
	case spt_sheetId:
		return p.SheetId
	case spt_itemId:
		return p.ItemId
	}
	return nil
}

// === Naming registration (Phase 1.2, improved from stub)
func registerWithNaming(logger *slog.Logger, namingHost, namingPort, myTCPAddr string) {
	addr := net.JoinHostPort(namingHost, namingPort)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		logger.Error("naming registration failed", "addr", addr, "err", err)
		return
	}
	defer conn.Close()

	// Build approximate CMessage "RG" (from naming_client.cpp: "RG", serial name, serialCont addr, serial sid)
	// CMessage: 4-byte header, uint32 nameLen + "RG", then payload.
	header := []byte{0, 0, 0, 0}
	name := "RG"
	nameLen := binary.LittleEndian.AppendUint32(nil, uint32(len(name)))
	payload := []byte{}
	// serial name "LGS"
	lgs := "LGS"
	payload = binary.LittleEndian.AppendUint32(payload, uint32(len(lgs)))
	payload = append(payload, []byte(lgs)...)
	// serialCont addr: count=1, then serial CInetAddress (approx as string len + bytes for host:port)
	addrs := []string{myTCPAddr}
	payload = binary.LittleEndian.AppendUint32(payload, uint32(len(addrs)))
	for _, a := range addrs {
		payload = binary.LittleEndian.AppendUint32(payload, uint32(len(a)))
		payload = append(payload, []byte(a)...)
	}
	// serial sid (uint16 0 for dynamic)
	payload = binary.LittleEndian.AppendUint16(payload, 0)

	msg := append(header, nameLen...)
	msg = append(msg, []byte(name)...)
	msg = append(msg, payload...)

	_, err = conn.Write(msg)
	if err != nil {
		logger.Error("naming send failed", "err", err)
		return
	}
	logger.Info("sent RG register to naming as LGS", "naming", addr, "myAddr", myTCPAddr)

	// Basic response handling (naming replies with "RG" or error)
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		logger.Warn("naming registration response read failed (may be async)", "err", err)
	} else {
		resp := string(buf[:n])
		logger.Info("naming registration response", "resp", resp)
		if len(resp) >= 2 && resp[:2] == "RG" {
			logger.Info("naming registration acknowledged as LGS")
		}
	}
	// TODO: full keepalive with "RRG", sid handling, periodic re-reg, etc.
	// For now, connection can be closed; real impl would keep it for updates.
}
