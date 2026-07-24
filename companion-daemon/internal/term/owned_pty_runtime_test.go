package term

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"

	"devremote/companion-daemon/internal/devicetrust"
)

// eofReader returns EOF immediately but doesn't block goroutines.
type eofReader struct{}

func (eofReader) Read([]byte) (int, error) { return 0, io.EOF }

// ── Barrier authorizer (commits, then blocks) ──

type barrierAuthorizer struct {
	mu       sync.Mutex
	blockCh  chan struct{}
	releaseC chan struct{}
	first    bool
}

func newBarrierAuthorizer() *barrierAuthorizer {
	return &barrierAuthorizer{
		blockCh:  make(chan struct{}),
		releaseC: make(chan struct{}),
		first:    true,
	}
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

// ── Tests ──

func TestOwnedCreate_StaleCreateCannotFinalizeWinner(t *testing.T) {
	auth := newBarrierAuthorizer()
	handle := &v1TestHandle{Reader: eofReader{}}
	o, err := NewOwnedPTYRuntime(auth, &v1TestLauncher{handle: handle}, nil)
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
	handle.mu.Lock()
	killed := handle.killed
	handle.mu.Unlock()
	if killed > 0 {
		t.Fatalf("winner was killed by stale create: kill count=%d", killed)
	}
}

func TestOwnedCreate_OlderFailureDoesNotEraseNewerPending(t *testing.T) {
	auth := newBarrierAuthorizer()
	o, err := NewOwnedPTYRuntime(auth, &v1TestLauncher{handle: &v1TestHandle{Reader: eofReader{}}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		o.Create(context.Background(), SpawnConfig{Name: "nf", Executable: "true"}, "", "test", "test-device", 0)
	}()

	<-auth.blockCh

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

func TestOwnedCreate_SameIDReverseCompletion(t *testing.T) {
	auth := newBarrierAuthorizer()
	handle := &v1TestHandle{Reader: eofReader{}}
	o, err := NewOwnedPTYRuntime(auth, &v1TestLauncher{handle: handle}, nil)
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
	handle.mu.Lock()
	killed := handle.killed
	handle.mu.Unlock()
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

func TestOwnedCreate_DeniedAuthorizerFailsClean(t *testing.T) {
	o, err := NewOwnedPTYRuntime(denyAuthorizer{}, &v1TestLauncher{handle: &v1TestHandle{Reader: eofReader{}}}, nil)
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

func TestOwnedCreate_LiveWinnerSurvivesRevokedSlowCreate(t *testing.T) {
	auth := newBarrierAuthorizer()
	handle := &v1TestHandle{Reader: eofReader{}}
	o, err := NewOwnedPTYRuntime(auth, &v1TestLauncher{handle: handle}, nil)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		o.Create(context.Background(), SpawnConfig{Name: "live", Executable: "true"}, "", "test", "test-device", 0)
	}()

	<-auth.blockCh

	id, err := o.Create(context.Background(), SpawnConfig{Name: "live", Executable: "true"}, "", "test", "test-device", 0)
	if err != nil {
		t.Fatalf("fast create: %v", err)
	}

	close(auth.releaseC)
	wg.Wait()

	entry, ok := o.Get(id)
	if !ok {
		t.Fatalf("winner entry missing")
	}
	if entry.Generation == 0 {
		t.Fatalf("winner generation must be > 0")
	}
}
