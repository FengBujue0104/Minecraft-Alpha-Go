package player

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

	"mc-go/blocks"
	"mc-go/input"
	"mc-go/world"
)

const (
	PlayerHeight     float32 = 1.62
	PlayerWidth      float32 = 0.6
	EyeHeight        float32 = 1.5
	Gravity          float32 = 25.0
	JumpVelocity     float32 = 9.0
	MoveSpeed        float32 = 8.0
	MouseSensitivity float32 = 0.005
	ReachDistance    float64 = 6.0

	// MaxStepBlocks caps how far the player may move vertically in one
	// physics step. collidesAtY samples a single block layer rather than
	// sweeping, so a step of a full block or more can skip a thin floor
	// entirely. Must stay below 1.0.
	MaxStepBlocks float32 = 0.9
	// MaxSinkSpeed limits downward speed in water.
	MaxSinkSpeed float32 = 3.0
	// FlightSpeed is deliberately independent from running speed so vertical
	// flight feels predictable even while sprint is held.
	FlightSpeed float32 = 8.0
)

// HotbarItems is the fixed nine-slot inventory shown by the HUD. Water is a
// bucket tool: it places water with right-click and only removes water with
// left-click.
var HotbarItems = []blocks.BlockType{
	blocks.Stone, blocks.Dirt, blocks.Grass,
	blocks.Wood, blocks.Leaves, blocks.Sand,
	blocks.Glass, blocks.Bedrock, blocks.Water,
}

// BlockAction identifies a successful world edit for the audio system.
type BlockAction uint8

const (
	NoBlockAction BlockAction = iota
	BreakBlock
	PlaceBlock
)

// Player holds all player state including camera and physics.
type Player struct {
	Camera         rl.Camera3D
	Position       rl.Vector3
	Velocity       rl.Vector3
	Yaw, Pitch     float32
	OnGround       bool
	InWater        bool
	Flying         bool
	AutoJump       bool    // 贴地移动遇 1 格高台阶自动起跳（设置项）
	mouseWarmup    int     // 光标（重）捕获后忽略视角增量的帧数：指针跳变
	                       // 的残余 delta 可能跨多帧到达，单帧丢弃不够
	waterExitTimer float32 // grace period after leaving water for continued swimming
	SelectedBlock  blocks.BlockType
	TargetBlockPos [3]int // World pos of the block being looked at
	TargetFace     int    // Face the player is looking at (-1 if none)
	lastAction     BlockAction
}

// NewPlayer creates a player at the given position.
func NewPlayer(x, y, z float32) *Player {
	p := &Player{
		Position:      rl.NewVector3(x, y, z),
		SelectedBlock: blocks.Stone,
		Yaw:           0,
		Pitch:         0,
	}
	p.Camera = rl.Camera3D{
		Position:   rl.NewVector3(x, y+EyeHeight, z),
		Target:     rl.NewVector3(x+1, y+EyeHeight, z), // yaw=0 faces +X
		Up:         rl.NewVector3(0, 1, 0),
		Fovy:       70,
		Projection: rl.CameraPerspective,
	}
	return p
}

// SkipNextMouseFrame tells the player to ignore the next mouse delta.
func (p *Player) SkipNextMouseFrame() { p.SkipNextMouseFrames(1) }

// SkipNextMouseFrames ignores look deltas for the next n frames. Used after
// cursor (re)capture: the pointer-jump residual can arrive across several
// frames (slow frames under software rendering especially).
func (p *Player) SkipNextMouseFrames(n int) {
	if n > p.mouseWarmup {
		p.mouseWarmup = n
	}
}

// SelectHotbarSlot selects a zero-based hotbar slot. It is kept separate from
// raylib input so the HUD and tests share exactly the same inventory mapping.
func (p *Player) SelectHotbarSlot(slot int) {
	if slot >= 0 && slot < len(HotbarItems) {
		p.SelectedBlock = HotbarItems[slot]
	}
}

