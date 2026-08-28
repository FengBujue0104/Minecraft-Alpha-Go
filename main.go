package main

import (
	"fmt"
	"math"
	"os"
	"runtime/debug"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"mc-go/audio"
	"mc-go/blocks"
	"mc-go/input"
	"mc-go/player"
	"mc-go/world"
)

const (
	WindowWidth           = 1280
	WindowHeight          = 720
	WindowTitle           = "Minecraft Go"
	PhysicsDt             = 1.0 / 60.0
	PauseFadeTime float32 = 0.22
)

// pauseOverlay separates the game's paused state from its brief dismiss
// animation, so resuming shows a play icon before the dimmer disappears.
type pauseOverlay struct {
	amount   float32
	paused   bool
	showPlay bool
}

func (o *pauseOverlay) SetPaused(paused bool) {
	o.paused = paused
	o.showPlay = !paused
}

func (o *pauseOverlay) Update(dt float32) {
	if o.paused {
		o.amount += dt / PauseFadeTime
	} else {
		o.amount -= dt / PauseFadeTime
	}
	if o.amount < 0 {
		o.amount = 0
		o.showPlay = false
	}
	if o.amount > 1 {
		o.amount = 1
	}
}

// Android 的 c-shared 构建不会执行 main()：raylib 的 native_app_glue 在库加载
// 后回调 android_run()，后者调用 SetMain 注册的函数。桌面端 SetMain 是空
// stub，main() 仍由 Go 运行时直接执行，两条路径共用同一个入口。
func init() {
	rl.SetMain(main)
}

// buildLayout 按当前窗口尺寸重建触屏控件命中区。缩放基于高度（设计基准
// 720p），触屏时对最小命中尺寸设下限；桌面 1280x720 下所有值与原硬编码
// 常数完全一致，保证桌面画面不变。
func buildLayout(l *input.Layout) {
	w := float32(rl.GetScreenWidth())
	h := float32(rl.GetScreenHeight())
	scale := h / 720
	if scale < 0.75 {
		scale = 0.75
	}
	if scale > 3 {
		scale = 3
	}

	// 快捷栏：drawHotbar 与触点命中共用这一组矩形
	slot := float32(52) * scale
	gap := float32(4) * scale
	if touchControls && slot < 64 {
		slot = 64
	}
	count := float32(len(player.HotbarItems))
	barWidth := count*slot + (count-1)*gap
	startX := (w - barWidth) / 2
	slotY := h - slot - 18*scale
	l.HotbarSlots = l.HotbarSlots[:0]
	for i := 0; i < len(player.HotbarItems); i++ {
		l.HotbarSlots = append(l.HotbarSlots, rl.Rectangle{
			X: startX + float32(i)*(slot+gap), Y: slotY, Width: slot, Height: slot,
		})
	}

	pad := float32(26) * scale
	btn := float32(76) * scale
	if touchControls && btn < 84 {
		btn = 84
	}
	small := float32(48) * scale

	l.PauseBtn = rl.Rectangle{X: w - pad - small, Y: pad, Width: small, Height: small}
	l.JumpBtn = rl.Rectangle{X: w - pad - btn, Y: h - pad - btn, Width: btn, Height: btn}
	l.FlyBtn = rl.Rectangle{X: w - pad - btn, Y: h - pad - btn - 14*scale - btn, Width: btn, Height: btn}
	l.DescendBtn = rl.Rectangle{X: w - pad - 2*btn - 14*scale, Y: h - pad - btn, Width: btn, Height: btn}

	joyR := float32(70) * scale
	if touchControls && joyR < 84 {
		joyR = 84
	}
	l.JoystickRadius = joyR
	l.JoystickCenter = rl.NewVector2(joyR+18*scale, h-joyR-18*scale)
	zoneHalf := joyR * 1.9
	l.JoystickZone = rl.Rectangle{
		X: l.JoystickCenter.X - zoneHalf,
		Y: l.JoystickCenter.Y - zoneHalf,
		Width: zoneHalf * 2, Height: zoneHalf * 2,
	}
	if top := h * 0.40; l.JoystickZone.Y < top {
		l.JoystickZone.Y = top
	}

	// 暂停界面中央恢复按钮，与 drawPauseOverlay 的全尺寸矩形同式
	l.ResumeBtn = rl.Rectangle{X: w/2 - 90*scale, Y: h/2 - 50*scale, Width: 180 * scale, Height: 100 * scale}

	l.Slop = float32(14) * scale
}

