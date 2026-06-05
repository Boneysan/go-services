// Logger service — Go replacement for C++ logger_service (Phase 1.2)
//
// Wire format notes (from Code Investigation Task 6):
//   C++ services communicate via NeL module interface using TCP.
//   Messages: "RC" (registerClient) and "LG" (reportLog).
//   TLogInfo fields: LogName (string), TimeStamp (uint32), Params ([]TParamValue).
//   TParamValue is a typed union: uint32, uint64, sint32, float, string, CEntityId, CSheetId, TItemId.
//
// Implementation sequence:
//   1. This HTTP endpoint accepts log entries from new Go services immediately.
//   2. Add NeL TCP compatibility shim (Task 6 findings) to also accept C++ service logs.
//   3. Retire C++ logger_service once all services route through this binary.
package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/boneysan/ryzom/go-services/internal/config"
	"github.com/boneysan/ryzom/go-services/internal/health"
)

type LogEntry struct {
	Service   string            `json:"service"`
	Level     string            `json:"level"`
	Timestamp string            `json:"timestamp"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields,omitempty"`
}

func main() {
	addr := config.Env("LOGGER_ADDR", ":47803")
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

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

	mux.HandleFunc("GET /health", health.Handler(nil))

	slog.Info("logger starting", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("logger exited", "err", err)
		os.Exit(1)
	}
}
