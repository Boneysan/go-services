// navmesh-bake bakes a Detour navmesh (Task 6.2, Phase 6 NPC AI) from
// either a synthetic placeholder zone (real Atys .zone terrain is not
// available in this checkout — see PROGRESS.md Task 6.2) or a Wavefront
// OBJ trimesh, and writes it as a .navmesh file pathfinding-api can load.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/boneysan/ryzom/go-services/internal/pathfinding"
)

func main() {
	var (
		synth   = flag.Bool("synth", false, "bake the built-in synthetic tutorial zone instead of -in")
		inPath  = flag.String("in", "", "input Wavefront OBJ mesh path")
		outPath = flag.String("out", "", "output .navmesh path (required)")
		objOut  = flag.String("write-obj", "", "with -synth, also write the generated mesh to this OBJ path")
		zone    = flag.String("zone", "", "zone name stored in the navmesh metadata (default: derived from input)")

		cellSize      = flag.Float64("cell-size", float64(pathfinding.DefaultBakeParams().CellSize), "Recast voxel cell size (meters)")
		cellHeight    = flag.Float64("cell-height", float64(pathfinding.DefaultBakeParams().CellHeight), "Recast voxel cell height (meters)")
		agentRadius   = flag.Float64("agent-radius", float64(pathfinding.DefaultBakeParams().AgentRadius), "agent radius (meters)")
		agentHeight   = flag.Float64("agent-height", float64(pathfinding.DefaultBakeParams().AgentHeight), "agent height (meters)")
		agentMaxClimb = flag.Float64("agent-max-climb", float64(pathfinding.DefaultBakeParams().AgentMaxClimb), "agent max climb (meters)")
		agentMaxSlope = flag.Float64("agent-max-slope", float64(pathfinding.DefaultBakeParams().AgentMaxSlope), "agent max walkable slope (degrees)")
	)
	flag.Parse()

	if *outPath == "" {
		fmt.Fprintln(os.Stderr, "navmesh-bake: -out is required")
		flag.Usage()
		os.Exit(2)
	}
	if *synth == (*inPath != "") {
		fmt.Fprintln(os.Stderr, "navmesh-bake: exactly one of -synth or -in must be given")
		os.Exit(2)
	}

	var (
		verts    []float32
		indices  []int32
		zoneName string
	)

	if *synth {
		verts, indices = GenerateTutorialZone()
		zoneName = "synthetic_tutorial"
		if *objOut != "" {
			f, err := os.Create(*objOut)
			if err != nil {
				log.Fatalf("navmesh-bake: create %s: %v", *objOut, err)
			}
			if err := WriteOBJ(f, verts, indices); err != nil {
				f.Close()
				log.Fatalf("navmesh-bake: write %s: %v", *objOut, err)
			}
			if err := f.Close(); err != nil {
				log.Fatalf("navmesh-bake: write %s: %v", *objOut, err)
			}
			fmt.Printf("wrote source mesh: %s\n", *objOut)
		}
	} else {
		f, err := os.Open(*inPath)
		if err != nil {
			log.Fatalf("navmesh-bake: open %s: %v", *inPath, err)
		}
		verts, indices, err = ParseOBJ(f)
		f.Close()
		if err != nil {
			log.Fatalf("navmesh-bake: parse %s: %v", *inPath, err)
		}
		zoneName = *inPath
	}

	if *zone != "" {
		zoneName = *zone
	}

	params := pathfinding.BakeParams{
		CellSize:      float32(*cellSize),
		CellHeight:    float32(*cellHeight),
		AgentRadius:   float32(*agentRadius),
		AgentHeight:   float32(*agentHeight),
		AgentMaxClimb: float32(*agentMaxClimb),
		AgentMaxSlope: float32(*agentMaxSlope),
	}

	fmt.Printf("baking %s: %d verts, %d triangles\n", zoneName, len(verts)/3, len(indices)/3)

	navData, err := pathfinding.BakeNavMesh(verts, indices, params)
	if err != nil {
		log.Fatalf("navmesh-bake: bake failed: %v", err)
	}

	meta := pathfinding.Metadata{
		Zone:    zoneName,
		BakedAt: time.Now().UTC(),
		Params:  params,
	}
	if err := pathfinding.SaveFile(*outPath, navData, meta); err != nil {
		log.Fatalf("navmesh-bake: save %s: %v", *outPath, err)
	}

	fmt.Printf("wrote %s: %d bytes of navmesh tile data\n", *outPath, len(navData))
}
