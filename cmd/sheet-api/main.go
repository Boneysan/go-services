// Sheet API — live game data read/write over PostgreSQL. (Phase 4.3)
// See Direction/API_Contracts.md for full endpoint specifications.
// See Phase_Plans/Phase_4_Data_Driven_Mechanics.md for implementation plan.
package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/boneysan/ryzom/go-services/internal/config"
	"github.com/boneysan/ryzom/go-services/internal/health"
)

func main() {
	addr := config.Env("SHEET_API_ADDR", ":47801")
	dbURL := config.Env("DATABASE_URL", "postgres://ryzom:ryzom_dev@localhost:5432/ryzom_sheets?sslmode=disable")

	// TODO: open postgres connection pool (database/sql + pgx driver)
	_ = dbURL

	mux := http.NewServeMux()

	// GET /sheets/items/{id}
	mux.HandleFunc("GET /sheets/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"not implemented","code":"not_implemented"}`, http.StatusNotImplemented)
	})

	// PATCH /sheets/items/{id}
	mux.HandleFunc("PATCH /sheets/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"not implemented","code":"not_implemented"}`, http.StatusNotImplemented)
	})

	// GET /sheets/items
	mux.HandleFunc("GET /sheets/items", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"not implemented","code":"not_implemented"}`, http.StatusNotImplemented)
	})

	// GET /sheets/creatures/{id}
	mux.HandleFunc("GET /sheets/creatures/{id}", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"not implemented","code":"not_implemented"}`, http.StatusNotImplemented)
	})

	mux.HandleFunc("GET /health", health.Handler(map[string]string{"db": "unchecked"}))

	slog.Info("sheet-api starting", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("sheet-api exited", "err", err)
		os.Exit(1)
	}
}
