package main

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ParseOBJ reads a (minimal) Wavefront OBJ mesh: "v x y z" vertex lines and
// "f i j k ..." face lines (1-indexed, optionally "i/t/n" — only the
// vertex-position index is used). Faces with more than 3 vertices are
// fan-triangulated from the first vertex. Everything else (vn, vt, o, g,
// usemtl, comments, blank lines) is ignored. No external dependency —
// real terrain meshes will be exported to this same minimal subset.
func ParseOBJ(r io.Reader) (verts []float32, indices []int32, err error) {
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		switch fields[0] {
		case "v":
			if len(fields) < 4 {
				return nil, nil, fmt.Errorf("obj:%d: vertex line needs 3 components: %q", lineNo, line)
			}
			for _, tok := range fields[1:4] {
				v, perr := strconv.ParseFloat(tok, 32)
				if perr != nil {
					return nil, nil, fmt.Errorf("obj:%d: bad vertex component %q: %w", lineNo, tok, perr)
				}
				verts = append(verts, float32(v))
			}
		case "f":
			if len(fields) < 4 {
				return nil, nil, fmt.Errorf("obj:%d: face line needs at least 3 vertices: %q", lineNo, line)
			}
			faceIdx := make([]int32, 0, len(fields)-1)
			for _, tok := range fields[1:] {
				vi, perr := parseFaceVertexIndex(tok)
				if perr != nil {
					return nil, nil, fmt.Errorf("obj:%d: bad face token %q: %w", lineNo, tok, perr)
				}
				faceIdx = append(faceIdx, vi)
			}
			// Fan-triangulate: (0,1,2), (0,2,3), (0,3,4), ...
			for i := 1; i < len(faceIdx)-1; i++ {
				indices = append(indices, faceIdx[0], faceIdx[i], faceIdx[i+1])
			}
		default:
			// vn, vt, o, g, usemtl, mtllib, s — not needed for navmesh baking.
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("obj: scan: %w", err)
	}
	return verts, indices, nil
}

// parseFaceVertexIndex parses an OBJ face token ("12", "12/3", "12/3/4",
// "12//4") and returns the 0-based vertex-position index.
func parseFaceVertexIndex(tok string) (int32, error) {
	posTok := tok
	if i := strings.IndexByte(tok, '/'); i >= 0 {
		posTok = tok[:i]
	}
	v, err := strconv.Atoi(posTok)
	if err != nil {
		return 0, err
	}
	if v == 0 {
		return 0, fmt.Errorf("vertex index must be non-zero")
	}
	if v < 0 {
		// OBJ allows negative (relative-to-end) indices; not needed by our
		// own exporter, but reject explicitly rather than silently
		// producing a wrong index.
		return 0, fmt.Errorf("negative (relative) face indices are not supported")
	}
	return int32(v - 1), nil // OBJ is 1-indexed
}
