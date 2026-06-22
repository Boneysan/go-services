package main

import (
	"bytes"
	"testing"

	"github.com/boneysan/ryzom/go-services/internal/pathfinding"
)

func TestParseOBJ_Triangle(t *testing.T) {
	src := "# comment\nv 0 0 0\nv 1 0 0\nv 1 0 1\nf 1 2 3\n"
	verts, indices, err := ParseOBJ(bytes.NewReader([]byte(src)))
	if err != nil {
		t.Fatalf("ParseOBJ: %v", err)
	}
	if len(verts) != 9 {
		t.Fatalf("expected 9 vert floats, got %d", len(verts))
	}
	if len(indices) != 3 || indices[0] != 0 || indices[1] != 1 || indices[2] != 2 {
		t.Fatalf("unexpected indices: %v", indices)
	}
}

func TestParseOBJ_QuadFanTriangulates(t *testing.T) {
	src := "v 0 0 0\nv 1 0 0\nv 1 0 1\nv 0 0 1\nf 1 2 3 4\n"
	_, indices, err := ParseOBJ(bytes.NewReader([]byte(src)))
	if err != nil {
		t.Fatalf("ParseOBJ: %v", err)
	}
	want := []int32{0, 1, 2, 0, 2, 3}
	if len(indices) != len(want) {
		t.Fatalf("expected %v, got %v", want, indices)
	}
	for i := range want {
		if indices[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, indices)
		}
	}
}

func TestParseOBJ_SlashIndices(t *testing.T) {
	src := "v 0 0 0\nv 1 0 0\nv 1 0 1\nvn 0 1 0\nf 1/1/1 2/2/1 3/3/1\n"
	_, indices, err := ParseOBJ(bytes.NewReader([]byte(src)))
	if err != nil {
		t.Fatalf("ParseOBJ: %v", err)
	}
	if len(indices) != 3 || indices[0] != 0 || indices[1] != 1 || indices[2] != 2 {
		t.Fatalf("unexpected indices: %v", indices)
	}
}

func TestGenerateTutorialZone_RoundTripsThroughOBJ(t *testing.T) {
	verts, indices := GenerateTutorialZone()
	if len(verts) == 0 || len(indices) == 0 {
		t.Fatal("GenerateTutorialZone produced an empty mesh")
	}

	var buf bytes.Buffer
	if err := WriteOBJ(&buf, verts, indices); err != nil {
		t.Fatalf("WriteOBJ: %v", err)
	}

	gotVerts, gotIndices, err := ParseOBJ(&buf)
	if err != nil {
		t.Fatalf("ParseOBJ round-trip: %v", err)
	}
	if len(gotVerts) != len(verts) {
		t.Fatalf("vert count mismatch: want %d, got %d", len(verts), len(gotVerts))
	}
	if len(gotIndices) != len(indices) {
		t.Fatalf("index count mismatch: want %d, got %d", len(indices), len(gotIndices))
	}
	for i := range verts {
		if gotVerts[i] != verts[i] {
			t.Fatalf("vert %d mismatch: want %g, got %g", i, verts[i], gotVerts[i])
		}
	}
}

func TestGenerateTutorialZone_Bakes(t *testing.T) {
	verts, indices := GenerateTutorialZone()
	_, err := pathfinding.BakeNavMesh(verts, indices, pathfinding.DefaultBakeParams())
	if err != nil {
		t.Fatalf("BakeNavMesh on synthetic tutorial zone: %v", err)
	}
}
