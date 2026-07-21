package term

import (
	"context"
	"io"
	"syscall"
	"time"

	"devremote/companion-daemon/internal/mux"
)

// launcherWrapper is test-only migration coverage for legacy adapter fixtures.
// Production has no V1 bridge.
func launcherWrapper(adapter mux.Adapter) ManagedPTYLauncherV1 {
	if adapter == nil {
		return nil
	}
	l, err := NewControlledPTYLauncher(adapter)
	if err != nil {
		return nil // adapter doesn't support managed PTY launch
	}
	return &testV1Launcher{old: l}
}

type testV1Launcher struct{ old ManagedPTYLauncher }

func (l *testV1Launcher) Spawn(ctx context.Context, cfg SpawnConfig) (LaunchResult, error) {
	id, sess, err := l.old.CreateSessionAndCapture(ctx, mux.CreateOptions{Name: cfg.Name, Command: cfg.Command, Executable: cfg.Executable, Args: cfg.Args, CWD: cfg.CWD})
	if err != nil {
		return LaunchResult{}, err
	}
	h := &testV1Handle{sess: sess}
	return LaunchResult{Handle: h, Identity: LaunchIdentity{InstanceID: id, StartedAt: time.Now()}, ProcessCleanup: testCleanup{old: l.old, id: id, sess: sess}}, nil
}

type testV1Handle struct{ sess mux.Session }

func (h *testV1Handle) Signal(syscall.Signal) SignalOutcome   { return SignalOutcome{Delivered: true} }
func (h *testV1Handle) Kill() KillOutcome                     { return KillOutcome{Killed: true} }
func (h *testV1Handle) Wait(context.Context) LifecycleOutcome { return LifecycleOutcome{Exited: true} }
func (h *testV1Handle) Write(p []byte) (int, error) {
	if w, ok := h.sess.(io.Writer); ok {
		return w.Write(p)
	}
	return len(p), nil
}
func (h *testV1Handle) Resize(r, c int) error {
	if x, ok := h.sess.(interface{ Resize(int, int) error }); ok {
		return x.Resize(r, c)
	}
	return nil
}
func (h *testV1Handle) CloseTransport() error {
	if x, ok := h.sess.(io.Closer); ok {
		return x.Close()
	}
	return nil
}
func (h *testV1Handle) Read(p []byte) (int, error) {
	if x, ok := h.sess.(io.Reader); ok {
		return x.Read(p)
	}
	if x, ok := h.sess.(mux.StreamOpener); ok {
		s, e := x.OpenStream(context.Background())
		if e != nil {
			return 0, e
		}
		return s.Read(p)
	}
	return 0, io.EOF
}

type testCleanup struct {
	old  ManagedPTYLauncher
	id   string
	sess mux.Session
}

func (c testCleanup) Execute(ctx context.Context) CleanupOutcome {
	err := c.old.CompareAndTerminate(ctx, "controlled_pty:"+c.id, c.sess)
	return CleanupOutcome{Completed: err == nil, Err: err}
}