// SelectHotbarBlock selects the slot holding the given block type (save
// loading). Unknown blocks keep the current selection.
func (p *Player) SelectHotbarBlock(b blocks.BlockType) {
	for _, item := range HotbarItems {
		if item == b {
			p.SelectedBlock = item
			return
		}
	}
}

// SelectedHotbarSlot returns the selected zero-based slot.
func (p *Player) SelectedHotbarSlot() int {
	for i, item := range HotbarItems {
		if item == p.SelectedBlock {
			return i
		}
	}
	return 0
}

// ConsumeBlockAction returns and clears the most recent successful block edit.
func (p *Player) ConsumeBlockAction() BlockAction {
	action := p.lastAction
	p.lastAction = NoBlockAction
	return action
}

// Update handles per-frame input, camera, and block interaction. Input arrives
// as a State snapshot so the same code serves keyboard/mouse and touch play.
func (p *Player) Update(in input.State, w *world.World) {
	// Look — cap first-frame spike. 钳制而非清零：快速拖动时单帧位移
	// 超过阈值很常见，清零会让视角瞬间停顿（不丝滑的主要来源）。
	mouseDelta := rl.Vector2{X: in.LookDX, Y: in.LookDY}
	if p.mouseWarmup > 0 {
		mouseDelta = rl.Vector2{}
		p.mouseWarmup--
	}
	const maxDelta = 90
	if mouseDelta.X > maxDelta {
		mouseDelta.X = maxDelta
	} else if mouseDelta.X < -maxDelta {
		mouseDelta.X = -maxDelta
	}
	if mouseDelta.Y > maxDelta {
		mouseDelta.Y = maxDelta
	} else if mouseDelta.Y < -maxDelta {
		mouseDelta.Y = -maxDelta
	}
	p.Yaw += mouseDelta.X * MouseSensitivity
	p.Pitch += mouseDelta.Y * MouseSensitivity
	if p.Pitch > 1.5 {
		p.Pitch = 1.5
	}
	if p.Pitch < -1.5 {
		p.Pitch = -1.5
	}

	// Check if player is in water (center of body)
	wasInWater := p.InWater
	p.InWater = w.GetBlock(
		int(math.Floor(float64(p.Position.X))),
		int(math.Floor(float64(p.Position.Y+PlayerHeight/2))),
		int(math.Floor(float64(p.Position.Z))),
	) == blocks.Water
	if wasInWater && !p.InWater && p.Velocity.Y > 0 {
		p.waterExitTimer = 0.2
	}
	if p.waterExitTimer > 0 {
		p.waterExitTimer -= rl.GetFrameTime()
	}

	if in.FlyToggle {
		p.Flying = !p.Flying
		p.Velocity.Y = 0
		if p.Flying {
			p.OnGround = false
		}
	}

	// Movement input
	moveDir := rl.Vector3{X: in.MoveFwd, Z: in.MoveRight}

	// Normalize horizontal movement only above full deflection so keyboard
	// stays all-or-nothing while an analog joystick keeps partial magnitudes.
	if length := float32(math.Sqrt(float64(moveDir.X*moveDir.X + moveDir.Z*moveDir.Z))); length > 1 {
		moveDir.X /= length
		moveDir.Z /= length
	}

	// Rotate movement by yaw
	cos := float32(math.Cos(float64(p.Yaw)))
	sin := float32(math.Sin(float64(p.Yaw)))
	worldMove := rl.Vector3{
		X: moveDir.X*cos - moveDir.Z*sin,
		Y: 0,
		Z: moveDir.X*sin + moveDir.Z*cos,
	}

	// Running (sprint)
	speed := MoveSpeed
	if in.Run {
		speed *= 1.5
	}

	// Jump / swim / flight. Flight uses held input rather than edge-triggered
	// presses, allowing continuous ascent and descent.
	if p.Flying {
		if in.JumpHeld {
			p.Velocity.Y = FlightSpeed
		} else if in.DescendHeld {
			p.Velocity.Y = -FlightSpeed
		} else {
			p.Velocity.Y = 0
		}
	} else if p.InWater || p.waterExitTimer > 0 {
		if in.JumpHeld {
			p.Velocity.Y = JumpVelocity * 0.55
		}
		if in.JumpPressed && p.OnGround {
			p.Velocity.Y = JumpVelocity * 0.8
			p.OnGround = false
		}
	} else {
		if in.JumpPressed && p.OnGround {
			p.Velocity.Y = JumpVelocity
			p.OnGround = false
		}
	}

	// 自动跳跃：贴地移动时，前方约半步处若是 1 格高台阶且其后两格净空，
	// 则自动起跳。放在手动跳跃之后，按住跳跃时手动输入优先。
	if p.AutoJump && p.OnGround && !p.InWater && p.waterExitTimer <= 0 &&
		(worldMove.X != 0 || worldMove.Z != 0) {
		if l := float32(math.Sqrt(float64(worldMove.X*worldMove.X + worldMove.Z*worldMove.Z))); l > 0.001 {
			dirX, dirZ := worldMove.X/l, worldMove.Z/l
			aheadX := p.Position.X + dirX*(PlayerWidth/2+0.35)
			aheadZ := p.Position.Z + dirZ*(PlayerWidth/2+0.35)
			fx := int(math.Floor(float64(aheadX)))
			fz := int(math.Floor(float64(aheadZ)))
			feet := int(math.Floor(float64(p.Position.Y + 0.05)))
			if w.GetBlock(fx, feet, fz).IsSolid() &&
				!w.GetBlock(fx, feet+1, fz).IsSolid() &&
				!w.GetBlock(fx, feet+2, fz).IsSolid() {
				p.Velocity.Y = JumpVelocity
				p.OnGround = false
			}
		}
	}

	// Apply horizontal movement
	p.Velocity.X = worldMove.X * speed
	p.Velocity.Z = worldMove.Z * speed

	// Update camera
	p.Camera.Position = rl.NewVector3(
		p.Position.X,
		p.Position.Y+EyeHeight,
		p.Position.Z,
	)
	lookDir := rl.Vector3{
		X: float32(math.Cos(float64(p.Pitch)) * math.Cos(float64(p.Yaw))),
		Y: -float32(math.Sin(float64(p.Pitch))),
		Z: float32(math.Cos(float64(p.Pitch)) * math.Sin(float64(p.Yaw))),
	}
	p.Camera.Target = rl.NewVector3(
		p.Camera.Position.X+lookDir.X,
		p.Camera.Position.Y+lookDir.Y,
		p.Camera.Position.Z+lookDir.Z,
	)

	p.updateHotbarSelection(in)

	// Ray casting and block interaction happen after item selection so pressing
	// a number and clicking in one frame uses the newly selected tool.
	p.updateBlockSelection(w)
	p.handleBlockActions(in, w)
}

