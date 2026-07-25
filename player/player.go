package player

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

	"mc-go/blocks"
	"mc-go/world"
)

const (
	PlayerHeight     float32 = 1.62
	PlayerWidth      float32 = 0.6
	EyeHeight        float32 = 1.62
	Gravity          float32 = 25.0
	JumpVelocity     float32 = 9.0
	MoveSpeed        float32 = 8.0
	MouseSensitivity float32 = 0.005
	ReachDistance    float64 = 6.0
)

// Player holds all player state including camera and physics.
type Player struct {
	Camera         rl.Camera3D
	Position       rl.Vector3
	Velocity       rl.Vector3
	Yaw, Pitch     float32
	OnGround       bool
	InWater        bool
	skipMouse      bool     // skip mouse delta for one frame after cursor re-hide
	waterExitTimer float32 // grace period after leaving water for continued swimming
	SelectedBlock  blocks.BlockType
	TargetBlockPos [3]int   // World pos of the block being looked at
	TargetFace     int      // Face the player is looking at (-1 if none)
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
		Position: rl.NewVector3(x, y+EyeHeight, z),
		Target:   rl.NewVector3(x+1, y+EyeHeight, z), // yaw=0 faces +X
		Up:       rl.NewVector3(0, 1, 0),
		Fovy:     70,
		Projection: rl.CameraPerspective,
	}
	return p
}

// SkipNextMouseFrame tells the player to ignore the next mouse delta.
func (p *Player) SkipNextMouseFrame() {
	p.skipMouse = true
}

