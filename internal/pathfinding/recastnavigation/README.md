# Vendored recastnavigation (Recast + Detour)

Source: https://github.com/recastnavigation/recastnavigation
Commit: 9f4ce64458dfae86e1239c525ddc219c4e9e06f1 (main, fetched 2026-06-14)
License: zlib (see LICENSE.txt — upstream `License.txt`, unmodified)

Only `Recast/Include` and `Detour/Include` live under this directory.
The corresponding `Source/*.cpp` files have been moved one level up, into
`go-services/internal/pathfinding/` itself (alongside `shim.cpp`) — `go
build`'s cgo support only auto-compiles `.c`/`.cpp` files that sit directly
in the package directory, not subdirectories, so the implementation files
had to be flattened there to be picked up. They are otherwise byte-for-byte
unmodified; only their location changed. The `Include/` headers stay here
and are reached via `-I` flags in `detour.go`'s `#cgo CXXFLAGS`.

`DetourCrowd`, `DetourTileCache`, `RecastDemo`, `Tests`, and the various build
system files are intentionally excluded. This project needs:

- Recast — offline navmesh baking (`go-services/cmd/navmesh-bake`)
- Detour — runtime navmesh loading + pathfinding query (`pathfinding-api`)

No source files were modified, only relocated (see above). Compiled via
cgo's C++ toolchain together with `go-services/internal/pathfinding/shim.cpp`
— see that file and `detour.go` for the Go-facing API (Task 6.2, Phase 6 NPC AI).
