// Task 6.2 (Phase 6 NPC AI) — C API wrapping vendored Recast (offline bake)
// and Detour (runtime query) for cgo. See detour.go for the Go-facing API.
#ifndef RYZOM_PATHFINDING_SHIM_H
#define RYZOM_PATHFINDING_SHIM_H

#ifdef __cplusplus
extern "C" {
#endif

typedef struct {
	float cellSize;
	float cellHeight;
	float agentRadius;
	float agentHeight;
	float agentMaxClimb;
	float agentMaxSlope; // degrees
} pf_bake_params;

// Bakes a single-tile Detour navmesh from an in-memory triangle mesh.
// verts: flat [x0,y0,z0,x1,y1,z1,...], vertCount = number of verts (not floats).
// indices: flat [a0,b0,c0, a1,b1,c1, ...], triCount = number of triangles.
// On success returns 1 and sets *outData/*outLen to a buffer the caller must
// free with pf_free_buffer; on failure returns 0 and *outData is NULL.
int pf_bake_navmesh(const float* verts, int vertCount,
                     const int* indices, int triCount,
                     const pf_bake_params* params,
                     unsigned char** outData, int* outLen,
                     char* errBuf, int errBufLen);

void pf_free_buffer(unsigned char* data);

// Opaque handle over a loaded dtNavMesh + dtNavMeshQuery pair.
typedef void* pf_navmesh_handle;

// Loads a navmesh produced by pf_bake_navmesh (the raw Detour tile data
// blob — NOT a file with our own header; the Go side strips that first).
// Returns NULL on failure.
pf_navmesh_handle pf_navmesh_load(const unsigned char* data, int len, char* errBuf, int errBufLen);

void pf_navmesh_free(pf_navmesh_handle h);

// Finds a path from start to end. outPath must hold at least maxPoints*3
// floats. Returns the number of waypoints written (0 on failure / no path).
int pf_navmesh_find_path(pf_navmesh_handle h,
                          const float* start, const float* end,
                          float* outPath, int maxPoints,
                          char* errBuf, int errBufLen);

#ifdef __cplusplus
}
#endif

#endif // RYZOM_PATHFINDING_SHIM_H
