package term

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
)

// ── channelBlockingReader ──

type channelBlockingReader struct {
	mu    sync.Mutex
	data  chan []byte
	close chan struct{}
}

func newChannelBlockingReader() *channelBlockingReader {
	return &channelBlockingReader{data: make(chan []byte), close: make(chan struct{})}
}

func (r *channelBlockingReader) Read(p []byte) (int, error) {
	select {
	case <-r.close:
		return 0, io.EOF
	case d := <-r.data:
		return copy(p, d), nil
	}
}

func (r *channelBlockingReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	select {
	case <-r.close:
	default:
		close(r.close)
	}
	return nil
}

// ── Barrier authorizer ──

type barrierAuthorizer struct {
	mu       sync.Mutex
	blockCh  chan struct{}
	releaseC chan struct{}
	first    bool
}

func newBarrierAuthorizer() *barrierAuthorizer {
	return &barrierAuthorizer{blockCh: make(chan struct{}), releaseC: make(chan struct{}), first: true}
}
func (a *barrierAuthorizer) AuthorizeCommit(string, uint64, devicetrust.MutationIntent) error {
	return nil
}
func (a *barrierAuthorizer) AuthorizeAndCommit(_ string, _ uint64, _ devicetrust.MutationIntent, commit func() error) error {
	a.mu.Lock()
	isFirst := a.first
	a.first = false
	a.mu.Unlock()
	if err := commit(); err != nil {
		return err
	}
	if isFirst {
		close(a.blockCh)
		<-a.releaseC
	}
	return nil
}

// ── Scripted launcher ──

type scriptedResult struct {
	handle  *v1TestHandle
	cleanup *v1TestCleanup
	err     error
}

type scriptedLauncher struct {
	mu      sync.Mutex
	results []scriptedResult
	next    int
}

func (l *scriptedLauncher) Spawn(_ context.Context, _ SpawnConfig) (LaunchResult, error) {
	l.mu.Lock()
	i := l.next
	l.next++
	l.mu.Unlock()
	r := l.results[i]
	if r.err != nil {
		return LaunchResult{}, r.err
	}
	c := r.cleanup
	if c == nil {
		c = &v1TestCleanup{}
	}
	return LaunchResult{
		Handle:         r.handle,
		Identity:       LaunchIdentity{InstanceID: fmt.Sprintf("inst-%d", i), StartedAt: time.Now()},
		ProcessCleanup: c,
	}, nil
}

// ── Gated launcher (blocks at Spawn via callback) ──

type gatedLauncher struct {
	inner ManagedPTYLauncherV1
	gate  func()
}

func (l *gatedLauncher) Spawn(ctx context.Context, cfg SpawnConfig) (LaunchResult, error) {
	l.gate()
	return l.inner.Spawn(ctx, cfg)
}

// ── Tests ──

func TestOwnedCreate_StaleCreateCannotFinalizeWinner(t *testing.T) {
	auth := newBarrierAuthorizer()

	fastReader := newChannelBlockingReader()
	defer fastReader.Close()
	fastHandle := &v1TestHandle{Reader: fastReader}
	fastCleanup := &v1TestCleanup{}

	slowReader := newChannelBlockingReader()
	defer slowReader.Close()
	slowHandle := &v1TestHandle{Reader: slowReader}
	slowCleanup := &v1TestCleanup{}

	// results[0] = FAST create (spawns first, before barrier release)
	// results[1] = SLOW create (spawns second, after barrier release)
	launcher := &scriptedLauncher{results: []scriptedResult{
		{handle: fastHandle, cleanup: fastCleanup},
		{handle: slowHandle, cleanup: slowCleanup},
	}}

	o, err := NewOwnedPTYRuntime(auth, launcher, nil)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var slowErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, slowErr = o.Create(context.Background(), SpawnConfig{Name: "race", Executable: "true"}, "", "test", "test-device", 0)
	}()

	<-auth.blockCh

	id, err := o.Create(context.Background(), SpawnConfig{Name: "race", Executable: "true"}, "", "test", "test-device", 0)
	if err != nil {
		t.Fatalf("fast create: %v", err)
	}

	close(auth.releaseC)
	wg.Wait()

	if slowErr == nil {
		t.Fatal("slow create must fail after reservation overtaken")
	}
	// Winner entry must exist.
	entry, ok := o.Get(id)
	if !ok {
		t.Fatalf("winner entry missing for id %s", id)
	}
	// Winner identity must match fast result.
	if entry.Identity.InstanceID != "inst-0" {
		t.Fatalf("winner identity = %q, want inst-0 (fast)", entry.Identity.InstanceID)
	}
	// Winner handle untouched.
	fastHandle.mu.Lock()
	if fastHandle.killed > 0 {
		t.Fatalf("winner was killed: kill count=%d", fastHandle.killed)
	}
	fastHandle.mu.Unlock()
	// Stale create's cleanup ran (rollback called ProcessCleanup.Execute).
	if slowCleanup.calls != 1 {
		t.Fatalf("stale cleanup calls = %d, want 1 (rollback)", slowCleanup.calls)
	}
	// Winner cleanup never ran.
	if fastCleanup.calls != 0 {
		t.Fatalf("winner cleanup calls = %d, want 0", fastCleanup.calls)
	}
}