// StepPhysics applies gravity and collision resolution at a fixed timestep.
func (p *Player) StepPhysics(w *world.World, dt float32) {
	if p.Flying {
		p.moveWithCollision(w, dt)
		return
	}
	if p.InWater {
		if !p.OnGround {
			p.Velocity.Y -= Gravity * dt * 0.3
		}
		if p.Velocity.Y < -MaxSinkSpeed {
			p.Velocity.Y = -MaxSinkSpeed
		}
	} else {
		if !p.OnGround {
			p.Velocity.Y -= Gravity * dt
		}
	}

	// Clamp so one step can never cross a whole block, in air or water.
	// Derived from dt rather than hardcoded so the bound holds if the
	// physics rate changes.
	maxFall := MaxStepBlocks / dt
	if p.Velocity.Y < -maxFall {
		p.Velocity.Y = -maxFall
	}

	p.moveWithCollision(w, dt)
}

func (p *Player) moveWithCollision(w *world.World, dt float32) {
	// Separately check each axis for simple AABB collision
	newPos := p.Position

	// X axis
	newPos.X += p.Velocity.X * dt
	if p.collidesAt(w, newPos) {
		newPos.X = p.Position.X
		p.Velocity.X = 0
	}

	// Y axis
	newPos.Y += p.Velocity.Y * dt
	// Floor clamp — don't fall below 0
	if newPos.Y < 0 {
		newPos.Y = 0
		p.Velocity.Y = 0
		p.OnGround = true
	}
	// Ceiling clamp — don't go above world height
	if newPos.Y > float32(world.ChunkHeight-1) {
		newPos.Y = float32(world.ChunkHeight - 1)
		p.Velocity.Y = 0
	}
	if hit, blockY := p.collidesAtY(w, newPos); hit {
		if p.Velocity.Y < 0 {
			p.OnGround = true
			// Snap to the top face of the block landed on. Reverting to the
			// previous Y instead would leave the player hovering by up to a
			// step's worth of travel after a fast fall, then visibly settle
			// over the next several frames.
			newPos.Y = float32(blockY) + 1.0
		} else {
			newPos.Y = p.Position.Y
		}
		p.Velocity.Y = 0
	} else {
		p.OnGround = false
	}

	// Z axis
	newPos.Z += p.Velocity.Z * dt
	if p.collidesAt(w, newPos) {
		newPos.Z = p.Position.Z
		p.Velocity.Z = 0
	}

	p.Position = newPos
}

