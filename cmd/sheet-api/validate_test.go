package main

import (
	"encoding/json"
	"testing"
)

func brick(id, btype string, sabrina int) Brick {
	extras, _ := json.Marshal(map[string]any{
		"Basics": map[string]string{"SabrinaCost": jsonInt(sabrina)},
	})
	b := Brick{ID: id, Extras: extras}
	if btype != "" {
		b.BrickType = &btype
	}
	return b
}

func jsonInt(n int) string {
	b, _ := json.Marshal(n)
	return string(b) // SabrinaCost is stored as a string atom, e.g. "-10"
}

func TestValidatePhrase(t *testing.T) {
	root := brick("bfpa01", "Mandatory", 10)
	root2 := brick("bfpa02", "Mandatory", 0)
	credit := brick("bfca01", "Credit", -15)
	opt := brick("bfma01", "Optional", 5)

	cases := []struct {
		name      string
		requested []string
		found     []Brick
		valid     bool
		errCount  int
	}{
		{"empty phrase", nil, nil, false, 1},
		{"unknown brick", []string{"nope"}, nil, false, 1},
		{"duplicate brick", []string{"bfpa01", "bfpa01"}, []Brick{root}, false, 2}, // dup + cost 10 uncovered
		{"two mandatory", []string{"bfpa01", "bfpa02"}, []Brick{root, root2}, false, 2},
		{"cost uncovered", []string{"bfpa01", "bfma01"}, []Brick{root, opt}, false, 1},
		{"cost covered by credit", []string{"bfpa01", "bfca01"}, []Brick{root, credit}, true, 0},
		{"root plus optional plus credit", []string{"bfpa01", "bfma01", "bfca01"}, []Brick{root, opt, credit}, true, 0},
		{"free root alone", []string{"bfpa02"}, []Brick{root2}, true, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidatePhrase(tc.requested, tc.found)
			if got.Valid != tc.valid {
				t.Fatalf("valid = %v, want %v (errors: %v)", got.Valid, tc.valid, got.Errors)
			}
			if len(got.Errors) != tc.errCount {
				t.Fatalf("errors = %v, want %d", got.Errors, tc.errCount)
			}
		})
	}
}

func TestSabrinaCostMissingExtras(t *testing.T) {
	if c := sabrinaCost(Brick{ID: "x", Extras: json.RawMessage(`{}`)}); c != 0 {
		t.Fatalf("missing SabrinaCost should read 0, got %d", c)
	}
	if c := sabrinaCost(Brick{ID: "x"}); c != 0 {
		t.Fatalf("nil extras should read 0, got %d", c)
	}
}
