package world

import (
	"math"
	"sort"

	rl "github.com/gen2brain/raylib-go/raylib"

	"mc-go/blocks"
)

const (
	SeaLevel    = 48
	GroundLevel = 64

	// DirtyBudgetPerFrame caps how many chunks rebuild visibility per frame.
	// ComputeVisibility is a 16x128x16 x 6-direction scan (~200k neighbour
	// checks), so an unbounded drain would stutter. Crossing a chunk border
	// dirties at most ~15 chunks, which settles in ~8 frames at this budget.
	DirtyBudgetPerFrame = 2

	// chunkRadius is the horizontal distance from a chunk's centre to its
	// far corner. Used as the margin on the behind-camera cull so a chunk is
	// only dropped once no part of it can be in front of the camera.
	chunkRadius = 12 // ceil(sqrt(8^2 + 8^2))
)

// World manages all chunks and provides block-level access.
type World struct {
	chunks     map[[2]int]*Chunk
	noise      *openSimplexNoise
	seed       int64
	loadDist   int // How many chunks to load around player in each direction
	generating map[[2]int]struct{}
	genResults chan *Chunk
	dirty      [][2]int            // FIFO of chunks needing a visibility rebuild
	dirtySet   map[[2]int]struct{} // membership of dirty, to avoid queueing twice
	order      [][2]int            // stable draw order; map order would flicker
	tOrder     [][2]int            // scratch: order re-sorted back to front
	visible    [][2]int            // scratch: order minus chunks behind the camera
	orderDirty bool
}

// NewWorld creates a new world with the given seed.
func NewWorld(seed int64) *World {
	w := &World{
		chunks:     make(map[[2]int]*Chunk),
		noise:      NewNoise(seed),
		seed:       seed,
		loadDist:   1, // 3x3 = 9 chunks
		generating: make(map[[2]int]struct{}),
		genResults: make(chan *Chunk, 16),
		dirtySet:   make(map[[2]int]struct{}),
	}
	return w
}

// GroundHeight returns the Y of the highest solid block near (wx, wz) and
// whether one was found. Scans a 3x3 area so the spawn does not end up inside
// adjacent higher terrain. The ok result matters: an unloaded chunk reads as
// Air, which is indistinguishable from genuine ground at y=0 without it.
func (w *World) GroundHeight(wx, wz int) (int, bool) {
	best := -1
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			for y := ChunkHeight - 1; y > best; y-- {
				if w.GetBlock(wx+dx, y, wz+dz).IsSolid() {
					best = y
					break
				}
			}
		}
	}
	if best < 0 {
		return 0, false
	}
	return best, true
}

// GetChunk returns the chunk at (cx, cz).
func (w *World) GetChunk(cx, cz int) *Chunk {
	key := [2]int{cx, cz}
	if c, ok := w.chunks[key]; ok {
		return c
	}
	return nil
}

// ChunkCount returns the number of currently loaded chunks.
func (w *World) ChunkCount() int {
	return len(w.chunks)
}

// LoadChunk starts async terrain generation for a chunk.
func (w *World) LoadChunk(cx, cz int) {
	key := [2]int{cx, cz}
	if _, ok := w.chunks[key]; ok {
		return
	}
	if _, ok := w.generating[key]; ok {
		return
	}
	w.generating[key] = struct{}{}

	go w.generateChunkAsync(NewChunk(cx, cz))
}

// generateChunkAsync fills a chunk with terrain on a worker goroutine.
// It deliberately touches nothing but c: the chunk is not reachable from
// w.chunks until the main thread receives it, so this needs no locking.
// Visibility is NOT computed here — it depends on neighbouring chunks that
// the main thread owns and mutates.
func (w *World) generateChunkAsync(c *Chunk) {
	w.generateTerrain(c)
	w.genResults <- c
}

// insertChunk publishes a finished chunk. Main thread only.
func (w *World) insertChunk(c *Chunk) {
	c.loaded = true
	key := [2]int{c.CX, c.CZ}
	w.chunks[key] = c
	delete(w.generating, key)
	w.orderDirty = true

	// A chunk's boundary faces depend on whether the adjacent chunk is
	// present, so arrival invalidates this chunk and all four neighbours.
	w.markDirty(c.CX, c.CZ)
	w.markDirty(c.CX-1, c.CZ)
	w.markDirty(c.CX+1, c.CZ)
	w.markDirty(c.CX, c.CZ-1)
	w.markDirty(c.CX, c.CZ+1)
}

// markDirty queues a chunk for a visibility rebuild. Absent chunks may be
// queued freely; ProcessDirty skips whatever is no longer loaded.
func (w *World) markDirty(cx, cz int) {
	key := [2]int{cx, cz}
	if _, ok := w.dirtySet[key]; ok {
		return
	}
	w.dirtySet[key] = struct{}{}
	w.dirty = append(w.dirty, key)
}

