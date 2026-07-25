# AGENTS.md — mc-go

A voxel-based Minecraft-like game written in Go using [raylib-go](https://github.com/gen2brain/raylib-go) for rendering and input.

## Essential Commands

```bash
go build .        # compile (requires raylib native lib via CGo)
go run .          # run the game
go vet ./...      # static analysis (no tests or lint config exists)
```

**CGo dependency**: raylib-go wraps the C raylib library. On Windows, `raylib.dll` must be findable (typically in the same directory as the binary or on `%PATH%`). On Linux/macOS, install raylib via system package manager or build from source. See the raylib-go docs for platform-specific setup.

## Architecture & Data Flow

```
main.go (game loop, fixed 60Hz physics)
  ├─ player.Update(w)             ← per-frame: input, camera, raycast, block actions
  ├─ [fixed-step loop]
  │    └─ player.StepPhysics(w, dt) ← 60Hz: gravity + collision
  ├─ world.EnsureChunksAround(x, z)  ← dynamic chunk load/unload requests
  ├─ world.ProcessGenerations()      ← finalize async chunk generations
  └─ world.Render()                  ← draw all loaded chunk meshes
       └─ drawFace()                 ← per-face triangle rendering for transparent blocks
```

### Packages

| Package   | Role |
|-----------|------|
| `mc-go/` (main) | Entry point, game loop, HUD rendering |
| `mc-go/blocks` | `BlockType` enum, solid/transparent queries, per-face colors |
| `mc-go/world` | World map, chunk lifecycle, terrain generation (noise), chunk rendering |
| `mc-go/player` | FPS camera, physics, AABB collision, DDA raycast, block place/break |

### Chunk System

- Chunks are **16×128×16** (`ChunkWidth`, `ChunkHeight`, `ChunkDepth`)
- World loads/unloads chunks dynamically: `loadDist = 1` → 3×3 grid (9 chunks) around the player
- Chunk key: `[2]int{cx, cz}` where `cx = worldX / 16` (with special handling for negative)
- Chunk lifecycle: `LoadChunk` (marks as generating) → goroutine `generateTerrain` → main thread `ProcessGenerations` (`ComputeVisibility` + insert + neighbor rebuild) → render; `UnloadChunk` clears and deletes
- Async generation: terrain runs in goroutines via `genResults` channel (buffered 16); visibility and map insertion happen on the main thread. `FlushGenerations()` blocks until all pending complete (used at startup).
- Visibility (`VisibleFace` slice) is recomputed when blocks change. Each visible face records: local coords, face index (0-5), and block type

### Terrain Generation

- Custom `openSimplexNoise` in `world/noise.go` (no external noise library)
- `OctaveNoise` with 4 octaves, persistence 0.5, lacunarity 2.0
- Height formula: `((noise+1)/2 * 82 + 8) + detail*4`
- Block layering: Bedrock at y=0 → Stone → Dirt (top 3 layers) → Grass/Sand surface; Water up to SeaLevel (48)

### Rendering

- Opaque blocks: `DrawCubeV` + `DrawCubeWires` (one cube per block, deduplicated via `seen` map)
- Transparent blocks (Water, Glass, Leaves): per-face `DrawTriangle3D` to avoid z-fighting
- Colors are hardcoded in `blocks.Color(face string)` — there is no texture atlas

## Critical Gotchas

### Negative Coordinate Chunk Math

Converting negative world coords to chunk coords uses a special floor-division pattern used consistently in `GetBlock`, `SetBlock`, and `EnsureChunksAround`:

```go
cx := wx / ChunkWidth
if wx < 0 {
    cx = (wx+1)/ChunkWidth - 1
}
```

This is necessary because Go's integer division truncates toward zero, not toward -∞. Do **not** replace this with simple `/ ChunkWidth` — it will break for negative coordinates.

### Face Index Convention (MUST STAY CONSISTENT)

Face indices are used in **five** places and must all agree:

| Index | Axis | Direction | `faceToName` |
|-------|------|-----------|--------------|
| 0 | +Y | Top | `"top"` |
| 1 | -Y | Bottom | `"bottom"` |
| 2 | +X | Right | `""` (side) |
| 3 | -X | Left | `""` (side) |
| 4 | +Z | Front | `""` (side) |
| 5 | -Z | Back | `""` (side) |

These appear in:
- `world/chunk.go` — `ComputeVisibility` directions array
- `world/world.go` — `drawFace` switch cases
- `world/world.go` — `faceToName` mapping
- `player/player.go` — DDA raycast face assignment
- `player/player.go` — `handleBlockActions` placement offset

If you change the indexing, you must update all five locations.

### Mouse Delta Spike Guard

The first frame after `DisableCursor()` returns huge mouse delta values. Two defenses: (1) `SkipNextMouseFrame()` is called right after `DisableCursor()`, zeroing the next delta entirely; (2) spike guard caps remaining deltas at ±20 (0.1 rad ~6°).

### Fixed Timestep Physics

Physics runs at 60Hz via an accumulator loop (`PhysicsDt = 1.0/60.0`). Input/camera/raycast run per-frame in `Update(w)`, gravity/collision run in `StepPhysics(w, dt)` inside the accumulator. Frame time is clamped to 0.25s to prevent spiral-of-death. Do not re-merge input and physics — `IsKeyPressed`/`IsMouseButtonPressed` are edge-triggered and would break if called multiple times per frame.

### Chunk Boundary Rebuild

`SetBlock` rebuilds visibility for the target chunk AND any adjacent chunks if the block is on a chunk edge (lx==0, lx==15, lz==0, lz==15). If you modify block placement/breaking, remember to rebuild neighbors.

### Player Collision is Per-Axis

X, Y, Z movement is applied and collision-checked **separately** in `moveWithCollision`. The Y-axis uses `collidesAtY` which checks across the full player width (not just center line). The X/Z axes use a 9-point corner sample of the player bounding box.

### Water Exit Grace Period

When the player leaves water with upward velocity, `waterExitTimer` is set to 0.2s. During this period, holding Space provides swimming thrust — enabling the player to jump out of water. Do not remove this timer without providing an alternative water exit mechanism.

### No Tests

There are no test files in this project. If you add tests, place them alongside the code they test (e.g., `world/world_test.go`).

## Code Conventions

- Constants for all tunable values defined at the top of each file
- No interfaces — concrete structs with pointer receiver methods
- `*World` and `*Player` are passed directly as arguments (no dependency injection container)
- World coordinates: `float32` for rendering/physics, `int` for block indices
- Block storage: 3D array `[16][128][16]BlockType` per chunk
- Error handling: nil checks return zero values (`blocks.Air`) or early return; main.go uses `recover()` with crash.log for panics