// Update handles per-frame input, camera, and block interaction.
func (p *Player) Update(w *world.World) {
	// Mouse look — cap first-frame spike
	mouseDelta := rl.GetMouseDelta()
	if p.skipMouse {
		mouseDelta = rl.Vector2{}
		p.skipMouse = false
	}
	if mouseDelta.X > 50 || mouseDelta.X < -50 {
		mouseDelta.X = 0
	}
	if mouseDelta.Y > 50 || mouseDelta.Y < -50 {
		mouseDelta.Y = 0
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

	// Movement input
	moveDir := rl.Vector3{}
	if rl.IsKeyDown(rl.KeyW) {
		moveDir.X += 1
	}
	if rl.IsKeyDown(rl.KeyS) {
		moveDir.X -= 1
	}
	if rl.IsKeyDown(rl.KeyA) {
		moveDir.Z -= 1
	}
	if rl.IsKeyDown(rl.KeyD) {
		moveDir.Z += 1
	}

	// Normalize horizontal movement
	if moveDir.X != 0 || moveDir.Z != 0 {
		length := float32(math.Sqrt(float64(moveDir.X*moveDir.X + moveDir.Z*moveDir.Z)))
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

	// Running (hold Shift to run)
	speed := MoveSpeed
	if rl.IsKeyDown(rl.KeyLeftShift) {
		speed *= 1.5
	}

	// Jump / swim
	if p.InWater || p.waterExitTimer > 0 {
		if rl.IsKeyDown(rl.KeySpace) {
			p.Velocity.Y = JumpVelocity * 0.55
		}
		if rl.IsKeyPressed(rl.KeySpace) && p.OnGround {
			p.Velocity.Y = JumpVelocity * 0.8
			p.OnGround = false
		}
	} else {
		if rl.IsKeyPressed(rl.KeySpace) && p.OnGround {
			p.Velocity.Y = JumpVelocity
			p.OnGround = false
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

	// Ray casting for block selection
	p.updateBlockSelection(w)

	// Place/Break blocks
	p.handleBlockActions(w)
}

// StepPhysics applies gravity and collision resolution at a fixed timestep.
func (p *Player) StepPhysics(w *world.World, dt float32) {
	if p.InWater {
		if !p.OnGround {
			p.Velocity.Y -= Gravity * dt * 0.3
		}
		if p.Velocity.Y < -3 {
			p.Velocity.Y = -3
		}
	} else {
		if !p.OnGround {
			p.Velocity.Y -= Gravity * dt
		}
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
	if p.collidesAtY(w, newPos) {
		if p.Velocity.Y < 0 {
			p.OnGround = true
		}
		newPos.Y = p.Position.Y
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

// collidesAtY checks vertical collisions across the full player width.
func (p *Player) collidesAtY(w *world.World, pos rl.Vector3) bool {
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
				return true
			}
		}
	}
	return false
}

func (p *Player) collidesAt(w *world.World, pos rl.Vector3) bool {
	hw := PlayerWidth/2.0 - 0.01 // slight inset avoids false collision with floor
	// Check corners of player bounding box, with epsilon on Y edges
	checkPoints := [][3]float32{
		{pos.X - hw, pos.Y + 0.01, pos.Z - hw},
		{pos.X + hw, pos.Y + 0.01, pos.Z - hw},
		{pos.X - hw, pos.Y + 0.01, pos.Z + hw},
		{pos.X + hw, pos.Y + 0.01, pos.Z + hw},
		{pos.X - hw, pos.Y + PlayerHeight - 0.01, pos.Z - hw},
		{pos.X + hw, pos.Y + PlayerHeight - 0.01, pos.Z - hw},
		{pos.X - hw, pos.Y + PlayerHeight - 0.01, pos.Z + hw},
		{pos.X + hw, pos.Y + PlayerHeight - 0.01, pos.Z + hw},
		{pos.X, pos.Y + PlayerHeight/2, pos.Z},
	}

	for _, cp := range checkPoints {
		bx := int(math.Floor(float64(cp[0])))
		by := int(math.Floor(float64(cp[1])))
		bz := int(math.Floor(float64(cp[2])))
		b := w.GetBlock(bx, by, bz)
		if b.IsSolid() {
			return true
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
		if b != blocks.Air && b != blocks.Water {
			p.TargetBlockPos = [3]int{vx, vy, vz}
			p.TargetFace = face
			return
		}

		// Step to the next voxel
		if tMaxX < tMaxY && tMaxX < tMaxZ {
			tMaxX += tDeltaX
			vx += stepX
			if stepX > 0 {
				face = 3 // Left face (entered from -X)
			} else {
				face = 2 // Right face (entered from +X)
			}
		} else if tMaxY < tMaxZ {
			tMaxY += tDeltaY
			vy += stepY
			if stepY > 0 {
				face = 1 // Bottom face (entered from -Y)
			} else {
				face = 0 // Top face (entered from +Y)
			}
		} else {
			tMaxZ += tDeltaZ
			vz += stepZ
			if stepZ > 0 {
				face = 5 // Back face (entered from -Z)
			} else {
				face = 4 // Front face (entered from +Z)
			}
		}

		// Check distance limit
		dx := float64(vx) + 0.5 - float64(start.X)
		dy := float64(vy) + 0.5 - float64(start.Y)
		dz := float64(vz) + 0.5 - float64(start.Z)
		if math.Sqrt(dx*dx+dy*dy+dz*dz) > ReachDistance {
			break
		}
	}
}

func (p *Player) handleBlockActions(w *world.World) {
	// Break block - left mouse button
	if rl.IsMouseButtonPressed(rl.MouseLeftButton) && p.TargetFace >= 0 {
		w.SetBlock(p.TargetBlockPos[0], p.TargetBlockPos[1], p.TargetBlockPos[2], blocks.Air)
	}

	// Place block - right mouse button
	if rl.IsMouseButtonPressed(rl.MouseRightButton) && p.TargetFace >= 0 {
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

		if w.GetBlock(px, py, pz) == blocks.Air {
			w.SetBlock(px, py, pz, p.SelectedBlock)
		}
	}

	// Scroll to change selected block
	scroll := rl.GetMouseWheelMove()
	if scroll != 0 {
		blockCycle := []blocks.BlockType{
			blocks.Stone, blocks.Dirt, blocks.Grass,
			blocks.Wood, blocks.Leaves, blocks.Sand,
			blocks.Glass, blocks.Water, blocks.Bedrock,
		}
		for i, b := range blockCycle {
			if b == p.SelectedBlock {
				idx := (i + int(scroll)) % len(blockCycle)
				if idx < 0 {
					idx += len(blockCycle)
				}
				p.SelectedBlock = blockCycle[idx]
				break
			}
		}
	}
}
