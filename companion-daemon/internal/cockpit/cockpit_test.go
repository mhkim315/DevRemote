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

func validTestItem(kind string) Item {
	return Item{Kind: kind, State: "ok", Summary: "test",
		Origin: Origin{Provider: "test", SessionID: "s1", RuntimeID: "r1", Generation: "1"}}
}

func TestCockpitStoreRejectsOversizedAndExcessItems(t *testing.T) {
	s := NewCockpitStore()
	if s.AppendSession(Item{Summary: strings.Repeat("x", MaxFieldBytes+1)}) {
		t.Fatal("oversized item accepted")
	}
	for range MaxItems {
		if !s.AppendSession(validTestItem("runtime")) {
			t.Fatal("bounded item rejected")
		}
	}
	if s.AppendSession(validTestItem("runtime")) {
		t.Fatal("N+1 item accepted")
	}
}

func TestCockpitStoreAppendReadRoundTrip(t *testing.T) {
	s := NewCockpitStore()
	s.AppendSession(validTestItem("runtime"))
	s.AppendApproval(validTestItem("approval"))
	s.AppendFinding(Item{Kind: "validation", State: "ok", Summary: "test", Stale: true,
		Origin: Origin{Provider: "test", SessionID: "s1", RuntimeID: "r1", Generation: "1"}})
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
		go func() { defer wg.Done(); s.AppendSession(validTestItem("runtime")) }()
		go func() { defer wg.Done(); _ = s.ReadAll() }()
	}
	wg.Wait()
}
