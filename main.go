package main

import (
	"fmt"
	"math"
	"os"
	"runtime/debug"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"mc-go/player"
	"mc-go/world"
)

const (
	WindowWidth  = 1280
	WindowHeight = 720
	WindowTitle  = "Minecraft Go"
	PhysicsDt    = 1.0 / 60.0
)

func main() {
	// Panic recovery: write crash info to log file
	defer func() {
		if r := recover(); r != nil {
			f, _ := os.Create("crash.log")
			defer f.Close()
			fmt.Fprintf(f, "PANIC: %v\n\nStack:\n%s\n", r, debug.Stack())
			fmt.Printf("CRASH! See crash.log for details.\n")
		}
	}()

	rl.SetConfigFlags(rl.FlagMsaa4xHint)
	rl.InitWindow(WindowWidth, WindowHeight, WindowTitle)
	defer rl.CloseWindow()

	rl.SetTargetFPS(60)
	rl.DisableCursor()

	seed := time.Now().UnixNano()
	fmt.Printf("Seed: %d\n", seed)
	w := world.NewWorld(seed)

	spawnX := float32(0)
	spawnZ := float32(0)

	fmt.Println("Loading initial chunks...")
	w.EnsureChunksAround(spawnX, spawnZ)
	w.FlushGenerations()
	fmt.Printf("Done! Loaded %d chunks.\n", w.ChunkCount())

	spawnY := float32(w.GroundHeight(int(spawnX), int(spawnZ))) + 2.5
	if spawnY < 2.5 {
		spawnY = 100 // Fallback if no ground found
	}
	p := player.NewPlayer(spawnX, spawnY, spawnZ)
	p.SkipNextMouseFrame() // initial cursor-disable spike

	showUI := true
	physicsAccum := float32(0)

	for !rl.WindowShouldClose() {
		frameTime := rl.GetFrameTime()
		if frameTime > 0.25 {
			frameTime = 0.25
		}

		if rl.IsKeyPressed(rl.KeyEscape) {
			if rl.IsCursorHidden() {
				rl.EnableCursor()
			} else {
				rl.DisableCursor()
				p.SkipNextMouseFrame()
			}
		}

		if rl.IsKeyPressed(rl.KeyF1) {
			showUI = !showUI
		}

		if rl.IsCursorHidden() {
			p.Update(w)

			physicsAccum += frameTime
			for physicsAccum >= PhysicsDt {
				p.StepPhysics(w, PhysicsDt)
				physicsAccum -= PhysicsDt
			}
		} else {
			physicsAccum = 0
		}

		w.EnsureChunksAround(p.Position.X, p.Position.Z)
		w.ProcessGenerations()
		w.ProcessDirty(world.DirtyBudgetPerFrame)

		rl.BeginDrawing()
		rl.ClearBackground(rl.SkyBlue)

		rl.BeginMode3D(p.Camera)
		w.Render()

		if p.TargetFace >= 0 {
			bx := float32(p.TargetBlockPos[0])
			by := float32(p.TargetBlockPos[1])
			bz := float32(p.TargetBlockPos[2])
			center := rl.NewVector3(bx+0.5, by+0.5, bz+0.5)
			rl.DrawCubeWires(center, 1.005, 1.005, 1.005, rl.Black)
		}

		rl.EndMode3D()

		if showUI {
			drawHUD(p)
		}

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

	selectedText := fmt.Sprintf("Block: %d | Scroll to change", p.SelectedBlock)
	rl.DrawText(selectedText, 10, int32(WindowHeight)-30, 14, rl.White)

	controls := []string{
		"WASD: Move | Space: Jump | Shift: Run",
		"Left Click: Break | Right Click: Place",
		"Mouse: Look | Scroll: Change Block",
		"Esc: Release cursor | F1: Toggle HUD",
	}
	for i, text := range controls {
		rl.DrawText(text, 10, int32(WindowHeight)-70+int32(i*16), 12, rl.Gray)
	}
}
