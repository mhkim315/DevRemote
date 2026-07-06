package mux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/creack/pty"
)

// NativeSession represents a running background PTY process
type NativeSession struct {
	id  string
	Cmd *exec.Cmd
	PTY *os.File
}

// SpawnPTY starts a new process in a PTY and returns it as a NativeSession.
// It does NOT keep track of it in a global map.
func SpawnPTY(id string, termEnv string, command string, args ...string) (*NativeSession, error) {
	cmd := exec.Command(command, args...)

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

// NewSession connects to or creates a tmux session
func NewSession(id string, termEnv string, command string, args ...string) (Session, error) {
	// If the client asks to create/connect, we just run `tmux new-session -A -s id`
	// This will attach if it exists, or create if it doesn't.
	return SpawnPTY(id, termEnv, "tmux", "new-session", "-A", "-s", id)
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

// StaticSession is a minimal Session implementation for use in tests.
// It satisfies the Session interface with no external dependencies.
type StaticSession struct {
	IDStr      string
	TitleStr   string
	AdapterStr string
}

func (s *StaticSession) ID() string          { return s.IDStr }
func (s *StaticSession) Title() string       { return s.TitleStr }
func (s *StaticSession) AdapterName() string { return s.AdapterStr }

// StaticAdapter is a minimal Adapter implementation for use in tests.
// It serves a pre-configured list of sessions.
type StaticAdapter struct {
	AdapterNameStr string
	SessionsList   []Session
}

func (a *StaticAdapter) Name() string                     { return a.AdapterNameStr }
func (a *StaticAdapter) ListSessions() ([]Session, error) { return a.SessionsList, nil }
func (a *StaticAdapter) GetSession(id string) (Session, error) {
	for _, s := range a.SessionsList {
		if s.ID() == id {
			return s, nil
		}
	}
	return nil, fmt.Errorf("session %s not found", id)
}

func (s *NativeSession) Close() error {
	if s.Cmd.Process != nil {
		s.Cmd.Process.Kill()
	}
	return s.PTY.Close()
}
