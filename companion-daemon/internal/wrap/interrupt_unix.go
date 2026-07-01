//go:build !windows

package wrap

import (
	"os"
)

// Interrupt sends a graceful Ctrl-C signal to the given process.
func Interrupt(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(os.Interrupt)
}

// KillProcess forcefully terminates the process and its children.
func KillProcess(p *os.Process) error {
	return p.Kill()
}
