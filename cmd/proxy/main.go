// Go WebSocket proxy — translates NeL binary bit-stream ↔ JSON over WebSocket.
// Permanent architectural component (ADR-003), not a transitional bridge.
// Client-facing: WebSocket on :47852
// Server-facing: UDP to nel-frontend :47851 + NATS subscription
//
// Message translation table defined in Phase 3.2 once NeL message codec
// is reverse-engineered. See ADR_003_Godot4_Client.md.
// See Phase_Plans/Phase_3_Godot_Client.md for implementation plan.
package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"

	"github.com/boneysan/ryzom/go-services/internal/config"
	"github.com/boneysan/ryzom/go-services/internal/health"
	"github.com/boneysan/ryzom/go-services/internal/natspub"
)

type server struct {
	nats natspub.Publisher
}

func main() {
	addr := config.Env("PROXY_ADDR", ":47852")
	frontendHost := config.Env("FRONTEND_HOST", "localhost")
	frontendPort := config.Env("FRONTEND_PORT", "47851")
	natsURL := config.Env("NATS_URL", "nats://localhost:4222")

	_ = frontendHost
	_ = frontendPort

	var pub natspub.Publisher = natspub.Noop{}
	if natsURL != "disabled" {
		nc, err := natspub.Connect(natsURL, "proxy")
		if err != nil {
			slog.Warn("proxy: NATS unavailable, admin reload disabled", "err", err)
		} else {
			defer nc.Close()
			pub = nc
		}
	}

	srv := &server{nats: pub}

	mux := http.NewServeMux()

	// TODO: WebSocket upgrade + NeL codec translation (Phase 3.2)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "proxy not yet implemented", http.StatusNotImplemented)
	})

	mux.HandleFunc("POST /admin/reload-sheets", srv.reloadSheets)
	mux.HandleFunc("GET /health", health.Handler(nil))

	slog.Info("proxy starting", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("proxy exited", "err", err)
		os.Exit(1)
	}
}

func (s *server) reloadSheets(w http.ResponseWriter, r *http.Request) {
	const subject = "sheet.updated.all"
	payload, _ := json.Marshal(map[string]string{"table": "bricks", "sheet_id": "*"})
	if err := s.nats.Publish(subject, payload); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error(), "code": "nats_error"})
		return
	}
	slog.Info("sheet reload requested", "subject", subject)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"queued":   true,
		"subject":  subject,
		"table":    "bricks",
		"sheet_id": "*",
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