// collidesAtY checks vertical collisions across the full player width and
// reports which block layer was tested, so the caller can snap to its surface.
func (p *Player) collidesAtY(w *world.World, pos rl.Vector3) (bool, int) {
	hw := PlayerWidth/2.0 - 0.01
	minBX := int(math.Floor(float64(pos.X - hw)))
	maxBX := int(math.Floor(float64(pos.X + hw)))
	minBZ := int(math.Floor(float64(pos.Z - hw)))
	maxBZ := int(math.Floor(float64(pos.Z + hw)))

	var checkY int
	if p.Velocity.Y > 0 {
		// Rising — check head for ceiling
		checkY = int(math.Floor(float64(pos.Y + PlayerHeight - 0.01)))
	} else if p.Velocity.Y < 0 {
		// Falling — check feet for ground penetration
		checkY = int(math.Floor(float64(pos.Y + 0.01)))
	} else {
		// Stationary — check block below feet for ground contact
		checkY = int(math.Floor(float64(pos.Y - 0.01)))
	}

	for bx := minBX; bx <= maxBX; bx++ {
		for bz := minBZ; bz <= maxBZ; bz++ {
			if w.GetBlock(bx, checkY, bz).IsSolid() {
				return true, checkY
			}
		}
	}
	return false, checkY
}

// collidesAt reports whether the player's box at pos overlaps a solid block.
// Every block layer the box spans is tested at every column it covers: with
// PlayerHeight 1.62 the box straddles three layers at most fractional heights,
// and sampling fixed points would leave the middle layer checked only along
// the centre axis, letting the player walk into a block at a corner.
func (p *Player) collidesAt(w *world.World, pos rl.Vector3) bool {
	hw := PlayerWidth/2.0 - 0.01 // slight inset avoids false collision with floor
	minBX := int(math.Floor(float64(pos.X - hw)))
	maxBX := int(math.Floor(float64(pos.X + hw)))
	minBY := int(math.Floor(float64(pos.Y + 0.01)))
	maxBY := int(math.Floor(float64(pos.Y + PlayerHeight - 0.01)))
	minBZ := int(math.Floor(float64(pos.Z - hw)))
	maxBZ := int(math.Floor(float64(pos.Z + hw)))

	for bx := minBX; bx <= maxBX; bx++ {
		for by := minBY; by <= maxBY; by++ {
			for bz := minBZ; bz <= maxBZ; bz++ {
				if w.GetBlock(bx, by, bz).IsSolid() {
					return true
				}
			}
		}
	}
	return false
}

