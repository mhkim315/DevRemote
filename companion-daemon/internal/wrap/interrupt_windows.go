//go:build windows

package wrap

import (
	"os"
	"syscall"
)

// Interrupt sends a graceful Ctrl-C signal to the given process.
// On Windows, os.Interrupt is not supported; we use GenerateConsoleCtrlEvent.
func Interrupt(pid int) error {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GenerateConsoleCtrlEvent")
	// CTRL_C_EVENT = 0, send to process group.
	r, _, err := proc.Call(0, uintptr(pid))
	if r == 0 {
		return err
	}
	return nil
}

// KillProcess forcefully terminates the process and its children.
func KillProcess(p *os.Process) error {
	return p.Kill()
}
