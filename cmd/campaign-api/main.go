// Campaign API — export and import a whole campaign as a portable JSON bundle
// (Phase 5.5, Task 5.5.6; MVP gates D9/D10).
//
// ARCHITECTURE NOTE: the Phase 5.5 plan sketches these endpoints "in the Go
// proxy", but the proxy's job is NeL<->Godot protocol translation (ADR-003) and
// it holds no database connection. Campaign data is PostgreSQL game data, which
// is the go-services domain — so export/import lives here as its own small
// service, reusing the same pgx/config/health plumbing as sheet-api. The route
// shapes (GET /campaign/{id}/export, POST /campaign/import) match the plan.
//
// A private shard runs a single campaign; characters are not yet campaign-scoped
// (Phase 5.8 adds party/campaign FKs), so export emits ALL characters on the
// shard. The bundle carries empty arrays for the Phase 5.8/5.3a tables
// (chronicle, faction standings, world events, party stash) so the v1 format is
// stable as those tables come online.
//
// Endpoints:
//
//	GET  /campaigns                list campaigns
//	GET  /campaign/{id}/export     full JSON bundle (schema ryzom-campaign/v1)
//	POST /campaign/import          restore a bundle (upsert campaign + characters)
//	GET  /health
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/boneysan/ryzom/go-services/internal/config"
	"github.com/boneysan/ryzom/go-services/internal/health"
)

const bundleSchema = "ryzom-campaign/v1"

func main() {
	addr := config.Env("CAMPAIGN_API_ADDR", ":47807")
	dbURL := config.Env("DATABASE_URL", "postgres://ryzom:ryzom_dev@localhost:5432/ryzom_sheets?sslmode=disable")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("campaign-api: pgx pool", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	srv := &server{db: pool}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /campaigns", srv.listCampaigns)
	mux.HandleFunc("GET /campaign/{id}/export", srv.exportCampaign)
	mux.HandleFunc("POST /campaign/import", srv.importCampaign)
	mux.HandleFunc("GET /health", health.Handler(map[string]string{"service": "campaign-api"}))

	slog.Info("campaign-api starting", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("campaign-api exited", "err", err)
		os.Exit(1)
	}
}

type server struct {
	db *pgxpool.Pool
}

// Campaign mirrors a row of the campaigns table.
type Campaign struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	SessionsPlayed int    `json:"sessions_played"`
}

// Character mirrors the exportable columns of the characters table. Nullable
// columns are pointers so a NULL stays absent rather than becoming a zero value.
type Character struct {
	AccountID     string     `json:"account_id"`
	Slot          int        `json:"slot"`
	Name          string     `json:"name"`
	Race          string     `json:"race"`
	Gender        string     `json:"gender"`
	EGSEntityID   *int64     `json:"egs_entity_id,omitempty"`
	IsGMCharacter bool       `json:"is_gm_character"`
	SourceFile    *string    `json:"source_file,omitempty"`
	CreatedAt     *time.Time `json:"created_at,omitempty"`
	LastLoginAt   *time.Time `json:"last_login_at,omitempty"`
}

// Bundle is the portable campaign archive (schema ryzom-campaign/v1). The
// trailing collections are reserved for Phase 5.8/5.3a tables and are emitted
// empty (but non-null) until those tables exist.
type Bundle struct {
	Schema           string           `json:"schema"`
	ExportedAt       time.Time        `json:"exported_at"`
	Campaign         Campaign         `json:"campaign"`
	Characters       []Character      `json:"characters"`
	Chronicle        []map[string]any `json:"chronicle"`
	FactionStandings map[string]any   `json:"faction_standings"`
	NPCAttitudes     map[string]any   `json:"npc_attitudes"`
	WorldEvents      []map[string]any `json:"world_events"`
	PartyStash       []map[string]any `json:"party_stash"`
}

// newBundle returns a Bundle with all reserved collections initialised non-nil
// so they marshal as [] / {} rather than null.
func newBundle() Bundle {
	return Bundle{
		Schema:           bundleSchema,
		ExportedAt:       time.Now().UTC(),
		Characters:       []Character{},
		Chronicle:        []map[string]any{},
		FactionStandings: map[string]any{},
		NPCAttitudes:     map[string]any{},
		WorldEvents:      []map[string]any{},
		PartyStash:       []map[string]any{},
	}
}

func (s *server) listCampaigns(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(),
		`SELECT id::text, name, sessions_played FROM campaigns ORDER BY created_at`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	out := []Campaign{}
	for rows.Next() {
		var c Campaign
		if err := rows.Scan(&c.ID, &c.Name, &c.SessionsPlayed); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"campaigns": out})
}

