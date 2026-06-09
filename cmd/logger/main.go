// Logger service — Go replacement for C++ logger_service (Phase 1.2)
//
// Current status:
//   - HTTP POST /log : primary path for new Go services (structured JSON logs via slog).
//   - GET /health    : for Docker healthchecks and readiness.
//
// Next (Task 1.2 TCP shim):
//   - Listen for NeL module interface TCP connections (as service "LGS").
//   - Handle "RC" (registerClient) + "LG" (reportLog) using the TLogInfo / TParamValue
//     binary layout from logger_service_itf.h (see Code_Investigation_Checklist.md Task 6).
//   - Register with nel-naming service so legacy C++ clients can discover it.
//   - Run alongside the old C++ logger_service until proven, then retire it.
//
// Wire format notes (from Code Investigation Task 6):
//   C++ services communicate via NeL module interface using TCP.
//   Messages: "RC" (registerClient) and "LG" (reportLog).
//   TLogInfo fields: LogName (string), TimeStamp (uint32), Params ([]TParamValue).
//   TParamValue is a typed union: uint32, uint64, sint32, float, string, CEntityId, CSheetId, TItemId.
//   Serialized with NeL NLMISC::IStream::serial() wrapped in NLNET::CMessage.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/boneysan/ryzom/go-services/internal/config"
	"github.com/boneysan/ryzom/go-services/internal/health"
)

// LogEntry is the JSON payload accepted on POST /log from Go services.
// For C++ compat we will also support the native TLogInfo / TParamValue format over TCP.
type LogEntry struct {
	Service   string            `json:"service"`
	Level     string            `json:"level"`
	Timestamp string            `json:"timestamp"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields,omitempty"`
}

func main() {
	httpAddr := config.Env("LOGGER_ADDR", ":47803")
	logFile := config.Env("LOGGER_FILE", "") // optional path for file output (in addition to stdout)

	// Structured logger to stdout (JSON by default — excellent for Docker + collection).
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	stdoutHandler := slog.NewJSONHandler(os.Stdout, opts)
	var logger *slog.Logger

	if logFile != "" {
		// Also write to a file (append mode). Useful for dev shard persistence.
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			slog.Error("failed to open log file", "path", logFile, "err", err)
			os.Exit(1)
		}
		defer f.Close()
		// Multi-writer: stdout + file
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
		// Emit as structured log. Downstream (journald, vector, etc.) can consume the JSON.
		logger.Info(entry.Message,
			"service", entry.Service,
			"level", entry.Level,
			"timestamp", entry.Timestamp,
			"fields", entry.Fields,
		)
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /health", health.Handler(map[string]string{
		"log_file": logFile,
	}))

	server := &http.Server{
		Addr:    httpAddr,
		Handler: mux,
	}

	// Start HTTP server
	go func() {
		slog.Info("logger HTTP starting", "addr", httpAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("logger HTTP exited", "err", err)
			os.Exit(1)
		}
	}()

	// === TCP shim scaffold (NeL module compat) ===
	// This will eventually accept connections from C++ services that think we are "LGS".
	// For now we just accept and log raw connections so we can capture real traffic
	// and implement the full "RC"/"LG" + TParamValue parser.
	tcpAddr := config.Env("LOGGER_TCP_ADDR", "") // e.g. ":47804" or leave empty to disable for now
	if tcpAddr != "" {
		go runTCPShim(logger, tcpAddr)
	}

	// Graceful shutdown on SIGINT/SIGTERM
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("logger shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
	slog.Info("logger stopped")
}

// runTCPShim is the beginning of the NeL TCP compatibility layer.
// It accepts raw connections (C++ services using the module interface will connect here
// once we register as "LGS" via naming_service).
// Current behaviour: accept, log the peer, read and dump a few bytes for analysis,
// then close. This lets us capture real "RC" / "LG" payloads without crashing clients yet.
func runTCPShim(logger *slog.Logger, addr string) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Error("failed to listen on TCP shim port", "addr", addr, "err", err)
		return
	}
	defer ln.Close()
	logger.Info("logger TCP shim listening (compat mode)", "addr", addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			logger.Error("TCP shim accept error", "err", err)
			continue
		}
		go func(c net.Conn) {
			defer c.Close()
			peer := c.RemoteAddr().String()
			logger.Info("C++ client connected to logger TCP shim", "peer", peer)

			// Read up to 4KB for initial analysis (real messages are usually small).
			buf := make([]byte, 4096)
			n, _ := c.Read(buf)
			if n > 0 {
				logger.Info("received bytes on logger TCP shim (first capture)",
					"peer", peer,
					"len", n,
					// Safe hex preview for reverse-engineering the NeL CMessage / "RC"/"LG" format.
					"hex_preview", hex.EncodeToString(buf[:min(n, 128)]),
				)
			}
			// TODO (full shim):
			// - Implement NeL CMessage framing + name extraction ("RC", "LG")
			// - Parse registerClient (shardId + []TLogDefinition)
			// - Parse reportLog ([]TLogInfo with TParamValue union)
			// - Enforce schema consistency like the C++ side does.
			// - Reply if the protocol requires acks.
		}(conn)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
