package main

import (
	"strings"
	"testing"
)

func TestParseAndRoll(t *testing.T) {
	// "1d0" used to panic via rand.Intn(0); must be reported invalid instead.
	for _, f := range []string{"1d0", "0d6", "", "garbage", "d20"} {
		got := parseAndRoll(f)
		if !strings.HasPrefix(got, "Invalid") {
			t.Errorf("parseAndRoll(%q) = %q, want Invalid…", f, got)
		}
	}

	// Valid formulas produce a rolled total.
	for _, f := range []string{"1d20", "2d6 + 3", "3d8"} {
		got := parseAndRoll(f)
		if !strings.HasPrefix(got, "Rolled") {
			t.Errorf("parseAndRoll(%q) = %q, want Rolled…", f, got)
		}
	}
}

func TestParseAndRollBounds(t *testing.T) {
	// Many iterations to shake out any panic on edge formulas.
	for i := 0; i < 1000; i++ {
		_ = parseAndRoll("100d1000 + 5")
		_ = parseAndRoll("1d1")
	}
}
