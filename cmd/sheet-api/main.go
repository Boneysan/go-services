// Sheet API — live game data over PostgreSQL (Phase 4, Tasks 4.1b/4.5).
//
// ARCHITECTURAL BOUNDARY (Phase 4 plan, Task 4.1b step 6): this service reads
// and writes PostgreSQL ONLY. It does NOT call into the EGS, the proxy, or any
// C++ service. The EGS learns about sheet changes via NATS invalidation
// (Task 4.2b), never by being queried from here.
//
// Endpoints:
//   GET    /sheets/items            ?family=&item_type=&page=&per_page=
//   GET    /sheets/items/{id}
//   PATCH  /sheets/items/{id}       live-balance partial update
//   GET    /sheets/creatures        ?category=&page=&per_page=
//   GET    /sheets/creatures/{id}   includes flattened loot_table
//   GET    /sheets/bricks           ?family=&skill_req=&brick_type=&page=&per_page=
//   GET    /sheets/skills           ?branch=
//   POST   /phrase/validate         {"bricks": ["bfpa01", ...]}
//   GET    /health
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/boneysan/ryzom/go-services/internal/config"
)

func main() {
	addr := config.Env("SHEET_API_ADDR", ":47801")
	dbURL := config.Env("DATABASE_URL", "postgres://ryzom:ryzom_dev@localhost:5432/ryzom_sheets?sslmode=disable")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("sheet-api: pgx pool", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	srv := &server{db: pool, start: time.Now()}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /sheets/items", srv.listItems)
	mux.HandleFunc("GET /sheets/items/{id}", srv.getItem)
	mux.HandleFunc("PATCH /sheets/items/{id}", srv.patchItem)
	mux.HandleFunc("GET /sheets/creatures", srv.listCreatures)
	mux.HandleFunc("GET /sheets/creatures/{id}", srv.getCreature)
	mux.HandleFunc("GET /sheets/bricks", srv.listBricks)
	mux.HandleFunc("GET /sheets/skills", srv.listSkills)
	mux.HandleFunc("POST /phrase/validate", srv.validatePhrase)
	mux.HandleFunc("GET /health", srv.health)

	slog.Info("sheet-api starting", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("sheet-api exited", "err", err)
		os.Exit(1)
	}
}