// updateBlockSelection does ray casting to find the block the player is looking at.
func (p *Player) updateBlockSelection(w *world.World) {
	p.TargetBlockPos = [3]int{0, 0, 0}
	p.TargetFace = -1

	// Use DDA (Digital Differential Analyzer) on the three axes
	start := p.Camera.Position
	dir := rl.NewVector3(
		p.Camera.Target.X-start.X,
		p.Camera.Target.Y-start.Y,
		p.Camera.Target.Z-start.Z,
	)
	// Normalize direction (guard against zero-length)
	length := float32(math.Sqrt(float64(dir.X*dir.X + dir.Y*dir.Y + dir.Z*dir.Z)))
	if length < 0.0001 {
		return
	}
	dir.X /= length
	dir.Y /= length
	dir.Z /= length

	// DDA algorithm
	const maxSteps = 100

	// Current voxel
	vx := int(math.Floor(float64(start.X)))
	vy := int(math.Floor(float64(start.Y)))
	vz := int(math.Floor(float64(start.Z)))

	// Step direction
	var stepX, stepY, stepZ int
	if dir.X >= 0 {
		stepX = 1
	} else {
		stepX = -1
	}
	if dir.Y >= 0 {
		stepY = 1
	} else {
		stepY = -1
	}
	if dir.Z >= 0 {
		stepZ = 1
	} else {
		stepZ = -1
	}

	// tMaxX, tMaxY, tMaxZ: distance to next voxel boundary
	nextX := float64(vx)
	if dir.X >= 0 {
		nextX += 1
	}
	nextY := float64(vy)
	if dir.Y >= 0 {
		nextY += 1
	}
	nextZ := float64(vz)
	if dir.Z >= 0 {
		nextZ += 1
	}

	tMaxX := (nextX - float64(start.X)) / float64(dir.X)
	tMaxY := (nextY - float64(start.Y)) / float64(dir.Y)
	tMaxZ := (nextZ - float64(start.Z)) / float64(dir.Z)
	if dir.X == 0 {
		tMaxX = math.MaxFloat64
	}
	if dir.Y == 0 {
		tMaxY = math.MaxFloat64
	}
	if dir.Z == 0 {
		tMaxZ = math.MaxFloat64
	}

	tDeltaX := math.Abs(float64(stepX) / float64(dir.X))
	tDeltaY := math.Abs(float64(stepY) / float64(dir.Y))
	tDeltaZ := math.Abs(float64(stepZ) / float64(dir.Z))
	if dir.X == 0 {
		tDeltaX = math.MaxFloat64
	}
	if dir.Y == 0 {
		tDeltaY = math.MaxFloat64
	}
	if dir.Z == 0 {
		tDeltaZ = math.MaxFloat64
	}

	// Which face was entered from
	face := -1

	for step := 0; step < maxSteps; step++ {
		b := w.GetBlock(vx, vy, vz)
		// Water is normally transparent to selection so the player can build
		// onto lake beds. The bucket specifically targets water so it can
		// remove it with its left-click action.
		if b != blocks.Air && (b != blocks.Water || p.SelectedBlock == blocks.Water) {
			p.TargetBlockPos = [3]int{vx, vy, vz}
			p.TargetFace = face
			return
		}

		// Step to the next voxel, keeping the ray parameter at which we
		// entered it. dir is normalised, so t is distance along the ray.
		var t float64
		if tMaxX < tMaxY && tMaxX < tMaxZ {
			t = tMaxX
			tMaxX += tDeltaX
			vx += stepX
			if stepX > 0 {
				face = 3 // Left face (entered from -X)
			} else {
				face = 2 // Right face (entered from +X)
			}
		} else if tMaxY < tMaxZ {
			t = tMaxY
			tMaxY += tDeltaY
			vy += stepY
			if stepY > 0 {
				face = 1 // Bottom face (entered from -Y)
			} else {
				face = 0 // Top face (entered from +Y)
			}
		} else {
			t = tMaxZ
			tMaxZ += tDeltaZ
			vz += stepZ
			if stepZ > 0 {
				face = 5 // Back face (entered from -Z)
			} else {
				face = 4 // Front face (entered from +Z)
			}
		}

		// Measuring to the voxel centre instead would vary by up to ~0.87
		// blocks with direction, making diagonal reach longer than axial.
		if t > ReachDistance {
			break
		}
	}
}

