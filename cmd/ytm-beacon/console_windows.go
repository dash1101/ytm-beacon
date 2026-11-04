//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var procGetConsoleProcessList = syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleProcessList")

// ownsConsole is true when this process is the only one attached to its
// console window, which means Windows opened the window just for us (the
// program was double-clicked rather than started from a terminal).
func ownsConsole() bool {
	var pids [2]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), 2)
	return n == 1
}
