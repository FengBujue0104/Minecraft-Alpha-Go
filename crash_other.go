//go:build !android

package main

import "fmt"

func initCrashLog() {}

func logLocal(msg string) { fmt.Println(msg) }
