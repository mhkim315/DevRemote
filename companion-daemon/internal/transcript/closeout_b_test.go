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
