package world

import "testing"

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
			if len(c.Visible) == 0 {
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
	for _, vf := range c.Visible {
		if vf.LocalX != 0 || vf.Face != 3 {
			continue
		}
		wx := -1
		wy := vf.LocalY
		wz := c.CZ*ChunkDepth + vf.LocalZ
		if nb := w.GetBlock(wx, wy, wz); !nb.IsTransparent() {
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

	before := len(w.GetChunk(0, 0).Visible)

	w.UnloadChunk(-1, 0)
	w.ProcessDirty(0)

	after := len(w.GetChunk(0, 0).Visible)
	if after <= before {
		t.Errorf("unloading the -X neighbour should expose more faces on (0,0): %d -> %d", before, after)
	}
}
