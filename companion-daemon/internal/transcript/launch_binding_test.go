package transcript

import (
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent/contract"
)

// S1.1-B — launch registry monotonic generation + atomic replacement, and
// PID+StartedAt process-identity correlation. These exercise the registry and
// LaunchCorrelation directly; the production-path wiring (create → register →
// process snapshot → TelemetryService → status acceptance) is proven in the term
// package.

func resetRegistry(t *testing.T) {
	t.Helper()
	globalLaunchRegistry.mu.Lock()
	globalLaunchRegistry.bindings = make(map[string]LaunchBinding)
	// Do NOT reset nextGen: generations are process-monotonic by design. Tests
	// assert strict increase, not specific values.
	globalLaunchRegistry.mu.Unlock()
}

// B-1: two launches of the same canonical ID receive strictly increasing
// generations, and the second REPLACES the first (no silent no-op).
func TestS11B_ReRegisterIsMonotonicAndReplaces(t *testing.T) {
	resetRegistry(t)
	sid := "controlled_pty:reg1"
	defer RemoveLaunch(sid)

	gen1, replaced1 := RegisterLaunch(LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1", PID: 100})
	if replaced1 {
		t.Errorf("first registration reported replaced=true")
	}
	gen2, replaced2 := RegisterLaunch(LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1", PID: 200})
	if !replaced2 {
		t.Errorf("second registration must report replaced=true (no silent no-op)")
	}
	if gen2 <= gen1 {
		t.Errorf("generations not strictly increasing: gen1=%d gen2=%d", gen1, gen2)
	}
	// The stale binding must be gone; the current binding is the replacement.
	b := LookupLaunch(sid)
	if b == nil || b.PID != 200 || b.Generation != gen2 {
		t.Errorf("binding not replaced: %+v (want PID 200, gen %d)", b, gen2)
	}
}

// B-2: delete → recreate cannot inherit the old generation; the recreation gets a
// strictly higher generation even though the map entry was removed.
func TestS11B_DeleteRecreateHigherGeneration(t *testing.T) {
	resetRegistry(t)
	sid := "controlled_pty:reg2"
	gen1, _ := RegisterLaunch(LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1"})
	RemoveLaunch(sid)
	if LookupLaunch(sid) != nil {
		t.Fatal("binding not removed")
	}
	gen2, replaced := RegisterLaunch(LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1"})
	defer RemoveLaunch(sid)
	if replaced {
		t.Errorf("recreation after delete reported replaced=true (map entry was gone)")
	}
	if gen2 <= gen1 {
		t.Errorf("recreated generation not higher: gen1=%d gen2=%d", gen1, gen2)
	}
}

// B-3: adapter + PID + StartedAt exact-match rules.
func TestS11B_CorrelationPIDStartRules(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	other := time.Unix(1_700_000_099, 0)
	base := &LaunchBinding{Adapter: "controlled_pty", Provider: "codex", AcceptedVersion: "0.144.1", PID: 4242, StartedAt: start}

	cases := []struct {
		name        string
		binding     *LaunchBinding
		adapter     string
		provider    string
		version     string
		pid         int
		start       time.Time
		wantManaged bool
	}{
		{"exact adapter+PID+start match", base, "controlled_pty", "codex", "0.144.1", 4242, start, true},
		{"adapter mismatch", base, "tmux", "codex", "0.144.1", 4242, start, false},
		{"empty discovered adapter", base, "", "codex", "0.144.1", 4242, start, false},
		{"same PID different start (reuse)", base, "controlled_pty", "codex", "0.144.1", 4242, other, false},
		{"same PID missing start", base, "controlled_pty", "codex", "0.144.1", 4242, time.Time{}, false},
		{"different PID", base, "controlled_pty", "codex", "0.144.1", 9999, start, false},
		{"missing discovered PID", base, "controlled_pty", "codex", "0.144.1", 0, start, false},
		{"provider mismatch", base, "controlled_pty", "claude", "0.144.1", 4242, start, false},
		{"version mismatch", base, "controlled_pty", "codex", "9.9.9", 4242, start, false},
		{"nil binding", nil, "controlled_pty", "codex", "0.144.1", 4242, start, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := LaunchCorrelation(tc.binding, tc.adapter, tc.provider, tc.version, tc.pid, tc.start)
			want := contract.CorrelationUnavailable
			if tc.wantManaged {
				want = contract.CorrelationManagedLaunch
			}
			if got != want {
				t.Errorf("LaunchCorrelation = %v, want %v", got, want)
			}
		})
	}
}

// B-3b (R2): a binding whose adapter is empty can never correlate, even when all
// other fields match — the adapter rule fails closed on an empty binding adapter.
func TestS11B_EmptyBindingAdapterFailsClosed(t *testing.T) {
	b := &LaunchBinding{Provider: "codex", AcceptedVersion: "0.144.1"} // Adapter == ""
	if got := LaunchCorrelation(b, "controlled_pty", "codex", "0.144.1", 0, time.Time{}); got != contract.CorrelationUnavailable {
		t.Errorf("empty binding adapter: got %v, want Unavailable", got)
	}
}

// B-4: a binding that claims NO PID does not gate on process identity (weaker
// correlation still holds on adapter/provider/version) but never wildcard-matches
// a claimed identity. This preserves the accepted S1 behavior for bindings with
// PID 0 while the PID rule strengthens bindings that do claim one.
func TestS11B_NoPIDBindingIgnoresProcessIdentity(t *testing.T) {
	b := &LaunchBinding{Adapter: "controlled_pty", Provider: "codex", AcceptedVersion: "0.144.1"} // PID 0, no start
	// Even with a discovered PID/start, adapter+provider+version suffice (managed).
	if got := LaunchCorrelation(b, "controlled_pty", "codex", "0.144.1", 12345, time.Unix(1, 0)); got != contract.CorrelationManagedLaunch {
		t.Errorf("no-PID binding: got %v, want ManagedLaunch (adapter/provider/version)", got)
	}
	// A provider mismatch still fails.
	if got := LaunchCorrelation(b, "controlled_pty", "claude", "0.144.1", 12345, time.Unix(1, 0)); got != contract.CorrelationUnavailable {
		t.Errorf("no-PID binding provider mismatch: got %v, want Unavailable", got)
	}
	// An adapter mismatch fails even with everything else matching.
	if got := LaunchCorrelation(b, "tmux", "codex", "0.144.1", 12345, time.Unix(1, 0)); got != contract.CorrelationUnavailable {
		t.Errorf("no-PID binding adapter mismatch: got %v, want Unavailable", got)
	}
}
