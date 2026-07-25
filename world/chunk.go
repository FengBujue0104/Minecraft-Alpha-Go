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
	CX, CZ  int
	Blocks  [ChunkWidth][ChunkHeight][ChunkDepth]blocks.BlockType
	Visible []VisibleFace
	loaded  bool
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
func (c *Chunk) ComputeVisibility(worldGet func(cx, cz int) *Chunk) {
	if !c.loaded {
		return
	}

	c.Visible = c.Visible[:0]

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
					var neighborExists bool

					if nlx >= 0 && nlx < ChunkWidth && nly >= 0 && nly < ChunkHeight && nlz >= 0 && nlz < ChunkDepth {
						neighbor = c.Blocks[nlx][nly][nlz]
						neighborExists = true
					} else if worldGet != nil && nly >= 0 && nly < ChunkHeight {
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
						if nc := worldGet(ncx, ncz); nc != nil && nc.loaded {
							if nb, ok := nc.GetBlock(nlx, nly, nlz); ok {
								neighbor = nb
								neighborExists = true
							}
						}
					}

					if !neighborExists || neighbor == blocks.Air || (neighbor.IsTransparent() && neighbor != b) {
						c.Visible = append(c.Visible, VisibleFace{
							LocalX: lx,
							LocalY: ly,
							LocalZ: lz,
							Face:   faceIdx,
							Block:  b,
						})
					}
				}
			}
		}
	}
}

// Unload clears the visible face list.
func (c *Chunk) Unload() {
	c.Visible = nil
}
