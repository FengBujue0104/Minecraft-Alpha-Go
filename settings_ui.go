package main

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"

	"mc-go/input"
	"mc-go/player"
	"mc-go/settings"
)

// 设置页与布局编辑器：即时模式 UI，逻辑与绘制在同一帧内完成（都在
// BeginDrawing 里调用）。指针交互统一用自管的按下/抬起边沿 + 防抖闩锁，
// 不依赖 raylib 的 IsMouseButtonPressed——个别平台的 prev 状态更新时序
// 会造成"按住期间每帧都触发 Pressed"的双重边沿，开关会被瞬间翻回
// （K70 上三个开关"闪一下弹回"的根因）。触屏上 raylib 把 touch[0] 映射
// 为鼠标，因此一套代码两端可用。

type uiModeT int8

const (
	uiGame     uiModeT = iota // 游戏中
	uiSettings                // 设置页
	uiEditor                  // 布局编辑器
)

var uiMode = uiGame

type editorState struct {
	selected  string  // "" | joystick | <settings.ButtonOrder 键名> | hotbar
	dragging  bool
	grabDX    float32 // 指针到控件中心的抓取偏移
	grabDY    float32
	sensXDrag bool
	sensYDrag bool
	sizeDrag  bool
	barDrag   bool
}

var ed editorState

var (
	uiPrevDown     bool
	lastToggleFire float32
)

// uiEdges 返回本帧的按下沿与持续状态。按下状态同时看鼠标与触摸两个
// 通道（安卓上 raylib 把 touch[0] 映射为鼠标，双保险）。
func uiEdges() (justPressed, down bool) {
	d := rl.IsMouseButtonDown(rl.MouseLeftButton) || rl.GetTouchPointCount() > 0
	jp := d && !uiPrevDown
	uiPrevDown = d
	return jp, d
}

func drawSettingsPages(lay *input.Layout) {
	w := float32(rl.GetScreenWidth())
	h := float32(rl.GetScreenHeight())
	rl.DrawRectangle(0, 0, int32(w), int32(h), rl.NewColor(13, 16, 24, 250))
	switch uiMode {
	case uiSettings:
		settingsFrame(w, h)
	case uiEditor:
		editorFrame(lay, w, h)
	}
}

// ---------- 通用控件 ----------

func pointerIn(r rl.Rectangle) bool {
	m := rl.GetMousePosition()
	return m.X >= r.X && m.X < r.X+r.Width && m.Y >= r.Y && m.Y < r.Y+r.Height
}

func buttonC(r rl.Rectangle, label string, fontSize float32, justPressed bool) bool {
	rl.DrawRectangleRounded(r, 0.25, 8, rl.NewColor(38, 47, 64, 235))
	line := rl.NewColor(210, 220, 238, 220)
	if pointerIn(r) {
		line = rl.NewColor(255, 224, 112, 240)
	}
	rl.DrawRectangleRoundedLinesEx(r, 0.25, 8, 2, line)
	drawTextCCentered(label, r.X+r.Width/2, r.Y+r.Height/2-fontSize/2, fontSize, rl.White)
	return pointerIn(r) && justPressed
}

func drawToggleC(r rl.Rectangle, on bool) {
	track := rl.NewColor(70, 80, 96, 255)
	knobC := rl.NewColor(150, 158, 170, 255)
	if on {
		track = rl.NewColor(72, 150, 96, 255)
		knobC = rl.NewColor(235, 240, 250, 255)
	}
	rl.DrawRectangleRounded(r, 0.5, 8, track)
	kh := r.Height * 0.8
	kx := r.X + r.Height*0.1
	if on {
		kx = r.X + r.Width - r.Height*0.9
	}
	rl.DrawCircleV(rl.NewVector2(kx+kh/2, r.Y+r.Height/2), kh/2, knobC)
}

