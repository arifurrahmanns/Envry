package main

import (
	"os/exec"
	"syscall"
	"unsafe"
)

// ownsConsole reports whether Envry is the only process attached to its
// console. That happens when envry.exe is started from Explorer or a
// Start-menu shortcut, which open a console window just for it, but not
// when it runs inside a terminal.
func ownsConsole() bool {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleProcessList")
	if proc.Find() != nil {
		return false
	}
	pids := make([]uint32, 4)
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	return n == 1
}

// openBrowser opens url in the default browser.
func openBrowser(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
