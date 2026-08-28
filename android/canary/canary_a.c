// 金丝雀 A：纯 C 的 NativeActivity，不含 Go、不含 raylib。
// 验证最底层链路：安装 → Manifest → dlopen → ANativeActivity_onCreate。
// 行为：黑屏约 6 秒后自动退出。若它也闪退，问题在打包/清单层。
#include <android/native_activity.h>
#include <pthread.h>
#include <unistd.h>

static void *finisher(void *arg) {
    ANativeActivity *a = (ANativeActivity *)arg;
    sleep(6);
    ANativeActivity_finish(a);
    return NULL;
}

void ANativeActivity_onCreate(ANativeActivity *activity, void *savedState,
                              size_t savedStateSize) {
    (void)savedState;
    (void)savedStateSize;
    pthread_t t;
    pthread_create(&t, NULL, finisher, activity);
    pthread_detach(t);
}
