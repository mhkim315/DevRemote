package term

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

// PB.5a-R4: Zero mux imports. Pure platform-neutral PTY boundary.

// SpawnConfig carries PTY creation parameters (plain types, no mux).
type SpawnConfig struct {
	Name       string
	Command    string
	Args       []string
	Executable string
	CWD        string
	Env        []string
	Rows       int
	Cols       int
}

// PTYHandle is the platform-neutral interface for a managed PTY session.
type PTYHandle interface {
	ID() string
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Close() error
	Resize(rows, cols int) error
	Signal(sig syscall.Signal) error
	Wait() error
}

// ManagedPTYLauncherV1 is the narrow PB.5b target interface.
type ManagedPTYLauncherV1 interface {
	Name() string
	Spawn(ctx context.Context, cfg SpawnConfig) (PTYHandle, error)
	List(ctx context.Context) ([]PTYHandle, error)
	Terminate(ctx context.Context, localID string, force bool) error
}

// NewPTYLauncher creates the pure-Go PTY launcher.
func NewPTYLauncher() ManagedPTYLauncherV1 {
	return &ptyLauncher{sessions: make(map[string]*ptyHandleImpl)}
}

type ptyLauncher struct {
	mu       sync.Mutex
	sessions map[string]*ptyHandleImpl
	nextID   int
}

type ptyHandleImpl struct {
	id      string
	cmd     *exec.Cmd
	ptyFile *os.File
}

func (h *ptyHandleImpl) ID() string                  { return h.id }
func (h *ptyHandleImpl) Read(p []byte) (int, error)  { return h.ptyFile.Read(p) }
func (h *ptyHandleImpl) Write(p []byte) (int, error) { return h.ptyFile.Write(p) }
func (h *ptyHandleImpl) Close() error {
	if h.cmd.Process != nil {
		h.cmd.Process.Kill()
	}
	return h.ptyFile.Close()
}
func (h *ptyHandleImpl) Resize(rows, cols int) error {
	return pty.Setsize(h.ptyFile, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}
func (h *ptyHandleImpl) Signal(sig syscall.Signal) error {
	if h.cmd.Process == nil {
		return fmt.Errorf("no process")
	}
	return syscall.Kill(-h.cmd.Process.Pid, sig)
}
func (h *ptyHandleImpl) Wait() error {
	if h.cmd.Process == nil {
		return nil
	}
	_, err := h.cmd.Process.Wait()
	return err
}

func (l *ptyLauncher) Name() string { return "controlled_pty" }

func (l *ptyLauncher) Spawn(ctx context.Context, cfg SpawnConfig) (PTYHandle, error) {
	var cmd *exec.Cmd
	if cfg.Executable != "" {
		cmd = exec.Command(cfg.Executable, cfg.Args...)
	} else {
		cmd = exec.Command(cfg.Command, cfg.Args...)
	}
	if cfg.CWD != "" {
		cmd.Dir = cfg.CWD
	}
	env := cfg.Env
	if len(env) == 0 {
		env = append(env, "TERM=xterm-256color")
	}
	cmd.Env = env

	ptm, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("pty start: %w", err)
	}

	rows, cols := cfg.Rows, cfg.Cols
	if rows == 0 {
		rows = 30
	}
	if cols == 0 {
		cols = 100
	}
	_ = pty.Setsize(ptm, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})

	id := cfg.Name
	if id == "" {
		l.mu.Lock()
		l.nextID++
		id = fmt.Sprintf("shell-%d", l.nextID)
		l.mu.Unlock()
	}

	h := &ptyHandleImpl{id: id, cmd: cmd, ptyFile: ptm}
	l.mu.Lock()
	l.sessions[id] = h
	l.mu.Unlock()

	go func() {
		cmd.Wait()
		ptm.Close()
		l.mu.Lock()
		delete(l.sessions, id)
		l.mu.Unlock()
	}()

	return h, nil
}

func (l *ptyLauncher) List(ctx context.Context) ([]PTYHandle, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]PTYHandle, 0, len(l.sessions))
	for _, h := range l.sessions {
		out = append(out, h)
	}
	return out, nil
}

func (l *ptyLauncher) Terminate(ctx context.Context, localID string, force bool) error {
	l.mu.Lock()
	h, ok := l.sessions[localID]
	l.mu.Unlock()
	if !ok {
		return fmt.Errorf("session not found")
	}
	var sig syscall.Signal = syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	return h.Signal(sig)
}
