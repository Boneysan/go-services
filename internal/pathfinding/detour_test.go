package pathfinding

import "testing"

// flatQuad returns a synthetic 20x20m flat ground mesh (y=0), centered at
// the origin, as two triangles. Big enough to clear Recast's minimum
// region-area thresholds at the default bake params.
func flatQuad() ([]float32, []int32) {
	verts := []float32{
		-10, 0, -10, // 0
		10, 0, -10, // 1
		10, 0, 10, // 2
		-10, 0, 10, // 3
	}
	// Wound so the cross product points +Y (up) — Recast culls
	// downward-facing triangles as non-walkable.
	indices := []int32{
		0, 2, 1,
		0, 3, 2,
	}
	return verts, indices
}

func TestBakeNavMesh_FlatQuad(t *testing.T) {
	verts, indices := flatQuad()
	data, err := BakeNavMesh(verts, indices, DefaultBakeParams())
	if err != nil {
		t.Fatalf("BakeNavMesh: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("BakeNavMesh: empty navmesh data")
	}
}

func TestBakeNavMesh_RejectsBadInput(t *testing.T) {
	if _, err := BakeNavMesh(nil, []int32{0, 1, 2}, DefaultBakeParams()); err == nil {
		t.Error("expected error for empty verts")
	}
	if _, err := BakeNavMesh([]float32{0, 0, 0}, nil, DefaultBakeParams()); err == nil {
		t.Error("expected error for empty indices")
	}
}

func TestLoadAndFindPath_FlatQuad(t *testing.T) {
	verts, indices := flatQuad()
	data, err := BakeNavMesh(verts, indices, DefaultBakeParams())
	if err != nil {
		t.Fatalf("BakeNavMesh: %v", err)
	}

	mesh, err := LoadNavMesh(data)
	if err != nil {
		t.Fatalf("LoadNavMesh: %v", err)
	}
	defer mesh.Close()

	path, err := mesh.FindPath(Vec3{X: -8, Y: 0, Z: -8}, Vec3{X: 8, Y: 0, Z: 8}, 32)
	if err != nil {
		t.Fatalf("FindPath: %v", err)
	}
	if len(path) < 2 {
		t.Fatalf("FindPath: expected at least start+end waypoints, got %d", len(path))
	}

	first, last := path[0], path[len(path)-1]
	if dist2(first, Vec3{X: -8, Y: 0, Z: -8}) > 4 {
		t.Errorf("first waypoint %v too far from requested start", first)
	}
	if dist2(last, Vec3{X: 8, Y: 0, Z: 8}) > 4 {
		t.Errorf("last waypoint %v too far from requested end", last)
	}
}

func TestFindPath_UnreachableOffMesh(t *testing.T) {
	verts, indices := flatQuad()
	data, err := BakeNavMesh(verts, indices, DefaultBakeParams())
	if err != nil {
		t.Fatalf("BakeNavMesh: %v", err)
	}
	mesh, err := LoadNavMesh(data)
	if err != nil {
		t.Fatalf("LoadNavMesh: %v", err)
	}
	defer mesh.Close()

	// Far outside the quad and outside findNearestPoly's search extents —
	// must fail cleanly, not crash or hang.
	if _, err := mesh.FindPath(Vec3{X: -8, Y: 0, Z: -8}, Vec3{X: 500, Y: 0, Z: 500}, 32); err == nil {
		t.Error("expected error finding path to a point far off the navmesh")
	}
}

func TestLoadNavMesh_RejectsGarbage(t *testing.T) {
	if _, err := LoadNavMesh([]byte{1, 2, 3, 4}); err == nil {
		t.Error("expected error loading garbage navmesh data")
	}
}

func dist2(a, b Vec3) float32 {
	dx, dy, dz := a.X-b.X, a.Y-b.Y, a.Z-b.Z
	return dx*dx + dy*dy + dz*dz
}
