// GM API — real-time GM commands routed through NATS to the EGS.
// (Phase 4, Task 4.5 — Go side; Phase 5.1 adds the dashboard-facing extras.)
//
// See Direction/API_Contracts.md and Phase_4_Data_Driven_Mechanics.md Task 4.5.
//
// Route note: the Phase 4 plan writes /gm/entity/{id}; API_Contracts.md and
// the rest of this API use the plural /gm/entities/{id} — the contract doc
// wins. Subjects published (EGS subscriber lands with Tasks 4.2b/4.5):
//
//	POST   /gm/spawn               -> gm.spawn
//	PATCH  /gm/entities/{id}       -> gm.entity.patch
//	DELETE /gm/entities/{id}       -> gm.entity.despawn
//	POST   /gm/weather             -> gm.weather
//	POST   /gm/event/trigger       -> gm.event.trigger
//	POST   /gm/script/run          -> gm.script.run
//	POST   /gm/party/frontend      -> gm.set_party_frontend
//	POST   /gm/instance/frontend   -> gm.set_instance_frontend
//	POST   /gm/party/instance      -> gm.assign_party_instance
//
// Until the EGS subscriber exists, accepted commands return 202 Accepted —
// "published, execution pending". They will stay 202: execution is async by
// design; the dashboard observes effects through the entity stream.
package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/boneysan/ryzom/go-services/internal/config"
	"github.com/boneysan/ryzom/go-services/internal/natspub"
)

func main() {
	addr := config.Env("GM_API_ADDR", ":47802")
	natsURL := config.Env("NATS_URL", "nats://localhost:4222")
	token := config.Env("GM_API_TOKEN", "")
	if token == "" {
		slog.Warn("GM_API_TOKEN not set — GM API is UNAUTHENTICATED (local dev only)")
	}

	geminiKey := config.Env("GEMINI_API_KEY", "")
	geminiModel := config.Env("GEMINI_MODEL", "gemini-2.0-flash-lite")

	nc, err := natspub.Connect(natsURL, "gm-api")
	if err != nil {
		slog.Error("gm-api: NATS connect", "err", err)
		os.Exit(1)
	}
	defer nc.Close()

	srv := &server{nats: nc, token: token, geminiKey: geminiKey, geminiModel: geminiModel, start: time.Now()}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /gm/spawn", srv.auth(srv.spawn))
	mux.HandleFunc("PATCH /gm/entities/{entity_id}", srv.auth(srv.patchEntity))
	mux.HandleFunc("DELETE /gm/entities/{entity_id}", srv.auth(srv.despawnEntity))
	mux.HandleFunc("POST /gm/weather", srv.auth(srv.weather))
	mux.HandleFunc("POST /gm/event/trigger", srv.auth(srv.eventTrigger))
	mux.HandleFunc("POST /gm/script/run", srv.auth(srv.scriptRun))
	mux.HandleFunc("POST /gm/quest/generate", srv.auth(srv.generateQuest))
	mux.HandleFunc("POST /gm/dungeon/generate", srv.auth(srv.generateDungeon))
	mux.HandleFunc("POST /gm/dialogue/generate", srv.auth(srv.generateDialogue))
	mux.HandleFunc("POST /gm/ambient/generate", srv.auth(srv.generateAmbient))

	mux.HandleFunc("POST /gm/tabletop/dice", srv.auth(srv.rollDice))
	mux.HandleFunc("POST /gm/tabletop/fow", srv.auth(srv.toggleFOW))
	mux.HandleFunc("POST /gm/tabletop/npc", srv.auth(srv.npcCommand))

	initRedis(config.Env("REDIS_ADDR", "localhost:6379"))

	// Phase 5.1 dashboard endpoints — not part of Task 4.5.
	mux.HandleFunc("POST /gm/teleport", srv.auth(srv.gmTeleport))
	mux.HandleFunc("GET /gm/zones/{zone_id}/entities", srv.auth(notImplemented))
	mux.HandleFunc("POST /gm/scenario/start", srv.auth(srv.scenarioStart))
	mux.HandleFunc("POST /gm/scenario/stop", srv.auth(notImplemented))
	mux.HandleFunc("POST /gm/scenario/import", srv.auth(srv.scenarioImport))
	mux.HandleFunc("POST /gm/fire_event", srv.auth(srv.fireEvent))
	mux.HandleFunc("POST /gm/award/skill", srv.auth(srv.awardSkill))

	// Phase 5.8 party management — assigns characters to parties and sets respawn anchors.
	mux.HandleFunc("POST /gm/party/join", srv.auth(srv.joinParty))
	mux.HandleFunc("POST /gm/party/leave", srv.auth(srv.leaveParty))
	mux.HandleFunc("POST /gm/party/anchor", srv.auth(srv.setAnchor))
	mux.HandleFunc("POST /gm/party/frontend", srv.auth(srv.setPartyFrontend))
	mux.HandleFunc("POST /gm/instance/frontend", srv.auth(srv.setInstanceFrontend))
	mux.HandleFunc("POST /gm/party/instance", srv.auth(srv.assignPartyInstance))
	mux.HandleFunc("POST /gm/party/instance/clear", srv.auth(srv.clearPartyInstance))

	mux.HandleFunc("GET /health", srv.health)

	slog.Info("gm-api starting", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("gm-api exited", "err", err)
		os.Exit(1)
	}
}

func notImplemented(w http.ResponseWriter, r *http.Request) {
	http.Error(w, `{"error":"not implemented","code":"not_implemented"}`, http.StatusNotImplemented)
}
