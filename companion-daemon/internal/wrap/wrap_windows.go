//go:build windows

package wrap

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
)

// Command runs the given command using standard pipes on Windows.
// PTY is not available natively; ConPTY support may be added later.
func Command(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	// Write IPC state.
	ipcPath, err := WriteIPC(os.TempDir(), IPCState{
		PID:  cmd.Process.Pid,
		Port: 0,
	})
	if err != nil {
		log.Printf("wrap: ipc write failed: %v", err)
	} else {
		defer os.Remove(ipcPath)
	}

	go io.Copy(stdinPipe, os.Stdin)
	go io.Copy(os.Stdout, stdoutPipe)
	go io.Copy(os.Stderr, stderrPipe)

	return cmd.Wait()
}
