//go:build android

// glue.go：编译 native_app_glue 并定义 android_main。cgo 会把 //export
// 所在文件的 prelude 复制进 _cgo_export.c，若在本文件同时 //export 会造成
// glue 符号重复定义，因此 export 放在 tick.go。
package main

/*
#cgo android LDFLAGS: -llog -landroid
#include "android_native_app_glue.c"

extern void goTick();

static struct android_app* g_app;

void android_main(struct android_app* app) {
	g_app = app;
	goTick();
}

void finishActivity() {
	if (g_app != NULL && g_app->activity != NULL) {
		ANativeActivity_finish(g_app->activity);
	}
}
*/
import "C"
