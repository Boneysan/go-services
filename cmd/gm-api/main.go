// GM API — real-time GM commands routed through NATS to EGS. (Phase 5.1)
// See Direction/API_Contracts.md for full endpoint specifications.
// See Phase_Plans/Phase_5_GM_Mode.md for implementation plan.
package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/boneysan/ryzom/go-services/internal/config"
	"github.com/boneysan/ryzom/go-services/internal/health"
)

func main() {
	addr := config.Env("GM_API_ADDR", ":47802")
	natsURL := config.Env("NATS_URL", "nats://localhost:4222")
	egsURL := config.Env("EGS_URL", "http://localhost:47800")

	// TODO: connect to NATS and EGS
	_ = natsURL
	_ = egsURL

	mux := http.NewServeMux()

	mux.HandleFunc("POST /gm/spawn", notImplemented)
	mux.HandleFunc("DELETE /gm/entities/{entity_id}", notImplemented)
	mux.HandleFunc("POST /gm/teleport", notImplemented)
	mux.HandleFunc("POST /gm/weather", notImplemented)
	mux.HandleFunc("GET /gm/zones/{zone_id}/entities", notImplemented)
	mux.HandleFunc("POST /gm/scenario/start", notImplemented)
	mux.HandleFunc("POST /gm/scenario/stop", notImplemented)
	mux.HandleFunc("POST /gm/award/skill", notImplemented)
	mux.HandleFunc("GET /health", health.Handler(nil))

	slog.Info("gm-api starting", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("gm-api exited", "err", err)
		os.Exit(1)
	}
}

func notImplemented(w http.ResponseWriter, r *http.Request) {
	http.Error(w, `{"error":"not implemented","code":"not_implemented"}`, http.StatusNotImplemented)
}
