//go:build android

// Package demo 是安卓构建链的最小冒烟测试：一个旋转立方体 + 触点坐标显示。
// 用于在接入完整游戏前验证 cgo 交叉编译、NativeActivity 入口、GLES2 渲染与触摸输入。
package main

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
)

func init() {
	// Android 的 c-shared 构建不会执行 main()，必须在 init() 里注册回调
	// （写在 main() 里永远不会被调用，表现为启动即退出）。
	rl.SetMain(gameMain)
}

func main() {}

func gameMain() {
	rl.SetConfigFlags(rl.FlagVsyncHint)
	rl.InitWindow(0, 0, "Minecraft Go - Android Demo")
	defer rl.CloseWindow()

	camera := rl.NewCamera3D(
		rl.NewVector3(4, 3, 4),
		rl.NewVector3(0, 0, 0),
		rl.NewVector3(0, 1, 0),
		60, rl.CameraPerspective,
	)

	rl.SetTargetFPS(60)

	for !rl.WindowShouldClose() {
		w := int32(rl.GetScreenWidth())
		h := int32(rl.GetScreenHeight())

		rl.BeginDrawing()
		rl.ClearBackground(rl.SkyBlue)

		rl.BeginMode3D(camera)
		rl.DrawGrid(10, 1.0)
		rl.DrawCube(rl.NewVector3(0, 0.5, 0), 1, 1, 1, rl.Green)
		rl.DrawCubeWires(rl.NewVector3(0, 0.5, 0), 1, 1, 1, rl.Black)
		rl.EndMode3D()

		rl.DrawText("Minecraft Go demo", 16, 16, 24, rl.Black)
		rl.DrawText(fmt.Sprintf("screen=%dx%d fps=%d", w, h, rl.GetFPS()), 16, 48, 20, rl.Black)

		if rl.GetTouchPointCount() > 0 {
			t := rl.GetTouchPosition(0)
			rl.DrawCircleV(t, 24, rl.NewColor(255, 90, 0, 160))
			rl.DrawText(fmt.Sprintf("touch[0]=(%.0f,%.0f)", t.X, t.Y), 16, 76, 20, rl.Red)
		} else {
			rl.DrawText("no touch", 16, 76, 20, rl.Gray)
		}

		rl.EndDrawing()
	}
}
