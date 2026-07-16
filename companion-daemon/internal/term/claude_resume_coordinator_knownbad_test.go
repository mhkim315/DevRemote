package term

import (
	"sync"
	"testing"
)

// knownBadModel is a standalone model of the coordinator's check-then-publish
// logic WITHOUT a mutex. Two barriers ensure deterministic interleaving:
//
//  1. checkedBarrier — both goroutines complete check() before either proceeds
//  2. readyBarrier  — both goroutines are ready to publish before any publish
//
// Publish is serialized by a separate mutex (the "safe" part), but check is
// unprotected. This guarantees exactly 2 check passes and exactly 1 publish
// success every time — proving the mutex is necessary.
//
// No sleep, no data races on shared state, no build-tag exclusion. Runs
// under `go test -race` with zero false positives.
type knownBadModel struct {
	mu        sync.Mutex // serializes publish only (check is unprotected)
	published map[string]bool
}

func newKnownBadModel() *knownBadModel {
	return &knownBadModel{published: make(map[string]bool)}
}

func (m *knownBadModel) check(key string) bool {
	return !m.published[key]
}

func (m *knownBadModel) publish(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.published[key] {
		return false
	}
	m.published[key] = true
	return true
}

func TestKnownBad_CheckThenPublishModel(t *testing.T) {
	m := newKnownBadModel()
	key := "claim-1"

	var checkedBarrier sync.WaitGroup
	var readyBarrier sync.WaitGroup
	var done sync.WaitGroup
	checks := make(chan bool, 2)
	pubs := make(chan bool, 2)

	for i := 0; i < 2; i++ {
		checkedBarrier.Add(1)
		readyBarrier.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			// Phase 1: check WITHOUT mutex — both pass.
			passed := m.check(key)
			checks <- passed

			// Barrier: wait for BOTH to finish checking.
			checkedBarrier.Done()
			checkedBarrier.Wait()

			// Phase 2: publish WITH mutex — only first succeeds.
			readyBarrier.Done()
			readyBarrier.Wait()
			ok := m.publish(key)
			pubs <- ok
		}()
	}

	// Wait for all goroutines to complete, then drain.
	done.Wait()
	close(checks)
	close(pubs)

	checkPasses := 0
	for c := range checks {
		if c {
			checkPasses++
		}
	}
	pubSuccesses := 0
	for p := range pubs {
		if p {
			pubSuccesses++
		}
	}

	if checkPasses != 2 {
		t.Fatalf("known-bad: expected 2 check passes, got %d", checkPasses)
	}
	if pubSuccesses != 1 {
		t.Fatalf("known-bad: expected 1 publish success, got %d", pubSuccesses)
	}
	t.Logf("known-bad: 2 checked, 1 published — proves 2 logical owners without mutex")
}
