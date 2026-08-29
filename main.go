package main

import (
	"fmt"
	"math"
	"os"
	"runtime/debug"
	"strings"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"mc-go/audio"
	"mc-go/blocks"
	"mc-go/input"
	"mc-go/player"
	"mc-go/save"
	"mc-go/settings"
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

// buildLayout 按当前窗口尺寸重建触屏控件命中区，并应用用户的布局自定义
// （settings 中的按钮位置/尺寸、物品栏缩放、摇杆锚点）。缩放基于高度
// （720p 基准）；桌面 1280x720 下与原硬编码一致。
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
	set := settings.Current

	// 快捷栏：drawHotbar 与触点命中共用这一组矩形
	slot := float32(52) * scale
	if touchControls {
		if slot < 64 {
			slot = 64
		}
		if set.HotbarScaleSet {
			slot = float32(52) * scale * set.HotbarScale
			if slot < 40 {
				slot = 40
			}
			if slot > 200 {
				slot = 200
			}
		}
	}
	gap := float32(4) * scale
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
	if touchControls {
		// 全面屏/刘海屏没有低成本的安全区查询，用加宽的边距做启发式规避
		pad = 36 * scale
		if btn < 84 {
			btn = 84
		}
	}
	small := float32(48) * scale

	l.PauseBtn = rl.Rectangle{X: w - pad - small, Y: pad, Width: small, Height: small}
	l.SettingsBtn = rl.Rectangle{X: w/2 - 85*scale, Y: h/2 + 60*scale, Width: 170 * scale, Height: 56 * scale}

	// 按钮：默认位置由公式给出，用户自定义则以归一化中心 + 相对缩放覆盖
	jumpDef := rl.Rectangle{X: w - pad - btn, Y: h - pad - btn, Width: btn, Height: btn}
	flyDef := rl.Rectangle{X: w - pad - btn, Y: h - pad - btn - 14*scale - btn, Width: btn, Height: btn}
	placeDef := rl.Rectangle{X: w - pad - btn, Y: flyDef.Y - 14*scale - btn, Width: btn, Height: btn}
	breakDef := rl.Rectangle{X: w - pad - 2*btn - 14*scale, Y: placeDef.Y, Width: btn, Height: btn}
	descDef := rl.Rectangle{X: w - pad - 2*btn - 14*scale, Y: h - pad - btn, Width: btn, Height: btn}
	applyBtn := func(name string, def rl.Rectangle) rl.Rectangle {
		o, ok := set.Buttons[name]
		if !ok {
			return def
		}
		size := btn * o.Scale
		return rl.Rectangle{X: o.NX*w - size/2, Y: o.NY*h - size/2, Width: size, Height: size}
	}
	l.JumpBtn = applyBtn("jump", jumpDef)
	l.FlyBtn = applyBtn("fly", flyDef)
	l.PlaceBtn = applyBtn("place", placeDef)
	l.BreakBtn = applyBtn("break", breakDef)
	l.DescendBtn = applyBtn("descend", descDef)

	joyR := float32(70) * scale
	if touchControls && joyR < 84 {
		joyR = 84
	}
	l.JoystickRadius = joyR
	l.FreeJoystick = set.FreeJoystick
	l.JoystickCenter = rl.NewVector2(joyR+18*scale, h-joyR-18*scale)
	if !set.FreeJoystick && set.AnchorSet {
		l.JoystickCenter = rl.NewVector2(set.AnchorX*w, set.AnchorY*h)
	}
	l.JoystickZone = rl.Rectangle{X: 0, Y: 0, Width: w * 0.5, Height: h}

	// 暂停界面中央恢复按钮，与 drawPauseOverlay 的全尺寸矩形同式
	l.ResumeBtn = rl.Rectangle{X: w/2 - 90*scale, Y: h/2 - 50*scale, Width: 180 * scale, Height: 100 * scale}

	l.Slop = float32(14) * scale
}

// startupMilestones 是启动黑匣子的屏上镜像：仅当以 `-tags androiddebug`
// 构建时才画到屏幕上，正式版静默（crash.log 文件与 panic 红屏不受影响）。
var startupMilestones []string