func main() {
	// Panic recovery: report the stack, and persist it if we can.
	defer func() {
		if r := recover(); r != nil {
			// Print first — if creating the log fails, the stack is still
			// visible rather than silently lost.
			fmt.Printf("PANIC: %v\n\nStack:\n%s\n", r, debug.Stack())
			if f, err := os.Create("crash.log"); err == nil {
				fmt.Fprintf(f, "PANIC: %v\n\nStack:\n%s\n", r, debug.Stack())
				f.Close()
				fmt.Println("Wrote crash.log")
			}
		}
	}()

	rl.SetConfigFlags(rl.FlagMsaa4xHint)
	rl.InitWindow(WindowWidth, WindowHeight, WindowTitle)
	defer rl.CloseWindow()
	disableIME()
	defer restoreIME()

	// raylib treats ESC as the exit key by default, so WindowShouldClose at the
	// top of the loop ends the game before the Esc handler below ever runs.
	// The title-bar X still closes the window.
	rl.SetExitKey(rl.KeyNull)

	rl.SetTargetFPS(60)
	rl.DisableCursor()
	effects := audio.New()
	defer effects.Close()

	seed := time.Now().UnixNano()
	fmt.Printf("Seed: %d\n", seed)
	w := world.NewWorld(seed)

	spawnX := float32(0)
	spawnZ := float32(0)

	fmt.Println("Loading initial chunks...")
	w.EnsureChunksAround(spawnX, spawnZ)
	w.FlushGenerations()
	fmt.Printf("Done! Loaded %d chunks.\n", w.ChunkCount())

	// Stand on the surface. Falls back to a safe height only when no ground
	// was found at all -- previously this read GroundHeight's 0 sentinel as a
	// real height, so the fallback could never trigger.
	spawnY := float32(100)
	if g, ok := w.GroundHeight(int(spawnX), int(spawnZ)); ok {
		spawnY = float32(g) + 1
	}
	p := player.NewPlayer(spawnX, spawnY, spawnZ)
	p.SkipNextMouseFrame() // initial cursor-disable spike

	showUI := true
	physicsAccum := float32(0)
	paused := false
	pauseUI := pauseOverlay{}
	var lay input.Layout

	for !rl.WindowShouldClose() {
		frameTime := rl.GetFrameTime()
		if frameTime > 0.25 {
			frameTime = 0.25
		}

		// 布局先行：触点命中区、HUD 绘制与暂停按钮共用同一组矩形。
		buildLayout(&lay)
		in := input.Read(&lay, p.Flying, paused)

		if in.PauseToggle {
			paused = !paused
			pauseUI.SetPaused(paused)
			if paused {
				rl.EnableCursor()
			} else {
				rl.DisableCursor()
				p.SkipNextMouseFrame()
			}
		}

		if in.ToggleHUD {
			showUI = !showUI
		}

		if !paused {
			p.Update(in, w)
			switch p.ConsumeBlockAction() {
			case player.BreakBlock:
				effects.PlayBreak()
			case player.PlaceBlock:
				effects.PlayPlace()
			}

			physicsAccum += frameTime
			for physicsAccum >= PhysicsDt {
				p.StepPhysics(w, PhysicsDt)
				physicsAccum -= PhysicsDt
			}
		} else {
			physicsAccum = 0
		}
		pauseUI.Update(frameTime)

		w.EnsureChunksAround(p.Position.X, p.Position.Z)
		w.ProcessGenerations()
		w.ProcessDirty(world.DirtyBudgetPerFrame)

		rl.BeginDrawing()
		rl.ClearBackground(rl.SkyBlue)

		rl.BeginMode3D(p.Camera)
		w.Render(p.Camera)

		if p.TargetFace >= 0 {
			bx := float32(p.TargetBlockPos[0])
			by := float32(p.TargetBlockPos[1])
			bz := float32(p.TargetBlockPos[2])
			center := rl.NewVector3(bx+0.5, by+0.5, bz+0.5)
			rl.DrawCubeWires(center, 1.005, 1.005, 1.005, rl.Black)
		}

		rl.EndMode3D()

		if showUI && !paused {
			drawHUD(p, &lay)
		}
		if touchControls && !paused {
			drawTouchControls(p, &lay)
		}
		drawPauseOverlay(&pauseUI, &lay)

		rl.EndDrawing()
	}
}

