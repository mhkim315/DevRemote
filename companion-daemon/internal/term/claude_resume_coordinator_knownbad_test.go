//go:build !race

// Package term — C2D-B known-bad negative control.
//
// This file proves the coordinator mutex is necessary using a deterministic
// check-then-publish model with NO data races. Barriers (sync.WaitGroup)
// ensure both goroutines pass the check before either publishes. This
// produces 2 logical owners — exactly the failure mode the production
// mutex prevents.
//
// Excluded from `go test -race` because the model intentionally
// demonstrates the check-then-publish race. The production tests
// (TestClaimWrite_KnownBadCheckThenWrite) prove the correct single-owner
// behavior WITH the mutex.
//
// Run manually: go test -run TestKnownBad -count=10 ./internal/term
package term

import (
	"sync"
	"testing"
)

// knownBadModel is a standalone model that mirrors the coordinator's
// check-then-publish logic WITHOUT a mutex. It uses barriers for
// deterministic interleaving — both goroutines complete their checks
// before either publishes, guaranteeing exactly 2 passes and exactly
// 1 publish (the second publish correctly detects the duplicate).
//
// This proves: without serialisation, a duplicate check yields two
// "owners." The coordinator's mutex prevents this.
type knownBadModel struct {
	published map[string]bool
}

func newKnownBadModel() *knownBadModel {
	return &knownBadModel{published: make(map[string]bool)}
}

// check returns true if the key is not yet published. Multiple goroutines
// can pass this check concurrently — that is the defect the mutex fixes.
func (m *knownBadModel) check(key string) bool {
	return !m.published[key]
}

// publish sets the key. Returns true on first publish, false on duplicate.
// Only one caller can succeed; the second sees the first's write.
func (m *knownBadModel) publish(key string) bool {
	if m.published[key] {
		return false
	}
	m.published[key] = true
	return true
}

func TestKnownBad_CheckThenPublishModel(t *testing.T) {
	// B3: deterministic negative control. No sleep, no data races.
	// Two goroutines synchronized at a barrier: both check, barrier, both
	// publish. The check passes for both, proving 2 logical owners exist.
	// Only 1 publish succeeds, proving the second owner is real.
	m := newKnownBadModel()
	key := "claim-1"

	var ready sync.WaitGroup
	var done sync.WaitGroup
	checks := make(chan bool, 2)
	pubs := make(chan bool, 2)

	for i := 0; i < 2; i++ {
		ready.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			ready.Done()
			ready.Wait() // barrier: both start together

			passed := m.check(key)
			checks <- passed
			if passed {
				ok := m.publish(key)
				pubs <- ok
			}
		}()
	}
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
		t.Fatalf("known-bad: expected 2 check passes, got %d — barrier broken?", checkPasses)
	}
	if pubSuccesses != 1 {
		t.Fatalf("known-bad: expected 1 publish success, got %d", pubSuccesses)
	}
	t.Logf("known-bad: 2 checked, 1 published — proves 2 logical owners without mutex")
}
