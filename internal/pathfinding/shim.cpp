// Task 6.2 (Phase 6 NPC AI) — Recast bake + Detour query, wrapped for cgo.
// Bake pipeline mirrors recastnavigation's RecastDemo Sample_SoloMesh::build()
// (single tile, watershed partitioning, BVTree enabled) — see
// recastnavigation/README.md for the vendored source/license.
#include "shim.h"

#include <cmath>
#include <cstdio>
#include <cstring>
#include <vector>

#include "recastnavigation/Recast/Include/Recast.h"
#include "recastnavigation/Detour/Include/DetourNavMesh.h"
#include "recastnavigation/Detour/Include/DetourNavMeshBuilder.h"
#include "recastnavigation/Detour/Include/DetourNavMeshQuery.h"
#include "recastnavigation/Detour/Include/DetourCommon.h"

namespace {

void setErr(char* buf, int len, const char* msg)
{
	if (!buf || len <= 0) return;
	std::snprintf(buf, (size_t)len, "%s", msg);
}

struct PfNavMesh
{
	dtNavMesh* mesh = nullptr;
	dtNavMeshQuery* query = nullptr;
	dtQueryFilter filter;

	~PfNavMesh()
	{
		if (query) dtFreeNavMeshQuery(query);
		if (mesh) dtFreeNavMesh(mesh);
	}
};

} // namespace

