package world

import (
	"testing"

	"mc-go/blocks"
)

// visibleCount is the total face count across both material passes.
func visibleCount(c *Chunk) int {
	return len(c.VisibleOpaque) + len(c.VisibleTransparent)
}

// eachFace calls fn for every visible face of every loaded chunk. Tests use
// this rather than a hardcoded window so they survive a loadDist change.
func eachFace(w *World, fn func(c *Chunk, vf VisibleFace)) {
	for _, c := range w.chunks {
		for _, list := range [][]VisibleFace{c.VisibleOpaque, c.VisibleTransparent} {
			for _, vf := range list {
				fn(c, vf)
			}
		}
	}
}

// TestGroundHeightReportsMissingGround covers the spawn fallback that could
// never fire: GroundHeight's old 0 sentinel was indistinguishable from real
// ground at y=0, so `spawnY < 2.5` was unsatisfiable and a player over
// ungenerated terrain spawned at 2.5 rather than the intended safe height.
func TestGroundHeightReportsMissingGround(t *testing.T) {
	w := NewWorld(31337)
	w.EnsureChunksAround(0, 0)
	w.FlushGenerations()

	if _, ok := w.GroundHeight(0, 0); !ok {
		t.Error("expected ground at spawn")
	}

	// Far outside any loaded chunk: every block reads as Air.
	if h, ok := w.GroundHeight(100000, 100000); ok {
		t.Errorf("expected no ground in unloaded terrain, got height %d", h)
	}
}

// TestBedrockAtWorldFloor guards the ordering in generateTerrain: the dirt
// band (ly >= height-3) used to be tested before ly == 0, so a low enough
// column would place dirt at the world floor instead of bedrock.
func TestBedrockAtWorldFloor(t *testing.T) {
	w := NewWorld(8080)
	w.EnsureChunksAround(0, 0)
	w.FlushGenerations()

	c := w.GetChunk(0, 0)
	for lx := 0; lx < ChunkWidth; lx++ {
		for lz := 0; lz < ChunkDepth; lz++ {
			if got := c.Blocks[lx][0][lz]; got != blocks.Bedrock {
				t.Fatalf("floor at local(%d,0,%d) is %v, want Bedrock", lx, lz, got)
			}
		}
	}
}

// TestChunksHaveVisibleFacesAfterFlush guards the regression where
// ComputeVisibility was called on the generation goroutine while the chunk's
// `loaded` flag was still false, so its guard returned early and every freshly
// generated chunk rendered nothing at all.
func TestChunksHaveVisibleFacesAfterFlush(t *testing.T) {
	w := NewWorld(12345)
	w.EnsureChunksAround(0, 0)
	w.FlushGenerations()

	side := 2*w.loadDist + 1
	if w.ChunkCount() != side*side {
		t.Fatalf("expected %dx%d = %d chunks loaded, got %d", side, side, side*side, w.ChunkCount())
	}

	for cx := -w.loadDist; cx <= w.loadDist; cx++ {
		for cz := -w.loadDist; cz <= w.loadDist; cz++ {
			c := w.GetChunk(cx, cz)
			if c == nil {
				t.Fatalf("chunk (%d,%d) missing", cx, cz)
			}
			if visibleCount(c) == 0 {
				t.Errorf("chunk (%d,%d) has no visible faces — terrain would be invisible", cx, cz)
			}
		}
	}
}

// TestInteriorSeamsAreHidden checks that a chunk which finished generating
// before its neighbour arrived gets its boundary re-evaluated. Solid blocks
// buried against a loaded neighbour must not emit a face, or chunk borders
// show interior walls (most visibly as sheets of water at the seam).
func TestInteriorSeamsAreHidden(t *testing.T) {
	w := NewWorld(999)
	w.EnsureChunksAround(0, 0)
	w.FlushGenerations()

	c := w.GetChunk(0, 0)
	if c == nil {
		t.Fatal("centre chunk missing")
	}

	// Face 3 is -X. A block at lx==0 whose -X neighbour (in chunk -1) is
	// solid is fully enclosed and must not be in the visible set.
	for _, vf := range c.VisibleOpaque {
		if vf.LocalX != 0 || vf.Face != 3 {
			continue
		}
		wz := c.CZ*ChunkDepth + vf.LocalZ
		if nb := w.GetBlock(-1, vf.LocalY, wz); !nb.IsTransparent() {
			t.Fatalf("face at local(0,%d,%d) emitted toward solid neighbour %v — stale seam",
				vf.LocalY, vf.LocalZ, nb)
		}
	}
}

// TestUnloadSealsNeighbourBoundary is the inverse: once a chunk is gone, the
// boundary it shared must be sealed rather than re-exposed. Those faces would
// point outward off the edge of the loaded window, where the player never is,
// so each costs two FFI crossings and produces no pixels.
func TestUnloadSealsNeighbourBoundary(t *testing.T) {
	w := NewWorld(4242)
	w.EnsureChunksAround(0, 0)
	w.FlushGenerations()

	before := visibleCount(w.GetChunk(0, 0))

	w.UnloadChunk(-1, 0)
	w.ProcessDirty(0)

	c := w.GetChunk(0, 0)
	for _, list := range [][]VisibleFace{c.VisibleOpaque, c.VisibleTransparent} {
		for _, vf := range list {
			if vf.LocalX == 0 && vf.Face == 3 {
				t.Fatalf("face at local(0,%d,%d) points at the unloaded -X chunk",
					vf.LocalY, vf.LocalZ)
			}
		}
	}

	if after := visibleCount(c); after > before {
		t.Errorf("unloading the -X neighbour should not add faces to (0,0): %d -> %d", before, after)
	}
}

