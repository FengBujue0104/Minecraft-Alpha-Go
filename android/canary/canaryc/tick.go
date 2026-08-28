//go:build android

// tick.go：//export 单独成文件（见 activity.go 注释）。
// goTick 里额外跑一个 goroutine + channel，深入检验 Go 调度器与定时器。
package main

/*
extern void goTick();
*/
import "C"

import "time"

func main() {}

//export goTick
func goTick() {
	ch := make(chan struct{})
	go func() {
		time.Sleep(500 * time.Millisecond)
		close(ch)
	}()
	<-ch
	time.Sleep(2 * time.Second)
}
