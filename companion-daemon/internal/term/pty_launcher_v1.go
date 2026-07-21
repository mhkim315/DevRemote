package term

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// SpawnConfig is the complete input to a managed PTY launch.
type SpawnConfig struct {
	Name, Command string
	Args          []string
	Executable    string
	CWD           string
	Env           []string
	Rows, Cols    int
}

type LaunchIdentity struct {
	InstanceID string
	StartedAt  time.Time
}

type LaunchResult struct {
	Handle         PTYHandle
	Identity       LaunchIdentity
	ProcessCleanup ProcessCleanup
}

type ProcessCleanup interface {
	Execute(context.Context) CleanupOutcome
}

type CleanupOutcome struct {
	Completed bool
	Err       error
}
type SignalOutcome struct {
	Delivered, AlreadyExited bool
	Err                      error
}
type KillOutcome struct {
	Killed, AlreadyExited bool
	Err                   error
}
type LifecycleOutcome struct {
	Exited    bool
	ExitCode  int
	Signaled  bool
	SignalNum int
	TimedOut  bool
	Err       error
}

type PTYHandle interface {
	Signal(syscall.Signal) SignalOutcome
	Kill() KillOutcome
	Wait(context.Context) LifecycleOutcome
	Write([]byte) (int, error)
	Resize(rows, cols int) error
	CloseTransport() error
}

type ManagedPTYLauncherV1 interface {
	Spawn(context.Context, SpawnConfig) (LaunchResult, error)
}

// NativePTYLauncher launches daemon-owned process groups without mux state.
type NativePTYLauncher struct{ now func() time.Time }

func NewNativePTYLauncher() *NativePTYLauncher { return &NativePTYLauncher{now: time.Now} }

func (l *NativePTYLauncher) Spawn(ctx context.Context, cfg SpawnConfig) (LaunchResult, error) {
	if err := ctx.Err(); err != nil {
		return LaunchResult{}, err
	}
	var cmd *exec.Cmd
	if cfg.Executable != "" {
		cmd = exec.Command(cfg.Executable, cfg.Args...)
	} else if cfg.Command != "" {
		cmd = exec.Command("bash", "-c", cfg.Command)
	} else {
		return LaunchResult{}, errors.New("spawn requires executable or command")
	}
	cmd.Dir = cfg.CWD
	if len(cfg.Env) > 0 {
		cmd.Env = cfg.Env
	}
	ptm, err := pty.Start(cmd)
	if err != nil {
		return LaunchResult{}, fmt.Errorf("start PTY: %w", err)
	}
	// TERM-G1: always set an explicit initial PTY size before the child
	// process produces output. Zero config values default to 30×100.
	rows, cols := cfg.Rows, cfg.Cols
	if rows <= 0 {
		rows = 30
	}
	if cols <= 0 {
		cols = 100
	}
	_ = pty.Setsize(ptm, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	h := &nativePTYHandle{cmd: cmd, ptm: ptm, waitDone: make(chan struct{})}
	go h.collectWait()
	id := LaunchIdentity{InstanceID: fmt.Sprintf("%d-%d", cmd.Process.Pid, l.now().UnixNano()), StartedAt: l.now()}
	return LaunchResult{Handle: h, Identity: id, ProcessCleanup: &nativeCleanup{handle: h}}, nil
}

type nativePTYHandle struct {
	cmd       *exec.Cmd
	ptm       *os.File
	waitOnce  sync.Once
	waitErr   error
	waitDone  chan struct{}
	closeOnce sync.Once
}

func (h *nativePTYHandle) collectWait() {
	h.waitOnce.Do(func() { h.waitErr = h.cmd.Wait(); close(h.waitDone) })
}
func (h *nativePTYHandle) Signal(sig syscall.Signal) SignalOutcome {
	select {
	case <-h.waitDone:
		return SignalOutcome{AlreadyExited: true}
	default:
	}
	if err := syscall.Kill(-h.cmd.Process.Pid, sig); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return SignalOutcome{AlreadyExited: true}
		}
		return SignalOutcome{Err: err}
	}
	return SignalOutcome{Delivered: true}
}
func (h *nativePTYHandle) Kill() KillOutcome {
	o := h.Signal(syscall.SIGKILL)
	return KillOutcome{Killed: o.Delivered, AlreadyExited: o.AlreadyExited, Err: o.Err}
}
func (h *nativePTYHandle) Wait(ctx context.Context) LifecycleOutcome {
	select {
	case <-h.waitDone:
	case <-ctx.Done():
		return LifecycleOutcome{TimedOut: true, Err: ctx.Err()}
	}
	o := LifecycleOutcome{Exited: true}
	if h.waitErr == nil {
		return o
	}
	if ee, ok := h.waitErr.(*exec.ExitError); ok {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok {
			o.ExitCode = ws.ExitStatus()
			o.Signaled = ws.Signaled()
			if o.Signaled {
				o.SignalNum = int(ws.Signal())
			}
			return o
		}
	}
	o.Err = h.waitErr
	return o
}
func (h *nativePTYHandle) Write(p []byte) (int, error) { return h.ptm.Write(p) }
func (h *nativePTYHandle) Resize(r, c int) error {
	return pty.Setsize(h.ptm, &pty.Winsize{Rows: uint16(r), Cols: uint16(c)})
}
func (h *nativePTYHandle) GetSize() (int, int, error) {
	return pty.Getsize(h.ptm)
}
func (h *nativePTYHandle) CloseTransport() error {
	var err error
	h.closeOnce.Do(func() { err = h.ptm.Close() })
	return err
}
func (h *nativePTYHandle) Read(p []byte) (int, error) { return h.ptm.Read(p) }

type nativeCleanup struct {
	handle *nativePTYHandle
	once   sync.Once
	done   chan struct{}
	result CleanupOutcome
}

func (c *nativeCleanup) Execute(ctx context.Context) CleanupOutcome {
	c.once.Do(func() {
		c.done = make(chan struct{})
		go func() {
			k := c.handle.Kill()
			c.result = CleanupOutcome{Completed: k.Killed || k.AlreadyExited, Err: k.Err}
			_ = c.handle.CloseTransport()
			close(c.done)
		}()
	})
	select {
	case <-c.done:
		return c.result
	case <-ctx.Done():
		return CleanupOutcome{Err: ctx.Err()}
	}
}

var _ io.Reader = (*nativePTYHandle)(nil)
