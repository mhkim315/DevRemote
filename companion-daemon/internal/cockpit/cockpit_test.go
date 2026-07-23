package cockpit

import (
	"strings"
	"sync"
	"testing"
)

func TestProjectionIsReadOnly(t *testing.T) {
	if !(Projection{}).ReadOnly() {
		t.Fatal("projection authoritative")
	}
}

func TestCockpitStoreRejectsOversizedAndExcessItems(t *testing.T) {
	s := NewCockpitStore()
	if s.AppendSession(Item{Summary: strings.Repeat("x", MaxFieldBytes+1)}) {
		t.Fatal("oversized item accepted")
	}
	for range MaxItems {
		if !s.AppendSession(Item{Kind: "runtime"}) {
			t.Fatal("bounded item rejected")
		}
	}
	if s.AppendSession(Item{Kind: "runtime"}) {
		t.Fatal("N+1 item accepted")
	}
}

func TestCockpitStoreAppendReadRoundTrip(t *testing.T) {
	s := NewCockpitStore()
	s.AppendSession(Item{Kind: "runtime"})
	s.AppendApproval(Item{Kind: "approval"})
	s.AppendFinding(Item{Kind: "validation", Stale: true})
	state := s.ReadAll()
	if len(state.Sessions) != 1 || len(state.Approvals) != 1 || len(state.Findings) != 1 || !state.Findings[0].Stale || !s.ReadOnly() {
		t.Fatal("round trip failed")
	}
}

func TestCockpitStoreConcurrentReads(t *testing.T) {
	s := NewCockpitStore()
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(2)
		go func() { defer wg.Done(); s.AppendSession(Item{Kind: "runtime"}) }()
		go func() { defer wg.Done(); _ = s.ReadAll() }()
	}
	wg.Wait()
}
