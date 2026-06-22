// Package pathfinding wraps the vendored Recast (offline navmesh bake) and
// Detour (runtime navmesh query) C++ libraries via cgo. Task 6.2, Phase 6
// NPC AI — see recastnavigation/README.md for vendoring details.
package pathfinding

/*
#cgo CXXFLAGS: -std=c++14 -I${SRCDIR}/recastnavigation/Recast/Include -I${SRCDIR}/recastnavigation/Detour/Include
#include <stdlib.h>
#include "shim.h"
*/
import "C"

import (
	"errors"
	"unsafe"
)

// BakeParams configures the Recast bake pipeline. Units match the source
// mesh (meters, for this project's zones).
type BakeParams struct {
	CellSize      float32
	CellHeight    float32
	AgentRadius   float32
	AgentHeight   float32
	AgentMaxClimb float32
	AgentMaxSlope float32 // degrees
}

// DefaultBakeParams are tuned for a humanoid-scale agent on a small (~160m)
// zone-sized tile, matching the established Ryzom zone size (see
// Task 3.3 zone streaming).
func DefaultBakeParams() BakeParams {
	return BakeParams{
		CellSize:      0.3,
		CellHeight:    0.2,
		AgentRadius:   0.4,
		AgentHeight:   2.0,
		AgentMaxClimb: 0.9,
		AgentMaxSlope: 45.0,
	}
}

func errBuf() (*C.char, C.int) {
	const n = 256
	buf := (*C.char)(C.malloc(n))
	return buf, C.int(n)
}

// BakeNavMesh runs the full Recast pipeline over an in-memory triangle mesh
// and returns the raw Detour navmesh tile data (suitable for LoadNavMesh).
// verts is flat [x0,y0,z0, x1,y1,z1, ...]; indices is flat triangle indices
// [a0,b0,c0, a1,b1,c1, ...] (0-based).
func BakeNavMesh(verts []float32, indices []int32, params BakeParams) ([]byte, error) {
	if len(verts) == 0 || len(verts)%3 != 0 {
		return nil, errors.New("pathfinding: verts must be a non-empty multiple of 3 floats")
	}
	if len(indices) == 0 || len(indices)%3 != 0 {
		return nil, errors.New("pathfinding: indices must be a non-empty multiple of 3 ints")
	}

	vertCount := C.int(len(verts) / 3)
	triCount := C.int(len(indices) / 3)

	cParams := C.pf_bake_params{
		cellSize:      C.float(params.CellSize),
		cellHeight:    C.float(params.CellHeight),
		agentRadius:   C.float(params.AgentRadius),
		agentHeight:   C.float(params.AgentHeight),
		agentMaxClimb: C.float(params.AgentMaxClimb),
		agentMaxSlope: C.float(params.AgentMaxSlope),
	}

	eBuf, eLen := errBuf()
	defer C.free(unsafe.Pointer(eBuf))

	var outData *C.uchar
	var outLen C.int

	ok := C.pf_bake_navmesh(
		(*C.float)(unsafe.Pointer(&verts[0])), vertCount,
		(*C.int)(unsafe.Pointer(&indices[0])), triCount,
		&cParams,
		&outData, &outLen,
		eBuf, eLen,
	)
	if ok == 0 {
		return nil, errors.New("pathfinding: bake failed: " + C.GoString(eBuf))
	}
	defer C.pf_free_buffer(outData)

	return C.GoBytes(unsafe.Pointer(outData), outLen), nil
}

// NavMesh is a loaded, query-ready Detour navmesh. Must be closed with
// Close when no longer needed.
type NavMesh struct {
	handle C.pf_navmesh_handle
}

// LoadNavMesh loads navmesh tile data previously produced by BakeNavMesh.
func LoadNavMesh(data []byte) (*NavMesh, error) {
	if len(data) == 0 {
		return nil, errors.New("pathfinding: empty navmesh data")
	}

	eBuf, eLen := errBuf()
	defer C.free(unsafe.Pointer(eBuf))

	h := C.pf_navmesh_load((*C.uchar)(unsafe.Pointer(&data[0])), C.int(len(data)), eBuf, eLen)
	if h == nil {
		return nil, errors.New("pathfinding: load failed: " + C.GoString(eBuf))
	}
	return &NavMesh{handle: h}, nil
}

// Close releases the underlying Detour navmesh/query. Safe to call once;
// further use of the NavMesh after Close is undefined.
func (n *NavMesh) Close() {
	if n.handle != nil {
		C.pf_navmesh_free(n.handle)
		n.handle = nil
	}
}

// Vec3 is a position in mesh space (x, y-up, z), matching Recast/Detour and
// NeL's CVector layout.
type Vec3 struct {
	X, Y, Z float32
}

// FindPath returns the straight-line waypoint path from start to end, or an
// error if no corridor exists (e.g. positions off the navmesh, or in
// disconnected regions). maxPoints bounds the returned waypoint count.
func (n *NavMesh) FindPath(start, end Vec3, maxPoints int) ([]Vec3, error) {
	if n.handle == nil {
		return nil, errors.New("pathfinding: navmesh is closed")
	}
	if maxPoints <= 0 {
		maxPoints = 64
	}

	cStart := [3]C.float{C.float(start.X), C.float(start.Y), C.float(start.Z)}
	cEnd := [3]C.float{C.float(end.X), C.float(end.Y), C.float(end.Z)}
	out := make([]C.float, maxPoints*3)

	eBuf, eLen := errBuf()
	defer C.free(unsafe.Pointer(eBuf))

	n2 := C.pf_navmesh_find_path(
		n.handle,
		&cStart[0], &cEnd[0],
		&out[0], C.int(maxPoints),
		eBuf, eLen,
	)
	if n2 == 0 {
		return nil, errors.New("pathfinding: " + C.GoString(eBuf))
	}

	path := make([]Vec3, int(n2))
	for i := range path {
		path[i] = Vec3{
			X: float32(out[i*3+0]),
			Y: float32(out[i*3+1]),
			Z: float32(out[i*3+2]),
		}
	}
	return path, nil
}
