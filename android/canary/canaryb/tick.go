//go:build android

// tick.go：//export 必须单独成文件，原因见 glue.go 头部注释。
package main

/*
// 仅声明：定义在 glue.go；export 文件的 prelude 会被复制进 _cgo_export.c，
// 放定义会符号重复。
extern void finishActivity();
*/
import "C"

import "time"

func main() {}

//export goTick
func goTick() {
	time.Sleep(6 * time.Second)
	C.finishActivity()
}