// ProcessDirty rebuilds visibility for up to budget chunks, oldest first.
// A budget <= 0 drains the whole queue.
func (w *World) ProcessDirty(budget int) {
	done := 0
	for len(w.dirty) > 0 {
		if budget > 0 && done >= budget {
			return
		}
		key := w.dirty[0]
		w.dirty = w.dirty[1:]
		delete(w.dirtySet, key)

		c := w.GetChunk(key[0], key[1])
		if c == nil {
			continue // unloaded while queued; nothing to rebuild
		}
		c.ComputeVisibility(w.GetChunk)
		done++ // only real work spends budget
	}
}

// FlushGenerations blocks until all in-progress chunk generations complete
// and every pending visibility rebuild has run. Used at startup so the
// player does not spawn into an unrendered world.
func (w *World) FlushGenerations() {
	for len(w.generating) > 0 {
		w.insertChunk(<-w.genResults)
	}
	w.ProcessDirty(0)
}

// ProcessGenerations finalizes any chunks that finished async generation.
// Must be called from the main thread (not concurrently with map access).
func (w *World) ProcessGenerations() {
	for {
		select {
		case c := <-w.genResults:
			w.insertChunk(c)
		default:
			return
		}
	}
}

// UnloadChunk frees GPU resources and removes the chunk.
func (w *World) UnloadChunk(cx, cz int) {
	key := [2]int{cx, cz}
	c, ok := w.chunks[key]
	if !ok {
		return
	}
	c.Unload()
	delete(w.chunks, key)
	w.orderDirty = true

	// Delete first, then invalidate: the neighbours' boundary faces must be
	// recomputed against this chunk being gone, not still present.
	w.markDirty(cx-1, cz)
	w.markDirty(cx+1, cz)
	w.markDirty(cx, cz-1)
	w.markDirty(cx, cz+1)
}

// generateTerrain fills the chunk with blocks based on noise.
func (w *World) generateTerrain(c *Chunk) {
	for lx := 0; lx < ChunkWidth; lx++ {
		for lz := 0; lz < ChunkDepth; lz++ {
			wx := float64(c.CX*ChunkWidth + lx)
			wz := float64(c.CZ*ChunkDepth + lz)

			scale := 0.008
			h := w.noise.OctaveNoise(wx*scale, wz*scale, 4, 0.5, 2.0)
			height := int((h+1.0)/2.0*82.0 + 8.0)

			detail := w.noise.OctaveNoise(wx*0.04, wz*0.04, 2, 0.5, 3.0) * 4
			height += int(detail)

			for ly := 0; ly < ChunkHeight; ly++ {
				if ly > height {
					if ly <= SeaLevel {
						c.Blocks[lx][ly][lz] = blocks.Water
						continue
					}
					break
				} else if ly == 0 {
					// Checked before the dirt band: for a low enough height
					// the two ranges overlap and bedrock would be skipped.
					c.Blocks[lx][ly][lz] = blocks.Bedrock
				} else if ly == height {
					if ly <= SeaLevel+1 {
						c.Blocks[lx][ly][lz] = blocks.Sand
					} else {
						c.Blocks[lx][ly][lz] = blocks.Grass
					}
				} else if ly >= height-3 {
					c.Blocks[lx][ly][lz] = blocks.Dirt
				} else {
					c.Blocks[lx][ly][lz] = blocks.Stone
				}
			}
		}
	}
}

// GetBlock returns the block at world coordinates.
func (w *World) GetBlock(wx, wy, wz int) blocks.BlockType {
	cx := wx / ChunkWidth
	if wx < 0 {
		cx = (wx+1)/ChunkWidth - 1
	}
	cz := wz / ChunkDepth
	if wz < 0 {
		cz = (wz+1)/ChunkDepth - 1
	}

	c := w.GetChunk(cx, cz)
	if c == nil || !c.loaded {
		return blocks.Air
	}

	lx := wx - cx*ChunkWidth
	lz := wz - cz*ChunkDepth
	if lx < 0 {
		lx += ChunkWidth
	}
	if lz < 0 {
		lz += ChunkDepth
	}

	b, ok := c.GetBlock(lx, wy, lz)
	if !ok {
		return blocks.Air
	}
	return b
}

