package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type server struct {
	db    *pgxpool.Pool
	start time.Time
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}

func paging(r *http.Request) (page, perPage, offset int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ = strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 || perPage > 500 {
		perPage = 50
	}
	return page, perPage, (page - 1) * perPage
}

// filterClause builds "WHERE a = $1 AND b = $2" from non-empty query params.
func filterClause(r *http.Request, cols map[string]string) (string, []any) {
	var conds []string
	var args []any
	for param, col := range cols {
		if v := r.URL.Query().Get(param); v != "" {
			args = append(args, v)
			conds = append(conds, fmt.Sprintf("%s = $%d", col, len(args)))
		}
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	dbStatus := "ok"
	if err := s.db.Ping(r.Context()); err != nil {
		dbStatus = "degraded"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"db":       dbStatus,
		"uptime_s": int64(time.Since(s.start).Seconds()),
	})
}

// --- items ---------------------------------------------------------------

func (s *server) listItems(w http.ResponseWriter, r *http.Request) {
	page, perPage, offset := paging(r)
	where, args := filterClause(r, map[string]string{"family": "family", "item_type": "item_type"})

	var total int
	if err := s.db.QueryRow(r.Context(), "SELECT count(*) FROM items"+where, args...).Scan(&total); err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	rows, err := s.db.Query(r.Context(),
		itemSelect+where+fmt.Sprintf(" ORDER BY id LIMIT %d OFFSET %d", perPage, offset), args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByName[Item])
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page})
}

func (s *server) getItem(w http.ResponseWriter, r *http.Request) {
	item, err := s.fetchItem(r, r.PathValue("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not_found", "no such item")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// patchItem is the live-balance endpoint: partial update, no restart needed.
// Only whitelisted columns are writable; enum/check violations surface as 400.
var itemPatchable = map[string]bool{
	"name": true, "origin": true, "family": true, "item_type": true,
	"stackable": true, "quality": true, "bulk": true, "weight": true,
	"saleable": true, "drop_or_sell": true, "price": true, "consumable": true,
	"craft_plan": true, "req_skill": true, "req_skill_min": true,
	"req_skill2": true, "req_skill2_min": true, "extras": true,
}

func (s *server) patchItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var patch map[string]any
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	if len(patch) == 0 {
		writeErr(w, http.StatusBadRequest, "empty_patch", "no fields to update")
		return
	}
	var sets []string
	args := []any{id}
	for k, v := range patch {
		if !itemPatchable[k] {
			writeErr(w, http.StatusBadRequest, "bad_field", "field not patchable: "+k)
			return
		}
		if k == "extras" {
			b, err := json.Marshal(v)
			if err != nil {
				writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
				return
			}
			v = string(b)
		}
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("%s = $%d", k, len(args)))
	}
	tag, err := s.db.Exec(r.Context(),
		"UPDATE items SET "+strings.Join(sets, ", ")+" WHERE id = $1", args...)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "update_failed", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "not_found", "no such item")
		return
	}
	item, err := s.fetchItem(r, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// --- creatures ------------------------------------------------------------

func (s *server) listCreatures(w http.ResponseWriter, r *http.Request) {
	page, perPage, offset := paging(r)
	where, args := filterClause(r, map[string]string{"category": "category", "race_code": "race_code"})

	var total int
	if err := s.db.QueryRow(r.Context(), "SELECT count(*) FROM creatures"+where, args...).Scan(&total); err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	rows, err := s.db.Query(r.Context(),
		creatureSelect+where+fmt.Sprintf(" ORDER BY id LIMIT %d OFFSET %d", perPage, offset), args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	creatures, err := pgx.CollectRows(rows, pgx.RowToStructByName[Creature])
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"creatures": creatures, "total": total, "page": page})
}

func (s *server) getCreature(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	row, err := s.db.Query(r.Context(), creatureSelect+" WHERE id = $1", id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	creature, err := pgx.CollectExactlyOneRow(row, pgx.RowToStructByName[Creature])
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not_found", "no such creature")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	loot, err := s.fetchLoot(r, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Creature
		LootTable []LootRow `json:"loot_table"`
	}{creature, loot})
}

// --- bricks / skills --------------------------------------------------------

func (s *server) listBricks(w http.ResponseWriter, r *http.Request) {
	page, perPage, offset := paging(r)
	where, args := filterClause(r, map[string]string{
		"family": "family", "skill_req": "skill_req", "brick_type": "brick_type"})

	var total int
	if err := s.db.QueryRow(r.Context(), "SELECT count(*) FROM bricks"+where, args...).Scan(&total); err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	rows, err := s.db.Query(r.Context(),
		brickSelect+where+fmt.Sprintf(" ORDER BY id LIMIT %d OFFSET %d", perPage, offset), args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	bricks, err := pgx.CollectRows(rows, pgx.RowToStructByName[Brick])
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bricks": bricks, "total": total, "page": page})
}

func (s *server) listSkills(w http.ResponseWriter, r *http.Request) {
	where, args := filterClause(r, map[string]string{"branch": "branch"})
	rows, err := s.db.Query(r.Context(),
		"SELECT id, name, branch, cost, prerequisites FROM skills"+where+" ORDER BY id", args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	skills, err := pgx.CollectRows(rows, pgx.RowToStructByName[Skill])
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": skills, "total": len(skills)})
}

// --- phrase validation ------------------------------------------------------

func (s *server) validatePhrase(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Bricks []string `json:"bricks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	found, err := s.fetchBricks(r, req.Bricks)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	result := ValidatePhrase(req.Bricks, found)
	writeJSON(w, http.StatusOK, result)
}
