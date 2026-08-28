// Package input 把每帧输入抽象成一个 State 快照。桌面实现直读 raylib 的
// 键盘/鼠标（与移植前的直读代码一一对应）；安卓实现把多点触控路由到虚拟
// 摇杆、按钮与手势后合成同样的 State。消费方（player）不再关心输入来源。
package input

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

// Layout 是触屏控件在屏幕上的命中区域，每帧由 UI 层按当前窗口尺寸重建。
// 桌面端 Read 不使用它，但它同时供 HUD 绘制使用，保证绘制与命中一致。
type Layout struct {
	JoystickZone   rl.Rectangle  // 摇杆捕获区（左下角）
	JoystickCenter rl.Vector2    // 摇杆基座中心
	JoystickRadius float32       // 摇杆基座半径
	JumpBtn        rl.Rectangle  // 跳跃/上升
	FlyBtn         rl.Rectangle  // 飞行切换
	DescendBtn     rl.Rectangle  // 飞行下降（仅飞行时生效）
	PauseBtn       rl.Rectangle  // 屏上暂停按钮
	ResumeBtn      rl.Rectangle  // 暂停界面中央恢复按钮
	HotbarSlots    []rl.Rectangle // 快捷栏槽位，索引与 player.HotbarItems 对齐
	Slop           float32       // 触点位移超过该值即视为滑动，取消轻点/长按
}

// State 是一帧输入的完整快照。"沿触发"字段（Pressed/Toggle 类）只在事件
// 发生的那一帧为 true，语义与原来 player 直读的 IsKeyPressed/IsMouseButtonPressed
// 完全一致；Held 类对应 IsKeyDown。
type State struct {
	LookDX, LookDY float32 // 视角增量（已按平台灵敏度折算的像素）
	MoveFwd        float32 // -1..1，+ 为前进（桌面 W/S，安卓摇杆推拉）
	MoveRight      float32 // -1..1，+ 为右移（桌面 D/A，安卓摇杆推拉）
	Run            bool    // 冲刺（桌面左 Shift，安卓摇杆推满）
	JumpPressed    bool    // 沿触发
	JumpHeld       bool
	FlyToggle      bool // 沿触发
	DescendHeld    bool // 飞行下降（桌面左 Ctrl）
	BreakPressed   bool // 沿触发（桌面左键，安卓轻点）
	PlacePressed   bool // 沿触发（桌面右键，安卓长按）
	HotbarSlot     int  // >=0 直接选中槽位，-1 表示本帧无
	HotbarDelta    int  // -1/+1 循环移动（桌面滚轮）
	PauseToggle    bool // 沿触发（桌面 Esc，安卓暂停按钮/BACK/恢复按钮）
	ToggleHUD      bool // 沿触发（桌面 F1）
}

// Read 产出本帧输入快照。
func Read(l *Layout, flying, paused bool) State {
	return platformRead(l, flying, paused)
}

// JoystickKnob 返回安卓虚拟摇杆的可视状态：基座中心、杆端位置、是否激活。
// 桌面端始终返回 false。
func JoystickKnob() (center rl.Vector2, knob rl.Vector2, active bool) {
	return platformJoystick()
}
