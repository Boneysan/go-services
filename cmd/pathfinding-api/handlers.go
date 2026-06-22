package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/boneysan/ryzom/go-services/internal/pathfinding"
)

// Vec3Req mirrors pathfinding.Vec3 for JSON requests/responses.
type Vec3Req struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
	Z float32 `json:"z"`
}

type findPathRequest struct {
	Zone      string  `json:"zone"`
	Start     Vec3Req `json:"start"`
	End       Vec3Req `json:"end"`
	MaxPoints int     `json:"max_points,omitempty"`
}

type findPathResponse struct {
	Path []Vec3Req `json:"path"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// zoneStore holds the navmeshes loaded at startup, keyed by zone name.
// Read-only after load, so no locking is needed for lookups — the mutex
// exists only to make that invariant explicit and safe if a future task
// adds hot-reload.
type zoneStore struct {
	mu     sync.RWMutex
	meshes map[string]*pathfinding.NavMesh
}

func newZoneStore() *zoneStore {
	return &zoneStore{meshes: make(map[string]*pathfinding.NavMesh)}
}

func (s *zoneStore) get(zone string) (*pathfinding.NavMesh, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.meshes[zone]
	return m, ok
}

func (s *zoneStore) set(zone string, mesh *pathfinding.NavMesh) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meshes[zone] = mesh
}

func (s *zoneStore) zoneNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.meshes))
	for z := range s.meshes {
		names = append(names, z)
	}
	return names
}

// findPath is the shared request handler used by both the HTTP endpoint
// and the NATS subscriber — same request/response shape either way.
func findPath(store *zoneStore, req findPathRequest) (findPathResponse, error) {
	if req.Zone == "" {
		return findPathResponse{}, fmt.Errorf("zone is required")
	}
	mesh, ok := store.get(req.Zone)
	if !ok {
		return findPathResponse{}, fmt.Errorf("unknown zone %q", req.Zone)
	}

	maxPoints := req.MaxPoints
	if maxPoints <= 0 {
		maxPoints = 64
	}

	start := pathfinding.Vec3{X: req.Start.X, Y: req.Start.Y, Z: req.Start.Z}
	end := pathfinding.Vec3{X: req.End.X, Y: req.End.Y, Z: req.End.Z}

	waypoints, err := mesh.FindPath(start, end, maxPoints)
	if err != nil {
		return findPathResponse{}, err
	}

	resp := findPathResponse{Path: make([]Vec3Req, len(waypoints))}
	for i, wp := range waypoints {
		resp.Path[i] = Vec3Req{X: wp.X, Y: wp.Y, Z: wp.Z}
	}
	return resp, nil
}

func (srv *server) handleFindPath(w http.ResponseWriter, r *http.Request) {
	var req findPathRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	resp, err := findPath(srv.zones, req)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(errorResponse{Error: msg})
}
