package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Phrase validation — mirrors the C++ client's phrase builder rules.
//
// Investigation note (2026-06-11): the EGS's own validateSabrinaGrammar() in
// phrase_manager/s_phrase.h is a stub returning true — the legacy server
// TRUSTED the client to compose legal phrases. Since the Godot client
// replaces that client, this validator is now the authoritative gatekeeper.
//
// Rules, mirrored from CDBGroupBuildPhrase / CSBrickSheet semantics:
//  1. the first brick must be a Root brick (the action anchor)
//  2. every other brick's family must appear in the root's Mandatory/
//     Optional/Parameter/Credit family lists (stored in extras by the
//     importer, e.g. extras.Optional = {"f0": "BFOA", ...})
//  3. every family in the root's Mandatory list must be present
//  4. at most one brick per family (families are progression tiers)
//  5. SabrinaCost balance: bricks cost positive, Credit bricks carry
//     negative cost; the phrase total must not exceed zero
//  6. no duplicate brick ids; all ids must exist
//
// brick_type comes from Basics.FamilyId classified through the TBrickFamily
// enum ranges (game_share/brick_families.h) at import time.

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

// brickExtras is the slice of the imported .sbrick form the validator needs.
type brickExtras struct {
	Basics struct {
		FamilyId    string `json:"FamilyId"`
		SabrinaCost string `json:"SabrinaCost"`
	} `json:"Basics"`
	Mandatory map[string]string `json:"Mandatory"`
	Optional  map[string]string `json:"Optional"`
	Parameter map[string]string `json:"Parameter"`
	Credit    map[string]string `json:"Credit"`
}

func parseExtras(b Brick) brickExtras {
	var e brickExtras
	json.Unmarshal(b.Extras, &e) // zero value on error: no family, cost 0
	return e
}

func (e brickExtras) sabrinaCost() int {
	n, err := strconv.Atoi(e.Basics.SabrinaCost)
	if err != nil {
		return 0
	}
	return n
}

// attachLists returns the family sets a root brick accepts, keyed by slot
// kind. Empty f-values in the form are skipped.
func (e brickExtras) attachLists() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for kind, fams := range map[string]map[string]string{
		"Mandatory": e.Mandatory, "Optional": e.Optional,
		"Parameter": e.Parameter, "Credit": e.Credit,
	} {
		set := map[string]bool{}
		for _, fam := range fams {
			if fam != "" {
				set[fam] = true
			}
		}
		out[kind] = set
	}
	return out
}

func isType(b Brick, t string) bool { return b.BrickType != nil && *b.BrickType == t }

func ValidatePhrase(requested []string, found []Brick) ValidateResult {
	errs := []string{}
	if len(requested) == 0 {
		return ValidateResult{Valid: false, Errors: []string{"phrase has no bricks"}}
	}

	byID := map[string]Brick{}
	for _, b := range found {
		byID[b.ID] = b
	}
	seen := map[string]bool{}
	allKnown := true
	for _, id := range requested {
		if seen[id] {
			errs = append(errs, "duplicate brick: "+id)
			continue
		}
		seen[id] = true
		if _, ok := byID[id]; !ok {
			errs = append(errs, "unknown brick: "+id)
			allKnown = false
		}
	}
	if !allKnown {
		// Structural rules need every brick's sheet data; stop here.
		return ValidateResult{Valid: false, Errors: errs}
	}

	root := byID[requested[0]]
	rootExtras := parseExtras(root)
	if !isType(root, "Root") {
		errs = append(errs, fmt.Sprintf("first brick %s is not a root brick (type %s)",
			root.ID, strOr(root.BrickType, "unknown")))
		return ValidateResult{Valid: false, Errors: errs}
	}
	lists := rootExtras.attachLists()

	total := rootExtras.sabrinaCost()
	famUsed := map[string]string{rootExtras.Basics.FamilyId: root.ID}
	for _, id := range requested[1:] {
		if id == requested[0] {
			continue // duplicate already reported
		}
		b := byID[id]
		e := parseExtras(b)
		fam := e.Basics.FamilyId
		total += e.sabrinaCost()

		if fam == "" {
			errs = append(errs, fmt.Sprintf("brick %s has no family — not a phrase component", id))
			continue
		}
		if prev, used := famUsed[fam]; used {
			errs = append(errs, fmt.Sprintf("bricks %s and %s are both family %s — one per family", prev, id, fam))
			continue
		}
		famUsed[fam] = id
		if !lists["Mandatory"][fam] && !lists["Optional"][fam] &&
			!lists["Parameter"][fam] && !lists["Credit"][fam] {
			errs = append(errs, fmt.Sprintf("brick %s (family %s) does not fit root %s — allowed: %s",
				id, fam, root.ID, joinLists(lists)))
		}
	}

	for fam := range lists["Mandatory"] {
		if _, ok := famUsed[fam]; !ok {
			errs = append(errs, fmt.Sprintf("root %s requires a brick from mandatory family %s", root.ID, fam))
		}
	}

	if total > 0 {
		errs = append(errs, fmt.Sprintf("brick cost %d exceeds credit — add credit bricks", total))
	}

	return ValidateResult{Valid: len(errs) == 0, Errors: errs}
}

func strOr(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
}

func joinLists(lists map[string]map[string]bool) string {
	var fams []string
	for _, kind := range []string{"Mandatory", "Optional", "Parameter", "Credit"} {
		for fam := range lists[kind] {
			fams = append(fams, fam)
		}
	}
	if len(fams) == 0 {
		return "none"
	}
	return strings.Join(fams, " ")
}
