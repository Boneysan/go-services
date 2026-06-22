package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/boneysan/ryzom/go-services/internal/pathfinding"
)

func testStoreWithFlatQuad(t *testing.T) *zoneStore {
	t.Helper()
	verts := []float32{
		-10, 0, -10,
		10, 0, -10,
		10, 0, 10,
		-10, 0, 10,
	}
	indices := []int32{0, 2, 1, 0, 3, 2}

	data, err := pathfinding.BakeNavMesh(verts, indices, pathfinding.DefaultBakeParams())
	if err != nil {
		t.Fatalf("BakeNavMesh: %v", err)
	}
	mesh, err := pathfinding.LoadNavMesh(data)
	if err != nil {
		t.Fatalf("LoadNavMesh: %v", err)
	}
	store := newZoneStore()
	store.set("test_zone", mesh)
	return store
}

func TestFindPath_Success(t *testing.T) {
	store := testStoreWithFlatQuad(t)
	resp, err := findPath(store, findPathRequest{
		Zone:  "test_zone",
		Start: Vec3Req{X: -8, Y: 0, Z: -8},
		End:   Vec3Req{X: 8, Y: 0, Z: 8},
	})
	if err != nil {
		t.Fatalf("findPath: %v", err)
	}
	if len(resp.Path) < 2 {
		t.Fatalf("expected at least 2 waypoints, got %d", len(resp.Path))
	}
}

func TestFindPath_UnknownZone(t *testing.T) {
	store := testStoreWithFlatQuad(t)
	_, err := findPath(store, findPathRequest{
		Zone:  "does_not_exist",
		Start: Vec3Req{X: 0, Y: 0, Z: 0},
		End:   Vec3Req{X: 1, Y: 0, Z: 1},
	})
	if err == nil {
		t.Fatal("expected error for unknown zone")
	}
}

func TestFindPath_MissingZone(t *testing.T) {
	store := testStoreWithFlatQuad(t)
	_, err := findPath(store, findPathRequest{
		Start: Vec3Req{X: 0, Y: 0, Z: 0},
		End:   Vec3Req{X: 1, Y: 0, Z: 1},
	})
	if err == nil {
		t.Fatal("expected error when zone is omitted")
	}
}

func TestHandleFindPath_HTTP(t *testing.T) {
	srv := &server{zones: testStoreWithFlatQuad(t)}

	body, _ := json.Marshal(findPathRequest{
		Zone:  "test_zone",
		Start: Vec3Req{X: -8, Y: 0, Z: -8},
		End:   Vec3Req{X: 8, Y: 0, Z: 8},
	})
	req := httptest.NewRequest(http.MethodPost, "/path/find", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	srv.handleFindPath(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp findPathResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Path) < 2 {
		t.Fatalf("expected at least 2 waypoints, got %d", len(resp.Path))
	}
}

func TestHandleFindPath_UnknownZoneReturns404(t *testing.T) {
	srv := &server{zones: testStoreWithFlatQuad(t)}

	body, _ := json.Marshal(findPathRequest{
		Zone:  "nope",
		Start: Vec3Req{X: 0, Y: 0, Z: 0},
		End:   Vec3Req{X: 1, Y: 0, Z: 1},
	})
	req := httptest.NewRequest(http.MethodPost, "/path/find", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	srv.handleFindPath(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandleFindPath_BadJSONReturns400(t *testing.T) {
	srv := &server{zones: testStoreWithFlatQuad(t)}

	req := httptest.NewRequest(http.MethodPost, "/path/find", bytes.NewReader([]byte("{not json")))
	rec := httptest.NewRecorder()

	srv.handleFindPath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestLoadNavMeshDir_LoadsTutorialZone(t *testing.T) {
	store := newZoneStore()
	count, err := loadNavMeshDir(store, "../navmesh-bake/testdata")
	if err != nil {
		t.Fatalf("loadNavMeshDir: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 navmesh loaded, got %d", count)
	}
	if _, ok := store.get("synthetic_tutorial"); !ok {
		t.Fatalf("expected zone %q to be loaded, got zones %v", "synthetic_tutorial", store.zoneNames())
	}
}

func TestLoadNavMeshDir_MissingDirIsNotFatal(t *testing.T) {
	store := newZoneStore()
	count, err := loadNavMeshDir(store, "/path/does/not/exist")
	if err == nil {
		t.Fatal("expected an error for a missing directory")
	}
	if count != 0 {
		t.Fatalf("expected 0 zones loaded, got %d", count)
	}
}
