# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

A voxel Minecraft-like game in Go, rendered with [raylib-go](https://github.com/gen2brain/raylib-go). ~1200 lines across four packages.

## Commands

```bash
go build .        # compile
go run .          # run the game
go vet ./...      # static analysis — the only checker configured
```

There are no tests, no linter config, and no CI. If you add tests, place them alongside the code (`world/world_test.go`).

### No native raylib setup required

`CGO_ENABLED=0` on this machine, so raylib-go builds via its **purego** path (`github.com/ebitengine/purego` + `github.com/jupiterrider/ffi`). On that path raylib-go embeds the native library (`libs/raylib-6.0_win64_msvc16.tar.gz`) and extracts it at runtime — you do **not** need to install raylib or place `raylib.dll` anywhere. `go build .` works from a clean checkout.

Only if you set `CGO_ENABLED=1` does the build switch to `cgo_windows.go` and require a system raylib + gcc toolchain. Prefer leaving CGo off.

## Architecture

```
main.go — game loop
  ├─ player.Update(w)              per-frame: mouse look, input, raycast, block place/break
  ├─ [accumulator @ 60Hz]
  │    └─ player.StepPhysics(w,dt) gravity + per-axis collision
  ├─ world.EnsureChunksAround(x,z) request chunk loads, unload distant
  ├─ world.ProcessGenerations()    drain finished chunks into the map
  └─ world.Render()                draw every loaded chunk's visible faces
```

| Package | Role |
|---------|------|
| `mc-go/` (main) | Window setup, game loop, HUD, panic→`crash.log` |
| `mc-go/blocks` | `BlockType` enum, `IsSolid`/`IsTransparent`, per-face vertex colors |
| `mc-go/world` | Chunk map, load/unload lifecycle, noise terrain gen, rendering |
| `mc-go/player` | FPS camera, physics, AABB collision, DDA raycast, block actions |

Concrete structs with pointer receivers throughout — no interfaces, no DI. `*World` and `*Player` are passed as plain arguments.

### Chunk pipeline

Chunks are 16×128×16, keyed `[2]int{cx, cz}`, stored as a flat `[16][128][16]BlockType` array. `loadDist = 1`, so a 3×3 = 9-chunk window follows the player.

The load path is asynchronous, and the split of work matters:

1. `LoadChunk` marks the key in `generating` and **snapshots the four neighbor `*Chunk` pointers**, then spawns `generateChunkAsync`.
2. In the goroutine: `generateTerrain` **and** `ComputeVisibility` both run. Visibility resolves cross-chunk neighbors only against that snapshot.
3. On the main thread, `ProcessGenerations` (non-blocking drain of `genResults`, buffered 16) only sets `loaded = true`, inserts into the map, and clears the `generating` entry.

Two consequences that are easy to get wrong:

- **`ComputeVisibility` is not a main-thread step.** Only map insertion is. Don't add map access to the goroutine side.
- **Chunk insertion does not rebuild neighbors.** A chunk that finishes after its neighbor already computed visibility can leave stale faces at the shared boundary. `UnloadChunk` *does* rebuild neighbors; the load path does not. Fixing seams means adding a rebuild in `ProcessGenerations`, not in the goroutine.

`FlushGenerations()` blocks until `generating` is empty — used once at startup so spawn height is computable.

Rendering: opaque blocks draw as whole cubes (`DrawCubeV` + wireframe), deduplicated per position via a `seen` map since one block contributes up to 6 visible faces. Transparent blocks (Water, Glass, Leaves) draw face-by-face with `DrawTriangle3D` to avoid z-fighting, with alpha forced to 140. Colors are hardcoded in `blocks.Color(face string)` — there is no texture atlas.

## Invariants and gotchas

### Face indices — five places must agree

| Index | Direction | `faceToName` |
|-------|-----------|--------------|
| 0 | +Y Top | `"top"` |
| 1 | -Y Bottom | `"bottom"` |
| 2 | +X Right | `""` |
| 3 | -X Left | `""` |
| 4 | +Z Front | `""` |
| 5 | -Z Back | `""` |

Used in `world/chunk.go` (`ComputeVisibility` directions array), `world/world.go` (`drawFace` switch, `faceToName`), and `player/player.go` (DDA face assignment, `handleBlockActions` placement offset). Change one, change all five.

### Negative-coordinate chunk math

Go truncates integer division toward zero, so world→chunk conversion needs an explicit floor:

```go
cx := wx / ChunkWidth
if wx < 0 {
    cx = (wx+1)/ChunkWidth - 1
}
```

Repeated in `GetBlock`, `SetBlock`, and `EnsureChunksAround`. Replacing it with plain `/ ChunkWidth` breaks everything west/north of the origin.

### Chunk-boundary rebuilds on edit

`SetBlock` recomputes the target chunk's visibility and, when the block sits on an edge (`lx==0`, `lx==15`, `lz==0`, `lz==15`), the adjacent chunk's too. Any new block-mutation path needs the same treatment.

### Fixed timestep

Input, camera, raycast, and block actions run once per frame in `Update`. Gravity and collision run at exactly 60Hz in `StepPhysics` inside the accumulator; frame time is clamped to 0.25s against the spiral of death. Do not move input into `StepPhysics` — `IsKeyPressed`/`IsMouseButtonPressed` are edge-triggered and would fire unpredictably when the accumulator runs 0 or 2+ times.

### Mouse delta spike guard

The frame after `DisableCursor()` reports a huge delta. Two defenses: `SkipNextMouseFrame()` zeroes the very next delta entirely (called at startup and on every Esc re-hide), and `Update` independently **zeroes** any axis whose delta exceeds ±50.

### Collision is per-axis

X, Y, and Z are advanced and tested separately in `moveWithCollision`. Y uses `collidesAtY`, which sweeps the full player footprint and picks its probe height from the sign of `Velocity.Y` (head when rising, feet when falling, below-feet when stationary — that last case is what keeps `OnGround` true). X and Z use `collidesAt`, a 9-point sample of the bounding box.

### Water exit grace period

Leaving water with upward velocity sets `waterExitTimer = 0.2`. While it runs, holding Space still gives swim thrust — without it the player cannot climb out of water. Don't drop the timer without an alternative.

### Terrain shape

Base height from 4-octave noise (persistence 0.5, lacunarity 2.0) at scale 0.008, mapped to roughly 8–90; a second 2-octave sample at scale 0.04 adds ±4 of detail. Layering top-down: surface Grass, or Sand at/below `SeaLevel+1` → 3 layers of Dirt → Stone → Bedrock at y=0. Water fills empty cells up to `SeaLevel` (48).

## Relationship to AGENTS.md

`AGENTS.md` covers the same ground and is largely accurate, but three of its claims were checked against the source and are wrong: it describes the build as requiring CGo and a manually-supplied `raylib.dll`; it places `ComputeVisibility` and a neighbor rebuild in `ProcessGenerations` on the main thread; and it states the mouse spike guard caps deltas at ±20 rather than zeroing them past ±50. Prefer this file where they disagree.
