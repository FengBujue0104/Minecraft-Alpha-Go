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

	for !rl.WindowShouldClose() {
		frameTime := rl.GetFrameTime()
		if frameTime > 0.25 {
			frameTime = 0.25
		}

		if rl.IsKeyPressed(rl.KeyEscape) {
			paused = !paused
			pauseUI.SetPaused(paused)
			if paused {
				rl.EnableCursor()
			} else {
				rl.DisableCursor()
				p.SkipNextMouseFrame()
			}
		}

		if rl.IsKeyPressed(rl.KeyF1) {
			showUI = !showUI
		}

		if !paused {
			p.Update(w)
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
			drawHUD(p)
		}
		drawPauseOverlay(&pauseUI)

		rl.EndDrawing()
	}
}

func drawHUD(p *player.Player) {
	cx := int32(WindowWidth / 2)
	cy := int32(WindowHeight / 2)
	rl.DrawLine(cx-8, cy, cx+8, cy, rl.Black)
	rl.DrawLine(cx, cy-8, cx, cy+8, rl.Black)

	posText := fmt.Sprintf("XYZ: %.1f / %.1f / %.1f",
		math.Round(float64(p.Position.X)*10)/10,
		math.Round(float64(p.Position.Y)*10)/10,
		math.Round(float64(p.Position.Z)*10)/10,
	)
	rl.DrawText(posText, 10, 10, 14, rl.White)

	controls := []string{
		"WASD: Move | Space: Jump | Shift: Run | F: Fly",
		"Left Click: Break | Right Click: Place | 1-9: Select item",
		"Water bucket: right-click place water, left-click remove water",
		"Esc: Pause | F1: Toggle HUD",
	}
	for i, text := range controls {
		rl.DrawText(text, 10, 34+int32(i*16), 12, rl.Gray)
	}
	if p.Flying {
		rl.DrawText("FLYING  Space: Up  Left Ctrl: Down", 10, 104, 14, rl.NewColor(255, 232, 120, 255))
	}
	drawHotbar(p)
}

func drawHotbar(p *player.Player) {
	const slotSize int32 = 52
	const slotGap int32 = 4
	count := int32(len(player.HotbarItems))
	barWidth := count*slotSize + (count-1)*slotGap
	startX := (int32(WindowWidth) - barWidth) / 2
	y := int32(WindowHeight) - slotSize - 18

	for i, item := range player.HotbarItems {
		x := startX + int32(i)*(slotSize+slotGap)
		rect := rl.Rectangle{X: float32(x), Y: float32(y), Width: float32(slotSize), Height: float32(slotSize)}
		rl.DrawRectangleRec(rect, rl.NewColor(24, 28, 34, 220))
		outline := rl.NewColor(130, 138, 150, 230)
		if i == p.SelectedHotbarSlot() {
			outline = rl.NewColor(255, 224, 112, 255)
			rl.DrawRectangleRec(rl.Rectangle{X: float32(x - 2), Y: float32(y - 2), Width: float32(slotSize + 4), Height: float32(slotSize + 4)}, outline)
			rl.DrawRectangleRec(rect, rl.NewColor(32, 36, 43, 245))
		}
		rl.DrawRectangleLinesEx(rect, 2, outline)
		drawItemIcon(item, x+10, y+13, 32)
		number := fmt.Sprintf("%d", i+1)
		rl.DrawText(number, x+4, y+3, 12, rl.White)
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

func drawPauseOverlay(o *pauseOverlay) {
	if o.amount <= 0 {
		return
	}
	alpha := uint8(170 * o.amount)
	rl.DrawRectangle(0, 0, WindowWidth, WindowHeight, rl.NewColor(7, 10, 18, alpha))

	// Ease the central control from a slightly smaller size on both enter and
	// exit. The play glyph is retained during the exit fade.
	scale := 0.78 + 0.22*o.amount
	buttonW := int32(180 * scale)
	buttonH := int32(100 * scale)
	x := int32(WindowWidth/2) - buttonW/2
	y := int32(WindowHeight/2) - buttonH/2
	button := rl.Rectangle{X: float32(x), Y: float32(y), Width: float32(buttonW), Height: float32(buttonH)}
	rl.DrawRectangleRounded(button, 0.18, 8, rl.NewColor(38, 47, 64, uint8(245*o.amount)))
	rl.DrawRectangleRoundedLinesEx(button, 0.18, 8, 2, rl.NewColor(210, 220, 238, uint8(230*o.amount)))

	if o.showPlay {
		rl.DrawTriangle(rl.NewVector2(float32(x+buttonW/2-12), float32(y+28)), rl.NewVector2(float32(x+buttonW/2-12), float32(y+buttonH-28)), rl.NewVector2(float32(x+buttonW/2+26), float32(y+buttonH/2)), rl.White)
	} else {
		rl.DrawRectangle(x+buttonW/2-25, y+27, 16, buttonH-54, rl.White)
		rl.DrawRectangle(x+buttonW/2+9, y+27, 16, buttonH-54, rl.White)
	}
	label := "PAUSED"
	if o.showPlay {
		label = "PLAYING"
	}
	labelWidth := rl.MeasureText(label, 20)
	rl.DrawText(label, int32(WindowWidth/2)-labelWidth/2, y+buttonH+18, 20, rl.NewColor(235, 240, 250, uint8(255*o.amount)))
}
