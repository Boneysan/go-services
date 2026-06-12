package main

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// Row types mirror migrations/001_sheet_schema.sql. JSONB columns decode into
// json.RawMessage so they pass through to clients untouched.

type Item struct {
	ID            string          `json:"id" db:"id"`
	Name          *string         `json:"name" db:"name"`
	Origin        *string         `json:"origin" db:"origin"`
	Family        *string         `json:"family" db:"family"`
	ItemType      *string         `json:"item_type" db:"item_type"`
	Stackable     *int32          `json:"stackable" db:"stackable"`
	Quality       *int32          `json:"quality" db:"quality"`
	Bulk          *float32        `json:"bulk" db:"bulk"`
	Weight        *float32        `json:"weight" db:"weight"`
	Saleable      *bool           `json:"saleable" db:"saleable"`
	DropOrSell    *bool           `json:"drop_or_sell" db:"drop_or_sell"`
	Price         *float32        `json:"price" db:"price"`
	Consumable    *bool           `json:"consumable" db:"consumable"`
	CraftPlan     *string         `json:"craft_plan" db:"craft_plan"`
	ReqSkill      *string         `json:"req_skill" db:"req_skill"`
	ReqSkillMin   *int32          `json:"req_skill_min" db:"req_skill_min"`
	ReqSkill2     *string         `json:"req_skill2" db:"req_skill2"`
	ReqSkill2Min  *int32          `json:"req_skill2_min" db:"req_skill2_min"`
	Extras        json.RawMessage `json:"extras" db:"extras"`
}

// origin/family/item_type are PostgreSQL enums — cast to text for scanning.
const itemSelect = `SELECT id, name, origin::text AS origin, family::text AS family,
	item_type::text AS item_type, stackable, quality, bulk, weight, saleable,
	drop_or_sell, price, consumable, craft_plan, req_skill, req_skill_min,
	req_skill2, req_skill2_min, extras FROM items`

type Creature struct {
	ID            string          `json:"id" db:"id"`
	Alias         *string         `json:"alias" db:"alias"`
	Category      *string         `json:"category" db:"category"`
	RaceCode      *string         `json:"race_code" db:"race_code"`
	GroupID       *string         `json:"group_id" db:"group_id"`
	CreatureLevel *float32        `json:"creature_level" db:"creature_level"`
	R2NPC         *bool           `json:"r2_npc" db:"r2_npc"`
	Basics        json.RawMessage `json:"basics" db:"basics"`
	Combat        json.RawMessage `json:"combat" db:"combat"`
	Properties    json.RawMessage `json:"properties" db:"properties"`
	Protections   json.RawMessage `json:"protections" db:"protections"`
	Resists       json.RawMessage `json:"resists" db:"resists"`
	DefaultStats  json.RawMessage `json:"default_stats" db:"default_stats"`
}

const creatureSelect = `SELECT id, alias, category, race_code, group_id,
	creature_level, r2_npc, basics, combat, properties, protections, resists,
	default_stats FROM creatures`

type LootRow struct {
	ItemID   string  `json:"item_id" db:"item_id"`
	DropRate float32 `json:"chance" db:"drop_rate"`
	MinQty   int32   `json:"min_qty" db:"min_qty"`
	MaxQty   int32   `json:"max_qty" db:"max_qty"`
}

type Skill struct {
	ID            string   `json:"id" db:"id"`
	Name          *string  `json:"name" db:"name"`
	Branch        *string  `json:"branch" db:"branch"`
	Cost          *int32   `json:"cost" db:"cost"`
	Prerequisites []string `json:"prerequisites" db:"prerequisites"`
}

func (s *server) fetchItem(r *http.Request, id string) (Item, error) {
	rows, err := s.db.Query(r.Context(), itemSelect+" WHERE id = $1", id)
	if err != nil {
		return Item{}, err
	}
	return pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[Item])
}

func (s *server) fetchLoot(r *http.Request, creatureID string) ([]LootRow, error) {
	rows, err := s.db.Query(r.Context(),
		"SELECT item_id, drop_rate, min_qty, max_qty FROM npc_loot_tables WHERE npc_id = $1 ORDER BY item_id",
		creatureID)
	if err != nil {
		return nil, err
	}
	loot, err := pgx.CollectRows(rows, pgx.RowToStructByName[LootRow])
	if loot == nil {
		loot = []LootRow{}
	}
	return loot, err
}

func (s *server) fetchBricks(r *http.Request, ids []string) ([]Brick, error) {
	rows, err := s.db.Query(r.Context(), brickSelect+" WHERE id = ANY($1)", ids)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[Brick])
}

func (s *server) fetchBrick(r *http.Request, id string) (Brick, error) {
	rows, err := s.db.Query(r.Context(), brickSelect+" WHERE id = $1", id)
	if err != nil {
		return Brick{}, err
	}
	return pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[Brick])
}
