package notification

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/timeline/contract"
)

func n1Event(id string, generation int64, kind contract.EventKind) contract.Envelope {
	return contract.Envelope{
		EventID: id, SessionID: "session-1", RuntimeID: "runtime-1",
		LaunchGeneration: generation, EventKind: kind,
		OccurredAt: time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC),
	}
}

func TestBuildClosedTaxonomyAndGenerationGate(t *testing.T) {
	for _, kind := range []contract.EventKind{
		contract.EventProviderInvocationFinished, contract.EventApprovalRequested,
		contract.EventApprovalResolved, contract.EventToolCallFinished,
	} {
		if _, ok := Build(n1Event("event-"+string(kind), 7, kind), 7); !ok {
			t.Errorf("allowed kind %q was suppressed", kind)
		}
	}
	if _, ok := Build(n1Event("unknown", 7, contract.EventKind("unknown")), 7); ok {
		t.Error("unknown event kind must fail closed")
	}
	if _, ok := Build(n1Event("stale", 6, contract.EventApprovalRequested), 7); ok {
		t.Error("stale generation must be suppressed before delivery")
	}
}

func TestLocatorStableTokenAndPrivacy(t *testing.T) {
	e := n1Event("event-1", 7, contract.EventApprovalRequested)
	first, ok := Build(e, 7)
	if !ok {
		t.Fatal("Build unexpectedly suppressed valid event")
	}
	second, ok := Build(e, 7)
	if !ok || first.N1Token != second.N1Token || first.N1Token != Token("event-1", 7) {
		t.Fatalf("token must be stable: first=%q second=%q", first.N1Token, second.N1Token)
	}
	if first.SessionID != e.SessionID || first.Generation != e.LaunchGeneration {
		t.Fatalf("locator lost event identity: %+v", first)
	}
	b, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"command", "secret", "payload", "approval payload"} {
		if strings.Contains(string(b), forbidden) {
			t.Errorf("locator leaked forbidden %q: %s", forbidden, b)
		}
	}
}

func TestDedupExactlyOnceAndBoundedEviction(t *testing.T) {
	d := NewDedup()
	if !d.Claim("same", 1) || d.Claim("same", 1) {
		t.Fatal("same event-generation must be claimable exactly once")
	}
	for i := 0; i < Window; i++ {
		if !d.Claim(fmt.Sprintf("event-%d", i), 1) {
			t.Fatalf("first claim %d suppressed", i)
		}
	}
	if d.l.Len() != Window {
		t.Fatalf("dedup length=%d want %d", d.l.Len(), Window)
	}
	if !d.Claim("same", 1) {
		t.Error("oldest entry should be eligible again after bounded eviction")
	}
}

func TestSelectSinceUsesPerDeviceCursorAndRecoversRingWrap(t *testing.T) {
	events := []contract.Envelope{
		n1Event("one", 1, contract.EventApprovalRequested),
		n1Event("two", 1, contract.EventApprovalRequested),
		n1Event("three", 2, contract.EventApprovalRequested),
	}
	selected, wrapped := SelectSince(events, Cursor{DeviceID: "device-a", LastEventID: "one", LastGeneration: 1})
	if wrapped || len(selected) != 2 || selected[0].EventID != "two" {
		t.Fatalf("cursor selection = %#v, wrapped=%v", selected, wrapped)
	}
	selected, wrapped = SelectSince(events, Cursor{DeviceID: "device-b", LastEventID: "gone", LastGeneration: 1})
	if !wrapped || len(selected) != len(events) {
		t.Fatalf("ring wrap must replay retained events: %#v, wrapped=%v", selected, wrapped)
	}
}