func (s *server) exportCampaign(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx := r.Context()

	b := newBundle()
	err := s.db.QueryRow(ctx,
		`SELECT id::text, name, sessions_played FROM campaigns WHERE id = $1`, id).
		Scan(&b.Campaign.ID, &b.Campaign.Name, &b.Campaign.SessionsPlayed)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "campaign not found: "+id)
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	chars, err := s.queryCharacters(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	b.Characters = chars

	slog.Info("campaign exported", "id", id, "characters", len(chars))
	w.Header().Set("Content-Disposition", `attachment; filename="campaign_`+id+`.json"`)
	writeJSON(w, http.StatusOK, b)
}

// queryCharacters returns every character on the shard (single-campaign shard).
func (s *server) queryCharacters(ctx context.Context) ([]Character, error) {
	rows, err := s.db.Query(ctx, `
		SELECT account_id, slot, name, race, gender,
		       egs_entity_id, is_gm_character, source_file, created_at, last_login_at
		FROM characters
		ORDER BY account_id, slot`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Character{}
	for rows.Next() {
		var c Character
		if err := rows.Scan(&c.AccountID, &c.Slot, &c.Name, &c.Race, &c.Gender,
			&c.EGSEntityID, &c.IsGMCharacter, &c.SourceFile, &c.CreatedAt, &c.LastLoginAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *server) importCampaign(w http.ResponseWriter, r *http.Request) {
	var b Bundle
	if err := decodeJSON(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid bundle: "+err.Error())
		return
	}
	if b.Schema != bundleSchema {
		writeErr(w, http.StatusBadRequest, "unsupported schema "+b.Schema+" (want "+bundleSchema+")")
		return
	}
	if b.Campaign.ID == "" || b.Campaign.Name == "" {
		writeErr(w, http.StatusBadRequest, "bundle campaign id and name are required")
		return
	}

	ctx := r.Context()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback(ctx) // no-op after Commit

	if _, err := tx.Exec(ctx, `
		INSERT INTO campaigns (id, name, sessions_played, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (id) DO UPDATE
		   SET name = EXCLUDED.name,
		       sessions_played = EXCLUDED.sessions_played,
		       updated_at = NOW()`,
		b.Campaign.ID, b.Campaign.Name, b.Campaign.SessionsPlayed); err != nil {
		writeErr(w, http.StatusInternalServerError, "campaign upsert: "+err.Error())
		return
	}

	imported := 0
	for _, c := range b.Characters {
		if _, err := tx.Exec(ctx, `
			INSERT INTO characters
			    (account_id, slot, name, race, gender, egs_entity_id,
			     is_gm_character, source_file, imported_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
			ON CONFLICT (account_id, slot) DO UPDATE
			   SET name = EXCLUDED.name,
			       race = EXCLUDED.race,
			       gender = EXCLUDED.gender,
			       egs_entity_id = EXCLUDED.egs_entity_id,
			       is_gm_character = EXCLUDED.is_gm_character,
			       source_file = EXCLUDED.source_file,
			       imported_at = NOW()`,
			c.AccountID, c.Slot, c.Name, c.Race, c.Gender, c.EGSEntityID,
			c.IsGMCharacter, c.SourceFile); err != nil {
			writeErr(w, http.StatusInternalServerError, "character upsert: "+err.Error())
			return
		}
		imported++
	}

	if err := tx.Commit(ctx); err != nil {
		writeErr(w, http.StatusInternalServerError, "commit: "+err.Error())
		return
	}

	// The reserved Phase 5.8/5.3a collections have no tables yet; flag if a
	// caller sent data so it isn't silently dropped.
	skipped := []string{}
	if len(b.Chronicle) > 0 {
		skipped = append(skipped, "chronicle")
	}
	if len(b.WorldEvents) > 0 {
		skipped = append(skipped, "world_events")
	}
	if len(b.PartyStash) > 0 {
		skipped = append(skipped, "party_stash")
	}
	if len(b.FactionStandings) > 0 {
		skipped = append(skipped, "faction_standings")
	}
	if len(b.NPCAttitudes) > 0 {
		skipped = append(skipped, "npc_attitudes")
	}

	slog.Info("campaign imported", "id", b.Campaign.ID, "characters", imported, "skipped_sections", skipped)
	writeJSON(w, http.StatusOK, map[string]any{
		"campaign_id":         b.Campaign.ID,
		"characters_imported": imported,
		"skipped_sections":    skipped,
		"skipped_note":        "sections without tables are reserved for Phase 5.8/5.3a",
	})
}
