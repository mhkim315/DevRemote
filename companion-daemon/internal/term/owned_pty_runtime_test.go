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

// ── channelBlockingReader: ACTUALLY blocks, never returns (0, nil) ──

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

// ── Scripted launcher (per-call results) ──

type scriptedResult struct {
	handle *v1TestHandle
	err    error
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
	return LaunchResult{
		Handle:         r.handle,
		Identity:       LaunchIdentity{InstanceID: fmt.Sprintf("inst-%d", i), StartedAt: time.Now()},
		ProcessCleanup: &v1TestCleanup{},
	}, nil
}

// ── Tests ──

func TestOwnedCreate_StaleCreateCannotFinalizeWinner(t *testing.T) {
	auth := newBarrierAuthorizer()
	fastReader := newChannelBlockingReader()
	defer fastReader.Close()
	fastHandle := &v1TestHandle{Reader: fastReader}

	// Scripted: first call succeeds (slow create's handle), second succeeds (fast).
	// Both use distinct handles.
	slowHandle := &v1TestHandle{Reader: newChannelBlockingReader()}
	launcher := &scriptedLauncher{results: []scriptedResult{
		{handle: slowHandle},
		{handle: fastHandle},
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
	if _, ok := o.Get(id); !ok {
		t.Fatalf("winner entry missing for id %s", id)
	}
	fastHandle.mu.Lock()
	killed := fastHandle.killed
	fastHandle.mu.Unlock()
	if killed > 0 {
		t.Fatalf("winner was killed by stale create: kill count=%d", killed)
	}
}

func TestOwnedCreate_OlderFailureDoesNotEraseNewerPending(t *testing.T) {
	auth := newBarrierAuthorizer()
	fastReader := newChannelBlockingReader()
	defer fastReader.Close()
	fastHandle := &v1TestHandle{Reader: fastReader}

	// Scripted: fast create calls first (slow is blocked) → succeeds.
	// Slow create calls second (after release) → fails.
	launcher := &scriptedLauncher{results: []scriptedResult{
		{handle: fastHandle},
		{err: fmt.Errorf("injected spawn failure")},
	}}

	o, err := NewOwnedPTYRuntime(auth, launcher, nil)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Slow create: AuthorizeAndCommit commits (reserves gen), blocks.
		// After release, Spawn gets scriptedResult[0] → FAIL.
		o.Create(context.Background(), SpawnConfig{Name: "nf", Executable: "true"}, "", "test", "test-device", 0)
	}()

	<-auth.blockCh

	// Fast create: gets scriptedResult[1] → SUCCEED.
	id, err := o.Create(context.Background(), SpawnConfig{Name: "nf", Executable: "true"}, "", "test", "test-device", 0)
	if err != nil {
		close(auth.releaseC)
		wg.Wait()
		t.Fatalf("fast create: %v", err)
	}

	close(auth.releaseC)
	wg.Wait()

	if _, ok := o.Get(id); !ok {
		t.Fatalf("fast create entry missing for id %s", id)
	}
}

func TestOwnedCreate_LiveWinnerSurvivesRevokedSlowCreate(t *testing.T) {
	liveReader := newChannelBlockingReader()
	defer liveReader.Close()
	liveHandle := &v1TestHandle{Reader: liveReader}

	// Real DeviceRegistry — seed a paired device, then revoke it.
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

	// Seed live entry at epoch 0.
	id, err := o.Create(context.Background(), SpawnConfig{Name: "live", Executable: "true"}, "", "test", dev.DeviceID, uint64(dev.Epoch))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := o.Get(id); !ok {
		t.Fatal("live entry not created")
	}

	// Revoke the device — bumps epoch to 1.
	if err := reg.Revoke(dev.DeviceID); err != nil {
		t.Fatal(err)
	}

	// Try to create again with the OLD (now revoked) epoch from the principal.
	_, err = o.Create(context.Background(), SpawnConfig{Name: "live", Executable: "true"}, "", "test", dev.DeviceID, uint64(dev.Epoch))
	if err == nil {
		t.Fatal("expected error from revoked device create")
	}

	// Live entry must still exist.
	if _, ok := o.Get(id); !ok {
		t.Fatalf("live entry missing after revoked create")
	}
	// Live handle must NOT have been killed.
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
	// No device paired — AuthorizeCommit rejects unknown device.
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

	slowReader := newChannelBlockingReader()
	slowHandle := &v1TestHandle{Reader: slowReader}

	launcher := &scriptedLauncher{results: []scriptedResult{
		{handle: slowHandle},
		{handle: fastHandle},
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
	fastHandle.mu.Lock()
	killed := fastHandle.killed
	fastHandle.mu.Unlock()
	if killed > 0 {
		t.Fatalf("winner was killed by stale create: kill count=%d", killed)
	}
	if _, ok := o.Get(id); !ok {
		t.Fatalf("winner entry missing for id %s", id)
	}
	o.mu.Lock()
	count := len(o.entries)
	o.mu.Unlock()
	if count != 1 {
		t.Fatalf("entry count = %d, want 1 (no leak)", count)
	}
	slowReader.Close()
}
