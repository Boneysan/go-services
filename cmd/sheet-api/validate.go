package main

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Phrase validation — first-pass permissive validator (Phase 4 plan, Task
// 4.1b step 5 + risk register: "ship a permissive validator that checks basic
// type constraints first, then tighten rules as the rule set is confirmed").
//
// Rules enforced now (Sabrina brick system, from .sbrick corpus semantics):
//  1. the brick list must be non-empty and free of duplicates
//  2. every brick must exist in the bricks table
//  3. at most one Mandatory (root) brick per phrase
//  4. SabrinaCost balance: bricks carry positive cost, Credit bricks carry
//     negative cost; the phrase total must not exceed zero
//
// Not yet enforced (needs CSPhraseCom dive in the EGS): per-family
// compatibility, Parameter brick requirements, skill-line consistency.

type Brick struct {
	ID        string          `json:"id" db:"id"`
	Family    *string         `json:"family" db:"family"`
	BrickType *string         `json:"brick_type" db:"brick_type"`
	SkillReq  *string         `json:"skill_req" db:"skill_req"`
	SkillMin  *int32          `json:"skill_min" db:"skill_min"`
	SapCost   *int32          `json:"sap_cost" db:"sap_cost"`
	HpCost    *int32          `json:"hp_cost" db:"hp_cost"`
	StaCost   *int32          `json:"sta_cost" db:"sta_cost"`
	Extras    json.RawMessage `json:"extras" db:"extras"`
}

const brickSelect = `SELECT id, family, brick_type, skill_req, skill_min,
	sap_cost, hp_cost, sta_cost, extras FROM bricks`

type ValidateResult struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors"`
}

// sabrinaCost reads extras.Basics.SabrinaCost ("-10", "15", absent -> 0).
func sabrinaCost(b Brick) int {
	var extras struct {
		Basics struct {
			SabrinaCost string `json:"SabrinaCost"`
		} `json:"Basics"`
	}
	if err := json.Unmarshal(b.Extras, &extras); err != nil {
		return 0
	}
	n, err := strconv.Atoi(extras.Basics.SabrinaCost)
	if err != nil {
		return 0
	}
	return n
}

func ValidatePhrase(requested []string, found []Brick) ValidateResult {
	errs := []string{}
	if len(requested) == 0 {
		errs = append(errs, "phrase has no bricks")
		return ValidateResult{Valid: false, Errors: errs}
	}

	seen := map[string]bool{}
	for _, id := range requested {
		if seen[id] {
			errs = append(errs, fmt.Sprintf("duplicate brick: %s", id))
		}
		seen[id] = true
	}

	byID := map[string]Brick{}
	for _, b := range found {
		byID[b.ID] = b
	}
	for _, id := range requested {
		if _, ok := byID[id]; !ok {
			errs = append(errs, fmt.Sprintf("unknown brick: %s", id))
		}
	}

	mandatory := 0
	total := 0
	for id := range seen {
		b, ok := byID[id]
		if !ok {
			continue
		}
		if b.BrickType != nil && *b.BrickType == "Mandatory" {
			mandatory++
		}
		total += sabrinaCost(b)
	}
	if mandatory > 1 {
		errs = append(errs, fmt.Sprintf("phrase has %d mandatory (root) bricks, max 1", mandatory))
	}
	if total > 0 {
		errs = append(errs, fmt.Sprintf("brick cost %d exceeds credit — add credit bricks", total))
	}

	return ValidateResult{Valid: len(errs) == 0, Errors: errs}
}
