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
	mux.HandleFunc("GET /party/{id}/stash", srv.getPartyStash)
	mux.HandleFunc("POST /party/{id}/stash", srv.updatePartyStash)
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

type ChronicleChoice struct {
	Storyline string    `json:"storyline"`
	Quest     string    `json:"quest"`
	Objective string    `json:"objective"`
	ChoiceID  string    `json:"choice_id"`
	AccountID string    `json:"account_id"`
	DecidedAt time.Time `json:"decided_at"`
}

type FactionStanding struct {
	AccountID string `json:"account_id"`
	Faction   string `json:"faction"`
	Standing  int    `json:"standing"`
}

type Party struct {
	ID        string    `json:"id"`
	CampaignID string   `json:"campaign_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type PartyMember struct {
	PartyID     string    `json:"party_id"`
	CharacterID int64     `json:"character_id"`
	JoinedAt    time.Time `json:"joined_at"`
}

type PartyStashItem struct {
	PartyID     string `json:"party_id"`
	ItemSheetID string `json:"item_sheet_id"`
	Quantity    int    `json:"quantity"`
}

type WorldState struct {
	CampaignID string    `json:"campaign_id"`
	StateKey   string    `json:"state_key"`
	StateValue any       `json:"state_value"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type WorldEvent struct {
	ID          string    `json:"id"`
	CampaignID  string    `json:"campaign_id"`
	EventType   string    `json:"event_type"`
	EventData   any       `json:"event_data"`
	TriggeredAt time.Time `json:"triggered_at"`
}

// Bundle is the portable campaign archive (schema ryzom-campaign/v1).
type Bundle struct {
	Schema           string            `json:"schema"`
	ExportedAt       time.Time         `json:"exported_at"`
	Campaign         Campaign          `json:"campaign"`
	Characters       []Character       `json:"characters"`
	Chronicle        []ChronicleChoice `json:"chronicle"`
	FactionStandings []FactionStanding `json:"faction_standings"`
	NPCAttitudes     map[string]any    `json:"npc_attitudes"`
	Parties          []Party           `json:"parties"`
	PartyMembers     []PartyMember     `json:"party_members"`
	PartyStash       []PartyStashItem  `json:"party_stash"`
	WorldState       []WorldState      `json:"world_state"`
	WorldEvents      []WorldEvent      `json:"world_events"`
}

// newBundle returns a Bundle with all reserved collections initialised non-nil
// so they marshal as [] / {} rather than null.
func newBundle() Bundle {
	return Bundle{
		Schema:           bundleSchema,
		ExportedAt:       time.Now().UTC(),
		Characters:       []Character{},
		Chronicle:        []ChronicleChoice{},
		FactionStandings: []FactionStanding{},
		NPCAttitudes:     map[string]any{},
		Parties:          []Party{},
		PartyMembers:     []PartyMember{},
		PartyStash:       []PartyStashItem{},
		WorldState:       []WorldState{},
		WorldEvents:      []WorldEvent{},
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

	chronicle, err := s.queryChronicle(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	b.Chronicle = chronicle

	factions, err := s.queryFactionStandings(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	b.FactionStandings = factions

	b.Parties, _ = s.queryParties(ctx, id)
	b.PartyMembers, _ = s.queryPartyMembers(ctx, id)
	b.PartyStash, _ = s.queryPartyStash(ctx, id)
	b.WorldState, _ = s.queryWorldState(ctx, id)
	b.WorldEvents, _ = s.queryWorldEvents(ctx, id)

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

func (s *server) queryChronicle(ctx context.Context) ([]ChronicleChoice, error) {
	rows, err := s.db.Query(ctx, `
		SELECT storyline, quest, objective, choice_id, account_id::text, decided_at
		FROM chronicle_choices
		ORDER BY decided_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ChronicleChoice{}
	for rows.Next() {
		var c ChronicleChoice
		if err := rows.Scan(&c.Storyline, &c.Quest, &c.Objective, &c.ChoiceID, &c.AccountID, &c.DecidedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *server) queryFactionStandings(ctx context.Context) ([]FactionStanding, error) {
	rows, err := s.db.Query(ctx, `
		SELECT account_id::text, faction, standing
		FROM faction_standings
		ORDER BY account_id, faction`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []FactionStanding{}
	for rows.Next() {
		var f FactionStanding
		if err := rows.Scan(&f.AccountID, &f.Faction, &f.Standing); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *server) queryParties(ctx context.Context, campaignID string) ([]Party, error) {
	rows, err := s.db.Query(ctx, `SELECT id::text, campaign_id::text, name, created_at FROM parties WHERE campaign_id = $1`, campaignID)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []Party
	for rows.Next() {
		var p Party
		if err := rows.Scan(&p.ID, &p.CampaignID, &p.Name, &p.CreatedAt); err != nil { return nil, err }
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *server) queryPartyMembers(ctx context.Context, campaignID string) ([]PartyMember, error) {
	rows, err := s.db.Query(ctx, `SELECT pm.party_id::text, pm.character_id, pm.joined_at FROM party_members pm JOIN parties p ON pm.party_id = p.id WHERE p.campaign_id = $1`, campaignID)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []PartyMember
	for rows.Next() {
		var m PartyMember
		if err := rows.Scan(&m.PartyID, &m.CharacterID, &m.JoinedAt); err != nil { return nil, err }
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *server) queryPartyStash(ctx context.Context, campaignID string) ([]PartyStashItem, error) {
	rows, err := s.db.Query(ctx, `SELECT ps.party_id::text, ps.item_sheet_id, ps.quantity FROM party_stash ps JOIN parties p ON ps.party_id = p.id WHERE p.campaign_id = $1`, campaignID)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []PartyStashItem
	for rows.Next() {
		var i PartyStashItem
		if err := rows.Scan(&i.PartyID, &i.ItemSheetID, &i.Quantity); err != nil { return nil, err }
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *server) queryWorldState(ctx context.Context, campaignID string) ([]WorldState, error) {
	rows, err := s.db.Query(ctx, `SELECT campaign_id::text, state_key, state_value, updated_at FROM world_state WHERE campaign_id = $1`, campaignID)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []WorldState
	for rows.Next() {
		var w WorldState
		if err := rows.Scan(&w.CampaignID, &w.StateKey, &w.StateValue, &w.UpdatedAt); err != nil { return nil, err }
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *server) queryWorldEvents(ctx context.Context, campaignID string) ([]WorldEvent, error) {
	rows, err := s.db.Query(ctx, `SELECT id::text, campaign_id::text, event_type, event_data, triggered_at FROM world_events WHERE campaign_id = $1`, campaignID)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []WorldEvent
	for rows.Next() {
		var w WorldEvent
		if err := rows.Scan(&w.ID, &w.CampaignID, &w.EventType, &w.EventData, &w.TriggeredAt); err != nil { return nil, err }
		out = append(out, w)
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

	for _, c := range b.Chronicle {
		if _, err := tx.Exec(ctx, `
			INSERT INTO chronicle_choices
			    (storyline, quest, objective, choice_id, account_id, decided_at)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			c.Storyline, c.Quest, c.Objective, c.ChoiceID, c.AccountID, c.DecidedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, "chronicle insert: "+err.Error())
			return
		}
	}

	for _, f := range b.FactionStandings {
		if _, err := tx.Exec(ctx, `
			INSERT INTO faction_standings
			    (account_id, faction, standing)
			VALUES ($1, $2, $3)
			ON CONFLICT (account_id, faction) DO UPDATE
			   SET standing = EXCLUDED.standing`,
			f.AccountID, f.Faction, f.Standing); err != nil {
			writeErr(w, http.StatusInternalServerError, "faction upsert: "+err.Error())
			return
		}
	}

	for _, p := range b.Parties {
		if _, err := tx.Exec(ctx, `
			INSERT INTO parties (id, campaign_id, name, created_at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name`,
			p.ID, p.CampaignID, p.Name, p.CreatedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, "party upsert: "+err.Error())
			return
		}
	}

	for _, m := range b.PartyMembers {
		if _, err := tx.Exec(ctx, `
			INSERT INTO party_members (party_id, character_id, joined_at)
			VALUES ($1, $2, $3)
			ON CONFLICT (party_id, character_id) DO NOTHING`,
			m.PartyID, m.CharacterID, m.JoinedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, "party member upsert: "+err.Error())
			return
		}
	}

	for _, s := range b.PartyStash {
		if _, err := tx.Exec(ctx, `
			INSERT INTO party_stash (party_id, item_sheet_id, quantity)
			VALUES ($1, $2, $3)
			ON CONFLICT (party_id, item_sheet_id) DO UPDATE SET quantity = EXCLUDED.quantity`,
			s.PartyID, s.ItemSheetID, s.Quantity); err != nil {
			writeErr(w, http.StatusInternalServerError, "party stash upsert: "+err.Error())
			return
		}
	}

	for _, ws := range b.WorldState {
		if _, err := tx.Exec(ctx, `
			INSERT INTO world_state (campaign_id, state_key, state_value, updated_at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (campaign_id, state_key) DO UPDATE SET state_value = EXCLUDED.state_value`,
			ws.CampaignID, ws.StateKey, ws.StateValue, ws.UpdatedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, "world state upsert: "+err.Error())
			return
		}
	}

	for _, we := range b.WorldEvents {
		if _, err := tx.Exec(ctx, `
			INSERT INTO world_events (id, campaign_id, event_type, event_data, triggered_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (id) DO NOTHING`,
			we.ID, we.CampaignID, we.EventType, we.EventData, we.TriggeredAt); err != nil {
			writeErr(w, http.StatusInternalServerError, "world event insert: "+err.Error())
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		writeErr(w, http.StatusInternalServerError, "commit: "+err.Error())
		return
	}

	skipped := []string{}
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

func (s *server) getPartyStash(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx := r.Context()
	
	// Use existing query function but adapted for direct party lookup
	rows, err := s.db.Query(ctx, `SELECT party_id::text, item_sheet_id, quantity FROM party_stash WHERE party_id = $1`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var out []PartyStashItem
	for rows.Next() {
		var i PartyStashItem
		if err := rows.Scan(&i.PartyID, &i.ItemSheetID, &i.Quantity); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, i)
	}
	writeJSON(w, http.StatusOK, map[string]any{"party_stash": out})
}

func (s *server) updatePartyStash(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx := r.Context()
	
	var req struct {
		ItemSheetID string `json:"item_sheet_id"`
		Quantity    int    `json:"quantity"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	
	if req.Quantity <= 0 {
		_, err := s.db.Exec(ctx, `DELETE FROM party_stash WHERE party_id = $1 AND item_sheet_id = $2`, id, req.ItemSheetID)
		if err != nil { writeErr(w, http.StatusInternalServerError, err.Error()); return }
	} else {
		_, err := s.db.Exec(ctx, `
			INSERT INTO party_stash (party_id, item_sheet_id, quantity) 
			VALUES ($1, $2, $3)
			ON CONFLICT (party_id, item_sheet_id) DO UPDATE SET quantity = EXCLUDED.quantity`, 
			id, req.ItemSheetID, req.Quantity)
		if err != nil { writeErr(w, http.StatusInternalServerError, err.Error()); return }
	}
	
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}