func (p *Player) handleBlockActions(in input.State, w *world.World) {
	// Break block - left mouse button / touch tap
	if in.BreakPressed && p.TargetFace >= 0 {
		target := w.GetBlock(p.TargetBlockPos[0], p.TargetBlockPos[1], p.TargetBlockPos[2])
		if p.SelectedBlock == blocks.Water {
			if target == blocks.Water {
				w.SetBlock(p.TargetBlockPos[0], p.TargetBlockPos[1], p.TargetBlockPos[2], blocks.Air)
				p.lastAction = BreakBlock
			}
		} else if target != blocks.Water {
			w.SetBlock(p.TargetBlockPos[0], p.TargetBlockPos[1], p.TargetBlockPos[2], blocks.Air)
			p.lastAction = BreakBlock
		}
	}

	// Place block - right mouse button / touch long-press
	if in.PlacePressed && p.TargetFace >= 0 {
		// Calculate placement position (adjacent to the hit face)
		px, py, pz := p.TargetBlockPos[0], p.TargetBlockPos[1], p.TargetBlockPos[2]
		switch p.TargetFace {
		case 0: // Top
			py++
		case 1: // Bottom
			py--
		case 2: // Right (+X)
			px++
		case 3: // Left (-X)
			px--
		case 4: // Front (+Z)
			pz++
		case 5: // Back (-Z)
			pz--
		}

		// Don't place inside the player — check full bounding box
		hw := PlayerWidth/2.0 - 0.01
		minBX := int(math.Floor(float64(p.Position.X - hw)))
		maxBX := int(math.Floor(float64(p.Position.X + hw)))
		minBY := int(math.Floor(float64(p.Position.Y + 0.01)))
		maxBY := int(math.Floor(float64(p.Position.Y + PlayerHeight - 0.01)))
		minBZ := int(math.Floor(float64(p.Position.Z - hw)))
		maxBZ := int(math.Floor(float64(p.Position.Z + hw)))
		for bx := minBX; bx <= maxBX; bx++ {
			for by := minBY; by <= maxBY; by++ {
				for bz := minBZ; bz <= maxBZ; bz++ {
					if px == bx && py == by && pz == bz {
						return
					}
				}
			}
		}

		// Anything non-solid (air or water) can be built over. The ray passes
		// through water, so water can never be targeted directly; refusing to
		// overwrite it too would make a misplaced block of water permanent and
		// leave lakes unbuildable.
		if !w.GetBlock(px, py, pz).IsSolid() {
			w.SetBlock(px, py, pz, p.SelectedBlock)
			p.lastAction = PlaceBlock
		}
	}
}

func (p *Player) updateHotbarSelection(in input.State) {
	// A direct slot selection (number keys on desktop, tapping a slot on
	// touch) always wins.
	if in.HotbarSlot >= 0 && in.HotbarSlot < len(HotbarItems) {
		p.SelectHotbarSlot(in.HotbarSlot)
		return
	}

	// A wheel roll away from the player is positive on desktop; treat that as
	// moving left through the bar, matching the physical scroll direction.
	if in.HotbarDelta != 0 {
		scroll := in.HotbarDelta
		idx := (p.SelectedHotbarSlot() - scroll) % len(HotbarItems)
		if idx < 0 {
			idx += len(HotbarItems)
		}
		p.SelectHotbarSlot(idx)
	}
}