// SetBlock sets a block at world coordinates and rebuilds affected chunks.
func (w *World) SetBlock(wx, wy, wz int, b blocks.BlockType) {
	cx := wx / ChunkWidth
	if wx < 0 {
		cx = (wx+1)/ChunkWidth - 1
	}
	cz := wz / ChunkDepth
	if wz < 0 {
		cz = (wz+1)/ChunkDepth - 1
	}

	c := w.GetChunk(cx, cz)
	if c == nil {
		return
	}

	lx := wx - cx*ChunkWidth
	lz := wz - cz*ChunkDepth
	if lx < 0 {
		lx += ChunkWidth
	}
	if lz < 0 {
		lz += ChunkDepth
	}

	c.SetBlock(lx, wy, lz, b)
	c.ComputeVisibility(w.GetChunk)

	// Rebuild neighbors if on chunk boundary
	if lx == 0 {
		if nc := w.GetChunk(cx-1, cz); nc != nil {
			nc.ComputeVisibility(w.GetChunk)
		}
	}
	if lx == ChunkWidth-1 {
		if nc := w.GetChunk(cx+1, cz); nc != nil {
			nc.ComputeVisibility(w.GetChunk)
		}
	}
	if lz == 0 {
		if nc := w.GetChunk(cx, cz-1); nc != nil {
			nc.ComputeVisibility(w.GetChunk)
		}
	}
	if lz == ChunkDepth-1 {
		if nc := w.GetChunk(cx, cz+1); nc != nil {
			nc.ComputeVisibility(w.GetChunk)
		}
	}
}

// EnsureChunksAround loads chunks within range and unloads distant ones.
func (w *World) EnsureChunksAround(worldX, worldZ float32) {
	cx := int(worldX) / ChunkWidth
	cz := int(worldZ) / ChunkDepth
	if worldX < 0 {
		cx = (int(worldX)+1)/ChunkWidth - 1
	}
	if worldZ < 0 {
		cz = (int(worldZ)+1)/ChunkDepth - 1
	}

	for dx := -w.loadDist; dx <= w.loadDist; dx++ {
		for dz := -w.loadDist; dz <= w.loadDist; dz++ {
			w.LoadChunk(cx+dx, cz+dz)
		}
	}

	// Unload distant chunks. A chunk still generating is not in w.chunks yet,
	// so it cannot appear here and needs no guard.
	keysToDelete := make([][2]int, 0)
	for key := range w.chunks {
		if key[0] < cx-w.loadDist || key[0] > cx+w.loadDist ||
			key[1] < cz-w.loadDist || key[1] > cz+w.loadDist {
			keysToDelete = append(keysToDelete, key)
		}
	}
	for _, key := range keysToDelete {
		w.UnloadChunk(key[0], key[1])
	}
}

// Render draws all loaded chunks: opaque geometry first so the depth buffer
// is complete, then transparent faces back to front.
func (w *World) Render(cam rl.Camera3D) {
	w.refreshOrder()

	// Horizontal forward vector, for the behind-camera cull below.
	fx := cam.Target.X - cam.Position.X
	fz := cam.Target.Z - cam.Position.Z
	if l := float32(math.Sqrt(float64(fx*fx + fz*fz))); l > 0.0001 {
		fx /= l
		fz /= l
	}

	// Visible set for this frame. A chunk is dropped only when its whole
	// bounding circle lies behind the camera plane, so this never culls
	// anything on screen regardless of FOV.
	w.visible = w.visible[:0]
	for _, key := range w.order {
		c := w.chunks[key]
		if c == nil || !c.loaded {
			continue
		}
		dx := float32(key[0]*ChunkWidth) + ChunkWidth/2 - cam.Position.X
		dz := float32(key[1]*ChunkDepth) + ChunkDepth/2 - cam.Position.Z
		if dx*fx+dz*fz < -chunkRadius {
			continue
		}
		w.visible = append(w.visible, key)
	}

	for _, key := range w.visible {
		w.drawFaces(w.chunks[key], w.chunks[key].VisibleOpaque)
	}

	// Sorted far-to-near for blending. Ranging over w.chunks directly would
	// reorder them every frame, since Go randomises map iteration — that alone
	// made the water surface flicker.
	w.tOrder = append(w.tOrder[:0], w.visible...)
	sort.Slice(w.tOrder, func(i, j int) bool {
		return chunkDistSq(w.tOrder[i], cam.Position) > chunkDistSq(w.tOrder[j], cam.Position)
	})
	for _, key := range w.tOrder {
		w.drawFaces(w.chunks[key], w.chunks[key].VisibleTransparent)
	}
}

// drawFaces draws one chunk's faces. Each face goes through a single
// DrawTriangle3D pair rather than rl.Begin(rl.Quads)/rl.Vertex3f: this binding
// is purego, so every rlgl call is an FFI crossing, and pushing vertices one at
// a time measured ~68% slower (29.1ms vs 17.3ms per frame) despite sending
// fewer vertices.
func (w *World) drawFaces(c *Chunk, faces []VisibleFace) {
	originX := float32(c.CX * ChunkWidth)
	originZ := float32(c.CZ * ChunkDepth)
	for _, vf := range faces {
		col := shade(vf.Block.Color(faceToName(vf.Face)), faceShade(vf.Face))
		drawFace(originX+float32(vf.LocalX), float32(vf.LocalY), originZ+float32(vf.LocalZ), vf.Face, col)
	}
}

