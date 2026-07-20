package transcript

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCloseoutB_CurrentLeaseSuccess(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	sid := "test:closeoutb-current-lease"
	gen := svc.EnableQueue(sid)
	if gen != 1 {
		t.Fatalf("first generation = %d, want 1", gen)
	}
	svc.FeedBytes(sid, []byte("hello world\n"), time.Now(), gen)
	svc.CloseSessionQueue(sid, gen)
	segs := svc.ListTranscript(sid)
	if len(segs) == 0 {
		t.Fatal("no segments after feeding with correct generation")
	}
	found := false
	for _, seg := range segs {
		if seg.Text == "hello world" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'hello world' in transcript")
	}
}

func TestCloseoutB_StaleFeedBytesDropped(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	sid := "test:closeoutb-stale-feed"
	gen1 := svc.EnableQueue(sid)
	gen2 := svc.ReplaceTranscript(sid)
	if gen2 != 2 {
		t.Fatalf("generation after replace = %d, want 2", gen2)
	}
	svc.FeedBytes(sid, []byte("stale data\n"), time.Now(), gen1)
	svc.FeedBytes(sid, []byte("current data\n"), time.Now(), gen2)
	svc.CloseSessionQueue(sid, gen2)
	segs := svc.ListTranscript(sid)
	hasStale, hasCurrent := false, false
	for _, seg := range segs {
		if seg.Text == "stale data" {
			hasStale = true
		}
		if seg.Text == "current data" {
			hasCurrent = true
		}
	}
	if hasStale {
		t.Error("stale (gen1) data appeared after ReplaceTranscript")
	}
	if !hasCurrent {
		t.Error("current (gen2) data missing from transcript")
	}
}

func TestCloseoutB_StaleCloseSessionQueueNoOp(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	sid := "test:closeoutb-stale-close"
	gen1 := svc.EnableQueue(sid)
	gen2 := svc.ReplaceTranscript(sid)
	svc.CloseSessionQueue(sid, gen1)
	svc.FeedBytes(sid, []byte("after stale close\n"), time.Now(), gen2)
	svc.CloseSessionQueue(sid, gen2)
	segs := svc.ListTranscript(sid)
	hasAfter := false
	for _, seg := range segs {
		if seg.Text == "after stale close" {
			hasAfter = true
		}
	}
	if !hasAfter {
		t.Error("gen2 data missing — stale CloseSessionQueue may have closed current queue")
	}
}

func TestCloseoutB_ReplaceTranscriptAtomic(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	sid := "test:closeoutb-atomic"
	gen1 := svc.EnableQueue(sid)
	svc.FeedBytes(sid, []byte("gen1 data\n"), time.Now(), gen1)
	svc.CloseSessionQueue(sid, gen1)
	if len(svc.ListTranscript(sid)) == 0 {
		t.Fatal("gen1 data missing before replace")
	}
	gen2 := svc.ReplaceTranscript(sid)
	if gen2 != 2 {
		t.Fatalf("generation after replace = %d, want 2", gen2)
	}
	if len(svc.ListTranscript(sid)) != 0 {
		t.Error("store not empty after replace")
	}
	svc.FeedBytes(sid, []byte("gen2 data\n"), time.Now(), gen2)
	svc.CloseSessionQueue(sid, gen2)
	segs2 := svc.ListTranscript(sid)
	for _, seg := range segs2 {
		if seg.Text == "gen1 data" {
			t.Error("gen1 data leaked into gen2 transcript")
		}
	}
	hasGen2 := false
	for _, seg := range segs2 {
		if seg.Text == "gen2 data" {
			hasGen2 = true
		}
	}
	if !hasGen2 {
		t.Error("gen2 data missing from transcript")
	}
	if svc.GetGeneration(sid) != 2 {
		t.Errorf("GetGeneration = %d, want 2", svc.GetGeneration(sid))
	}
}

func TestCloseoutB_FailedReplacementRollback(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	sid := "test:closeoutb-rollback"
	gen1 := svc.EnableQueue(sid)
	svc.FeedBytes(sid, []byte("rollback data\n"), time.Now(), gen1)
	gen2 := svc.ReplaceTranscript(sid)
	svc.FeedBytes(sid, []byte("gen2 burst\n"), time.Now(), gen2)
	gen3 := svc.ReplaceTranscript(sid)
	if gen3 != 3 {
		t.Fatalf("generation after second replace = %d, want 3", gen3)
	}
	svc.FeedBytes(sid, []byte("gen3 clean\n"), time.Now(), gen3)
	svc.CloseSessionQueue(sid, gen3)
	segs := svc.ListTranscript(sid)
	for _, seg := range segs {
		if seg.Text == "rollback data" || seg.Text == "gen2 burst" {
			t.Errorf("stale data leaked: %q", seg.Text)
		}
	}
	hasGen3 := false
	for _, seg := range segs {
		if seg.Text == "gen3 clean" {
			hasGen3 = true
		}
	}
	if !hasGen3 {
		t.Error("gen3 data missing after double replace")
	}
}

