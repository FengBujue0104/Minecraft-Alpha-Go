package world

import (
	rl "github.com/gen2brain/raylib-go/raylib"

	"mc-go/blocks"
)

const (
	SeaLevel    = 48
	GroundLevel = 64
)

// World manages all chunks and provides block-level access.
type World struct {
	chunks     map[[2]int]*Chunk
	noise      *openSimplexNoise
	seed       int64
	loadDist   int // How many chunks to load around player in each direction
	generating map[[2]int]struct{}
	genResults chan *Chunk
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
	}
	return w
}

// GroundHeight returns the Y coordinate of the highest solid block near (wx, wz).
// Scans a 3x3 area to avoid spawning inside nearby elevated terrain.
// Returns 0 if no solid block is found.
func (w *World) GroundHeight(wx, wz int) int {
	maxH := 0
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			for y := ChunkHeight - 1; y >= maxH; y-- {
				b := w.GetBlock(wx+dx, y, wz+dz)
				if b.IsSolid() {
					if y > maxH {
						maxH = y
					}
					break
				}
			}
		}
	}
	return maxH
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

	c := NewChunk(cx, cz)
	// Snapshot neighbor references before spawning goroutine
	neighbors := [4]*Chunk{
		w.GetChunk(cx-1, cz),
		w.GetChunk(cx+1, cz),
		w.GetChunk(cx, cz-1),
		w.GetChunk(cx, cz+1),
	}
	go w.generateChunkAsync(c, neighbors)
}

// generateChunkAsync runs terrain generation and visibility in a goroutine.
func (w *World) generateChunkAsync(c *Chunk, neighbors [4]*Chunk) {
	w.generateTerrain(c)
	c.ComputeVisibility(func(ncx, ncz int) *Chunk {
		for _, n := range neighbors {
			if n != nil && n.CX == ncx && n.CZ == ncz {
				return n
			}
		}
		return nil
	})
	w.genResults <- c
}

// FlushGenerations blocks until all in-progress chunk generations complete.
func (w *World) FlushGenerations() {
	for len(w.generating) > 0 {
		c := <-w.genResults
		c.loaded = true
		key := [2]int{c.CX, c.CZ}
		w.chunks[key] = c
		delete(w.generating, key)
	}
}

// ProcessGenerations finalizes any chunks that finished async generation.
// Must be called from the main thread (not concurrently with map access).
func (w *World) ProcessGenerations() {
	for {
		select {
		case c := <-w.genResults:
			c.loaded = true
			key := [2]int{c.CX, c.CZ}
			w.chunks[key] = c
			delete(w.generating, key)
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
	// Rebuild neighbors so faces at the boundary become visible again
	for _, off := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
		if nc := w.GetChunk(cx+off[0], cz+off[1]); nc != nil {
			nc.ComputeVisibility(w.GetChunk)
		}
	}
	c.Unload()
	delete(w.chunks, key)
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
				} else if ly == height {
					if ly <= SeaLevel+1 {
						c.Blocks[lx][ly][lz] = blocks.Sand
					} else {
						c.Blocks[lx][ly][lz] = blocks.Grass
					}
				} else if ly >= height-3 {
					c.Blocks[lx][ly][lz] = blocks.Dirt
				} else if ly == 0 {
					c.Blocks[lx][ly][lz] = blocks.Bedrock
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

	// Unload distant chunks (skip chunks still generating)
	keysToDelete := make([][2]int, 0)
	for key := range w.chunks {
		if key[0] < cx-w.loadDist || key[0] > cx+w.loadDist ||
			key[1] < cz-w.loadDist || key[1] > cz+w.loadDist {
			keysToDelete = append(keysToDelete, key)
		}
	}
	for _, key := range keysToDelete {
		if _, generating := w.generating[key]; !generating {
			w.UnloadChunk(key[0], key[1])
		}
	}
}

// Render draws all loaded chunks.
// Opaque blocks: DrawCubeV (one cube per block position, with wireframes).
// Transparent blocks: individual faces via DrawTriangle3D (no z-fighting, correct adjacency).
func (w *World) Render() {
	for _, c := range w.chunks {
		if !c.loaded {
			continue
		}
		originX := float32(c.CX * ChunkWidth)
		originZ := float32(c.CZ * ChunkDepth)

		// Track drawn opaque block positions for deduplication
		seen := make(map[[3]int]struct{})

		for _, vf := range c.Visible {
			wx := originX + float32(vf.LocalX)
			wy := float32(vf.LocalY)
			wz := originZ + float32(vf.LocalZ)

			if vf.Block.IsTransparent() {
				// Transparent: draw each exposed face individually
				col := vf.Block.Color(faceToName(vf.Face))
				col.A = 140
				drawFace(wx, wy, wz, vf.Face, col)
			} else {
				// Opaque: deduplicate and draw as cube
				pos := [3]int{vf.LocalX, vf.LocalY, vf.LocalZ}
				if _, ok := seen[pos]; ok {
					continue
				}
				seen[pos] = struct{}{}

				px := wx + 0.5
				py := wy + 0.5
				pz := wz + 0.5
				col := vf.Block.Color("")
				rl.DrawCubeV(rl.NewVector3(px, py, pz), rl.NewVector3(1, 1, 1), col)
				darkCol := rl.NewColor(col.R/2, col.G/2, col.B/2, 255)
				rl.DrawCubeWires(rl.NewVector3(px, py, pz), 1.002, 1.002, 1.002, darkCol)
			}
		}
	}
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

// drawFace draws a single block face as two CCW triangles.
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
