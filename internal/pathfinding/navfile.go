package pathfinding

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// fileMagic identifies our on-disk navmesh container: a small header plus
// JSON metadata in front of the raw Detour tile data blob from
// BakeNavMesh. This is our own format, not Recast/Detour's — RecastDemo's
// NavMeshSetHeader is for multi-tile sets, which this single-tile slice
// doesn't need.
const fileMagic = "RZNM"
const fileVersion = uint32(1)

// Metadata describes how a .navmesh file was produced, for diagnostics and
// for pathfinding-api's directory listing.
type Metadata struct {
	Zone    string    `json:"zone"`
	BakedAt time.Time `json:"baked_at"`
	Params  BakeParams `json:"params"`
}

// SaveFile writes a baked navmesh (as returned by BakeNavMesh) plus
// metadata to path.
func SaveFile(path string, navData []byte, meta Metadata) error {
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("pathfinding: marshal metadata: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("pathfinding: create %s: %w", path, err)
	}
	defer f.Close()

	if _, err := f.Write([]byte(fileMagic)); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, fileVersion); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(len(metaJSON))); err != nil {
		return err
	}
	if _, err := f.Write(metaJSON); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(len(navData))); err != nil {
		return err
	}
	if _, err := f.Write(navData); err != nil {
		return err
	}
	return nil
}

// LoadFile reads a .navmesh file written by SaveFile, returning the raw
// navmesh tile data (pass to LoadNavMesh) and its metadata.
func LoadFile(path string) ([]byte, Metadata, error) {
	var meta Metadata

	f, err := os.Open(path)
	if err != nil {
		return nil, meta, fmt.Errorf("pathfinding: open %s: %w", path, err)
	}
	defer f.Close()

	magic := make([]byte, 4)
	if _, err := io.ReadFull(f, magic); err != nil {
		return nil, meta, fmt.Errorf("pathfinding: read magic: %w", err)
	}
	if string(magic) != fileMagic {
		return nil, meta, fmt.Errorf("pathfinding: %s is not a RZNM navmesh file", path)
	}

	var version uint32
	if err := binary.Read(f, binary.LittleEndian, &version); err != nil {
		return nil, meta, fmt.Errorf("pathfinding: read version: %w", err)
	}
	if version != fileVersion {
		return nil, meta, fmt.Errorf("pathfinding: unsupported navmesh file version %d", version)
	}

	var metaLen uint32
	if err := binary.Read(f, binary.LittleEndian, &metaLen); err != nil {
		return nil, meta, fmt.Errorf("pathfinding: read metadata length: %w", err)
	}
	metaJSON := make([]byte, metaLen)
	if _, err := io.ReadFull(f, metaJSON); err != nil {
		return nil, meta, fmt.Errorf("pathfinding: read metadata: %w", err)
	}
	if err := json.Unmarshal(metaJSON, &meta); err != nil {
		return nil, meta, fmt.Errorf("pathfinding: unmarshal metadata: %w", err)
	}

	var dataLen uint32
	if err := binary.Read(f, binary.LittleEndian, &dataLen); err != nil {
		return nil, meta, fmt.Errorf("pathfinding: read data length: %w", err)
	}
	navData := make([]byte, dataLen)
	if _, err := io.ReadFull(f, navData); err != nil {
		return nil, meta, fmt.Errorf("pathfinding: read navmesh data: %w", err)
	}

	return navData, meta, nil
}