// uiScale 与 buildLayout 用同一缩放基准（720p 高度），保证绘制与命中一致。
func uiScale() float32 {
	scale := float32(rl.GetScreenHeight()) / 720
	if scale < 0.75 {
		scale = 0.75
	}
	if scale > 3 {
		scale = 3
	}
	return scale
}

func drawHUD(p *player.Player, l *input.Layout) {
	w := float32(rl.GetScreenWidth())
	h := float32(rl.GetScreenHeight())
	s := uiScale()

	cx := int32(w / 2)
	cy := int32(h / 2)
	arm := int32(8 * s)
	rl.DrawLine(cx-arm, cy, cx+arm, cy, rl.Black)
	rl.DrawLine(cx, cy-arm, cx, cy+arm, rl.Black)

	posText := fmt.Sprintf("XYZ: %.1f / %.1f / %.1f",
		math.Round(float64(p.Position.X)*10)/10,
		math.Round(float64(p.Position.Y)*10)/10,
		math.Round(float64(p.Position.Z)*10)/10,
	)
	rl.DrawText(posText, int32(10*s), int32(10*s), int32(14*s), rl.White)

	var controls []string
	if touchControls {
		controls = []string{
			"Left stick: move (push to edge = run) | Drag right half: look",
			"Tap: break | Hold: place | Tap a hotbar slot: select",
		}
	} else {
		controls = []string{
			"WASD: Move | Space: Jump | Shift: Run | F: Fly",
			"Left Click: Break | Right Click: Place | 1-9: Select item",
			"Water bucket: right-click place water, left-click remove water",
			"Esc: Pause | F1: Toggle HUD",
		}
	}
	for i, text := range controls {
		rl.DrawText(text, int32(10*s), int32(34*s)+int32(float32(i)*16*s), int32(12*s), rl.Gray)
	}
	if p.Flying {
		hint := "FLYING  Space: Up  Left Ctrl: Down"
		if touchControls {
			hint = "FLYING"
		}
		rl.DrawText(hint, int32(10*s), int32(104*s), int32(14*s), rl.NewColor(255, 232, 120, 255))
	}
	drawHotbar(p, l)
}

func drawHotbar(p *player.Player, l *input.Layout) {
	s := uiScale()

	for i, item := range player.HotbarItems {
		if i >= len(l.HotbarSlots) {
			break
		}
		rect := l.HotbarSlots[i]
		x := int32(rect.X)
		y := int32(rect.Y)
		rl.DrawRectangleRec(rect, rl.NewColor(24, 28, 34, 220))
		outline := rl.NewColor(130, 138, 150, 230)
		if i == p.SelectedHotbarSlot() {
			outline = rl.NewColor(255, 224, 112, 255)
			rl.DrawRectangleRec(rl.Rectangle{X: rect.X - 2, Y: rect.Y - 2, Width: rect.Width + 4, Height: rect.Height + 4}, outline)
			rl.DrawRectangleRec(rect, rl.NewColor(32, 36, 43, 245))
		}
		rl.DrawRectangleLinesEx(rect, 2*s*0.5+1, outline)
		drawItemIcon(item, x+int32(10*s), y+int32(13*s), int32(32*s))
		number := fmt.Sprintf("%d", i+1)
		rl.DrawText(number, x+int32(4*s), y+int32(3*s), int32(12*s), rl.White)
	}
}

