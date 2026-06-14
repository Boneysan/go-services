// Package ambientc defines the ambient (background-NPC) dialogue library:
// pre-generated flavor lines keyed by archetype × faction × region. Unlike the
// authored scenario-graph dialogue, ambient lines have no branching and no quest
// impact — a shopkeeper or townsperson just speaks a contextually fitting bark.
// The library is generated once and shipped as data; nothing calls an LLM at play
// time. The runtime picker (Godot) mirrors Key/CandidateKeys.
package ambientc

import (
	"errors"
	"fmt"
	"strings"
)

const SchemaVersion = "ambient-dialogue/v1"

// Library is the on-disk format. Pools are keyed by Key(archetype,faction,region).
type Library struct {
	Schema string              `json:"schema"`
	Pools  map[string][]string `json:"pools"`
}

func norm(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// Key joins the non-empty, normalized segments with dots (e.g. "shopkeeper.fyros.pyr").
func Key(parts ...string) string {
	var out []string
	for _, p := range parts {
		if n := norm(p); n != "" {
			out = append(out, n)
		}
	}
	return strings.Join(out, ".")
}

// CandidateKeys lists pool keys from most to least specific, so a missing
// region falls back to faction, and a missing faction falls back to archetype.
func CandidateKeys(archetype, faction, region string) []string {
	a, f, r := norm(archetype), norm(faction), norm(region)
	var keys []string
	if a == "" {
		return keys
	}
	if f != "" && r != "" {
		keys = append(keys, a+"."+f+"."+r)
	}
	if f != "" {
		keys = append(keys, a+"."+f)
	}
	keys = append(keys, a)
	return keys
}

// Lookup returns the most specific non-empty pool for the given context, or nil.
func (l Library) Lookup(archetype, faction, region string) []string {
	for _, k := range CandidateKeys(archetype, faction, region) {
		if lines, ok := l.Pools[k]; ok && len(lines) > 0 {
			return lines
		}
	}
	return nil
}

// Validate checks the library is well-formed.
func Validate(l Library) error {
	if l.Schema != SchemaVersion {
		return fmt.Errorf("schema must be %q, got %q", SchemaVersion, l.Schema)
	}
	if len(l.Pools) == 0 {
		return errors.New("library has no pools")
	}
	for key, lines := range l.Pools {
		if strings.TrimSpace(key) == "" {
			return errors.New("pool key is empty")
		}
		if len(lines) == 0 {
			return fmt.Errorf("pool %q has no lines", key)
		}
		for i, ln := range lines {
			if strings.TrimSpace(ln) == "" {
				return fmt.Errorf("pool %q line %d is empty", key, i)
			}
		}
	}
	return nil
}
