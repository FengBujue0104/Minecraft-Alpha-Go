//go:build android && androiddebug

package main

// 以 `go build -tags androiddebug` 构建时开启屏上启动里程碑（黑底绿字）。
const debugUI = true
