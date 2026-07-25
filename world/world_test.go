package world

import "testing"

// visibleCount is the total face count across both material passes.
func visibleCount(c *Chunk) int {
	return len(c.VisibleOpaque) + len(c.VisibleTransparent)
}

// TestChunksHaveVisibleFacesAfterFlush guards the regression where
// ComputeVisibility was called on the generation goroutine while the chunk's
// `loaded` flag was still false, so its guard returned early and every freshly
// generated chunk rendered nothing at all.
func TestChunksHaveVisibleFacesAfterFlush(t *testing.T) {
	w := NewWorld(12345)
	w.EnsureChunksAround(0, 0)
	w.FlushGenerations()

	if w.ChunkCount() != 9 {
		t.Fatalf("expected 3x3 = 9 chunks loaded, got %d", w.ChunkCount())
	}

	for cx := -1; cx <= 1; cx++ {
		for cz := -1; cz <= 1; cz++ {
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

// TestUnloadReexposesNeighbourBoundary is the inverse: once a chunk is gone,
// the neighbour it was hiding must expose that boundary again.
func TestUnloadReexposesNeighbourBoundary(t *testing.T) {
	w := NewWorld(4242)
	w.EnsureChunksAround(0, 0)
	w.FlushGenerations()

	before := visibleCount(w.GetChunk(0, 0))

	w.UnloadChunk(-1, 0)
	w.ProcessDirty(0)

	after := visibleCount(w.GetChunk(0, 0))
	if after <= before {
		t.Errorf("unloading the -X neighbour should expose more faces on (0,0): %d -> %d", before, after)
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
