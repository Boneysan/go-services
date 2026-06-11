package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Fixtures mirror the real fight bricks: bfpa01 is the combat root accepting
// Optional families BFOA.. and Credit families BFCA..; bfca01 is a -6 credit.

func mkBrick(id, btype, family string, sabrina int, lists map[string][]string) Brick {
	extras := map[string]any{
		"Basics": map[string]string{
			"FamilyId":    family,
			"SabrinaCost": fmt.Sprintf("%d", sabrina),
		},
	}
	for kind, fams := range lists {
		m := map[string]string{}
		for i, f := range fams {
			m[fmt.Sprintf("f%d", i)] = f
		}
		extras[kind] = m
	}
	raw, _ := json.Marshal(extras)
	b := Brick{ID: id, Extras: raw}
	if btype != "" {
		b.BrickType = &btype
	}
	return b
}

var (
	rootFight = mkBrick("bfpa01", "Root", "BFPA", 0, map[string][]string{
		"Optional": {"BFOA", "BFOB"},
		"Credit":   {"BFCA", "BFCB"},
	})
	rootNeedy = mkBrick("bmpa01", "Root", "BMPA", 0, map[string][]string{
		"Mandatory": {"BMMA"},
		"Credit":    {"BMCA"},
	})
	optA    = mkBrick("bfoa01", "Optional", "BFOA", 5, nil)
	optA2   = mkBrick("bfoa02", "Optional", "BFOA", 8, nil)
	optB    = mkBrick("bfob01", "Optional", "BFOB", 0, nil)
	credit6 = mkBrick("bfca01", "Credit", "BFCA", -6, nil)
	mandM   = mkBrick("bmma01", "Mandatory", "BMMA", 0, nil)
	alien   = mkBrick("bhfa01", "Optional", "BHFA", 0, nil) // family not in root lists
	noFam   = mkBrick("big01", "", "", 0, nil)              // interface brick
)

func validate(req []string, found ...Brick) ValidateResult {
	return ValidatePhrase(req, found)
}

func TestValidatePhraseStructure(t *testing.T) {
	cases := []struct {
		name    string
		req     []string
		found   []Brick
		valid   bool
		errPart string
	}{
		{"empty", nil, nil, false, "no bricks"},
		{"unknown", []string{"nope"}, nil, false, "unknown brick"},
		{"duplicate", []string{"bfpa01", "bfpa01"}, []Brick{rootFight}, false, "duplicate"},
		{"root alone", []string{"bfpa01"}, []Brick{rootFight}, true, ""},
		{"first brick not root", []string{"bfoa01", "bfpa01"}, []Brick{optA, rootFight}, false, "not a root"},
		{"optional fits", []string{"bfpa01", "bfob01"}, []Brick{rootFight, optB}, true, ""},
		{"alien family rejected", []string{"bfpa01", "bhfa01"}, []Brick{rootFight, alien}, false, "does not fit root"},
		{"no-family brick rejected", []string{"bfpa01", "big01"}, []Brick{rootFight, noFam}, false, "no family"},
		{"two bricks same family", []string{"bfpa01", "bfoa01", "bfoa02"},
			[]Brick{rootFight, optA, optA2}, false, "one per family"},
		{"missing mandatory slot", []string{"bmpa01"}, []Brick{rootNeedy}, false, "mandatory family BMMA"},
		{"mandatory slot filled", []string{"bmpa01", "bmma01"}, []Brick{rootNeedy, mandM}, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := validate(tc.req, tc.found...)
			if got.Valid != tc.valid {
				t.Fatalf("valid = %v, want %v (errors: %v)", got.Valid, tc.valid, got.Errors)
			}
			if tc.errPart != "" && !strings.Contains(strings.Join(got.Errors, "; "), tc.errPart) {
				t.Fatalf("errors %v missing %q", got.Errors, tc.errPart)
			}
		})
	}
}

func TestValidatePhraseCredit(t *testing.T) {
	// cost 5 optional uncovered -> invalid
	got := validate([]string{"bfpa01", "bfoa01"}, rootFight, optA)
	if got.Valid || !strings.Contains(got.Errors[0], "cost 5 exceeds credit") {
		t.Fatalf("uncovered cost should fail: %+v", got)
	}
	// -6 credit covers it
	got = validate([]string{"bfpa01", "bfoa01", "bfca01"}, rootFight, optA, credit6)
	if !got.Valid {
		t.Fatalf("credit-covered phrase should pass: %v", got.Errors)
	}
	// 5 + 8 = 13 > 6 -> invalid again, remaining cost 7
	optC := mkBrick("bfob02", "Optional", "BFOB", 8, nil)
	got = validate([]string{"bfpa01", "bfoa01", "bfob02", "bfca01"}, rootFight, optA, optC, credit6)
	if got.Valid || !strings.Contains(strings.Join(got.Errors, " "), "cost 7 exceeds") {
		t.Fatalf("overdrawn credit should fail with remaining 7: %+v", got)
	}
}

func TestParseExtrasTolerant(t *testing.T) {
	if e := parseExtras(Brick{ID: "x"}); e.Basics.FamilyId != "" || e.sabrinaCost() != 0 {
		t.Fatalf("nil extras should be zero value: %+v", e)
	}
	if e := parseExtras(Brick{ID: "x", Extras: json.RawMessage(`{"Basics":{"SabrinaCost":"oops"}}`)}); e.sabrinaCost() != 0 {
		t.Fatal("non-numeric SabrinaCost should read 0")
	}
}