func drawItemIcon(item blocks.BlockType, x, y, size int32) {
	if item == blocks.Water {
		drawWaterBucketIcon(x, y, size)
		return
	}

	// A large opaque colour tile with a dark edge is deliberately simple and
	// stays readable against every terrain and HUD background.
	color := pureItemColor(item)
	rl.DrawRectangle(x+4, y+5, size-8, size-8, rl.NewColor(8, 10, 14, 255))
	rl.DrawRectangle(x+6, y+7, size-12, size-12, color)
	rl.DrawRectangleLines(x+6, y+7, size-12, size-12, rl.NewColor(235, 238, 242, 255))
}

func pureItemColor(item blocks.BlockType) rl.Color {
	switch item {
	case blocks.Grass:
		return rl.NewColor(86, 171, 65, 255)
	case blocks.Dirt:
		return rl.NewColor(143, 92, 57, 255)
	case blocks.Stone:
		return rl.NewColor(132, 132, 132, 255)
	case blocks.Wood:
		return rl.NewColor(153, 106, 63, 255)
	case blocks.Leaves:
		return rl.NewColor(59, 143, 67, 255)
	case blocks.Sand:
		return rl.NewColor(226, 203, 128, 255)
	case blocks.Glass:
		return rl.NewColor(126, 196, 222, 255)
	case blocks.Bedrock:
		return rl.NewColor(69, 69, 75, 255)
	default:
		return rl.Magenta
	}
}

func drawWaterBucketIcon(x, y, size int32) {
	centerX := x + size/2
	outline := rl.NewColor(8, 10, 14, 255)
	blue := rl.NewColor(64, 136, 245, 255)

	// Clear bucket silhouette: dark handle and border around a solid-blue body.
	rl.DrawCircleLines(centerX, y+11, 10, outline)
	rl.DrawCircleLines(centerX, y+11, 8, blue)
	rl.DrawRectangle(centerX-10, y+13, 20, 16, outline)
	rl.DrawRectangle(centerX-8, y+15, 16, 12, blue)
	rl.DrawRectangleLines(centerX-8, y+15, 16, 12, rl.NewColor(235, 238, 242, 255))
}

func drawPauseOverlay(o *pauseOverlay, l *input.Layout) {
	if o.amount <= 0 {
		return
	}
	w := float32(rl.GetScreenWidth())
	h := float32(rl.GetScreenHeight())
	alpha := uint8(170 * o.amount)
	rl.DrawRectangle(0, 0, int32(w), int32(h), rl.NewColor(7, 10, 18, alpha))

	// Ease the central control from a slightly smaller size on both enter and
	// exit. The play glyph is retained during the exit fade. Glyph offsets
	// follow the original 180x100 design, scaled by the animated button size.
	anim := 0.78 + 0.22*o.amount
	buttonW := l.ResumeBtn.Width * anim
	buttonH := l.ResumeBtn.Height * anim
	x := w/2 - buttonW/2
	y := h/2 - buttonH/2
	fx := buttonW / 180
	fy := buttonH / 100
	button := rl.Rectangle{X: x, Y: y, Width: buttonW, Height: buttonH}
	rl.DrawRectangleRounded(button, 0.18, 8, rl.NewColor(38, 47, 64, uint8(245*o.amount)))
	rl.DrawRectangleRoundedLinesEx(button, 0.18, 8, 2, rl.NewColor(210, 220, 238, uint8(230*o.amount)))

	if o.showPlay {
		rl.DrawTriangle(rl.NewVector2(x+buttonW/2-12*fx, y+28*fy), rl.NewVector2(x+buttonW/2-12*fx, y+buttonH-28*fy), rl.NewVector2(x+buttonW/2+26*fx, y+buttonH/2), rl.White)
	} else {
		rl.DrawRectangle(int32(x+buttonW/2-25*fx), int32(y+27*fy), int32(16*fx), int32(buttonH-54*fy), rl.White)
		rl.DrawRectangle(int32(x+buttonW/2+9*fx), int32(y+27*fy), int32(16*fx), int32(buttonH-54*fy), rl.White)
	}
	label := "PAUSED"
	if o.showPlay {
		label = "PLAYING"
	}
	labelSize := int32(20 * (buttonH / 100))
	labelWidth := rl.MeasureText(label, labelSize)
	rl.DrawText(label, int32(w/2)-labelWidth/2, int32(y+buttonH+18*fy), labelSize, rl.NewColor(235, 240, 250, uint8(255*o.amount)))
}