extern "C" int pf_bake_navmesh(const float* verts, int vertCount,
                                const int* indices, int triCount,
                                const pf_bake_params* params,
                                unsigned char** outData, int* outLen,
                                char* errBuf, int errBufLen)
{
	*outData = nullptr;
	*outLen = 0;

	if (!verts || vertCount <= 0 || !indices || triCount <= 0 || !params)
	{
		setErr(errBuf, errBufLen, "pf_bake_navmesh: empty input mesh or params");
		return 0;
	}

	rcContext ctx;

	float bmin[3], bmax[3];
	rcCalcBounds(verts, vertCount, bmin, bmax);

	rcConfig cfg;
	std::memset(&cfg, 0, sizeof(cfg));
	cfg.cs = params->cellSize;
	cfg.ch = params->cellHeight;
	cfg.walkableSlopeAngle = params->agentMaxSlope;
	cfg.walkableHeight = (int)std::ceil(params->agentHeight / cfg.ch);
	cfg.walkableClimb = (int)std::floor(params->agentMaxClimb / cfg.ch);
	cfg.walkableRadius = (int)std::ceil(params->agentRadius / cfg.cs);
	cfg.maxEdgeLen = (int)(12.0f / cfg.cs);
	cfg.maxSimplificationError = 1.3f;
	cfg.minRegionArea = (int)rcSqr(8.0f);
	cfg.mergeRegionArea = (int)rcSqr(20.0f);
	cfg.maxVertsPerPoly = 6;
	cfg.detailSampleDist = cfg.cs * 6.0f;
	cfg.detailSampleMaxError = cfg.ch * 1.0f;
	rcVcopy(cfg.bmin, bmin);
	rcVcopy(cfg.bmax, bmax);
	rcCalcGridSize(cfg.bmin, cfg.bmax, cfg.cs, &cfg.width, &cfg.height);

	rcHeightfield* solid = rcAllocHeightfield();
	if (!solid || !rcCreateHeightfield(&ctx, *solid, cfg.width, cfg.height, cfg.bmin, cfg.bmax, cfg.cs, cfg.ch))
	{
		rcFreeHeightField(solid);
		setErr(errBuf, errBufLen, "rcCreateHeightfield failed");
		return 0;
	}

	std::vector<unsigned char> triAreas((size_t)triCount, 0);
	rcMarkWalkableTriangles(&ctx, cfg.walkableSlopeAngle, verts, vertCount, indices, triCount, triAreas.data());
	if (!rcRasterizeTriangles(&ctx, verts, vertCount, indices, triAreas.data(), triCount, *solid, cfg.walkableClimb))
	{
		rcFreeHeightField(solid);
		setErr(errBuf, errBufLen, "rcRasterizeTriangles failed");
		return 0;
	}

	rcFilterLowHangingWalkableObstacles(&ctx, cfg.walkableClimb, *solid);
	rcFilterLedgeSpans(&ctx, cfg.walkableHeight, cfg.walkableClimb, *solid);
	rcFilterWalkableLowHeightSpans(&ctx, cfg.walkableHeight, *solid);

	rcCompactHeightfield* chf = rcAllocCompactHeightfield();
	if (!chf || !rcBuildCompactHeightfield(&ctx, cfg.walkableHeight, cfg.walkableClimb, *solid, *chf))
	{
		rcFreeHeightField(solid);
		rcFreeCompactHeightfield(chf);
		setErr(errBuf, errBufLen, "rcBuildCompactHeightfield failed");
		return 0;
	}
	rcFreeHeightField(solid);

	if (!rcErodeWalkableArea(&ctx, cfg.walkableRadius, *chf))
	{
		rcFreeCompactHeightfield(chf);
		setErr(errBuf, errBufLen, "rcErodeWalkableArea failed");
		return 0;
	}

	if (!rcBuildDistanceField(&ctx, *chf) ||
	    !rcBuildRegions(&ctx, *chf, 0, cfg.minRegionArea, cfg.mergeRegionArea))
	{
		rcFreeCompactHeightfield(chf);
		setErr(errBuf, errBufLen, "rcBuildRegions (watershed) failed");
		return 0;
	}

	rcContourSet* cset = rcAllocContourSet();
	if (!cset || !rcBuildContours(&ctx, *chf, cfg.maxSimplificationError, cfg.maxEdgeLen, *cset))
	{
		rcFreeCompactHeightfield(chf);
		rcFreeContourSet(cset);
		setErr(errBuf, errBufLen, "rcBuildContours failed");
		return 0;
	}

	rcPolyMesh* pmesh = rcAllocPolyMesh();
	if (!pmesh || !rcBuildPolyMesh(&ctx, *cset, cfg.maxVertsPerPoly, *pmesh))
	{
		rcFreeCompactHeightfield(chf);
		rcFreeContourSet(cset);
		rcFreePolyMesh(pmesh);
		setErr(errBuf, errBufLen, "rcBuildPolyMesh failed");
		return 0;
	}

	rcPolyMeshDetail* dmesh = rcAllocPolyMeshDetail();
	if (!dmesh || !rcBuildPolyMeshDetail(&ctx, *pmesh, *chf, cfg.detailSampleDist, cfg.detailSampleMaxError, *dmesh))
	{
		rcFreeCompactHeightfield(chf);
		rcFreeContourSet(cset);
		rcFreePolyMesh(pmesh);
		rcFreePolyMeshDetail(dmesh);
		setErr(errBuf, errBufLen, "rcBuildPolyMeshDetail failed");
		return 0;
	}

	rcFreeCompactHeightfield(chf);
	rcFreeContourSet(cset);

	if (cfg.maxVertsPerPoly > DT_VERTS_PER_POLYGON)
	{
		rcFreePolyMesh(pmesh);
		rcFreePolyMeshDetail(dmesh);
		setErr(errBuf, errBufLen, "maxVertsPerPoly exceeds DT_VERTS_PER_POLYGON");
		return 0;
	}

	// All walkable polys get area=ground(1)/flags=walk(1) — this slice has
	// one traversable surface type; richer area/flag classification is a
	// follow-on once real terrain data (water, roads, doors) is available.
	for (int i = 0; i < pmesh->npolys; ++i)
	{
		if (pmesh->areas[i] == RC_WALKABLE_AREA)
		{
			pmesh->areas[i] = 1;
			pmesh->flags[i] = 1;
		}
	}

	dtNavMeshCreateParams dtParams;
	std::memset(&dtParams, 0, sizeof(dtParams));
	dtParams.verts = pmesh->verts;
	dtParams.vertCount = pmesh->nverts;
	dtParams.polys = pmesh->polys;
	dtParams.polyAreas = pmesh->areas;
	dtParams.polyFlags = pmesh->flags;
	dtParams.polyCount = pmesh->npolys;
	dtParams.nvp = pmesh->nvp;
	dtParams.detailMeshes = dmesh->meshes;
	dtParams.detailVerts = dmesh->verts;
	dtParams.detailVertsCount = dmesh->nverts;
	dtParams.detailTris = dmesh->tris;
	dtParams.detailTriCount = dmesh->ntris;
	dtParams.walkableHeight = params->agentHeight;
	dtParams.walkableRadius = params->agentRadius;
	dtParams.walkableClimb = params->agentMaxClimb;
	rcVcopy(dtParams.bmin, pmesh->bmin);
	rcVcopy(dtParams.bmax, pmesh->bmax);
	dtParams.cs = cfg.cs;
	dtParams.ch = cfg.ch;
	dtParams.buildBvTree = true;

	unsigned char* navData = nullptr;
	int navDataSize = 0;
	bool ok = dtCreateNavMeshData(&dtParams, &navData, &navDataSize);

	rcFreePolyMesh(pmesh);
	rcFreePolyMeshDetail(dmesh);

	if (!ok)
	{
		setErr(errBuf, errBufLen, "dtCreateNavMeshData failed");
		return 0;
	}

	*outData = navData;
	*outLen = navDataSize;
	return 1;
}