// refreshOrder rebuilds the stable chunk draw order. Only chunk load/unload
// changes it, so this is near-free on a typical frame.
func (w *World) refreshOrder() {
	if !w.orderDirty {
		return
	}
	w.order = w.order[:0]
	for key := range w.chunks {
		w.order = append(w.order, key)
	}
	sort.Slice(w.order, func(i, j int) bool {
		if w.order[i][0] != w.order[j][0] {
			return w.order[i][0] < w.order[j][0]
		}
		return w.order[i][1] < w.order[j][1]
	})
	w.orderDirty = false
}

// chunkDistSq is the squared horizontal distance from a chunk's centre to p.
func chunkDistSq(key [2]int, p rl.Vector3) float32 {
	dx := float32(key[0]*ChunkWidth) + ChunkWidth/2 - p.X
	dz := float32(key[1]*ChunkDepth) + ChunkDepth/2 - p.Z
	return dx*dx + dz*dz
}

// faceShade fakes directional light so neighbouring blocks of the same colour
// stay distinguishable. This replaces the per-block wireframe, which cost more
// geometry than the blocks it outlined.
func faceShade(face int) float32 {
	switch face {
	case 0: // Top
		return 1.0
	case 1: // Bottom
		return 0.55
	case 2, 3: // +X / -X
		return 0.8
	default: // +Z / -Z
		return 0.68
	}
}

func shade(c rl.Color, f float32) rl.Color {
	return rl.NewColor(
		uint8(float32(c.R)*f),
		uint8(float32(c.G)*f),
		uint8(float32(c.B)*f),
		c.A,
	)
}

// faceToName maps a face index to the name expected by blocks.Color.
func faceToName(face int) string {
	switch face {
	case 0:
		return "top"
	case 1:
		return "bottom"
	default:
		return ""
	}
}

// drawFace draws a single block face as two CCW triangles, wound so the
// outward normal points away from the block. Face indices follow chunk.go.
func drawFace(x, y, z float32, face int, col rl.Color) {
	switch face {
	case 0: // Top (+Y)
		rl.DrawTriangle3D(rl.NewVector3(x, y+1, z), rl.NewVector3(x, y+1, z+1), rl.NewVector3(x+1, y+1, z+1), col)
		rl.DrawTriangle3D(rl.NewVector3(x, y+1, z), rl.NewVector3(x+1, y+1, z+1), rl.NewVector3(x+1, y+1, z), col)
	case 1: // Bottom (-Y)
		rl.DrawTriangle3D(rl.NewVector3(x, y, z), rl.NewVector3(x+1, y, z), rl.NewVector3(x+1, y, z+1), col)
		rl.DrawTriangle3D(rl.NewVector3(x, y, z), rl.NewVector3(x+1, y, z+1), rl.NewVector3(x, y, z+1), col)
	case 2: // Right (+X)
		rl.DrawTriangle3D(rl.NewVector3(x+1, y, z), rl.NewVector3(x+1, y+1, z), rl.NewVector3(x+1, y+1, z+1), col)
		rl.DrawTriangle3D(rl.NewVector3(x+1, y, z), rl.NewVector3(x+1, y+1, z+1), rl.NewVector3(x+1, y, z+1), col)
	case 3: // Left (-X)
		rl.DrawTriangle3D(rl.NewVector3(x, y, z), rl.NewVector3(x, y, z+1), rl.NewVector3(x, y+1, z+1), col)
		rl.DrawTriangle3D(rl.NewVector3(x, y, z), rl.NewVector3(x, y+1, z+1), rl.NewVector3(x, y+1, z), col)
	case 4: // Front (+Z)
		rl.DrawTriangle3D(rl.NewVector3(x, y, z+1), rl.NewVector3(x+1, y, z+1), rl.NewVector3(x+1, y+1, z+1), col)
		rl.DrawTriangle3D(rl.NewVector3(x, y, z+1), rl.NewVector3(x+1, y+1, z+1), rl.NewVector3(x, y+1, z+1), col)
	case 5: // Back (-Z)
		rl.DrawTriangle3D(rl.NewVector3(x, y, z), rl.NewVector3(x, y+1, z), rl.NewVector3(x+1, y+1, z), col)
		rl.DrawTriangle3D(rl.NewVector3(x, y, z), rl.NewVector3(x+1, y+1, z), rl.NewVector3(x+1, y, z), col)
	}
}
