package player

import (
	"math"
	"testing"

	"mc-go/blocks"
	"mc-go/world"
)

const testDt = 1.0 / 60.0

func testWorld(t *testing.T) *world.World {
	t.Helper()
	w := world.NewWorld(2024)
	w.EnsureChunksAround(0, 0)
	w.FlushGenerations()
	return w
}

// dropHeight is a spawn Y safely inside the world. Starting above
// ChunkHeight-1 would trip the world-ceiling clamp in moveWithCollision,
// which legitimately moves the player many blocks in one step.
const dropHeight float32 = world.ChunkHeight - 5

// TestFastFallDoesNotTunnel covers the missing terminal-velocity clamp.
// Gravity was unbounded in air while collidesAtY samples a single block layer
// rather than sweeping, so past roughly 2.4s of free fall a step exceeded one
// block and the player passed straight through the ground.
func TestFastFallDoesNotTunnel(t *testing.T) {
	w := testWorld(t)
	ground, ok := w.GroundHeight(0, 0)
	if !ok {
		t.Fatal("no ground at spawn")
	}

	p := NewPlayer(0.5, dropHeight, 0.5)
	p.Velocity.Y = -100000 // absurd: the clamp must contain it

	for i := 0; i < 1200 && !p.OnGround; i++ {
		p.StepPhysics(w, testDt)
	}

	if !p.OnGround {
		t.Fatalf("player never landed, ended at Y=%.2f (ground %d)", p.Position.Y, ground)
	}
	if p.Position.Y < float32(ground) {
		t.Errorf("player tunnelled through terrain: Y=%.2f is below ground %d", p.Position.Y, ground)
	}
}

// TestStepNeverCrossesAWholeBlock asserts the invariant the clamp exists to
// protect, independently of whether any particular fall happens to tunnel.
func TestStepNeverCrossesAWholeBlock(t *testing.T) {
	w := testWorld(t)

	p := NewPlayer(0.5, dropHeight, 0.5)
	for i := 0; i < 600 && !p.OnGround; i++ {
		before := p.Position.Y
		p.StepPhysics(w, testDt)
		if d := float64(before - p.Position.Y); d >= 1.0 {
			t.Fatalf("step %d moved %.3f blocks; a thin floor could be skipped", i, d)
		}
	}
}

// TestLandingSnapsToSurface covers the landing that reverted Y instead of
// snapping, leaving the player hovering and then settling over several frames.
func TestLandingSnapsToSurface(t *testing.T) {
	w := testWorld(t)

	p := NewPlayer(0.5, dropHeight, 0.5)
	for i := 0; i < 600 && !p.OnGround; i++ {
		p.StepPhysics(w, testDt)
	}
	if !p.OnGround {
		t.Fatal("player never landed")
	}

	if got := float64(p.Position.Y); got != math.Trunc(got) {
		t.Errorf("landed at Y=%.4f, expected to sit exactly on a block face", got)
	}

	// And it must stay there rather than drift downward frame by frame.
	rest := p.Position.Y
	for i := 0; i < 30; i++ {
		p.StepPhysics(w, testDt)
	}
	if p.Position.Y != rest {
		t.Errorf("player drifted after landing: %.4f -> %.4f", rest, p.Position.Y)
	}
}

// TestCornerCollisionAtMiddleLayer covers the 9-point sample in collidesAt,
// which tested the middle block layer only along the player's centre axis.
// The player here straddles columns 8/9 on both axes and spans layers
// 120..122, so a block at corner column (8,8) in layer 121 was invisible to
// every one of the old sample points.
func TestCornerCollisionAtMiddleLayer(t *testing.T) {
	w := testWorld(t)

	const bx, by, bz = 8, 121, 8
	if got := w.GetBlock(bx, by, bz); got != blocks.Air {
		t.Fatalf("test spot (%d,%d,%d) is not air but %v", bx, by, bz, got)
	}

	p := NewPlayer(9.0, 120.5, 9.0)
	if p.collidesAt(w, p.Position) {
		t.Fatal("player should be in open air before the block is placed")
	}

	w.SetBlock(bx, by, bz, blocks.Stone)
	if !p.collidesAt(w, p.Position) {
		t.Error("block at a corner column in the middle layer was not detected")
	}
}