// TestNoFacesAtWorldFloor: the underside of y=0 is unreachable, since
// moveWithCollision clamps the player to y >= 0.
func TestNoFacesAtWorldFloor(t *testing.T) {
	w := NewWorld(1717)
	w.EnsureChunksAround(0, 0)
	w.FlushGenerations()

	eachFace(w, func(c *Chunk, vf VisibleFace) {
		if vf.LocalY == 0 && vf.Face == 1 {
			t.Fatalf("chunk (%d,%d) emits a bottom face at the world floor, local(%d,0,%d)",
				c.CX, c.CZ, vf.LocalX, vf.LocalZ)
		}
	})
}

// TestPerimeterWallsAreSealed: the outer ring of the loaded window must not
// emit faces toward the chunks beyond it. Before this was fixed, each exposed
// chunk border produced a solid 16-wide wall of terrain-height faces.
func TestPerimeterWallsAreSealed(t *testing.T) {
	w := NewWorld(31415)
	w.EnsureChunksAround(0, 0)
	w.FlushGenerations()

	eachFace(w, func(c *Chunk, vf VisibleFace) {
		if facesUnloadedNeighbour(w, c, vf) {
			t.Fatalf("chunk (%d,%d) emits face %d at local(%d,%d,%d) toward an unloaded chunk",
				c.CX, c.CZ, vf.Face, vf.LocalX, vf.LocalY, vf.LocalZ)
		}
	})
}

// TestChunkKeyForNegativeFractionalPosition guards the load window against
// Go's truncation: int(-16.5) is -16, which used to resolve to chunk -1 while
// the player stood in chunk -2, so terrain ahead loaded a block late.
func TestChunkKeyForNegativeFractionalPosition(t *testing.T) {
	w := NewWorld(606)
	w.EnsureChunksAround(-16.5, 0)
	w.FlushGenerations()

	// x = -16.5 is in chunk -2, so the window is centred there.
	const centre = -2
	for cx := centre - w.loadDist; cx <= centre+w.loadDist; cx++ {
		if w.GetChunk(cx, 0) == nil {
			t.Errorf("chunk (%d,0) not loaded for a player at x=-16.5", cx)
		}
	}
	if outside := centre + w.loadDist + 1; w.GetChunk(outside, 0) != nil {
		t.Errorf("chunk (%d,0) is outside the window for a player at x=-16.5", outside)
	}
}

// TestFacesSplitByMaterial guards the two-pass render contract: a face must
// land in exactly the slice matching its block's transparency, or opaque
// geometry would blend and water would write depth ahead of it.
func TestFacesSplitByMaterial(t *testing.T) {
	w := NewWorld(777)
	w.EnsureChunksAround(0, 0)
	w.FlushGenerations()

	for _, key := range [][2]int{{0, 0}, {1, 0}, {0, 1}} {
		c := w.GetChunk(key[0], key[1])
		for _, vf := range c.VisibleOpaque {
			if vf.Block.IsTransparent() {
				t.Fatalf("transparent %v in the opaque pass", vf.Block)
			}
		}
		for _, vf := range c.VisibleTransparent {
			if !vf.Block.IsTransparent() {
				t.Fatalf("opaque %v in the transparent pass", vf.Block)
			}
		}
	}
}

// TestFaceBudget is a measurement, not an assertion: it prints how many faces
// the loaded window submits per frame and how many of those point at nothing
// -- the world floor, or a chunk that is not loaded. Every face costs two
// DrawTriangle3D FFI crossings whether or not it produces a pixel, so this
// number is the render budget. Run it before and after a visibility change.
func TestFaceBudget(t *testing.T) {
	w := NewWorld(2468)
	w.EnsureChunksAround(0, 0)
	w.FlushGenerations()

	total, floor, perimeter := 0, 0, 0
	eachFace(w, func(c *Chunk, vf VisibleFace) {
		total++
		if vf.LocalY == 0 && vf.Face == 1 {
			floor++
		}
		if facesUnloadedNeighbour(w, c, vf) {
			perimeter++
		}
	})
	t.Logf("%d chunks, faces/frame: %d total, %d world-floor, %d toward unloaded chunks (%d wasted, %.1f%%)",
		w.ChunkCount(), total, floor, perimeter, floor+perimeter,
		100*float64(floor+perimeter)/float64(total))
}

// facesUnloadedNeighbour reports whether vf sits on a chunk edge and points
// outward at a chunk that is not loaded.
func facesUnloadedNeighbour(w *World, c *Chunk, vf VisibleFace) bool {
	switch {
	case vf.Face == 3 && vf.LocalX == 0:
		return w.GetChunk(c.CX-1, c.CZ) == nil
	case vf.Face == 2 && vf.LocalX == ChunkWidth-1:
		return w.GetChunk(c.CX+1, c.CZ) == nil
	case vf.Face == 5 && vf.LocalZ == 0:
		return w.GetChunk(c.CX, c.CZ-1) == nil
	case vf.Face == 4 && vf.LocalZ == ChunkDepth-1:
		return w.GetChunk(c.CX, c.CZ+1) == nil
	}
	return false
}

// TestDrawOrderIsStable covers the flicker: Render used to range over the
// chunk map directly, and Go randomises map iteration on every pass.
func TestDrawOrderIsStable(t *testing.T) {
	w := NewWorld(555)
	w.EnsureChunksAround(0, 0)
	w.FlushGenerations()

	w.refreshOrder()
	first := append([][2]int(nil), w.order...)

	for i := 0; i < 20; i++ {
		w.orderDirty = true
		w.refreshOrder()
		for j := range first {
			if w.order[j] != first[j] {
				t.Fatalf("draw order changed on rebuild %d at index %d: %v vs %v",
					i, j, w.order[j], first[j])
			}
		}
	}
}
