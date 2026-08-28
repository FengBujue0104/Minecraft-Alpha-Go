//go:build android

// 金丝雀 E：绕过 raylib 封装，先用 ANativeWindow 原生绘制色块里程碑
// （红→绿→蓝），再进入 raylib 的 InitWindow。崩溃点的肉眼判定：
//   - 没出现任何颜色 → rl 包初始化（dlopen 阶段）就崩了
//   - 停在红色 → 等窗口句柄阶段崩
//   - 停在绿色 → 原生色块绘制崩
//   - 出现蓝色后闪退 → 死在 rl.InitWindow 内部（EGL/GL 初始化）
//   - 看到 "RAYLIB OK" 文字 → 一切正常（约 6 秒后退出）
//
// 注意：glue 的 .c 已由 rl 包编译（android_main 由它提供），这里只引用
// 头文件并 extern raylib-go 导出的 GetAndroidApp()，避免符号重复。
package main

/*
#include <android_native_app_glue.h>
#include <android/native_window.h>
#include <unistd.h>

extern struct android_app* GetAndroidApp();

static int waitWindow(int timeoutMs) {
	struct android_app* app = GetAndroidApp();
	if (app == NULL) return 0;
	for (int waited = 0; app->window == NULL && waited < timeoutMs; waited += 100) {
		usleep(100 * 1000);
		if (app->destroyRequested) return 0;
	}
	return app->window != NULL;
}

static void paintScreen(int r, int g, int b) {
	struct android_app* app = GetAndroidApp();
	if (app == NULL || app->window == NULL) return;
	ANativeWindow_Buffer buf;
	if (ANativeWindow_lock(app->window, &buf, NULL) != 0) return;
	uint32_t c = 0xFF000000u | ((uint32_t)r << 16) | ((uint32_t)g << 8) | (uint32_t)b;
	uint32_t* px = (uint32_t*)buf.bits;
	for (int y = 0; y < buf.height; y++) {
		uint32_t* line = px + (size_t)y * buf.stride;
		for (int x = 0; x < buf.width; x++) line[x] = c;
	}
	ANativeWindow_unlockAndPost(app->window);
}

static void sleepMs(int ms) { usleep(ms * 1000); }
*/
import "C"

import (
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
)

func init() { rl.SetMain(run) } // c-shared 下 main() 不执行，必须在 init 注册

func main() {}

func run() {
	// 阶段 0：等窗口句柄（不需要 raylib）
	if C.waitWindow(8000) == 0 {
		return
	}
	C.paintScreen(220, 30, 30) // 红
	C.sleepMs(900)
	C.paintScreen(30, 200, 60) // 绿
	C.sleepMs(900)
	C.paintScreen(40, 60, 220) // 蓝：蓝屏停留期间进入 raylib 初始化

	// 阶段 1：raylib 初始化 + 绘制
	rl.SetConfigFlags(rl.FlagVsyncHint)
	rl.InitWindow(1280, 720, "MC-Go Canary E")
	defer rl.CloseWindow()
	rl.SetTargetFPS(60)

	rl.BeginDrawing()
	rl.ClearBackground(rl.SkyBlue)
	rl.DrawText("RAYLIB OK - INIT + DRAW WORKS", 40, 60, 30, rl.Black)
	rl.EndDrawing()
	time.Sleep(4 * time.Second)
}
