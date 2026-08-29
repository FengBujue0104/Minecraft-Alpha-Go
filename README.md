# Minecraft Alpha Go

一个使用 Go 和 [raylib-go](https://github.com/gen2brain/raylib-go) 制作的 Minecraft 风格体素沙盒原型。项目包含程序化地形、动态区块、第一人称建造、飞行与简洁的 HUD。

## 功能

- 16×128×16 动态区块与异步程序化地形生成
- 草地、泥土、石头、木头、树叶、沙子、玻璃、水和基岩
- 第一人称视角、碰撞、跳跃、游泳和飞行
- 鼠标射线选方块，以及放置/破坏方块和音效
- 九格物品快捷栏：数字键或鼠标滚轮切换
- 水桶工具：右键放水，左键移除水
- ESC 半透明暂停界面与播放/暂停过渡动画
- Windows 游戏窗口自动屏蔽中文输入法候选和组合输入
- 安卓触屏版：虚拟摇杆 + 手势操作（见 `android/`，`android-port` 分支）

## 操作

| 按键 / 输入 | 操作 |
| --- | --- |
| `W` `A` `S` `D` | 移动 |
| 鼠标 | 转动视角 |
| `Space` | 跳跃；飞行时上升 |
| `Left Shift` | 奔跑 |
| `F` | 开关飞行模式 |
| `Left Ctrl` | 飞行时下降 |
| 左键 | 破坏方块；水桶时移除水 |
| 右键 | 放置当前物品；水桶时放水 |
| `1`–`9` / 鼠标滚轮 | 切换快捷栏物品 |
| `Esc` | 暂停 / 继续 |
| `F1` | 显示 / 隐藏 HUD |

### 安卓触屏

| 触屏操作 | 功能 |
| --- | --- |
| 左半屏按下 | 在按下处生成动态虚拟摇杆，推动移动，推到边缘冲刺 |
| 右半屏滑动 | 转动视角 |
| 轻点屏幕 | 破坏准星指向的方块 |
| 长按屏幕 | 放置当前物品 |
| 破坏按钮（X） | 破坏准星指向的方块，按住连续破坏 |
| 放置按钮（方块） | 放置当前物品 |
| 跳跃按钮 | 跳跃；按住上升/游泳上浮 |
| `FLY` 按钮 | 开关飞行模式，飞行时出现下降按钮 |
| 点快捷栏槽位 | 选择物品 |
| 右上按钮 / 系统返回键 | 暂停；暂停时点中央按钮继续 |
| 左上角信息行 | 坐标、FPS 与当前渲染档位（设置内可调） |

## 构建与运行

### 前置条件

- Go 1.26 或更新版本
- 支持 CGo 的编译环境
- raylib 原生库

Windows 下，请确保 `raylib.dll` 与生成的可执行文件在同一目录，或已位于 `%PATH%` 中。

```powershell
go build .
.\mc-go.exe
```

开发时也可以直接运行：

```powershell
go run .
```

### 安卓（arm64 APK）

需要 Android SDK + **NDK r26**（r27 暂不支持），并设置 `ANDROID_SDK_ROOT`：

```bash
android/build-android.sh demo   # 冒烟测试 demo（旋转立方体 + 触点显示）
android/build-android.sh game   # 完整游戏
# 产物: android/build/mcgo-debug.apk / mcgo-demo-debug.apk
```

打包不走 Gradle：NativeActivity + `hasCode=false` 的应用没有 Java 代码，
脚本直接用 build-tools 的 `aapt2`/`zipalign`/`apksigner` 手工组装签名，
对构建机内存友好。

## 验证

```powershell
go test ./...
go vet ./...
```

## 项目结构

```text
main.go       游戏循环、HUD、暂停界面、触屏布局与控件绘制
blocks/       方块类型、颜色与碰撞属性
world/        区块生命周期、地形生成与渲染
player/       输入消费、相机、物理、射线检测与方块交互
input/        输入抽象：桌面键盘/鼠标适配 + 安卓多点触控路由
audio/        程序生成的放置与破坏音效
android/      安卓壳工程：Manifest、构建脚本与冒烟测试 demo
```