extern "C" void pf_free_buffer(unsigned char* data)
{
	dtFree(data);
}

extern "C" pf_navmesh_handle pf_navmesh_load(const unsigned char* data, int len, char* errBuf, int errBufLen)
{
	if (!data || len <= 0)
	{
		setErr(errBuf, errBufLen, "pf_navmesh_load: empty buffer");
		return nullptr;
	}

	// dtNavMesh::init(data, ...) takes ownership of the buffer when
	// DT_TILE_FREE_DATA is set, and may need to mutate it in place — give
	// it a copy it owns rather than the caller's cgo-managed slice.
	unsigned char* owned = (unsigned char*)dtAlloc((size_t)len, DT_ALLOC_PERM);
	if (!owned)
	{
		setErr(errBuf, errBufLen, "pf_navmesh_load: out of memory");
		return nullptr;
	}
	std::memcpy(owned, data, (size_t)len);

	dtNavMesh* mesh = dtAllocNavMesh();
	if (!mesh)
	{
		dtFree(owned);
		setErr(errBuf, errBufLen, "dtAllocNavMesh failed");
		return nullptr;
	}

	dtStatus status = mesh->init(owned, len, DT_TILE_FREE_DATA);
	if (dtStatusFailed(status))
	{
		dtFree(owned);
		dtFreeNavMesh(mesh);
		setErr(errBuf, errBufLen, "dtNavMesh::init failed (corrupt or incompatible navmesh data)");
		return nullptr;
	}

	dtNavMeshQuery* query = dtAllocNavMeshQuery();
	if (!query || dtStatusFailed(query->init(mesh, 2048)))
	{
		dtFreeNavMeshQuery(query);
		dtFreeNavMesh(mesh);
		setErr(errBuf, errBufLen, "dtNavMeshQuery::init failed");
		return nullptr;
	}

	PfNavMesh* h = new PfNavMesh();
	h->mesh = mesh;
	h->query = query;
	h->filter.setIncludeFlags(0xffff);
	h->filter.setExcludeFlags(0);
	return (pf_navmesh_handle)h;
}

extern "C" void pf_navmesh_free(pf_navmesh_handle h)
{
	delete (PfNavMesh*)h;
}

extern "C" int pf_navmesh_find_path(pf_navmesh_handle handle,
                                     const float* start, const float* end,
                                     float* outPath, int maxPoints,
                                     char* errBuf, int errBufLen)
{
	PfNavMesh* h = (PfNavMesh*)handle;
	if (!h || !h->query || !start || !end || !outPath || maxPoints <= 0)
	{
		setErr(errBuf, errBufLen, "pf_navmesh_find_path: invalid arguments");
		return 0;
	}

	const float halfExtents[3] = { 2.0f, 4.0f, 2.0f };
	dtPolyRef startRef = 0, endRef = 0;
	float startNearest[3], endNearest[3];

	dtStatus status = h->query->findNearestPoly(start, halfExtents, &h->filter, &startRef, startNearest);
	if (dtStatusFailed(status) || startRef == 0)
	{
		setErr(errBuf, errBufLen, "no navmesh polygon near start position");
		return 0;
	}
	status = h->query->findNearestPoly(end, halfExtents, &h->filter, &endRef, endNearest);
	if (dtStatusFailed(status) || endRef == 0)
	{
		setErr(errBuf, errBufLen, "no navmesh polygon near end position");
		return 0;
	}

	static const int kMaxPolys = 256;
	dtPolyRef polyPath[kMaxPolys];
	int polyPathCount = 0;
	status = h->query->findPath(startRef, endRef, startNearest, endNearest, &h->filter, polyPath, &polyPathCount, kMaxPolys);
	if (dtStatusFailed(status) || polyPathCount == 0)
	{
		setErr(errBuf, errBufLen, "findPath failed (no corridor between start and end)");
		return 0;
	}

	int straightCount = 0;
	status = h->query->findStraightPath(startNearest, endNearest, polyPath, polyPathCount,
	                                     outPath, nullptr, nullptr, &straightCount, maxPoints, 0);
	if (dtStatusFailed(status))
	{
		setErr(errBuf, errBufLen, "findStraightPath failed");
		return 0;
	}

	return straightCount;
}
