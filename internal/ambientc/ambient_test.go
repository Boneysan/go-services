package ambientc

import "testing"

func TestKeyAndCandidates(t *testing.T) {
	if got := Key("Shopkeeper", " Fyros ", "PYR"); got != "shopkeeper.fyros.pyr" {
		t.Errorf("Key = %q", got)
	}
	if got := Key("guard", "", "pyr"); got != "guard.pyr" {
		// empty faction is dropped — note Key just joins non-empties
		t.Errorf("Key with empty faction = %q", got)
	}

	got := CandidateKeys("shopkeeper", "fyros", "pyr")
	want := []string{"shopkeeper.fyros.pyr", "shopkeeper.fyros", "shopkeeper"}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candidate[%d] = %q want %q", i, got[i], want[i])
		}
	}
	if CandidateKeys("", "fyros", "pyr") != nil {
		t.Error("no archetype should yield no candidates")
	}
}

func TestLookupFallback(t *testing.T) {
	lib := Library{Schema: SchemaVersion, Pools: map[string][]string{
		"shopkeeper.fyros": {"Fyros steel is not cheap."},
		"guard":            {"Move along."},
	}}
	if got := lib.Lookup("shopkeeper", "fyros", "pyr"); len(got) != 1 || got[0] != "Fyros steel is not cheap." {
		t.Errorf("region fallback failed: %v", got)
	}
	if got := lib.Lookup("guard", "matis", "yrkanis"); len(got) != 1 || got[0] != "Move along." {
		t.Errorf("archetype fallback failed: %v", got)
	}
	if got := lib.Lookup("farmer", "tryker", "fairhaven"); got != nil {
		t.Errorf("unknown archetype should miss: %v", got)
	}
}

func TestValidate(t *testing.T) {
	ok := Library{Schema: SchemaVersion, Pools: map[string][]string{"guard": {"Halt."}}}
	if err := Validate(ok); err != nil {
		t.Errorf("valid library rejected: %v", err)
	}
	for _, bad := range []Library{
		{Schema: "wrong", Pools: map[string][]string{"guard": {"x"}}},
		{Schema: SchemaVersion},
		{Schema: SchemaVersion, Pools: map[string][]string{"guard": {}}},
		{Schema: SchemaVersion, Pools: map[string][]string{"guard": {"  "}}},
	} {
		if err := Validate(bad); err == nil {
			t.Errorf("expected error for %+v", bad)
		}
	}
}
