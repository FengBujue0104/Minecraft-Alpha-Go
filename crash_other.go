//go:build !android

package main

import "fmt"

func initCrashLog() {}

// appDataDir 桌面端使用当前目录。
func appDataDir() string { return "." }

func logLocal(msg string) {
	fmt.Println(msg)
	startupMilestones = append(startupMilestones, msg)
}
