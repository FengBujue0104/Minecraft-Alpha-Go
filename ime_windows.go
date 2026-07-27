//go:build windows

package main

import "syscall"

var (
	user32              = syscall.NewLazyDLL("user32.dll")
	imm32               = syscall.NewLazyDLL("imm32.dll")
	getForegroundWindow = user32.NewProc("GetForegroundWindow")
	immAssociateContext = imm32.NewProc("ImmAssociateContext")
	previousIMEContext  uintptr
	imeDisabled         bool
)

// disableIME detaches the Windows IME context from the game window. The game
// has no text fields, so this prevents Chinese input candidates and composition
// overlays from appearing while WASD/number input is used.
func disableIME() {
	if imeDisabled {
		return
	}
	hwnd, _, _ := getForegroundWindow.Call()
	if hwnd == 0 {
		return
	}
	previousIMEContext, _, _ = immAssociateContext.Call(hwnd, 0)
	imeDisabled = true
}

func restoreIME() {
	if !imeDisabled {
		return
	}
	hwnd, _, _ := getForegroundWindow.Call()
	if hwnd != 0 {
		immAssociateContext.Call(hwnd, previousIMEContext)
	}
	imeDisabled = false
}
