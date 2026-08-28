//go:build !android

package input

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

// platformRead 直读键盘/鼠标，与移植前 player/main 的直读代码一一对应，
// 保证桌面行为零变化。
func platformRead(l *Layout, flying, paused bool) State {
	st := State{HotbarSlot: -1}

	md := rl.GetMouseDelta()
	st.LookDX, st.LookDY = md.X, md.Y

	if rl.IsKeyDown(rl.KeyW) {
		st.MoveFwd += 1
	}
	if rl.IsKeyDown(rl.KeyS) {
		st.MoveFwd -= 1
	}
	if rl.IsKeyDown(rl.KeyD) {
		st.MoveRight += 1
	}
	if rl.IsKeyDown(rl.KeyA) {
		st.MoveRight -= 1
	}
	st.Run = rl.IsKeyDown(rl.KeyLeftShift)

	st.JumpPressed = rl.IsKeyPressed(rl.KeySpace)
	st.JumpHeld = rl.IsKeyDown(rl.KeySpace)
	st.FlyToggle = rl.IsKeyPressed(rl.KeyF)
	st.DescendHeld = rl.IsKeyDown(rl.KeyLeftControl)

	st.BreakPressed = rl.IsMouseButtonPressed(rl.MouseLeftButton)
	st.PlacePressed = rl.IsMouseButtonPressed(rl.MouseRightButton)

	for i := 0; i < len(l.HotbarSlots) && i < 9; i++ {
		if rl.IsKeyPressed(rl.KeyOne+int32(i)) {
			st.HotbarSlot = i
		}
	}
	st.HotbarDelta = int(rl.GetMouseWheelMove())

	st.PauseToggle = rl.IsKeyPressed(rl.KeyEscape)
	st.ToggleHUD = rl.IsKeyPressed(rl.KeyF1)
	return st
}

func platformJoystick() (center rl.Vector2, knob rl.Vector2, active bool) {
	return
}
