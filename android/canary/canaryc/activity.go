//go:build android

// activity.go：纯 C 的 NativeActivity 生命周期（与金丝雀 A 相同的流程），
// 不含 native_app_glue。//export 放在 tick.go（cgo 会把含 //export 文件的
// prelude 复制进 _cgo_export.c，同文件放定义会符号重复）。
package main

/*
#cgo android LDFLAGS: -llog -landroid
#include <android/native_activity.h>
#include <pthread.h>
#include <unistd.h>

extern void goTick();

static void* finisher(void* arg) {
	ANativeActivity* a = (ANativeActivity*)arg;
	goTick();   // C → Go 回调：验证运行时已初始化且可调用
	sleep(3);   // Go 侧返回后再等 3 秒
	ANativeActivity_finish(a);
	return NULL;
}

void ANativeActivity_onCreate(ANativeActivity* activity, void* savedState,
                              size_t savedStateSize) {
	(void)savedState;
	(void)savedStateSize;
	pthread_t t;
	pthread_create(&t, NULL, finisher, activity);
	pthread_detach(t);
}
*/
import "C"