// drawTouchControls 绘制虚拟摇杆与按钮（仅安卓）。绘制用的矩形与 input
// 的命中区域来自同一个 Layout，保证所见即可点。
func drawTouchControls(p *player.Player, l *input.Layout) {
	btnFill := rl.NewColor(20, 26, 36, 150)
	btnLine := rl.NewColor(205, 215, 235, 180)

	center, knob, active := input.JoystickKnob()
	rl.DrawCircleLines(int32(center.X), int32(center.Y), l.JoystickRadius, btnLine)
	rl.DrawCircleLines(int32(center.X), int32(center.Y), l.JoystickRadius*0.55, rl.NewColor(205, 215, 235, 90))
	knobR := l.JoystickRadius * 0.38
	if active {
		rl.DrawCircleV(knob, knobR, rl.NewColor(235, 240, 250, 200))
	} else {
		rl.DrawCircleV(center, knobR, rl.NewColor(235, 240, 250, 90))
	}

	drawRoundBtn(l.JumpBtn, btnFill, btnLine)
	arrowUp(l.JumpBtn, 0.26, rl.White)

	drawRoundBtn(l.FlyBtn, btnFill, btnLine)
	if p.Flying {
		rl.DrawRectangleRounded(shrinkRect(l.FlyBtn, 6), 0.25, 8, rl.NewColor(255, 224, 112, 90))
	}
	flyGlyph(l.FlyBtn, rl.White)

	if p.Flying {
		drawRoundBtn(l.DescendBtn, btnFill, btnLine)
		arrowDown(l.DescendBtn, 0.26, rl.White)
	}

	drawRoundBtn(l.PauseBtn, btnFill, btnLine)
	bw := l.PauseBtn.Width
	bh := l.PauseBtn.Height
	barW := bw * 0.12
	barH := bh * 0.5
	x0 := l.PauseBtn.X + bw/2 - barW - bw*0.08
	y0 := l.PauseBtn.Y + bh*0.25
	rl.DrawRectangle(int32(x0), int32(y0), int32(barW), int32(barH), rl.White)
	rl.DrawRectangle(int32(x0+barW+bw*0.16), int32(y0), int32(barW), int32(barH), rl.White)
}

func drawRoundBtn(b rl.Rectangle, fill, line rl.Color) {
	rl.DrawRectangleRounded(b, 0.25, 8, fill)
	rl.DrawRectangleRoundedLinesEx(b, 0.25, 8, 2, line)
}

func arrowUp(b rl.Rectangle, size float32, c rl.Color) {
	cx := b.X + b.Width/2
	cy := b.Y + b.Height/2
	s := b.Width * size
	rl.DrawTriangle(rl.NewVector2(cx, cy-s), rl.NewVector2(cx-s, cy+s*0.7), rl.NewVector2(cx+s, cy+s*0.7), c)
}

func arrowDown(b rl.Rectangle, size float32, c rl.Color) {
	cx := b.X + b.Width/2
	cy := b.Y + b.Height/2
	s := b.Width * size
	rl.DrawTriangle(rl.NewVector2(cx, cy+s), rl.NewVector2(cx+s, cy-s*0.7), rl.NewVector2(cx-s, cy-s*0.7), c)
}

func flyGlyph(b rl.Rectangle, c rl.Color) {
	size := int32(b.Height * 0.30)
	tw := rl.MeasureText("FLY", size)
	rl.DrawText("FLY", int32(b.X+b.Width/2)-tw/2, int32(b.Y+b.Height/2)-size/2, size, c)
}

func shrinkRect(b rl.Rectangle, px float32) rl.Rectangle {
	return rl.Rectangle{X: b.X + px, Y: b.Y + px, Width: b.Width - 2*px, Height: b.Height - 2*px}
}