func TestOwnedCreate_OlderFailureDoesNotEraseNewerPending(t *testing.T) {
	// Per-goroutine release channels: slow runs first (fails), fast runs second.
	slowReleaseC := make(chan struct{})
	fastReleaseC := make(chan struct{})
	slowBlocked := make(chan struct{})
	fastBlocked := make(chan struct{})
	slowDone := make(chan struct{})

	fastReader := newChannelBlockingReader()
	defer fastReader.Close()
	fastHandle := &v1TestHandle{Reader: fastReader}

	// Scripted: call 0 = SLOW (error), call 1 = FAST (success).
	inner := &scriptedLauncher{results: []scriptedResult{
		{err: fmt.Errorf("injected spawn failure")},
		{handle: fastHandle},
	}}

	var mu sync.Mutex
	callCount := 0
	gatedLauncher := &gatedLauncher{
		inner: inner,
		gate: func() {
			mu.Lock()
			n := callCount
			callCount++
			mu.Unlock()
			if n == 0 {
				close(slowBlocked)
				<-slowReleaseC
			} else {
				close(fastBlocked)
				<-fastReleaseC
			}
		},
	}

	o, err := NewOwnedPTYRuntime(&barrierAuthorizer{}, gatedLauncher, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Slow create: AuthorizeAndCommit (gen 1) → Spawn → blocked at gate.
	var slowErr error
	go func() {
		defer close(slowDone)
		_, slowErr = o.Create(context.Background(), SpawnConfig{Name: "nf", Executable: "true"}, "", "test", "test-device", 0)
	}()

	<-slowBlocked

	// Fast create: AuthorizeAndCommit (gen 2) → Spawn → blocked at gate.
	var fastErr error
	var id string
	fastDone := make(chan struct{})
	go func() {
		defer close(fastDone)
		id, fastErr = o.Create(context.Background(), SpawnConfig{Name: "nf", Executable: "true"}, "", "test", "test-device", 0)
	}()
	<-fastBlocked

	// Both blocked. Fast's pending must still exist.
	o.mu.Lock()
	pg, ok := o.pending["controlled_pty:nf"]
	o.mu.Unlock()
	if !ok || pg != 2 {
		t.Fatalf("pending[nf] = %d ok=%v, want gen 2 still pending", pg, ok)
	}

	// Release SLOW → Spawn fails → slowDone closed.
	close(slowReleaseC)
	<-slowDone

	if slowErr == nil || slowErr.Error() != "injected spawn failure" {
		t.Fatalf("slow create error = %v, want injected spawn failure", slowErr)
	}

	// Fast's pending must STILL exist (slow failure didn't erase it).
	o.mu.Lock()
	pg, ok = o.pending["controlled_pty:nf"]
	o.mu.Unlock()
	if !ok || pg != 2 {
		t.Fatalf("pending[nf] after slow failure = %d ok=%v, want gen 2 still pending", pg, ok)
	}

	// Release FAST → Spawn succeeds → publish → fastDone closed.
	close(fastReleaseC)
	<-fastDone

	if fastErr != nil {
		t.Fatalf("fast create: %v", fastErr)
	}
	if _, ok := o.Get(id); !ok {
		t.Fatalf("fast create entry missing for id %s", id)
	}
}

func TestOwnedCreate_LiveWinnerSurvivesRevokedSlowCreate(t *testing.T) {
	liveReader := newChannelBlockingReader()
	defer liveReader.Close()
	liveHandle := &v1TestHandle{Reader: liveReader}

	store := &devicetrust.FileDeviceStore{Path: t.TempDir() + "/devices.json"}
	reg, err := devicetrust.NewDeviceRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	_, pub, _ := devicetrust.GenKeypair(t)
	dev, err := reg.Add(pub, "test-device")
	if err != nil {
		t.Fatal(err)
	}

	o, err := NewOwnedPTYRuntime(reg, &v1TestLauncher{handle: liveHandle}, nil)
	if err != nil {
		t.Fatal(err)
	}

	id, err := o.Create(context.Background(), SpawnConfig{Name: "live", Executable: "true"}, "", "test", dev.DeviceID, uint64(dev.Epoch))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := o.Get(id); !ok {
		t.Fatal("live entry not created")
	}

	if err := reg.Revoke(dev.DeviceID); err != nil {
		t.Fatal(err)
	}

	_, err = o.Create(context.Background(), SpawnConfig{Name: "live", Executable: "true"}, "", "test", dev.DeviceID, uint64(dev.Epoch))
	if err == nil {
		t.Fatal("expected error from revoked device create")
	}

	if _, ok := o.Get(id); !ok {
		t.Fatalf("live entry missing after revoked create")
	}
	liveHandle.mu.Lock()
	killed := liveHandle.killed
	liveHandle.mu.Unlock()
	if killed > 0 {
		t.Fatalf("live winner was killed by revoked create: kill count=%d", killed)
	}
}

func TestOwnedCreate_DeniedAuthorizerFailsClean(t *testing.T) {
	store := &devicetrust.FileDeviceStore{Path: t.TempDir() + "/devices.json"}
	reg, err := devicetrust.NewDeviceRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	o, err := NewOwnedPTYRuntime(reg, &v1TestLauncher{handle: &v1TestHandle{Reader: newChannelBlockingReader()}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = o.Create(context.Background(), SpawnConfig{Name: "denied", Executable: "true"}, "", "test", "unknown-device", 0)
	if err == nil {
		t.Fatal("expected error from unknown device")
	}
	if _, ok := o.Get("controlled_pty:denied"); ok {
		t.Fatal("entry should not exist after denied authorization")
	}
}

func TestOwnedCreate_SameIDReverseCompletion(t *testing.T) {
	auth := newBarrierAuthorizer()

	fastReader := newChannelBlockingReader()
	defer fastReader.Close()
	fastHandle := &v1TestHandle{Reader: fastReader}
	fastCleanup := &v1TestCleanup{}

	slowReader := newChannelBlockingReader()
	defer slowReader.Close()
	slowHandle := &v1TestHandle{Reader: slowReader}
	slowCleanup := &v1TestCleanup{}

	launcher := &scriptedLauncher{results: []scriptedResult{
		{handle: fastHandle, cleanup: fastCleanup},
		{handle: slowHandle, cleanup: slowCleanup},
	}}

	o, err := NewOwnedPTYRuntime(auth, launcher, nil)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var slowErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, slowErr = o.Create(context.Background(), SpawnConfig{Name: "same", Executable: "true"}, "", "test", "test-device", 0)
	}()

	<-auth.blockCh

	id, err := o.Create(context.Background(), SpawnConfig{Name: "same", Executable: "true"}, "", "test", "test-device", 0)
	if err != nil {
		t.Fatalf("fast create: %v", err)
	}

	close(auth.releaseC)
	wg.Wait()

	if slowErr == nil {
		t.Fatal("slow create must fail after reservation overtaken")
	}
	entry, ok := o.Get(id)
	if !ok {
		t.Fatalf("winner entry missing for id %s", id)
	}
	if entry.Identity.InstanceID != "inst-0" {
		t.Fatalf("winner identity = %q, want inst-0 (fast)", entry.Identity.InstanceID)
	}
	fastHandle.mu.Lock()
	if fastHandle.killed > 0 {
		t.Fatalf("winner was killed: kill count=%d", fastHandle.killed)
	}
	fastHandle.mu.Unlock()
	if slowCleanup.calls != 1 {
		t.Fatalf("stale cleanup calls = %d, want 1 (rollback)", slowCleanup.calls)
	}
	if fastCleanup.calls != 0 {
		t.Fatalf("winner cleanup calls = %d, want 0", fastCleanup.calls)
	}
	o.mu.Lock()
	count := len(o.entries)
	o.mu.Unlock()
	if count != 1 {
		t.Fatalf("entry count = %d, want 1 (no leak)", count)
	}
	slowReader.Close()
}
