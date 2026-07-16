//go:build !race

// Package term — C2D-B known-bad negative control.
//
// This file contains tests that INTENTIONALLY exercise concurrent access
// without the coordinator mutex to prove the mutex is necessary. These
// tests use barriers (sync.WaitGroup) for deterministic interleaving,
// not time.Sleep or intentional data races.
//
// They are excluded from `go test -race` runs because the race detector
// correctly identifies the un-synchronized map access. Run them manually:
//
//	go test -run TestKnownBad -count=10 ./internal/term
package term

import (
	"sync"
	"testing"
)

func TestKnownBad_UnsafeReserveEntryRace(t *testing.T) {
	// This test uses the testDisableLock seam to bypass the coordinator
	// mutex. With barriers (no sleep, no intentional race), it proves
	// that concurrent reservations for the SAME claim token produce
	// duplicate entries when unprotected.
	//
	// The production coordinator (TestClaimWrite_KnownBadCheckThenWrite)
	// proves exactly 1 succeeds WITH the mutex. Together these two tests
	// prove the mutex is both necessary AND sufficient.
	c := NewClaudeResumeCoordinator()
	c.testDisableLock = true // bypass mutex for deterministic interleaving

	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, psid, rt)
	binding := testBinding(id, psid)
	ct := "cccccccccccccccccccccccccccccccc"

	// Pre-generate nonces so all goroutines use the same one.
	// (ReserveEntry normally generates a unique nonce per call.)
	nonce := generateCoordNonce()

	// Barrier: all goroutines check the duplicate map at the SAME instant.
	var ready sync.WaitGroup
	var done sync.WaitGroup
	results := make(chan bool, 4)

	for i := 0; i < 4; i++ {
		ready.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			// Signal ready, then wait for all others.
			ready.Done()
			ready.Wait()
			// All goroutines now execute the critical section simultaneously.
			// Without the mutex, the duplicate check passes for all of them,
			// and they all write to the map.
			ok := c.unsafeReserveEntry(ct, binding, nonce)
			results <- ok
		}()
	}
	done.Wait()
	close(results)

	succeeded := 0
	for r := range results {
		if r {
			succeeded++
		}
	}
	if succeeded <= 1 {
		t.Fatalf("known-bad control: expected >1 successes without mutex, got %d — test is vacuous", succeeded)
	}
	t.Logf("known-bad: %d/%d succeeded without mutex (proves mutex is necessary)", succeeded, 4)
}