func TestCloseoutB_ConcurrentGenerationCreation(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	sid := "test:closeoutb-concurrent"
	const goroutines = 20
	var wg sync.WaitGroup
	gens := make([]int64, goroutines)
	var panics atomic.Int64
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panics.Add(1)
				}
			}()
			gens[idx] = svc.EnableQueue(sid)
		}(i)
	}
	wg.Wait()
	if panics.Load() > 0 {
		t.Errorf("%d goroutines panicked", panics.Load())
	}
	for i, g := range gens {
		if g != 1 {
			t.Errorf("goroutine %d: gen=%d want 1", i, g)
		}
	}
	gen := svc.EnableQueue(sid)
	svc.FeedBytes(sid, []byte("concurrent ok\n"), time.Now(), gen)
	svc.CloseSessionQueue(sid, gen)
	hasData := false
	for _, seg := range svc.ListTranscript(sid) {
		if seg.Text == "concurrent ok" {
			hasData = true
		}
	}
	if !hasData {
		t.Error("concurrent EnableQueue left queue broken")
	}
}

// ── PA3 Closeout B R2: TOCTOU-proof and fail-closed tests ──

// TestCloseoutB_GuardedMethodsRejectStaleGeneration proves that the
// guarded service methods (processChunk, emitDegradedGuarded,
// flushBytesGuarded) atomically check generation and mutate state under
// one lock hold — no TOCTOU gap between check and mutation.
func TestCloseoutB_GuardedMethodsRejectStaleGeneration(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	sid := "test:closeoutb-guarded"

	gen1 := svc.EnableQueue(sid)
	gen2 := svc.ReplaceTranscript(sid)

	// Directly call guarded methods with stale gen1 — each must be dropped.
	svc.processChunk(sid, []byte("stale chunk\n"), time.Now(), gen1)
	svc.emitDegradedGuarded(sid, "stale overflow", time.Now(), gen1)
	svc.flushBytesGuarded(sid, time.Now(), gen1)

	// Now feed with current gen2 — must appear.
	svc.FeedBytes(sid, []byte("current chunk\n"), time.Now(), gen2)
	svc.CloseSessionQueue(sid, gen2)

	segs := svc.ListTranscript(sid)
	for _, seg := range segs {
		if seg.Text == "stale chunk" {
			t.Error("stale processChunk leaked into gen2 transcript — TOCTOU gap")
		}
		if seg.DegradedReason == "stale overflow" {
			t.Error("stale emitDegradedGuarded leaked into gen2 transcript — TOCTOU gap")
		}
	}
	hasCurrent := false
	for _, seg := range segs {
		if seg.Text == "current chunk" {
			hasCurrent = true
		}
	}
	if !hasCurrent {
		t.Error("current gen2 data missing — guarded methods may have blocked valid writes")
	}
}

// TestCloseoutB_ZeroLeaseRejectedAfterGeneration proves that once a
// generation is established, a zero generation (untracked sentinel) is
// rejected. This is fail-closed: after ReplaceTranscript advances the
// generation, callers without a lease cannot feed data.
func TestCloseoutB_ZeroLeaseRejectedAfterGeneration(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	sid := "test:closeoutb-zero-lease"

	// Establish generation 1.
	svc.EnableQueue(sid)
	// Advance to generation 2.
	svc.ReplaceTranscript(sid)

	// Feed with generation 0 (untracked sentinel) — must be rejected
	// because a generation (2) already exists.
	svc.FeedBytes(sid, []byte("zero lease\n"), time.Now(), 0)
	svc.CloseSessionQueue(sid, 2)

	segs := svc.ListTranscript(sid)
	for _, seg := range segs {
		if seg.Text == "zero lease" {
			t.Error("zero lease accepted after generation was established — not fail-closed")
		}
	}

	// Verify that a valid generation still works.
	gen3 := svc.ReplaceTranscript(sid)
	svc.FeedBytes(sid, []byte("valid gen3\n"), time.Now(), gen3)
	svc.CloseSessionQueue(sid, gen3)

	segs3 := svc.ListTranscript(sid)
	hasGen3 := false
	for _, seg := range segs3 {
		if seg.Text == "valid gen3" {
			hasGen3 = true
		}
	}
	if !hasGen3 {
		t.Error("valid gen3 data missing — zero-lease rejection may have broken state")
	}
}

// TestCloseoutB_GuardedFlushRejectsStaleGeneration proves that the
// guarded flush (flushBytesGuarded) atomically checks generation and
// does not flush partial projector state into a replacement transcript.
func TestCloseoutB_GuardedFlushRejectsStaleGeneration(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	sid := "test:closeoutb-flush-guard"

	gen1 := svc.EnableQueue(sid)
	// Feed a partial line (no newline) to accumulate state in the projector.
	svc.FeedBytes(sid, []byte("partial"), time.Now(), gen1)

	// ReplaceTranscript — new gen, new projector.
	svc.ReplaceTranscript(sid)

	// Stale flush with gen1 must NOT emit the partial line into gen2 transcript.
	svc.flushBytesGuarded(sid, time.Now(), gen1)

	// Gen2 flush should have nothing to emit (clean projector).
	svc.flushBytesGuarded(sid, time.Now(), 2)

	segs := svc.ListTranscript(sid)
	for _, seg := range segs {
		if seg.Text == "partial" {
			t.Error("stale flushBytesGuarded leaked partial state into replacement transcript")
		}
	}
}
