#!/usr/bin/env bash
# Minecraft-Alpha-Go 安卓构建脚本（arm64-v8a）
#
# 用法:
#   ./build-android.sh demo   # 构建冒烟测试 demo（旋转立方体）
#   ./build-android.sh game   # 构建完整游戏（Phase 1 之后可用）
#
# 说明: 不用 Gradle——NativeActivity + hasCode=false 的应用没有 Java 代码，
# 直接用 build-tools 的 aapt2/zipalign/apksigner 手工打包，内存占用极小。
set -euo pipefail

TARGET="${1:-demo}"

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="$PATH:/usr/local/go/bin"
SDK="${ANDROID_SDK_ROOT:-/opt/android-sdk}"
NDK="${ANDROID_NDK_ROOT:-$SDK/ndk/26.3.11579264}"
API=21
ABI=arm64-v8a
BUILD_TOOLS="$SDK/build-tools/34.0.0"
CC="$NDK/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android${API}-clang"

case "$TARGET" in
  demo)
    GO_PKG="./android/demo"
    MANIFEST="android/demo/AndroidManifest.xml"
    APK_NAME="mcgo-demo-debug.apk"
    ;;
  game)
    GO_PKG="."
    MANIFEST="android/app/AndroidManifest.xml"
    APK_NAME="mcgo-debug.apk"
    ;;
  *)
    echo "未知目标: $TARGET (可选: demo | game)" >&2
    exit 1
    ;;
esac

BUILD_DIR="$REPO_ROOT/android/build"
mkdir -p "$BUILD_DIR"

echo "==> [1/5] Go 交叉编译 ($GO_PKG -> libgame.so, $ABI)"
cd "$REPO_ROOT"
CGO_ENABLED=1 GOOS=android GOARCH=arm64 CC="$CC" \
  go build -buildmode=c-shared -buildvcs=false -trimpath \
  -o "$BUILD_DIR/libgame.so" "$GO_PKG"

echo "==> [2/5] aapt2 生成基础 APK"
"$BUILD_TOOLS/aapt2" link \
  -o "$BUILD_DIR/base.apk" \
  --manifest "$MANIFEST" \
  -I "$SDK/platforms/android-34/android.jar" \
  --auto-add-overlay

echo "==> [3/5] 注入 native 库 lib/$ABI/libgame.so"
mkdir -p "$BUILD_DIR/lib/$ABI"
cp "$BUILD_DIR/libgame.so" "$BUILD_DIR/lib/$ABI/libgame.so"
cd "$BUILD_DIR"
rm -f "$APK_NAME" aligned.apk
# 用 python 的 zipfile 保证只追加、不压缩 .so 以外的文件顺序问题；.so 本身允许 deflate（extractNativeLibs=true）
python3 - "$BUILD_DIR" <<'EOF'
import sys, zipfile, os
build = sys.argv[1]
with zipfile.ZipFile(os.path.join(build, "base.apk"), "a") as z:
    so = os.path.join(build, f"lib/arm64-v8a/libgame.so")
    z.write(so, f"lib/arm64-v8a/libgame.so")
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
