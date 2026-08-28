#!/usr/bin/env bash
# Minecraft-Alpha-Go 安卓构建脚本（arm64-v8a）
#
# 用法:
#   ./build-android.sh game     # 完整游戏
#   ./build-android.sh demo     # 冒烟测试 demo（旋转立方体 + 触点显示）
#   ./build-android.sh canaryA  # 金丝雀 A：纯 C，测打包/加载链
#   ./build-android.sh canaryB  # 金丝雀 B：纯 Go 运行时，测 Go c-shared 初始化
#
# 说明: 不用 Gradle——NativeActivity + hasCode=false 的应用没有 Java 代码，
# 直接用 build-tools 的 aapt2/zipalign/apksigner 手工打包，内存占用极小。
set -euo pipefail

TARGET="${1:-game}"

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="$PATH:/usr/local/go/bin"
SDK="${ANDROID_SDK_ROOT:-/opt/android-sdk}"
NDK="${ANDROID_NDK_ROOT:-$SDK/ndk/26.3.11579264}"
API=21
ABI=arm64-v8a
BUILD_TOOLS="$SDK/build-tools/34.0.0"
CC="$NDK/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android${API}-clang"
BUILD_DIR="$REPO_ROOT/android/build"
mkdir -p "$BUILD_DIR"

# 目标参数: MODE(go|c) LIB(库名) PKG/Manifest/APK
MODE=go LIB=game PKG="." MANIFEST="android/app/AndroidManifest.xml" APK_NAME="mcgo-debug.apk"
case "$TARGET" in
  demo)
    PKG="./android/demo"; MANIFEST="android/demo/AndroidManifest.xml"; APK_NAME="mcgo-demo-debug.apk"
    ;;
  canaryA)
    MODE=c LIB=canarya SRC="android/canary/canary_a.c"
    MANIFEST="android/canary/AndroidManifestCanaryA.xml"; APK_NAME="canary-a-debug.apk"
    ;;
  canaryB)
    LIB=canaryb PKG="./android/canary/canaryb"
    MANIFEST="android/canary/AndroidManifestCanaryB.xml"; APK_NAME="canary-b-debug.apk"
    ;;
  game)
    ;;
  *)
    echo "未知目标: $TARGET (可选: game | demo | canaryA | canaryB)" >&2
    exit 1
    ;;
esac

cd "$REPO_ROOT"
echo "==> [1/5] 编译 native 库 (lib$LIB.so, $ABI)"
if [ "$MODE" = "c" ]; then
  # 纯 C：NDK clang 直接编译，链接系统库
  "$CC" -shared -fPIC -O2 "$SRC" -o "$BUILD_DIR/lib$LIB.so" -llog -landroid
else
  # 16KB 页对齐：NDK r26 默认 4KB，在 16KB 页内核的新设备（Pixel 8/9 等）上
  # 这类 .so 会加载即崩；显式升到 16384 对两种设备都兼容。
  # CGO_CFLAGS 引入 raylib-go vendor 的 native_app_glue 头（崩溃日志辅助与
  # 金丝雀 B 都会用到）。
  RL_DIR="$(go list -m -f '{{.Dir}}' github.com/gen2brain/raylib-go/raylib)"
  CGO_ENABLED=1 GOOS=android GOARCH=arm64 CC="$CC" \
    CGO_CFLAGS="-I$RL_DIR/external/android/native_app_glue" \
    go build -buildmode=c-shared -buildvcs=false -trimpath \
    -ldflags "-extldflags '-Wl,-z,max-page-size=16384 -Wl,-z,common-page-size=16384'" \
    -o "$BUILD_DIR/lib$LIB.so" "$PKG"
fi

echo "==> [2/5] aapt2 生成基础 APK"
"$BUILD_TOOLS/aapt2" link \
  -o "$BUILD_DIR/base.apk" \
  --manifest "$MANIFEST" \
  -I "$SDK/platforms/android-34/android.jar" \
  --auto-add-overlay

echo "==> [3/5] 注入 native 库 lib/$ABI/lib$LIB.so"
mkdir -p "$BUILD_DIR/lib/$ABI"
cp "$BUILD_DIR/lib$LIB.so" "$BUILD_DIR/lib/$ABI/lib$LIB.so"
cd "$BUILD_DIR"
rm -f "$APK_NAME" aligned.apk
python3 - "$BUILD_DIR" "$ABI" "$LIB" <<'EOF'
import sys, zipfile, os
build, abi, lib = sys.argv[1], sys.argv[2], sys.argv[3]
with zipfile.ZipFile(os.path.join(build, "base.apk"), "a") as z:
    z.write(os.path.join(build, f"lib/{abi}/lib{lib}.so"), f"lib/{abi}/lib{lib}.so")
EOF

echo "==> [4/5] zipalign"
"$BUILD_TOOLS/zipalign" -f 4 base.apk aligned.apk

echo "==> [5/5] apksigner 签名"
KEYSTORE="$BUILD_DIR/debug.keystore"
if [ ! -f "$KEYSTORE" ]; then
  keytool -genkeypair -keystore "$KEYSTORE" -storetype PKCS12 \
    -alias androiddebugkey -storepass android -keypass android \
    -keyalg RSA -keysize 2048 -validity 10000 \
    -dname "CN=Android Debug,O=Android,C=US" >/dev/null 2>&1
fi
"$BUILD_TOOLS/apksigner" sign \
  --ks "$KEYSTORE" --ks-pass pass:android --key-pass pass:android \
  --out "$APK_NAME" aligned.apk
"$BUILD_TOOLS/apksigner" verify "$APK_NAME"

echo ""
echo "✅ 构建完成: $BUILD_DIR/$APK_NAME"
ls -lh "$APK_NAME"
