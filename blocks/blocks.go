package blocks

import rl "github.com/gen2brain/raylib-go/raylib"

// BlockType represents the type of a block.
type BlockType uint8

const (
	Air   BlockType = iota
	Grass
	Dirt
	Stone
	Wood
	Leaves
	Sand
	Water
	Glass
	Bedrock
)

// IsSolid returns true if the block should have collision.
func (b BlockType) IsSolid() bool {
	switch b {
	case Air, Water:
		return false
	default:
		return true
	}
}

// IsTransparent returns true if the block is see-through.
func (b BlockType) IsTransparent() bool {
	switch b {
	case Air, Water, Glass, Leaves:
		return true
	default:
		return false
	}
}

// Color returns the vertex color for the block (used when no texture atlas is loaded).
func (b BlockType) Color(face string) rl.Color {
	switch b {
	case Grass:
		if face == "top" {
			return rl.NewColor(95, 159, 53, 255) // Green top
		}
		if face == "bottom" {
			return rl.NewColor(134, 96, 62, 255) // Dirt bottom
		}
		return rl.NewColor(134, 134, 87, 255) // Grass-dirt side
	case Dirt:
		return rl.NewColor(134, 96, 62, 255)
	case Stone:
		return rl.NewColor(128, 128, 128, 255)
	case Wood:
		if face == "top" || face == "bottom" {
			return rl.NewColor(160, 130, 80, 255) // Rings
		}
		return rl.NewColor(140, 110, 70, 255) // Bark
	case Leaves:
		return rl.NewColor(48, 116, 48, 255)
	case Sand:
		return rl.NewColor(219, 211, 160, 255)
	case Water:
		return rl.NewColor(64, 128, 255, 160)
	case Glass:
		return rl.NewColor(200, 220, 255, 100)
	case Bedrock:
		return rl.NewColor(48, 48, 48, 255)
	default:
		return rl.Magenta // Error color
	}
}
