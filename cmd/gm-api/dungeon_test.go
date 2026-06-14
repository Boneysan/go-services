package main

import (
	"testing"
)

func TestGenerateLayout(t *testing.T) {
	size := 5
	rooms := generateLayout(size)

	if len(rooms) == 0 {
		t.Fatalf("generateLayout returned 0 rooms")
	}

	hasStart := false
	hasBoss := false

	for _, room := range rooms {
		if room.Type == "Start" {
			hasStart = true
		}
		if room.Type == "Boss" {
			hasBoss = true
		}
		if room.X < 0 || room.X >= size || room.Y < 0 || room.Y >= size {
			t.Errorf("room out of bounds: %+v", room)
		}
	}

	if !hasStart {
		t.Errorf("generateLayout did not produce a Start room")
	}
	if !hasBoss {
		// Because size=5 and rand > 0, we expect at least a boss usually, but WFC-lite is random.
		// If size is > 1 it should almost always produce a boss if at least one room was generated besides Start.
		if len(rooms) > 1 {
			t.Errorf("generateLayout did not produce a Boss room when rooms > 1")
		}
	}
}
