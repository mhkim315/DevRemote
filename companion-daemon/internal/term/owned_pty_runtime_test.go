package term

import (
	"context"
	"sync"
	"testing"

	"devremote/companion-daemon/internal/devicetrust"
)

// barrierAuthorizer runs commit, then blocks until released.
// The first call blocks after commit; subsequent calls proceed normally.
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

	// Always run commit first (reserves generation).
	if err := commit(); err != nil {
		return err
	}

	if isFirst {
		// Signal that commit completed, then block.
		close(a.blockCh)
		<-a.releaseC
	}
	return nil
}

// TestOwnedCreate_StaleCreateCannotFinalizeWinner verifies that a slow create
// whose reservation is overtaken by a faster one rolls back and does NOT kill
// the winner.
func TestOwnedCreate_StaleCreateCannotFinalizeWinner(t *testing.T) {
	auth := newBarrierAuthorizer()
	handle := &v1TestHandle{Reader: emptyReader{}}
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

	// Wait for slow create to commit (reserve gen) and block.
	<-auth.blockCh

	// Fast create acquires its own reservation and publishes.
	id, err := o.Create(context.Background(), SpawnConfig{Name: "race", Executable: "true"}, "", "test", "test-device", 0)
	if err != nil {
		t.Fatalf("fast create: %v", err)
	}

	// Release the slow create.
	close(auth.releaseC)
	wg.Wait()

	// Slow create must fail — its reservation was overtaken.
	if slowErr == nil {
		t.Fatal("slow create must fail after reservation overtaken")
	}

	// Fast create's entry must still exist.
	if _, ok := o.Get(id); !ok {
		t.Fatalf("winner entry missing for id %s", id)
	}
}

// TestOwnedCreate_OlderFailureDoesNotEraseNewerPending verifies that a
// failing create only deletes its own pending reservation, never a newer one.
func TestOwnedCreate_OlderFailureDoesNotEraseNewerPending(t *testing.T) {
	auth := newBarrierAuthorizer()
	o, err := NewOwnedPTYRuntime(auth, &v1TestLauncher{handle: &v1TestHandle{Reader: emptyReader{}}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Slow create blocks after commit.
		o.Create(context.Background(), SpawnConfig{Name: "nf", Executable: "true"}, "", "test", "test-device", 0)
	}()

	// Wait for slow create to commit and block.
	<-auth.blockCh

	// Fast create succeeds.
	id, err := o.Create(context.Background(), SpawnConfig{Name: "nf", Executable: "true"}, "", "test", "test-device", 0)
	if err != nil {
		// Release slow create before failing.
		close(auth.releaseC)
		wg.Wait()
		t.Fatalf("fast create: %v", err)
	}

	// Release slow create.
	close(auth.releaseC)
	wg.Wait()

	// Fast create's entry must still be running (not erased by slow failure).
	entry, ok := o.Get(id)
	if !ok || entry.State != LifecycleRunning {
		t.Fatalf("fast create entry = %+v ok=%v, want running", entry, ok)
	}
}

// TestOwnedCreate_SameIDReverseCompletion verifies that a slow create does NOT
// kill the winner's process when it overtakes.
func TestOwnedCreate_SameIDReverseCompletion(t *testing.T) {
	auth := newBarrierAuthorizer()
	handle := &v1TestHandle{Reader: emptyReader{}}
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

	// Wait for slow create to commit and block.
	<-auth.blockCh

	// Fast create acquires its own reservation and publishes.
	id, err := o.Create(context.Background(), SpawnConfig{Name: "same", Executable: "true"}, "", "test", "test-device", 0)
	if err != nil {
		t.Fatalf("fast create: %v", err)
	}

	// Release slow create.
	close(auth.releaseC)
	wg.Wait()

	// Slow create must fail.
	if slowErr == nil {
		t.Fatal("slow create must fail after reservation overtaken")
	}

	// Fast create's handle must NOT have been killed by the slow one.
	handle.mu.Lock()
	killed := handle.killed
	handle.mu.Unlock()
	if killed > 0 {
		t.Fatalf("winner was killed by stale create: kill count=%d", killed)
	}

	// Fast create's entry must still be running.
	entry, ok := o.Get(id)
	if !ok || entry.State != LifecycleRunning {
		t.Fatalf("winner entry = %+v ok=%v, want running", entry, ok)
	}

	// Verify there is exactly one entry (no leak).
	o.mu.Lock()
	count := len(o.entries)
	o.mu.Unlock()
	if count != 1 {
		t.Fatalf("entry count = %d, want 1 (no leak)", count)
	}
}
