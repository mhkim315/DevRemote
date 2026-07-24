package term

import (
	"context"
	"io"
	"sync"
	"syscall"
	"testing"
	"time"
)

type migrationReader struct{ done chan struct{} }

func (r migrationReader) Read([]byte) (int, error) { <-r.done; return 0, io.EOF }

type migrationHandle struct {
	reader         migrationReader
	mu             sync.Mutex
	signals, kills int
}

func (h *migrationHandle) Read(p []byte) (int, error) { return h.reader.Read(p) }
func (h *migrationHandle) Signal(syscall.Signal) SignalOutcome {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.signals++
	return SignalOutcome{Delivered: true}
}
func (h *migrationHandle) Kill() KillOutcome {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.kills++
	return KillOutcome{Killed: true}
}
func (*migrationHandle) Wait(context.Context) LifecycleOutcome { return LifecycleOutcome{Exited: true} }
func (*migrationHandle) Write(p []byte) (int, error)           { return len(p), nil }
func (*migrationHandle) Resize(int, int) error                 { return nil }
func (*migrationHandle) CloseTransport() error                 { return nil }

type migrationCleanup struct {
	mu    sync.Mutex
	calls int
	once  sync.Once
}

func (c *migrationCleanup) Execute(context.Context) CleanupOutcome {
	c.once.Do(func() { c.mu.Lock(); c.calls++; c.mu.Unlock() })
	return CleanupOutcome{Completed: true}
}
func (c *migrationCleanup) count() int { c.mu.Lock(); defer c.mu.Unlock(); return c.calls }

type migrationLauncher struct {
	results []LaunchResult
	calls   int
}

func (l *migrationLauncher) Spawn(context.Context, SpawnConfig) (LaunchResult, error) {
	r := l.results[l.calls]
	l.calls++
	return r, nil
}

func TestPB5V1_TransportLookupMissingIsSafe(t *testing.T) {
	o := testOwnedPTYRuntime(nil, nil)
	if tr, ok := o.Transport("controlled_pty:missing"); ok || tr != nil {
		t.Fatal("missing transport must fail closed")
	}
}

func TestPB5V1_ReplacementCleansOnlyPriorGeneration(t *testing.T) {
	r1, r2 := make(chan struct{}), make(chan struct{})
	c1, c2 := &migrationCleanup{}, &migrationCleanup{}
	l := &migrationLauncher{results: []LaunchResult{
		{Handle: &migrationHandle{reader: migrationReader{r1}}, Identity: LaunchIdentity{InstanceID: "one", StartedAt: time.Now()}, ProcessCleanup: c1},
		{Handle: &migrationHandle{reader: migrationReader{r2}}, Identity: LaunchIdentity{InstanceID: "two", StartedAt: time.Now().Add(time.Nanosecond)}, ProcessCleanup: c2},
	}}
	o := testOwnedPTYRuntime(l, nil)
	cfg := SpawnConfig{Name: "same", Executable: "true"}
	if _, err := o.Create(context.Background(), cfg, "", "", "test-device", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Create(context.Background(), cfg, "", "", "test-device", 0); err != nil {
		t.Fatal(err)
	}
	if c1.count() != 1 || c2.count() != 0 {
		t.Fatalf("cleanup counts prior=%d current=%d", c1.count(), c2.count())
	}
	close(r1)
	close(r2)
}

func TestPB5V1_StopUsesTypedSignalAndWait(t *testing.T) {
	r := make(chan struct{})
	h := &migrationHandle{reader: migrationReader{r}}
	c := &migrationCleanup{}
	o := testOwnedPTYRuntime(&migrationLauncher{results: []LaunchResult{{Handle: h, Identity: LaunchIdentity{InstanceID: "one", StartedAt: time.Now()}, ProcessCleanup: c}}}, nil)
	id, err := o.Create(context.Background(), SpawnConfig{Name: "stop", Executable: "true"}, "", "", "test-device", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = o.Stop(context.Background(), id, "test", 0); err != nil {
		t.Fatal(err)
	}
	if h.signals != 1 || c.count() != 1 {
		t.Fatalf("signals=%d cleanup=%d", h.signals, c.count())
	}
	close(r)
}

func TestPB5V1_ProcessCleanupIsExactlyOnce(t *testing.T) {
	c := &migrationCleanup{}
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !c.Execute(context.Background()).Completed {
				t.Error("cleanup incomplete")
			}
		}()
	}
	wg.Wait()
	if c.count() != 1 {
		t.Fatalf("cleanup calls=%d", c.count())
	}
}
