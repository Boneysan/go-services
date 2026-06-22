// pathfinding-api serves Detour navmesh pathfinding queries (Task 6.2,
// Phase 6 NPC AI) over HTTP and NATS request/reply. It loads pre-baked
// .navmesh files (produced by navmesh-bake) from NAVMESH_DIR at startup —
// baking itself is an offline step, not done by this service.
//
// Real Atys terrain is not available in this checkout (.zone files are
// 0-byte placeholders); see PROGRESS.md Task 6.2 and
// cmd/navmesh-bake/testdata/tutorial_zone.navmesh for the synthetic
// placeholder zone this serves by default.
package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/nats-io/nats.go"

	"github.com/boneysan/ryzom/go-services/internal/config"
	"github.com/boneysan/ryzom/go-services/internal/health"
	"github.com/boneysan/ryzom/go-services/internal/natspub"
	"github.com/boneysan/ryzom/go-services/internal/pathfinding"
)

const natsSubject = "ai.pathfind.request"

type server struct {
	zones *zoneStore
}

func main() {
	addr := config.Env("PATHFINDING_API_ADDR", ":47808")
	natsURL := config.Env("NATS_URL", "nats://localhost:4222")
	navDir := config.Env("NAVMESH_DIR", "./navmeshes")

	srv := &server{zones: newZoneStore()}

	loaded, err := loadNavMeshDir(srv.zones, navDir)
	if err != nil {
		slog.Warn("pathfinding-api: loading navmesh dir", "dir", navDir, "err", err)
	}
	slog.Info("pathfinding-api: loaded navmeshes", "dir", navDir, "zones", loaded)

	nc, err := natspub.Connect(natsURL, "pathfinding-api")
	if err != nil {
		slog.Error("pathfinding-api: NATS connect", "err", err)
		os.Exit(1)
	}
	defer nc.Close()

	sub, err := nc.Subscribe(natsSubject, srv.natsHandler)
	if err != nil {
		slog.Error("pathfinding-api: NATS subscribe", "subject", natsSubject, "err", err)
		os.Exit(1)
	}
	defer sub.Unsubscribe()
	slog.Info("pathfinding-api: subscribed", "subject", natsSubject)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /path/find", srv.handleFindPath)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		health.Handler(map[string]string{
			"zones": strings.Join(srv.zones.zoneNames(), ","),
		})(w, r)
	})

	slog.Info("pathfinding-api starting", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("pathfinding-api exited", "err", err)
		os.Exit(1)
	}
}

// loadNavMeshDir loads every *.navmesh file in dir into store, keyed by
// each file's metadata.Zone (falling back to the filename stem if Zone is
// empty). Returns the number of zones loaded; a missing/empty directory is
// not fatal — the service still starts (and reports zero zones via
// /health) so it can be brought up before any navmesh has been baked.
func loadNavMeshDir(store *zoneStore, dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".navmesh") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, meta, err := pathfinding.LoadFile(path)
		if err != nil {
			slog.Warn("pathfinding-api: skipping unloadable navmesh file", "path", path, "err", err)
			continue
		}
		mesh, err := pathfinding.LoadNavMesh(data)
		if err != nil {
			slog.Warn("pathfinding-api: skipping invalid navmesh data", "path", path, "err", err)
			continue
		}
		zone := meta.Zone
		if zone == "" {
			zone = strings.TrimSuffix(e.Name(), ".navmesh")
		}
		store.set(zone, mesh)
		count++
	}
	return count, nil
}

// natsHandler implements the ai.pathfind.request request/reply contract:
// same JSON request/response shape as POST /path/find.
func (srv *server) natsHandler(msg *nats.Msg) {
	var req findPathRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		respondJSON(msg, errorResponse{Error: "invalid JSON request: " + err.Error()})
		return
	}

	resp, err := findPath(srv.zones, req)
	if err != nil {
		respondJSON(msg, errorResponse{Error: err.Error()})
		return
	}
	respondJSON(msg, resp)
}

func respondJSON(msg *nats.Msg, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("pathfinding-api: marshal NATS response", "err", err)
		return
	}
	if err := msg.Respond(data); err != nil {
		slog.Error("pathfinding-api: NATS respond", "err", err)
	}
}
