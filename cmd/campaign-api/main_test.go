package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestNewBundleMarshalsEmptyCollections guards the v1 wire shape: the reserved
// Phase 5.8/5.3a sections must serialise as [] / {} (never null) so consumers
// can iterate them unconditionally.
func TestNewBundleMarshalsEmptyCollections(t *testing.T) {
	b := newBundle()
	if b.Schema != bundleSchema {
		t.Fatalf("schema = %q, want %q", b.Schema, bundleSchema)
	}
	out, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		`"characters":[]`,
		`"chronicle":[]`,
		`"faction_standings":{}`,
		`"npc_attitudes":{}`,
		`"world_events":[]`,
		`"party_stash":[]`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("bundle JSON missing %s\n got: %s", want, s)
		}
	}
	if strings.Contains(s, "null") {
		t.Errorf("bundle JSON should have no null collections: %s", s)
	}
}

// TestBundleRoundTripJSON confirms a populated bundle survives marshal/unmarshal
// with nullable character fields preserved as pointers.
func TestBundleRoundTripJSON(t *testing.T) {
	egs := int64(42)
	in := newBundle()
	in.Campaign = Campaign{ID: "cid", Name: "Test", SessionsPlayed: 3}
	in.Characters = []Character{
		{AccountID: "1", Slot: 0, Name: "A", Race: "fyros", Gender: "male", EGSEntityID: &egs},
		{AccountID: "1", Slot: 1, Name: "B", Race: "matis", Gender: "female"}, // EGSEntityID nil
	}

	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Bundle
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Campaign != in.Campaign || len(out.Characters) != 2 {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
	if out.Characters[0].EGSEntityID == nil || *out.Characters[0].EGSEntityID != 42 {
		t.Fatalf("egs entity id not preserved: %+v", out.Characters[0])
	}
	if out.Characters[1].EGSEntityID != nil {
		t.Fatalf("nil egs entity id should stay nil, got %v", *out.Characters[1].EGSEntityID)
	}
}
