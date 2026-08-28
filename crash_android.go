//go:build android

package main

/*
#include "android_native_app_glue.h"

// raylib-go 的 android 平台层导出（同一 .so 内链接）。
extern struct android_app* GetAndroidApp();

static const char* mcInternalDataPath() {
	struct android_app* app = GetAndroidApp();
	if (app == NULL || app->activity == NULL) return NULL;
	return app->activity->internalDataPath;
}
*/
import "C"

import (
	"fmt"
	"os"
	"time"
)

var crashLogPath string

// initCrashLog 拿到应用私有目录。崩溃堆栈和执行里程碑写在这里，闪退后可用
// `adb shell run-as com.fengbujue0104.mcgo cat files/crash.log` 取出
// （需要 manifest 里的 android:debuggable="true"）。
func initCrashLog() {
	if p := C.mcInternalDataPath(); p != nil {
		crashLogPath = C.GoString(p) + "/crash.log"
	}
}

// logLocal 同时输出到 stdout、私有目录 crash.log 和屏上里程碑镜像
// （main.go 的 drawMilestones 会把它画出来，无 adb 也能看到死在哪个阶段）。
func logLocal(msg string) {
	fmt.Println(msg)
	startupMilestones = append(startupMilestones, msg)
	if crashLogPath == "" {
		return
	}
	f, err := os.OpenFile(crashLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("15:04:05.000"), msg)
}
