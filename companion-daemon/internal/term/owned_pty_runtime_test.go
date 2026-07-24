package term

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"

	"devremote/companion-daemon/internal/devicetrust"
)

// blockingReader blocks until closed — the recorder stays alive so we can
// prove the winner's handle is not killed by a stale create.
type blockingReader struct {
	mu     sync.Mutex
	closed bool
}

func (r *blockingReader) Read([]byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, io.EOF
	}
	return 0, nil
}

func (r *blockingReader) Close() error {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
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

// ── Denying authorizer ──

type denyAuthorizer struct{}

func (denyAuthorizer) AuthorizeCommit(string, uint64, devicetrust.MutationIntent) error {
	return fmt.Errorf("denied")
}
func (denyAuthorizer) AuthorizeAndCommit(_ string, _ uint64, _ devicetrust.MutationIntent, _ func() error) error {
	return fmt.Errorf("denied")
}

// ── Failing PTY launcher ──

type failingPTYLauncher struct{}

func (failingPTYLauncher) Spawn(_ context.Context, _ SpawnConfig) (LaunchResult, error) {
	return LaunchResult{}, fmt.Errorf("injected spawn failure")
}

// ── Tests ──

func TestOwnedCreate_StaleCreateCannotFinalizeWinner(t *testing.T) {
	auth := newBarrierAuthorizer()
	fastReader := &blockingReader{}
	defer fastReader.Close()
	fastHandle := &v1TestHandle{Reader: fastReader}
	launcher := &v1TestLauncher{handle: fastHandle}

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
	fastReader := &blockingReader{}
	defer fastReader.Close()
	fastHandle := &v1TestHandle{Reader: fastReader}

	// Start with a failing launcher for the slow create.
	o, err := NewOwnedPTYRuntime(auth, failingPTYLauncher{}, nil)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Slow create: AuthorizeAndCommit commits (reserves gen), then blocks.
		// After release, Spawn fails.
		o.Create(context.Background(), SpawnConfig{Name: "nf", Executable: "true"}, "", "test", "test-device", 0)
	}()

	<-auth.blockCh

	// Swap to a succeeding launcher for the fast create.
	o.v1Spawn = &v1TestLauncher{handle: fastHandle}
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
	liveReader := &blockingReader{}
	liveHandle := &v1TestHandle{Reader: liveReader}
	liveLauncher := &v1TestLauncher{handle: liveHandle}

	// Seed a live entry with a permissive authorizer.
	o, err := NewOwnedPTYRuntime(&barrierAuthorizer{first: false}, liveLauncher, nil)
	if err != nil {
		t.Fatal(err)
	}
	id, err := o.Create(context.Background(), SpawnConfig{Name: "live", Executable: "true"}, "", "test", "test-device", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := o.Get(id); !ok {
		t.Fatal("live entry not created")
	}

	// Try to create over it with a denied authorizer.
	o.authorizer = denyAuthorizer{}
	_, err = o.Create(context.Background(), SpawnConfig{Name: "live", Executable: "true"}, "", "test", "test-device", 0)
	if err == nil {
		t.Fatal("expected error from denied authorizer")
	}

	if _, ok := o.Get(id); !ok {
		t.Fatalf("live entry missing after denied create")
	}
	liveHandle.mu.Lock()
	killed := liveHandle.killed
	liveHandle.mu.Unlock()
	if killed > 0 {
		t.Fatalf("live winner was killed by denied create: kill count=%d", killed)
	}
	liveReader.Close()
}

func TestOwnedCreate_DeniedAuthorizerFailsClean(t *testing.T) {
	o, err := NewOwnedPTYRuntime(denyAuthorizer{}, &v1TestLauncher{handle: &v1TestHandle{Reader: &blockingReader{}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = o.Create(context.Background(), SpawnConfig{Name: "denied", Executable: "true"}, "", "test", "test-device", 0)
	if err == nil {
		t.Fatal("expected error from denied authorizer")
	}
	if _, ok := o.Get("controlled_pty:denied"); ok {
		t.Fatal("entry should not exist after denied authorization")
	}
}

func TestOwnedCreate_SameIDReverseCompletion(t *testing.T) {
	auth := newBarrierAuthorizer()
	fastReader := &blockingReader{}
	defer fastReader.Close()
	fastHandle := &v1TestHandle{Reader: fastReader}
	launcher := &v1TestLauncher{handle: fastHandle}

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
}
