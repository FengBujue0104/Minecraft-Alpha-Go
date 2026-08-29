//go:build android

package input

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
)

const (
	tapMaxDuration  float32 = 0.25 // 轻点判定上限（秒），超过不算破坏
	longPressTime   float32 = 0.40 // 长按触发放置（秒）
	breakRepeatTime float32 = 0.30 // 破坏按钮按住时的连续触发间隔（秒）
	lookSensitivity float32 = 0.5  // 触点像素增量乘数，叠加在玩家 MouseSensitivity 上
	joyDeadzone     float32 = 0.14 // 摇杆死区（基座半径比例）
	joyRunThreshold float32 = 0.92 // 推满判定为冲刺（比例）
)

// 触点角色。rolePause/roleFly/roleHotbar/roleResume/rolePlace 在按下的那一
// 帧立即触发（等价于 IsKeyPressed 的沿语义）；roleJump 沿+持续双语义；
// roleBreak 沿触发且按住时按固定间隔连发；roleDescend 仅持续；
// roleJoystick/roleLook 跟踪位移。
const (
	roleNone = iota
	roleJoystick
	roleLook
	roleJump
	roleFly
	roleDescend
	roleBreak
	rolePlace
	rolePause
	roleResume
	roleHotbar
)

type finger struct {
	id             int32
	x, y           float32
	startX, startY float32
	lastX, lastY   float32
	role           int
	moved          bool    // 位移超出 slop，取消轻点/长按
	downTime       float32 // rl.GetTime 秒
	longFired      bool
	lastFire       float32 // roleBreak 上次连发时刻
}

var (
	fingers   = map[int32]*finger{}
	joyCenter rl.Vector2
	joyKnob   rl.Vector2
	joyOn     bool
)

// platformRead 把多点触控按区域路由：左下摇杆、右侧按钮、快捷栏槽位、
// 其余区域为"视角/手势"区（滑动转视角、轻点破坏、长按放置）。
// raylib 把 touch[0] 同时映射为鼠标，但那只支持单点，无法边走边转视角，
// 因此这里按 GetTouchPointId 独立跟踪每个触点，不依赖该映射。
func platformRead(l *Layout, flying, paused bool) State {
	st := State{HotbarSlot: -1}
	now := float32(rl.GetTime())
	seen := make(map[int32]bool, len(fingers))

	count := int(rl.GetTouchPointCount())
	for i := 0; i < count; i++ {
		id := rl.GetTouchPointId(int32(i))
		pos := rl.GetTouchPosition(int32(i))
		seen[id] = true

		f, ok := fingers[id]
		if !ok {
			f = &finger{
				id: id, x: pos.X, y: pos.Y,
				startX: pos.X, startY: pos.Y,
				lastX: pos.X, lastY: pos.Y,
				downTime: now,
				lastFire: now,
			}
			fingers[id] = f
			f.role = assignRole(l, flying, paused, pos, &st)
		}

		dx, dy := pos.X-f.lastX, pos.Y-f.lastY
		f.x, f.y = pos.X, pos.Y
		f.lastX, f.lastY = pos.X, pos.Y

		switch f.role {
		case roleJoystick:
			// 动态摇杆：基座中心 = 按下点（assignRole 时记入 joyCenter）
			vx, vy := pos.X-joyCenter.X, pos.Y-joyCenter.Y
			r := float32(math.Sqrt(float64(vx*vx + vy*vy)))
			clampR := r
			if clampR > l.JoystickRadius {
				clampR = l.JoystickRadius
			}
			nx, ny := float32(0), float32(0)
			if r > 0.0001 {
				nx, ny = vx/r, vy/r
			}
			joyKnob = rl.NewVector2(joyCenter.X+nx*clampR, joyCenter.Y+ny*clampR)
			mag := clampR / l.JoystickRadius
			if mag < joyDeadzone {
				mag = 0
			}
			st.MoveRight = nx * mag
			st.MoveFwd = -ny * mag
			st.Run = mag > joyRunThreshold
		case roleLook:
			st.LookDX += dx * lookSensitivity
			st.LookDY += dy * lookSensitivity
			if !f.moved {
				sx, sy := f.x-f.startX, f.y-f.startY
				if sx*sx+sy*sy > l.Slop*l.Slop {
					f.moved = true
				}
			}
			// 长按放置：单次触发；继续按住不再重复（破坏/放置都作用于准星）。
			if !f.moved && !f.longFired && now-f.downTime >= longPressTime {
				f.longFired = true
				st.PlacePressed = true
			}
		case roleJump:
			st.JumpHeld = true
		case roleDescend:
			st.DescendHeld = true
		case roleBreak:
			// 按住连续破坏：按下沿 + 固定间隔连发
			if now-f.lastFire >= breakRepeatTime {
				f.lastFire = now
				st.BreakPressed = true
			}
		}
	}

	// 本帧消失的触点 = 抬起
	for id, f := range fingers {
		if seen[id] {
			continue
		}
		switch f.role {
		case roleLook:
			if !f.moved && !f.longFired && now-f.downTime < tapMaxDuration {
				st.BreakPressed = true
			}
		case roleJoystick:
			joyOn = false
			joyKnob = rl.Vector2{}
		}
		delete(fingers, id)
	}

	// 安卓系统返回键 = 暂停切换
	if rl.IsKeyPressed(rl.KeyBack) {
		st.PauseToggle = true
	}
	return st
}

// assignRole 在触点首次出现时决定它的角色；沿触发类控件在这里就地触发。
func assignRole(l *Layout, flying, paused bool, pos rl.Vector2, st *State) int {
	if pointIn(l.PauseBtn, pos) {
		st.PauseToggle = true
		return rolePause
	}
	if paused {
		// 暂停时点中央按钮恢复；其余触点不产生世界输入。
		if pointIn(l.ResumeBtn, pos) {
			st.PauseToggle = true
			return roleResume
		}
		return roleLook
	}
	if pointIn(l.JumpBtn, pos) {
		st.JumpPressed = true
		return roleJump
	}
	if pointIn(l.FlyBtn, pos) {
		st.FlyToggle = true
		return roleFly
	}
	if flying && pointIn(l.DescendBtn, pos) {
		st.DescendHeld = true
		return roleDescend
	}
	if pointIn(l.BreakBtn, pos) {
		st.BreakPressed = true // 按下沿；连发由 roleBreak 分支按间隔续触
		return roleBreak
	}
	if pointIn(l.PlaceBtn, pos) {
		st.PlacePressed = true
		return rolePlace
	}
	for i, r := range l.HotbarSlots {
		if pointIn(r, pos) {
			st.HotbarSlot = i
			return roleHotbar
		}
	}
	if pointIn(l.JoystickZone, pos) {
		// 动态摇杆：按下点即基座中心
		joyCenter = pos
		joyOn = true
		joyKnob = pos
		return roleJoystick
	}
	return roleLook
}

func pointIn(r rl.Rectangle, p rl.Vector2) bool {
	return p.X >= r.X && p.X < r.X+r.Width && p.Y >= r.Y && p.Y < r.Y+r.Height
}

func platformJoystick() (center rl.Vector2, knob rl.Vector2, active bool) {
	return joyCenter, joyKnob, joyOn
}
