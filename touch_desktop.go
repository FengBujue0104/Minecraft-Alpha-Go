//go:build !android

package main

// 桌面端不绘制触屏控件，输入仍走键盘/鼠标（见 input/input_desktop.go）。
const touchControls = false
