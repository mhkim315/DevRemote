package term

import (
	"context"
	"io"
	"sync"
	"syscall"
	"testing"
	"time"
)

type v1TestLauncher struct {
	calls  int
	handle *v1TestHandle
}

func (l *v1TestLauncher) Spawn(_ context.Context, _ SpawnConfig) (LaunchResult, error) {
	l.calls++
	return LaunchResult{Handle: l.handle, Identity: LaunchIdentity{InstanceID: "one", StartedAt: time.Now()}, ProcessCleanup: &v1TestCleanup{}}, nil
}

type v1TestHandle struct {
	io.Reader
	mu      sync.Mutex
	signals int
	killed  int
}

func (h *v1TestHandle) Signal(syscall.Signal) SignalOutcome {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.signals++
	return SignalOutcome{Delivered: true}
}
func (h *v1TestHandle) Kill() KillOutcome {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.killed++
	return KillOutcome{Killed: true}
}
func (h *v1TestHandle) Wait(context.Context) LifecycleOutcome { return LifecycleOutcome{Exited: true} }
func (h *v1TestHandle) Write(p []byte) (int, error)           { return len(p), nil }
func (*v1TestHandle) Resize(int, int) error                   { return nil }
func (*v1TestHandle) CloseTransport() error                   { return nil }

type v1TestCleanup struct {
	once  sync.Once
	calls int
}

func (c *v1TestCleanup) Execute(context.Context) CleanupOutcome {
	c.once.Do(func() { c.calls++ })
	return CleanupOutcome{Completed: true}
}

func TestPB5_V1CreateUsesExactlyOneSpawn(t *testing.T) {
	l := &v1TestLauncher{handle: &v1TestHandle{Reader: emptyReader{}}}
	o := testOwnedPTYRuntime(l, nil)
	if _, err := o.Create(context.Background(), SpawnConfig{Name: "test", Executable: "true"}, "", "test", "test-device", 0); err != nil {
		t.Fatal(err)
	}
	if l.calls != 1 {
		t.Fatalf("Spawn calls=%d, want 1", l.calls)
	}
}

type emptyReader struct{}

func (emptyReader) Read([]byte) (int, error) { return 0, io.EOF }
