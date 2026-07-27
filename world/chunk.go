package world

import (
	"mc-go/blocks"
)

const (
	ChunkWidth  = 16
	ChunkHeight = 128
	ChunkDepth  = 16
)

// VisibleFace records a single exposed block face to render.
type VisibleFace struct {
	LocalX, LocalY, LocalZ int
	Face                   int // 0=Top(+Y), 1=Bottom(-Y), 2=Right(+X), 3=Left(-X), 4=Front(+Z), 5=Back(-Z)
	Block                  blocks.BlockType
}

// Chunk represents a 16x128x16 section of the world.
type Chunk struct {
	CX, CZ int
	Blocks [ChunkWidth][ChunkHeight][ChunkDepth]blocks.BlockType
	// Visible faces are split by material so Render can draw all opaque
	// geometry before any blending, rather than interleaving the two.
	VisibleOpaque      []VisibleFace
	VisibleTransparent []VisibleFace
	loaded             bool
}

func NewChunk(cx, cz int) *Chunk {
	return &Chunk{CX: cx, CZ: cz}
}

func (c *Chunk) GetBlock(lx, ly, lz int) (blocks.BlockType, bool) {
	if lx < 0 || lx >= ChunkWidth || ly < 0 || ly >= ChunkHeight || lz < 0 || lz >= ChunkDepth {
		return blocks.Air, false
	}
	return c.Blocks[lx][ly][lz], true
}

func (c *Chunk) SetBlock(lx, ly, lz int, b blocks.BlockType) {
	if lx < 0 || lx >= ChunkWidth || ly < 0 || ly >= ChunkHeight || lz < 0 || lz >= ChunkDepth {
		return
	}
	c.Blocks[lx][ly][lz] = b
}

// ComputeVisibility scans the chunk and records every exposed block face.
// Must run on the main thread: it resolves cross-chunk neighbors through
// worldGet, so it reads chunks the main thread may concurrently mutate.
func (c *Chunk) ComputeVisibility(worldGet func(cx, cz int) *Chunk) {
	c.VisibleOpaque = c.VisibleOpaque[:0]
	c.VisibleTransparent = c.VisibleTransparent[:0]

	// Face indices match DDA raycast: 0=+Y(top), 1=-Y(bottom), 2=+X(right), 3=-X(left), 4=+Z(front), 5=-Z(back)
	directions := [6][3]int{
		{0, 1, 0}, {0, -1, 0}, {1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1},
	}

	for lx := 0; lx < ChunkWidth; lx++ {
		for ly := 0; ly < ChunkHeight; ly++ {
			for lz := 0; lz < ChunkDepth; lz++ {
				b := c.Blocks[lx][ly][lz]
				if b == blocks.Air {
					continue
				}

				for faceIdx, d := range directions {
					nlx, nly, nlz := lx+d[0], ly+d[1], lz+d[2]

					var neighbor blocks.BlockType
					known := false  // the adjacent block's type is known
					sealed := false // nothing can ever see this face

					switch {
					case nlx >= 0 && nlx < ChunkWidth && nly >= 0 && nly < ChunkHeight && nlz >= 0 && nlz < ChunkDepth:
						neighbor = c.Blocks[nlx][nly][nlz]
						known = true

					case nly < 0:
						// World floor. moveWithCollision clamps the player to
						// y >= 0, so the underside of y=0 is unreachable.
						sealed = true

					case nly >= ChunkHeight:
						// Sky: stay exposed, or a tower built to y=127 loses
						// its top face. This is the one unbounded direction.

					case worldGet == nil:
						// No way to resolve the neighbour; take the
						// conservative side rather than emitting a wall.
						sealed = true

					default:
						ncx, ncz := c.CX, c.CZ
						if nlx < 0 {
							ncx--
							nlx = ChunkWidth - 1
						} else if nlx >= ChunkWidth {
							ncx++
							nlx = 0
						}
						if nlz < 0 {
							ncz--
							nlz = ChunkDepth - 1
						} else if nlz >= ChunkDepth {
							ncz++
							nlz = 0
						}
						// A chunk in w.chunks is always loaded: insertChunk
						// sets the flag before the map write.
						if nc := worldGet(ncx, ncz); nc != nil {
							if nb, ok := nc.GetBlock(nlx, nly, nlz); ok {
								neighbor = nb
								known = true
							}
						} else {
							// Unloaded neighbour. The face points outward off
							// the edge of the loaded window and the player is
							// always inside it, so it can never be seen —
							// emitting it used to cost a solid 16-wide wall
							// per exposed chunk border. insertChunk and
							// UnloadChunk both markDirty the four neighbours,
							// so this is re-evaluated when that changes.
							sealed = true
						}
					}

					if sealed {
						continue
					}

					if !known || neighbor == blocks.Air || (neighbor.IsTransparent() && neighbor != b) {
						vf := VisibleFace{
							LocalX: lx,
							LocalY: ly,
							LocalZ: lz,
							Face:   faceIdx,
							Block:  b,
						}
						if b.IsTransparent() {
							c.VisibleTransparent = append(c.VisibleTransparent, vf)
						} else {
							c.VisibleOpaque = append(c.VisibleOpaque, vf)
						}
					}
				}
			}
		}
	}
}

// Unload clears the visible face lists.
func (c *Chunk) Unload() {
	c.VisibleOpaque = nil
	c.VisibleTransparent = nil
}
