//go:build android

// 金丝雀 D：raylib 分级冒烟测试。每个阶段成功后立即把状态画到屏幕上；
// 闪退时"最后看到的文字"就是最后成功的阶段，无需 adb 即可定位故障层。
package main

import (
	"fmt"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"mc-go/audio"
)

func main() { rl.SetMain(run) }

func run() {
	draw := func(lines ...string) {
		rl.BeginDrawing()
		rl.ClearBackground(rl.SkyBlue)
		for i, l := range lines {
			rl.DrawText(l, 20, 20+int32(i)*28, 22, rl.Black)
		}
		rl.EndDrawing()
	}

	rl.SetConfigFlags(rl.FlagVsyncHint)
	rl.InitWindow(0, 0, "MC-Go Canary D")
	defer rl.CloseWindow()

	// S1 窗口 + EGL/GLES 上下文
	draw("S1 WINDOW OK",
		fmt.Sprintf("%dx%d", rl.GetScreenWidth(), rl.GetScreenHeight()))
	time.Sleep(2 * time.Second)

	// S2 帧率/主循环
	rl.SetTargetFPS(60)
	draw("S1 WINDOW OK", "S2 MAIN LOOP OK")
	time.Sleep(1 * time.Second)

	// S3 音频设备 + 游戏同款程序化波形
	effects := audio.New()
	defer effects.Close()
	effects.PlayPlace()
	draw("S1-S2 OK", "S3 AUDIO OK")
	time.Sleep(2 * time.Second)

	// S4 3D 渲染原语（游戏渲染全靠 DrawTriangle3D）
	draw("S1-S3 OK", "S4 3D TRIANGLE ...")
	cam := rl.NewCamera3D(rl.NewVector3(2, 2, 2), rl.NewVector3(0, 0, 0),
		rl.NewVector3(0, 1, 0), 60, rl.CameraPerspective)
	rl.BeginDrawing()
	rl.ClearBackground(rl.SkyBlue)
	rl.BeginMode3D(cam)
	rl.DrawTriangle3D(rl.NewVector3(0, 0, 0), rl.NewVector3(1, 0, 0),
		rl.NewVector3(0, 1, 0), rl.Red)
	rl.EndMode3D()
	rl.DrawText("S4 3D TRIANGLE OK", 20, 20, 22, rl.Black)
	rl.EndDrawing()
	time.Sleep(2 * time.Second)

	// S5 触摸
	draw(fmt.Sprintf("ALL OK! touch_points=%d", rl.GetTouchPointCount()),
		"exiting in 3s ...")
	time.Sleep(3 * time.Second)
}
