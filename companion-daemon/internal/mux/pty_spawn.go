package mux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/creack/pty"
)

// RETAINED: Platform-neutral PTY spawn boundary (PB.1).
// Used by controlled_pty_adapter.go (managed) and the tmux adapter (legacy).
// Must NOT be deleted during PB legacy removal. These are the canonical
// PTY creation primitives behind the managed transport boundary.

// NativeSession represents a running background PTY process.
type NativeSession struct {
	id  string
	Cmd *exec.Cmd
	PTY *os.File
}

// SpawnPTY starts a new process in a PTY and returns it as a NativeSession.
// It does NOT keep track of it in a global map.
func SpawnPTY(id string, termEnv string, command string, args ...string) (*NativeSession, error) {
	return SpawnPTYWithDir(id, termEnv, "", command, args...)
}

// SpawnPTYWithDir starts a new process with an optional working directory.
// cmd.Dir is set before pty.Start so the child process inherits the cwd.
func SpawnPTYWithDir(id string, termEnv string, cwd string, command string, args ...string) (*NativeSession, error) {
	cmd := exec.Command(command, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}

	// Inherit shell environment (API keys, PATH, etc.)
	env := os.Environ()
	termFound := false
	for i, e := range env {
		if strings.HasPrefix(e, "TERM=") {
			if termEnv != "" {
				env[i] = "TERM=" + termEnv
			} else {
				env[i] = "TERM=xterm-256color"
			}
			termFound = true
			break
		}
	}
	if !termFound {
		if termEnv != "" {
			env = append(env, "TERM="+termEnv)
		} else {
			env = append(env, "TERM=xterm-256color")
		}
	}
	cmd.Env = env

	ptm, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to start pty: %w", err)
	}

	// Default terminal size so TUI apps render correctly on launch.
	_ = pty.Setsize(ptm, &pty.Winsize{Rows: 30, Cols: 100})

	s := &NativeSession{
		id:  id,
		Cmd: cmd,
		PTY: ptm,
	}

	// Clean up process on exit
	go func() {
		_ = cmd.Wait()
		ptm.Close()
	}()

	return s, nil
}

func (s *NativeSession) ID() string {
	return s.id
}

func (s *NativeSession) AdapterName() string {
	return "native"
}

func (s *NativeSession) Title() string {
	return "Native Shell"
}

func (s *NativeSession) Read(p []byte) (n int, err error) {
	return s.PTY.Read(p)
}

func (s *NativeSession) Write(p []byte) (n int, err error) {
	return s.PTY.Write(p)
}

func (s *NativeSession) Resize(rows, cols int) error {
	return pty.Setsize(s.PTY, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	})
}

// GetSize returns the PTY's current winsize (rows, cols).
func (s *NativeSession) GetSize() (rows, cols int, err error) {
	return pty.Getsize(s.PTY)
}

func (s *NativeSession) Close() error {
	if s.Cmd.Process != nil {
		s.Cmd.Process.Kill()
	}
	return s.PTY.Close()
}
