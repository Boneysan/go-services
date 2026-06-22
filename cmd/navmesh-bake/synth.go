package main

import (
	"fmt"
	"io"
)

// TutorialZoneSize is the synthetic zone's footprint in meters, matching
// the established Ryzom zone size (see Task 3.3 zone streaming).
const TutorialZoneSize = 160.0

// meshBuilder accumulates verts/triangles for the synthetic generator. Real
// terrain (once Atys .zone data is available — currently a documented
// blocker, see PROGRESS.md Task 6.2) would feed BakeNavMesh the same way,
// just from a real exporter instead of this placeholder.
type meshBuilder struct {
	verts   []float32
	indices []int32
}

func (m *meshBuilder) addVert(x, y, z float32) int32 {
	idx := int32(len(m.verts) / 3)
	m.verts = append(m.verts, x, y, z)
	return idx
}

// addQuad adds two triangles for the quad p0->p1->p2->p3 (in order around
// the perimeter), wound so the face normal points toward `up`-ish
// (+Y for a floor, but works for any quad whose vertices are listed
// counter-clockwise when viewed from the desired front face).
func (m *meshBuilder) addQuad(p0, p1, p2, p3 [3]float32) {
	i0 := m.addVert(p0[0], p0[1], p0[2])
	i1 := m.addVert(p1[0], p1[1], p1[2])
	i2 := m.addVert(p2[0], p2[1], p2[2])
	i3 := m.addVert(p3[0], p3[1], p3[2])
	m.indices = append(m.indices, i0, i2, i1, i0, i3, i2)
}

// addBoxObstacle adds a solid axis-aligned box from (minX,0,minZ) to
// (maxX,height,maxZ) sitting on the ground. Tall/solid enough (relative to
// DefaultBakeParams' agent height/climb) that Recast treats the footprint
// as unwalkable and routes agents around it rather than over it.
func (m *meshBuilder) addBoxObstacle(minX, minZ, maxX, maxZ, height float32) {
	// Top.
	m.addQuad(
		[3]float32{minX, height, minZ},
		[3]float32{maxX, height, minZ},
		[3]float32{maxX, height, maxZ},
		[3]float32{minX, height, maxZ},
	)
	// Four sides (winding doesn't matter for walkability — near-vertical
	// faces are excluded by the slope test regardless — but consistent
	// winding keeps the mesh sane for any future renderer/debug tool).
	m.addQuad([3]float32{minX, 0, minZ}, [3]float32{minX, height, minZ}, [3]float32{maxX, height, minZ}, [3]float32{maxX, 0, minZ})
	m.addQuad([3]float32{maxX, 0, minZ}, [3]float32{maxX, height, minZ}, [3]float32{maxX, height, maxZ}, [3]float32{maxX, 0, maxZ})
	m.addQuad([3]float32{maxX, 0, maxZ}, [3]float32{maxX, height, maxZ}, [3]float32{minX, height, maxZ}, [3]float32{minX, 0, maxZ})
	m.addQuad([3]float32{minX, 0, maxZ}, [3]float32{minX, height, maxZ}, [3]float32{minX, height, minZ}, [3]float32{minX, 0, minZ})
}

// GenerateTutorialZone builds a synthetic placeholder zone: a flat
// TutorialZoneSize x TutorialZoneSize ground plane with three box
// obstacles straddling the diagonal between the zone's corners, so a
// straight-line path is not a valid solution and FindPath must actually
// route around them.
//
// This stands in for real Atys terrain, which is not available in this
// checkout (.zone files are 0-byte placeholders — the same documented
// blocker as Tasks 2.2/3.3). See PROGRESS.md Task 6.2.
func GenerateTutorialZone() (verts []float32, indices []int32) {
	m := &meshBuilder{}

	half := float32(TutorialZoneSize / 2)
	m.addQuad(
		[3]float32{-half, 0, -half},
		[3]float32{half, 0, -half},
		[3]float32{half, 0, half},
		[3]float32{-half, 0, half},
	)

	const obstacleHeight = 3.0 // taller than DefaultBakeParams' agent height + climb
	m.addBoxObstacle(-50, -10, -38, 2, obstacleHeight)
	m.addBoxObstacle(-10, -25, 2, -13, obstacleHeight)
	m.addBoxObstacle(15, 10, 27, 22, obstacleHeight)

	return m.verts, m.indices
}

// WriteOBJ writes verts/indices (the same flat layout BakeNavMesh takes)
// as a minimal Wavefront OBJ, readable by ParseOBJ.
func WriteOBJ(w io.Writer, verts []float32, indices []int32) error {
	for i := 0; i < len(verts); i += 3 {
		if _, err := fmt.Fprintf(w, "v %g %g %g\n", verts[i], verts[i+1], verts[i+2]); err != nil {
			return err
		}
	}
	for i := 0; i < len(indices); i += 3 {
		// OBJ is 1-indexed.
		if _, err := fmt.Fprintf(w, "f %d %d %d\n", indices[i]+1, indices[i+1]+1, indices[i+2]+1); err != nil {
			return err
		}
	}
	return nil
}
