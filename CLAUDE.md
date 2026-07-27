# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

A voxel Minecraft-like game in Go, rendered with [raylib-go](https://github.com/gen2brain/raylib-go). ~1300 lines across four packages.

## Commands

```bash
go build .        # compile
go run .          # run the game
go vet ./...      # static analysis
go test ./...     # world and player packages have tests; blocks and main do not
```

Tests run headless — they never call `InitWindow`, so the raylib import is harmless in CI.

### No native raylib setup required

`CGO_ENABLED=0`, so raylib-go builds via its **purego** path (`github.com/ebitengine/purego` + `github.com/jupiterrider/ffi`). On that path raylib-go embeds the native library (`libs/raylib-6.0_win64_msvc16.tar.gz`) and extracts it at runtime — you do **not** need to install raylib or place `raylib.dll` anywhere. `go build .` works from a clean checkout.

Two consequences worth knowing:

- **`-race` is unavailable.** The race detector requires cgo. Enabling `CGO_ENABLED=1` switches raylib-go to `cgo_windows.go`, which needs a system raylib plus a gcc that Go can find on the Windows `%PATH%` (a gcc visible only to Git Bash is not enough). Correctness around the generation goroutine has to be argued structurally instead — see below.
- **Every rlgl call is an FFI crossing**, and in the render loop those crossings dominate. This inverts the usual raylib advice; see the rendering section.

## Architecture

```
main.go — game loop
  ├─ player.Update(w)              per-frame: mouse look, input, raycast, block place/break
  ├─ [accumulator @ 60Hz]
  │    └─ player.StepPhysics(w,dt) gravity + per-axis collision
  ├─ world.EnsureChunksAround(x,z) request chunk loads, unload distant
  ├─ world.ProcessGenerations()    drain finished chunks into the map
  ├─ world.ProcessDirty(n)         rebuild visibility for up to n chunks
  └─ world.Render(cam)             opaque pass, then transparent back-to-front
```

| Package | Role |
|---------|------|
| `mc-go/` (main) | Window setup, game loop, HUD, panic→`crash.log` |
| `mc-go/blocks` | `BlockType` enum, `IsSolid`/`IsTransparent`, per-face colors |
| `mc-go/world` | Chunk map, load/unload lifecycle, noise terrain gen, rendering |
| `mc-go/player` | FPS camera, physics, AABB collision, DDA raycast, block actions |

Concrete structs with pointer receivers throughout — no interfaces, no DI. `*World` and `*Player` are passed as plain arguments.

### Chunk pipeline

Chunks are 16×128×16, keyed `[2]int{cx, cz}`, stored as a flat `[16][128][16]BlockType` array. `loadDist = 2`, so a 5×5 = 25-chunk window follows the player.

The split of work across threads is the thing to get right:

1. `LoadChunk` marks the key in `generating` and spawns `generateChunkAsync`.
2. The goroutine runs **`generateTerrain` only**, then sends the chunk on `genResults` (buffered 32, above the 25-chunk window so a burst never parks a worker on the send).
3. `insertChunk` on the main thread sets `loaded`, inserts into the map, and calls `markDirty` on the new chunk **and its four neighbours**.
4. `ProcessDirty(budget)` recomputes visibility for up to `budget` queued chunks per frame (`DirtyBudgetPerFrame = 2`).

**The goroutine must not touch anything but its own chunk.** That is what makes the pipeline race-free without a mutex, and since `-race` cannot run here it is the only guarantee available. The argument: the chunk is unreachable from `w.chunks` until the main thread receives it, and the only other state the goroutine reads is `w.noise`, which is written once in `NewWorld` and read-only thereafter. Adding any cross-chunk read back into the goroutine reintroduces a data race against `SetBlock` with nothing to catch it.

Boundary faces depend on whether the adjacent chunk is loaded, which is why both `insertChunk` and `UnloadChunk` mark neighbours dirty. `UnloadChunk` deletes from the map *before* marking, so the rebuild sees the chunk as gone. `FlushGenerations` drains generations and then the entire dirty queue, so the player never spawns into an unrendered world.

`SetBlock` recomputes visibility synchronously rather than via the queue, so block edits appear the same frame.

### Rendering

`Render(cam)` builds a per-frame visible set by dropping chunks whose bounding circle is entirely behind the camera plane (horizontal forward vector, `chunkRadius` margin), then draws in two passes: all opaque geometry, then transparent chunks sorted far-to-near. Both matter:

- **Opaque first**, so the depth buffer is complete before anything blends.
- **A stable, sorted order.** Ranging over the chunk map directly reorders chunks every frame, because Go randomises map iteration — that alone made the water surface flicker.

Faces are split into `VisibleOpaque` / `VisibleTransparent` at `ComputeVisibility` time so neither pass has to filter. Every face is drawn individually via a `DrawTriangle3D` pair, with a per-face shading multiplier (`faceShade`) standing in for directional light. Colors are hardcoded in `blocks.Color(face string)` — there is no texture atlas.

**Do not "optimise" this into `rl.Begin(rl.Quads)` + `rl.Vertex3f`.** It was tried and measured: 29.1 ms/frame versus 17.3 ms for the `DrawTriangle3D` pairs, despite sending fewer vertices. Under purego, quads cost five FFI crossings per face against two, and the crossings dominate. Output is identical either way (a pinned-camera A/B differed in 0 of 921600 pixels).

Because the cost is per face and not per pixel, **the cheapest face is the one never emitted**. See the sealed-boundary rule below; `TestFaceBudget` prints the current per-frame face count and is the tool for any change in this area.

Measured frame times, pinned camera, 300 timed frames after 60 warmup, target FPS off, GTX 750 Ti — before and after the boundary sealing:

| | emitting boundary faces | sealed |
|---|---|---|
| loadDist 1 (9 chunks) | 5.54 ms | 1.77 ms |
| loadDist 2 (25 chunks) | 8.34 ms | 4.12 ms |
| loadDist 3 (49 chunks) | 13.10 ms | 6.18 ms |
| loadDist 4 (81 chunks) | — | 9.21 ms |

Stable across seeds: at loadDist 2, five different seeds measured 2.81–4.15 ms. These numbers are *not* comparable to the earlier table this replaced (17.3/27.3/41.8 ms), which used a different pinned camera and seed; only same-harness A/B comparisons mean anything here. `loadDist` is at 2 with room to spare — 3 and 4 also measured comfortably, and going further is a judgement call about pop-in and generation cost rather than frame time.

A real per-chunk `rl.Mesh` built once on visibility change is still the only way past the per-draw-call FFI cost outright. The dirty queue is already the "geometry changed" signal such a rewrite would hang off.

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

Used in `world/chunk.go` (`ComputeVisibility` directions array), `world/world.go` (`drawFace` switch, `faceToName`, `faceShade`), and `player/player.go` (DDA face assignment, `handleBlockActions` placement offset). Change one, change all of them.

### Sealed boundaries — `ComputeVisibility` is three-state, not two

A neighbour is *known* (emit a face only if it is air or a different transparent block), *sealed* (never emit), or neither (emit). Sealed covers two cases, and both exist purely to avoid paying FFI for geometry no camera can reach:

- **`nly < 0`** — the underside of the world floor. `moveWithCollision` clamps the player to `y >= 0`, so it is unreachable.
- **A horizontal neighbour chunk that is not loaded** — the face points outward off the edge of the loaded window, and the player is always inside that window.

Removing these took the 3×3 window from 17335 faces/frame to 4681, i.e. **73% of all submitted geometry was invisible**: a solid 16-wide, terrain-height wall per exposed chunk border, plus 256 floor faces per chunk. Backface culling meant it produced no pixels, so it was invisible in both senses.

`nly >= ChunkHeight` must stay **exposed** — it is the one unbounded direction, and sealing it would leave a hole in the top of any tower built to y=127.

No extra invalidation machinery is needed: `insertChunk` and `UnloadChunk` already `markDirty` the four neighbours, so a boundary is unsealed when a chunk arrives and re-sealed when one leaves. `TestPerimeterWallsAreSealed`, `TestNoFacesAtWorldFloor`, and `TestUnloadSealsNeighbourBoundary` pin this down.

### Negative-coordinate chunk math

Go truncates integer division toward zero, so world→chunk conversion goes through `floorDiv` in `world/world.go` rather than plain `/`. Used by `GetBlock`, `SetBlock`, and `EnsureChunksAround`; replacing it with `/ ChunkWidth` breaks everything west/north of the origin.

`EnsureChunksAround` takes `float32`, and `int()` truncates toward zero too — so it must `math.Floor` **before** converting, then `floorDiv`. Doing only one of the two put the load window a chunk behind the player on a 1-block strip at every negative chunk border (`TestChunkKeyForNegativeFractionalPosition`).

### ESC is raylib's exit key

`rl.SetExitKey(rl.KeyNull)` in `main.go` is load-bearing. By default `WindowShouldClose()` returns true when ESC is pressed, which ends the loop at the top of the next iteration — so the in-loop Esc handler that releases the cursor could never run, and Esc simply quit the game. The title-bar X still closes the window (the other half of `WindowShouldClose`).

### Fixed timestep

Input, camera, raycast, and block actions run once per frame in `Update`. Gravity and collision run at exactly 60Hz in `StepPhysics` inside the accumulator; frame time is clamped to 0.25s against the spiral of death. Do not move input into `StepPhysics` — `IsKeyPressed`/`IsMouseButtonPressed` are edge-triggered and would fire unpredictably when the accumulator runs 0 or 2+ times.

### Collision

Both collision functions iterate every block column and layer the player's box touches; neither samples fixed points. `collidesAtY` picks one Y plane from the sign of `Velocity.Y` (head when rising, feet when falling, below-feet when stationary — that last case is what keeps `OnGround` true) and returns which layer it hit so the caller can snap to its surface. `collidesAt` covers the full Y span for X/Z movement, which matters because at most fractional heights the 1.62-tall player straddles three block layers.

`MaxStepBlocks` caps vertical travel per step below one block. Collision samples rather than sweeps, so without that cap a long fall skips clean through a floor.

### Mouse delta spike guard

The frame after `DisableCursor()` reports a huge delta. Two defenses: `SkipNextMouseFrame()` zeroes the very next delta entirely (called at startup and on every Esc re-hide), and `Update` independently **zeroes** any axis whose delta exceeds ±50.

### Water exit grace period

Leaving water with upward velocity sets `waterExitTimer = 0.2`. While it runs, holding Space still gives swim thrust — without it the player cannot climb out of water. Don't drop the timer without an alternative.

### Placement overwrites non-solid blocks, not just air

`handleBlockActions` gates on `!IsSolid()`, so a placed block replaces air *or water*. This is not cosmetic: the DDA raycast passes straight through water (`b != Air && b != Water`), so water can never be targeted and broken directly. If placement also refused to overwrite it, a misplaced block of water would be permanent and lakes would be unbuildable — you cannot place on a lake bed, because the cell above it is water. Overwrite-then-break is the only way to clear water. Don't stop the ray on water instead: clicking anywhere near a lake would then only ever hit the surface.

### Terrain shape

Base height from 4-octave noise (persistence 0.5, lacunarity 2.0) at scale 0.008, mapped to roughly 8–90; a second 2-octave sample at scale 0.04 adds ±4 of detail, giving a final range of about 4–94. Layering: bedrock at y=0 (tested **before** the dirt band, whose range would otherwise overlap it), then stone, 3 layers of dirt, and a surface of grass — or sand at/below `SeaLevel+1`. Water fills empty cells up to `SeaLevel` (48).

`GroundHeight` returns `(int, bool)`. The bool is load-bearing: an unloaded chunk reads as Air, which is otherwise indistinguishable from genuine ground at y=0.

## Note on AGENTS.md

`AGENTS.md` predates the fixes recorded in the git history and is now substantially wrong — among other things it describes a CGo build requiring a manually supplied `raylib.dll`, and places `ComputeVisibility` plus a neighbour rebuild inside `ProcessGenerations`. Prefer this file. It should be updated or removed rather than left to drift further.
