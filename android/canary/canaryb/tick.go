//go:build android

// tick.go：//export 必须单独成文件，原因见 glue.go 头部注释。
package main

import "time"

func main() {}

//export goTick
func goTick() {
	time.Sleep(6 * time.Second)
	C.finishActivity()
}