func sliderC(r rl.Rectangle, v, lo, hi float32, dragging *bool, down bool) float32 {
	rl.DrawRectangleRounded(r, 0.5, 6, rl.NewColor(58, 66, 82, 255))
	t := (v - lo) / (hi - lo)
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	if t > 0 {
		rl.DrawRectangleRounded(rl.Rectangle{X: r.X, Y: r.Y, Width: r.Width * t, Height: r.Height}, 0.5, 6, rl.NewColor(86, 130, 200, 255))
	}
	m := rl.GetMousePosition()
	grab := rl.Rectangle{X: r.X - 12, Y: r.Y - 16, Width: r.Width + 24, Height: r.Height + 32}
	if pointerIn(grab) && down {
		*dragging = true
	}
	if *dragging {
		if down {
			t = (m.X - r.X) / r.Width
			if t < 0 {
				t = 0
			}
			if t > 1 {
				t = 1
			}
			v = lo + t*(hi-lo)
		} else {
			*dragging = false
		}
	}
	rl.DrawCircleV(rl.NewVector2(r.X+r.Width*t, r.Y+r.Height/2), r.Height*0.85, rl.NewColor(235, 240, 250, 255))
	return v
}

func clampf(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func rectsOverlap(a, b rl.Rectangle) bool {
	return a.X < b.X+b.Width && a.X+a.Width > b.X && a.Y < b.Y+b.Height && a.Y+a.Height > b.Y
}

func buttonRectByName(lay *input.Layout, name string) rl.Rectangle {
	switch name {
	case "jump":
		return lay.JumpBtn
	case "fly":
		return lay.FlyBtn
	case "descend":
		return lay.DescendBtn
	case "break":
		return lay.BreakBtn
	case "place":
		return lay.PlaceBtn
	}
	return rl.Rectangle{}
}

func buttonNameC(name string) string {
	switch name {
	case "jump":
		return "跳跃"
	case "fly":
		return "飞行"
	case "descend":
		return "下降"
	case "break":
		return "破坏"
	case "place":
		return "放置"
	}
	return name
}

func rectCenter(r rl.Rectangle) rl.Vector2 {
	return rl.NewVector2(r.X+r.Width/2, r.Y+r.Height/2)
}

func distSq(a, b rl.Vector2) float32 {
	dx, dy := a.X-b.X, a.Y-b.Y
	return dx*dx + dy*dy
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func mapOnOff(b bool) string {
	if b {
		return "开"
	}
	return "关"
}

func backPressed() bool {
	return rl.IsKeyPressed(rl.KeyBack) || rl.IsKeyPressed(rl.KeyEscape)
}

// ---------- 设置页 ----------

func settingsFrame(w, h float32) {
	s := uiScale()
	set := settings.Current
	justPressed, down := uiEdges()

	drawTextCCentered("设置", w/2, 14*s, 28*s, rl.White)
	backRect := rl.Rectangle{X: 12 * s, Y: 12 * s, Width: 100 * s, Height: 46 * s}
	if backPressed() || buttonC(backRect, "返回", 18*s, justPressed) {
		settings.Save()
		uiMode = uiGame
		return
	}

	rowX := 24 * s
	rowW := w - 48*s
	rowH := 54 * s
	y := 72 * s
	now := float32(rl.GetTime())

	toggleRow := func(label string, on bool) bool {
		rl.DrawRectangleRec(rl.Rectangle{X: rowX, Y: y, Width: rowW, Height: rowH}, rl.NewColor(26, 32, 44, 220))
		drawTextC(label, rowX+14*s, y+rowH/2-11*s, 19*s, rl.White)
		row := rl.Rectangle{X: rowX, Y: y, Width: rowW, Height: rowH}
		sw := rl.Rectangle{X: rowX + rowW - 84*s, Y: y + rowH/2 - 17*s, Width: 70 * s, Height: 34 * s}
		// 整行可点；300ms 防抖闩锁兜底任何边沿异常，杜绝"闪一下弹回"
		if justPressed && pointerIn(row) && now-lastToggleFire >= 0.3 {
			on = !on
			lastToggleFire = now
		}
		drawToggleC(sw, on)
		drawTextCCentered("开", sw.X+sw.Width*0.25, y+rowH/2-8*s, 13*s, rl.NewColor(20, 26, 36, 200))
		drawTextCCentered("关", sw.X+sw.Width*0.75, y+rowH/2-8*s, 13*s, rl.NewColor(200, 208, 222, 160))
		y += rowH + 10*s
		return on
	}
	sliderRow := func(label string, v, lo, hi float32, d *bool) float32 {
		rl.DrawRectangleRec(rl.Rectangle{X: rowX, Y: y, Width: rowW, Height: rowH}, rl.NewColor(26, 32, 44, 220))
		drawTextC(label, rowX+14*s, y+rowH/2-11*s, 19*s, rl.White)
		bar := rl.Rectangle{X: rowX + rowW*0.44, Y: y + rowH/2 - 8*s, Width: rowW*0.32, Height: 16 * s}
		v = sliderC(bar, v, lo, hi, d, down)
		drawTextC(fmt.Sprintf("×%.2f", v), bar.X+bar.Width+12*s, y+rowH/2-10*s, 16*s, rl.NewColor(255, 224, 112, 255))
		y += rowH + 10*s
		return v
	}

	set.InvertX = toggleRow("水平反转", set.InvertX)
	set.InvertY = toggleRow("垂直反转", set.InvertY)
	set.SensX = settings.ClampSens(sliderRow("水平灵敏度", set.SensX, 0.2, 3, &ed.sensXDrag))
	set.SensY = settings.ClampSens(sliderRow("垂直灵敏度", set.SensY, 0.2, 3, &ed.sensYDrag))
	set.AutoJump = toggleRow("自动跳跃", set.AutoJump)

	entry := rl.Rectangle{X: rowX, Y: y, Width: rowW, Height: rowH}
	if buttonC(entry, "自定义布局", 18*s, justPressed) {
		uiMode = uiEditor
		ed = editorState{}
		return
	}
	drawTextC("设置会自动保存", rowX+14*s, y+rowH+12*s, 14*s, rl.Gray)
}

// ---------- 布局编辑器 ----------

func resetRectFor(lay *input.Layout) rl.Rectangle {
	w := lay.PauseBtn.Width * 1.35
	return rl.Rectangle{X: lay.PauseBtn.X - 10 - w, Y: lay.PauseBtn.Y, Width: w, Height: lay.PauseBtn.Height}
}

func drawButtonGlyphsEditor(lay *input.Layout) {
	btnFill := rl.NewColor(20, 26, 36, 150)
	btnLine := rl.NewColor(205, 215, 235, 180)
	drawRoundBtn(lay.JumpBtn, btnFill, btnLine)
	arrowUp(lay.JumpBtn, 0.26, rl.White)
	drawRoundBtn(lay.FlyBtn, btnFill, btnLine)
	flyGlyph(lay.FlyBtn, rl.White)
	drawRoundBtn(lay.BreakBtn, btnFill, btnLine)
	crossGlyph(lay.BreakBtn, rl.White)
	drawRoundBtn(lay.PlaceBtn, btnFill, btnLine)
	cubeGlyph(lay.PlaceBtn, rl.White)
	drawRoundBtn(lay.DescendBtn, btnFill, btnLine)
	arrowDown(lay.DescendBtn, 0.26, rl.White)
}

func editorFrame(lay *input.Layout, w, h float32) {
	s := uiScale()
	set := settings.Current
	resetRect := resetRectFor(lay)
	justPressed, down := uiEdges()

	// 顶部滑块带（物品栏/按钮尺寸滑块都在这里）：也是禁区的一部分
	band := rl.Rectangle{X: 12 * s, Y: 44 * s, Width: resetRect.X - 36*s, Height: 40 * s}
	forbidden := []rl.Rectangle{lay.PauseBtn, resetRect, band}

	// ---- 指针交互 ----
	m := rl.GetMousePosition()

	backRect := rl.Rectangle{X: 12 * s, Y: 8 * s, Width: 100 * s, Height: 34 * s}
	if backPressed() || buttonC(backRect, "返回", 16*s, justPressed) {
		settings.Save()
		uiMode = uiSettings
		ed = editorState{}
		return
	}
	chip := rl.Rectangle{X: 124 * s, Y: 8 * s, Width: 190 * s, Height: 34 * s}
	if buttonC(chip, "自由摇杆："+mapOnOff(set.FreeJoystick), 14*s, justPressed) {
		set.FreeJoystick = !set.FreeJoystick
	}

	if justPressed {
		switch {
		case pointerIn(resetRect):
			settings.ResetLayout()
			ed.selected = ""
			ed.dragging = false
		case pointerIn(backRect) || pointerIn(chip) || pointerIn(band):
			// 顶部控件已由各自的处理逻辑响应
		default:
			found := ""
			for _, name := range settings.ButtonOrder {
				if pointerIn(buttonRectByName(lay, name)) {
					found = name
					break
				}
			}
			if found != "" {
				ed.selected = found
				ed.dragging = true
				c := rectCenter(buttonRectByName(lay, found))
				ed.grabDX, ed.grabDY = c.X-m.X, c.Y-m.Y
			} else if !set.FreeJoystick && distSq(m, lay.JoystickCenter) < lay.JoystickRadius*lay.JoystickRadius {
				ed.selected = "joystick"
				ed.dragging = true
			} else {
				hot := false
				for _, r := range lay.HotbarSlots {
					if pointerIn(r) {
						hot = true
						break
					}
				}
				if hot {
					ed.selected = "hotbar"
				} else {
					ed.selected = ""
				}
				ed.dragging = false
			}
		}
	}

	if ed.dragging && down {
		switch ed.selected {
		case "joystick":
			nx := settings.ClampAnchorX(m.X / w)
			ny := settings.ClampAnchorY(m.Y / h)
			cand := rl.Rectangle{
				X: nx*w - lay.JoystickRadius, Y: ny*h - lay.JoystickRadius,
				Width: lay.JoystickRadius * 2, Height: lay.JoystickRadius * 2,
			}
			hit := false
			for _, f := range forbidden {
				if rectsOverlap(cand, f) {
					hit = true
					break
				}
			}
			if !hit {
				set.AnchorSet = true
				set.AnchorX = nx
				set.AnchorY = ny
			}
		default:
			if r := buttonRectByName(lay, ed.selected); r.Width > 0 {
				newRect := rl.Rectangle{
					X: m.X + ed.grabDX - r.Width/2, Y: m.Y + ed.grabDY - r.Height/2,
					Width: r.Width, Height: r.Height,
				}
				newRect.X = clampf(newRect.X, 4, w-4-newRect.Width)
				newRect.Y = clampf(newRect.Y, 4, h-4-newRect.Height)
				hit := false
				for _, f := range forbidden {
					if rectsOverlap(newRect, f) {
						hit = true
						break
					}
				}
				// 禁区（暂停/重置/顶部滑块带）不可重叠，保证随时能恢复默认
				if !hit {
					settings.SetButtonRect(ed.selected, newRect, w, h)
				}
			}
		}
	}
	if !down {
		ed.dragging = false
	}

	// ---- 绘制 ----
	// 禁区
	for _, f := range forbidden {
		rl.DrawRectangleRec(f, rl.NewColor(150, 40, 40, 50))
		rl.DrawRectangleLinesEx(f, 2, rl.NewColor(220, 90, 90, 170))
	}
	drawTextC("禁区", lay.PauseBtn.X+4, lay.PauseBtn.Y+lay.PauseBtn.Height+4, 12*s, rl.NewColor(230, 120, 120, 200))

	// 重置按钮
	drawRoundBtn(resetRect, rl.NewColor(60, 32, 32, 220), rl.NewColor(230, 120, 120, 220))
	drawTextCCentered("重置", resetRect.X+resetRect.Width/2, resetRect.Y+resetRect.Height/2-10*s, 14*s, rl.White)
	drawTextCCentered("恢复默认", resetRect.X+resetRect.Width/2, resetRect.Y+resetRect.Height/2+7*s, 11*s, rl.NewColor(230, 180, 180, 220))

	// 暂停按钮本体（禁区内，固定不可动）
	drawRoundBtn(lay.PauseBtn, rl.NewColor(20, 26, 36, 150), rl.NewColor(205, 215, 235, 120))

	// 各控件（与实际游戏同一套矩形）
	drawButtonGlyphsEditor(lay)

	// 摇杆
	if set.FreeJoystick {
		drawTextCCentered("自由摇杆：左半屏按下生成", w*0.25, h/2-10*s, 16*s, rl.NewColor(205, 215, 235, 160))
	} else {
		c := lay.JoystickCenter
		rl.DrawCircleLines(int32(c.X), int32(c.Y), lay.JoystickRadius, rl.NewColor(205, 215, 235, 200))
		rl.DrawCircleLines(int32(c.X), int32(c.Y), lay.JoystickRadius*0.55, rl.NewColor(205, 215, 235, 100))
		rl.DrawCircleV(c, lay.JoystickRadius*0.38, rl.NewColor(235, 240, 250, 120))
	}

	// 快捷栏
	for i, r := range lay.HotbarSlots {
		rl.DrawRectangleRec(r, rl.NewColor(24, 28, 34, 220))
		outline := rl.NewColor(130, 138, 150, 200)
		if ed.selected == "hotbar" {
			outline = rl.NewColor(255, 224, 112, 255)
		}
		rl.DrawRectangleLinesEx(r, 2, outline)
		iconSize := int32(32 * s)
		ix := int32(r.X) + (int32(r.Width)-iconSize)/2
		iy := int32(r.Y) + (int32(r.Height)-iconSize)/2
		drawItemIcon(player.HotbarItems[i], ix, iy, iconSize)
	}

	// 选中高亮
	if ed.selected != "" && ed.selected != "hotbar" {
		var r rl.Rectangle
		if ed.selected == "joystick" {
			half := lay.JoystickRadius + 6
			r = rl.Rectangle{X: lay.JoystickCenter.X - half, Y: lay.JoystickCenter.Y - half, Width: half * 2, Height: half * 2}
		} else {
			r = buttonRectByName(lay, ed.selected)
		}
		rl.DrawRectangleLinesEx(r, 3, rl.NewColor(255, 224, 112, 255))
	}

	// 选中项的尺寸滑块（顶部滑块带内）
	if ed.selected != "" {
		label := ""
		v := float32(1)
		switch {
		case containsStr(settings.ButtonOrder, ed.selected):
			label = buttonNameC(ed.selected) + "尺寸"
			if o, ok := set.Buttons[ed.selected]; ok {
				v = o.Scale
			}
		case ed.selected == "hotbar":
			label = "物品栏尺寸"
			v = set.HotbarScale
		default:
			label = "拖动调整位置"
		}
		bar := rl.Rectangle{X: band.X + 150*s, Y: band.Y + band.Height/2 - 9*s, Width: band.Width - 260*s, Height: 18 * s}
		drawTextC(label, band.X + 14*s, band.Y + band.Height/2 - 11*s, 16*s, rl.White)
		switch {
		case containsStr(settings.ButtonOrder, ed.selected):
			if nv := settings.ClampScale(sliderC(bar, v, 0.5, 2, &ed.sizeDrag, down)); nv != v {
				live := buttonRectByName(lay, ed.selected)
				c := rectCenter(live)
				// 中心不动，仅改缩放；buildLayout 下一帧按新尺寸重建矩形
				set.Buttons[ed.selected] = settings.Btn{NX: c.X / w, NY: c.Y / h, Scale: nv}
			}
		case ed.selected == "hotbar":
			set.HotbarScaleSet = true
			set.HotbarScale = settings.ClampScale(sliderC(bar, set.HotbarScale, 0.5, 2, &ed.barDrag, down))
		default:
			rl.DrawRectangleRounded(bar, 0.5, 6, rl.NewColor(58, 66, 82, 120))
		}
	}
}
