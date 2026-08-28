//go:build android

// 金丝雀 F：裸 EGL 阶梯。不经 raylib，直接逐级调用 EGL/GLES2，每级成功
// 画一种亮色、失败画对应暗色并退出。最后再尝试 rl.InitWindow 验证 raylib。
//
//	白色 = 进入了 run() 且拿到窗口句柄
//	红色 = eglGetDisplay + eglInitialize 成功
//	绿色 = eglChooseConfig（ES2/RGB8/深度24）成功
//	蓝色 = eglCreateContext + eglCreateWindowSurface 成功
//	黄色 = eglMakeCurrent + glGetString 成功
//	"RAYLIB OK" 文字 = rl.InitWindow + 绘制成功
//
// 失败暗色：暗红=display 失败，暗绿=config 失败，暗蓝=context/surface 失败，
// 暗黄=makeCurrent 失败（每种停留 2.5 秒后自动退出）。
// glue 的 .c 由 rl 包编译，这里只引头文件 + extern GetAndroidApp。
package main

/*
#include <android_native_app_glue.h>
#include <android/native_window.h>
#include <EGL/egl.h>
#include <GLES2/gl2.h>
#include <unistd.h>

extern struct android_app* GetAndroidApp();

static EGLDisplay f_dpy = EGL_NO_DISPLAY;
static EGLConfig  f_cfg;
static EGLContext f_ctx = EGL_NO_CONTEXT;
static EGLSurface f_surf = EGL_NO_SURFACE;

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
	// 显式统一为 RGBA8888：若缓冲区实为 RGB565 等 2 字节格式，按 4 字节
	// uint32 逐像素写会越界 2 倍直接 SIGSEGV（疑似 K70 秒崩的元凶）。
	ANativeWindow_setBuffersGeometry(app->window, 0, 0, WINDOW_FORMAT_RGBA_8888);
	ANativeWindow_Buffer buf;
	if (ANativeWindow_lock(app->window, &buf, NULL) != 0) return;
	if (buf.format == WINDOW_FORMAT_RGBA_8888 || buf.format == WINDOW_FORMAT_RGBX_8888) {
		uint32_t c = 0xFF000000u | ((uint32_t)r << 16) | ((uint32_t)g << 8) | (uint32_t)b;
		uint32_t* px = (uint32_t*)buf.bits;
		for (int y = 0; y < buf.height; y++) {
			uint32_t* line = px + (size_t)y * buf.stride;
			for (int x = 0; x < buf.width; x++) line[x] = c;
		}
	} else if (buf.format == WINDOW_FORMAT_RGB_565) {
		uint16_t c = (uint16_t)(((uint16_t)(r >> 3) << 11) | ((uint16_t)(g >> 2) << 5) | (uint16_t)(b >> 3));
		uint16_t* px = (uint16_t*)buf.bits;
		for (int y = 0; y < buf.height; y++) {
			uint16_t* line = px + (size_t)y * buf.stride;
			for (int x = 0; x < buf.width; x++) line[x] = c;
		}
	}
	ANativeWindow_unlockAndPost(app->window);
}

static void sleepMs(int ms) { usleep(ms * 1000); }

static int stageDisplay() {
	f_dpy = eglGetDisplay(EGL_DEFAULT_DISPLAY);
	if (f_dpy == EGL_NO_DISPLAY) return 0;
	if (!eglInitialize(f_dpy, NULL, NULL)) return 0;
	return 1;
}

static int stageConfig() {
	const EGLint attribs[] = {
		EGL_RENDERABLE_TYPE, EGL_OPENGL_ES2_BIT,
		EGL_RED_SIZE, 8, EGL_GREEN_SIZE, 8, EGL_BLUE_SIZE, 8,
		EGL_DEPTH_SIZE, 24,
		EGL_NONE };
	EGLint n = 0;
	if (!eglChooseConfig(f_dpy, attribs, &f_cfg, 1, &n) || n < 1) return 0;
	return 1;
}

static int stageContextSurface() {
	struct android_app* app = GetAndroidApp();
	if (app == NULL || app->window == NULL) return 0;
	const EGLint ctxAttribs[] = { EGL_CONTEXT_CLIENT_VERSION, 2, EGL_NONE };
	f_ctx = eglCreateContext(f_dpy, f_cfg, EGL_NO_CONTEXT, ctxAttribs);
	if (f_ctx == EGL_NO_CONTEXT) return 0;
	f_surf = eglCreateWindowSurface(f_dpy, f_cfg, app->window, NULL);
	if (f_surf == EGL_NO_SURFACE) return 0;
	return 1;
}

static int stageMakeCurrent() {
	if (!eglMakeCurrent(f_dpy, f_surf, f_surf, f_ctx)) return 0;
	const char* v = (const char*)glGetString(GL_VERSION);
	if (v == NULL) return 0;
	eglMakeCurrent(f_dpy, EGL_NO_SURFACE, EGL_NO_SURFACE, EGL_NO_CONTEXT);
	return 1;
}

static void stageCleanup() {
	if (f_surf != EGL_NO_SURFACE) { eglDestroySurface(f_dpy, f_surf); f_surf = EGL_NO_SURFACE; }
	if (f_ctx != EGL_NO_CONTEXT) { eglDestroyContext(f_dpy, f_ctx); f_ctx = EGL_NO_CONTEXT; }
}
*/
import "C"

import (
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
)

func init() { rl.SetMain(run) } // c-shared 下 main() 不执行，必须在 init 注册

func main() {}

func run() {
	if C.waitWindow(8000) == 0 {
		return
	}
	C.paintScreen(255, 255, 255) // 白：进入 run() 且窗口就绪
	C.sleepMs(800)

	if C.stageDisplay() == 0 {
		C.paintScreen(80, 0, 0)
		C.sleepMs(2500)
		return
	}
	C.paintScreen(255, 40, 40) // 红
	C.sleepMs(700)

	if C.stageConfig() == 0 {
		C.paintScreen(0, 80, 0)
		C.sleepMs(2500)
		return
	}
	C.paintScreen(40, 220, 60) // 绿
	C.sleepMs(700)

	if C.stageContextSurface() == 0 {
		C.paintScreen(0, 0, 80)
		C.sleepMs(2500)
		return
	}
	C.paintScreen(40, 60, 230) // 蓝
	C.sleepMs(700)

	if C.stageMakeCurrent() == 0 {
		C.paintScreen(80, 80, 0)
		C.sleepMs(2500)
		return
	}
	C.paintScreen(240, 220, 40) // 黄：裸 EGL/GLES2 全部可用
	C.stageCleanup()
	C.sleepMs(900)

	// 终极验证：raylib 自己的初始化 + 绘制
	rl.SetConfigFlags(rl.FlagVsyncHint)
	rl.InitWindow(1280, 720, "MC-Go Canary F")
	defer rl.CloseWindow()
	rl.SetTargetFPS(60)
	rl.BeginDrawing()
	rl.ClearBackground(rl.SkyBlue)
	rl.DrawText("RAYLIB OK - INIT + DRAW WORKS", 40, 60, 30, rl.Black)
	rl.EndDrawing()
	time.Sleep(3 * time.Second)
}