func drawMilestones() {
	if !debugUI || !rl.IsWindowReady() {
		return
	}
	rl.BeginDrawing()
	rl.ClearBackground(rl.Black)
	for i := 0; i < len(startupMilestones) && i < 30; i++ {
		rl.DrawText(startupMilestones[i], 12, 12+int32(i)*16, 12, rl.Green)
	}
	rl.EndDrawing()
}

// drawPanicOnScreen 把 panic 堆栈画成红屏并停留，供拍照回传。
func drawPanicOnScreen(r interface{}, stack string) {
	if !rl.IsWindowReady() {
		return
	}
	defer func() { _ = recover() }() // 显示失败也不能掩盖原始 panic
	rl.BeginDrawing()
	rl.ClearBackground(rl.NewColor(120, 10, 10, 255))
	lines := strings.Split("PANIC: "+fmt.Sprint(r)+"\n"+stack, "\n")
	for i := 0; i < len(lines) && i < 45; i++ {
		l := lines[i]
		if len(l) > 90 {
			l = l[:90]
		}
		rl.DrawText(l, 10, 10+int32(i)*14, 12, rl.White)
	}
	rl.EndDrawing()
}

func main() {
	// android_main 已把 android_app 装好，这里能拿到私有目录路径。
	initCrashLog()
	// Panic recovery: report the stack, and persist it if we can.
	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			fmt.Printf("PANIC: %v\n\nStack:\n%s\n", r, stack)
			// Android: stdout is invisible; TraceLog routes to logcat
			// (rcore_android uses __android_log_print), the local file to
			// the app-private crash.log black box.
			rl.TraceLog(rl.LogError, "PANIC: %v", r)
			logLocal("PANIC: " + fmt.Sprint(r) + "\n" + stack)
			drawPanicOnScreen(r, stack)
			time.Sleep(20 * time.Second) // 红屏停留，供拍照回传
			if f, err := os.Create("crash.log"); err == nil {
				fmt.Fprintf(f, "PANIC: %v\n\nStack:\n%s\n", r, stack)
				f.Close()
				fmt.Println("Wrote crash.log")
			}
		}
	}()

	logLocal("main: enter")
	rl.SetConfigFlags(rl.FlagMsaa4xHint)
	logLocal("main: before InitWindow")
	rl.InitWindow(WindowWidth, WindowHeight, WindowTitle)
	logLocal("main: window ready")
	drawMilestones()
	defer rl.CloseWindow()
	disableIME()
	defer restoreIME()

	// raylib treats ESC as the exit key by default, so WindowShouldClose at the
	// top of the loop ends the game before the Esc handler below ever runs.
	// The title-bar X still closes the window.
	rl.SetExitKey(rl.KeyNull)

	rl.SetTargetFPS(60)
	rl.DisableCursor()
	logLocal("main: audio init")
	effects := audio.New()
	defer effects.Close()
	logLocal("main: audio ok")
	loadMenuFont()
	settings.SetPath(appDataDir() + "/settings.bin")
	settings.Load()
	drawMilestones()

	// ---- 会话管理：封面选槽后才创建世界与玩家 ----
	appDir := appDataDir()
	var w *world.World
	var p *player.Player
	sessionActive := false
	paused := false
	physicsAccum := float32(0)
	pauseUI := pauseOverlay{}
	var lay input.Layout
	activeSlot := 0 // 0=无存档开始；1..3=存档槽
	autosaveTimer := float32(0)
	var lookSmX, lookSmY float32
	pendingSlot := -1
	var title titleState
	title.refresh(appDir, false)
	uiMode = uiTitle

	writeSave := func() {
		if !sessionActive || activeSlot <= 0 || w == nil || p == nil {
			return
		}
		save.Write(appDir, activeSlot, save.Data{
			Seed:      w.Seed(),
			LastSaved: time.Now().Unix(),
			Player: save.PlayerState{
				X: p.Position.X, Y: p.Position.Y, Z: p.Position.Z,
				Yaw: p.Yaw, Pitch: p.Pitch,
				Flying: p.Flying, Selected: uint8(p.SelectedBlock),
			},
			Edits: w.ExportEdits(),
		})
	}

	startSession := func(slot int) {
		seed := time.Now().UnixNano()
		var saved *save.Data
		if slot > 0 {
			if d, err := save.Read(appDir, slot); err == nil {
				saved = &d
				seed = d.Seed
			}
		}
		fmt.Printf("Seed: %d (slot %d)\n", seed, slot)
		if w != nil {
			w.UnloadAll()
		}
		w = world.NewWorld(seed)
		if saved != nil && len(saved.Edits) > 0 {
			w.ImportEdits(saved.Edits)
		}
		w.SetLoadDist(settings.Current.RenderTier)

		spawnX, spawnZ := float32(0), float32(0)
		if saved != nil {
			spawnX, spawnZ = saved.Player.X, saved.Player.Z
		}
		fmt.Println("Loading initial chunks...")
		w.EnsureChunksAround(spawnX, spawnZ)
		w.FlushGenerations()
		fmt.Printf("Done! Loaded %d chunks.\n", w.ChunkCount())

		if saved != nil {
			p = player.NewPlayer(saved.Player.X, saved.Player.Y, saved.Player.Z)
			p.Yaw = saved.Player.Yaw
			p.Pitch = saved.Player.Pitch
			p.Flying = saved.Player.Flying
			p.SelectHotbarBlock(blocks.BlockType(saved.Player.Selected))
		} else {
			spawnY := float32(100)
			if g, ok := w.GroundHeight(int(spawnX), int(spawnZ)); ok {
				spawnY = float32(g) + 1
			}
			p = player.NewPlayer(spawnX, spawnY, spawnZ)
		}
		p.AutoJump = settings.Current.AutoJump
		p.SkipNextMouseFrames(12)
		logLocal("main: session ready, entering loop")

		activeSlot = slot
		sessionActive = true
		autosaveTimer = 0
		lookSmX, lookSmY = 0, 0
		paused = false
		pauseUI.SetPaused(false)
		uiMode = uiGame
		rl.DisableCursor()
	}

	onSwitchSave = func() {
		writeSave()
		paused = false
		uiMode = uiTitle
		title.refresh(appDir, true)
		rl.EnableCursor()
	}
	setRenderTier = func(tier int) {
		if w != nil {
			w.SetLoadDist(tier)
		}
	}

	showUI := true

	for !rl.WindowShouldClose() {
		frameTime := rl.GetFrameTime()
		if frameTime > 0.25 {
			frameTime = 0.25
		}

		// 布局先行：触点命中区、HUD 绘制与暂停按钮共用同一组矩形。
		buildLayout(&lay)

		if pendingSlot == -2 {
			// 封面「返回当前游戏」：会话未销毁，直接恢复
			pendingSlot = -1
			uiMode = uiGame
			paused = false
			pauseUI.SetPaused(false)
			rl.DisableCursor()
			p.SkipNextMouseFrame()
		} else if pendingSlot >= 0 {
			slot := pendingSlot
			pendingSlot = -1
			startSession(slot)
		}

		if uiMode == uiGame && sessionActive {
			in := input.Read(&lay, p.Flying, paused)

			if in.PauseToggle {
				paused = !paused
				pauseUI.SetPaused(paused)
				if paused {
					rl.EnableCursor()
					writeSave() // 暂停即存档
				} else {
					rl.DisableCursor()
					p.SkipNextMouseFrames(12)
				}
			}

			if in.SettingsOpen && paused {
				uiMode = uiSettings
				ed = editorState{}
				writeSave()
			}

			if in.ToggleHUD {
				showUI = !showUI
			}

			if !paused {
				// 用户偏好：自动跳跃 + 视角反转/灵敏度；快速拖动用轻微
				// 指数平滑抹平触点抖动与偶发掉帧的顿挫，松手后残量快衰。
				p.AutoJump = settings.Current.AutoJump
				lookSmX = lookSmX*0.55 + in.LookDX*0.45
				lookSmY = lookSmY*0.55 + in.LookDY*0.45
				if in.LookDX == 0 {
					lookSmX *= 0.4
				}
				if in.LookDY == 0 {
					lookSmY *= 0.4
				}
				in.LookDX, in.LookDY = lookSmX, lookSmY
				if settings.Current.InvertX {
					in.LookDX = -in.LookDX
				}
				if settings.Current.InvertY {
					in.LookDY = -in.LookDY
				}
				in.LookDX *= settings.Current.SensX
				in.LookDY *= settings.Current.SensY
				p.Update(in, w)
				if act := p.ConsumeBlockAction(); act != player.NoBlockAction {
					if act == player.BreakBlock {
						effects.PlayBreak()
					}
					if activeSlot > 0 {
						writeSave() // 方块改动即时保存
					}
				}

				physicsAccum += frameTime
				for physicsAccum >= PhysicsDt {
					p.StepPhysics(w, PhysicsDt)
					physicsAccum -= PhysicsDt
				}

				autosaveTimer += frameTime
				if autosaveTimer >= 4 {
					autosaveTimer = 0
					if activeSlot > 0 {
						writeSave()
					}
				}
			} else {
				physicsAccum = 0
			}
		} else if uiMode == uiTitle {
			physicsAccum = 0
		} else {
			// 设置页/布局编辑器：输入与绘制都在绘制阶段自处理（即时模式），
			// 这里只需保证游戏保持暂停。
			physicsAccum = 0
		}
		pauseUI.Update(frameTime)

		if sessionActive && uiMode == uiGame {
			w.EnsureChunksAround(p.Position.X, p.Position.Z)
			w.ProcessGenerations()
			w.ProcessDirty(world.DirtyBudgetPerFrame)
		}

		rl.BeginDrawing()
		rl.ClearBackground(rl.SkyBlue)

		if uiMode == uiTitle {
			// 封面页选中槽位（含 -2 返回当前游戏）交给更新阶段处理
			pendingSlot = titleFrame(&title, pendingSlot >= 0)
			rl.EndDrawing()
			continue
		}

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
		if touchControls && !paused && uiMode == uiGame {
			drawTouchControls(p, &lay)
		}
		drawPauseOverlay(&pauseUI, &lay)
		if uiMode != uiGame {
			drawSettingsPages(&lay, sessionActive)
		}

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

	// 键鼠操作提示仅桌面端显示；触屏的操作方式就是屏幕上的控件本身。
	if !touchControls {
		controls := []string{
			"WASD: Move | Space: Jump | Shift: Run | F: Fly",
			"Left Click: Break | Right Click: Place | 1-9: Select item",
			"Water bucket: right-click place water, left-click remove water",
			"Esc: Pause | F1: Toggle HUD",
		}
		for i, text := range controls {
			rl.DrawText(text, int32(10*s), int32(34*s)+int32(float32(i)*16*s), int32(12*s), rl.Gray)
		}
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
		// 图标在槽位内居中；触屏隐藏角标数字（点击即选择，数字无意义且挤占空间）
		iconSize := int32(32 * s)
		ix := x + (int32(rect.Width)-iconSize)/2
		iy := y + (int32(rect.Height)-iconSize)/2
		drawItemIcon(item, ix, iy, iconSize)
		if !touchControls {
			number := fmt.Sprintf("%d", i+1)
			rl.DrawText(number, x+int32(4*s), y+int32(3*s), int32(12*s), rl.White)
		}
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
	// 标签放在暂停按钮上方，避免与下方设置按钮重叠
	rl.DrawText(label, int32(w/2)-labelWidth/2, int32(y-24*fy)-labelSize, labelSize, rl.NewColor(235, 240, 250, uint8(255*o.amount)))

	// 设置入口：暂停完全展开时显示（触屏与桌面鼠标都可点，命中区在
	// buildLayout 的 l.SettingsBtn）
	if o.amount > 0.95 {
		s := uiScale()
		a := uint8(255 * (o.amount - 0.95) / 0.05)
		btn := l.SettingsBtn
		rl.DrawRectangleRounded(btn, 0.3, 8, rl.NewColor(38, 47, 64, uint8(200*(float32(a)/255))))
		rl.DrawRectangleRoundedLinesEx(btn, 0.3, 8, 2, rl.NewColor(210, 220, 238, a))
		fs := 17 * s
		drawTextCCentered("设置", btn.X+btn.Width/2, btn.Y+btn.Height/2-fs/2, fs, rl.NewColor(235, 240, 250, a))
	}
}

// drawTouchControls 绘制触屏控件（仅安卓）。绘制用的矩形与 input 的命中
// 区域来自同一个 Layout，保证所见即可点。摇杆是动态的：按下时在按下点
// 展开，松手后只在锚点留一圈淡淡的提示。
func drawTouchControls(p *player.Player, l *input.Layout) {
	btnFill := rl.NewColor(20, 26, 36, 150)
	btnLine := rl.NewColor(205, 215, 235, 180)

	center, knob, active := input.JoystickKnob()
	if active {
		rl.DrawCircleLines(int32(center.X), int32(center.Y), l.JoystickRadius, btnLine)
		rl.DrawCircleLines(int32(center.X), int32(center.Y), l.JoystickRadius*0.55, rl.NewColor(205, 215, 235, 90))
		rl.DrawCircleV(knob, l.JoystickRadius*0.38, rl.NewColor(235, 240, 250, 200))
	} else {
		// 待机提示环：提示左半屏可按下生成摇杆
		hint := l.JoystickCenter
		rl.DrawCircleLines(int32(hint.X), int32(hint.Y), l.JoystickRadius*0.8, rl.NewColor(205, 215, 235, 45))
		rl.DrawCircleLines(int32(hint.X), int32(hint.Y), l.JoystickRadius*0.44, rl.NewColor(205, 215, 235, 30))
	}

	drawRoundBtn(l.JumpBtn, btnFill, btnLine)
	arrowUp(l.JumpBtn, 0.26, rl.White)

	drawRoundBtn(l.FlyBtn, btnFill, btnLine)
	if p.Flying {
		rl.DrawRectangleRounded(shrinkRect(l.FlyBtn, 6), 0.25, 8, rl.NewColor(255, 224, 112, 90))
	}
	flyGlyph(l.FlyBtn, rl.White)

	// 破坏（X）与放置（方块）按钮，位于 Fly 上方一行
	drawRoundBtn(l.BreakBtn, btnFill, btnLine)
	crossGlyph(l.BreakBtn, rl.White)
	drawRoundBtn(l.PlaceBtn, btnFill, btnLine)
	cubeGlyph(l.PlaceBtn, rl.White)

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

// crossGlyph 画破坏按钮的 X 形图标。
func crossGlyph(b rl.Rectangle, c rl.Color) {
	cx := b.X + b.Width/2
	cy := b.Y + b.Height/2
	s := b.Width * 0.24
	th := b.Width * 0.08
	rl.DrawLineEx(rl.NewVector2(cx-s, cy-s), rl.NewVector2(cx+s, cy+s), th, c)
	rl.DrawLineEx(rl.NewVector2(cx-s, cy+s), rl.NewVector2(cx+s, cy-s), th, c)
}

// cubeGlyph 画放置按钮的方块图标：菱形顶面 + 矩形正面。
func cubeGlyph(b rl.Rectangle, c rl.Color) {
	cx := b.X + b.Width/2
	cy := b.Y + b.Height/2
	w := b.Width * 0.24
	h := w * 0.75
	tip := rl.NewVector2(cx, cy-h)
	left := rl.NewVector2(cx-w, cy-h+w*0.4)
	right := rl.NewVector2(cx+w, cy-h+w*0.4)
	bottom := rl.NewVector2(cx, cy-h+w*0.8)
	rl.DrawTriangle(tip, left, bottom, c)
	rl.DrawTriangle(tip, bottom, right, c)
	rl.DrawRectangle(int32(cx-w), int32(cy-h+w*0.8), int32(2*w), int32(w*1.05), c)
}

func shrinkRect(b rl.Rectangle, px float32) rl.Rectangle {
	return rl.Rectangle{X: b.X + px, Y: b.Y + px, Width: b.Width - 2*px, Height: b.Height - 2*px}
}
