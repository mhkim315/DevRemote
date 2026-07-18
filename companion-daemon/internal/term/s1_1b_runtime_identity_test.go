package term

import (
	"context"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/transcript"
)

// S1.1-B — runtime identity binding, production path. These prove the launch
// generation is real (not the hard-coded 1), that a launch replacement installs a
// non-current high-water BEFORE new evidence, that a delayed prior-launch write is
// rejected, that controlled_pty exposes a trustworthy PID+StartedAt through the
// existing ProcessProvider, and that the create→register→snapshot→TelemetryService
// →status-acceptance chain correlates on real runtime identity.

// B-race: the L1→L2 replacement race (handoff §4 B-5, and the reviewer's explicit
// scenario). L1 status=working; same-ID L2 registers and installs a high-water; a
// DELAYED L1 update/revoke is rejected; only L2's fresh evidence produces status.
func TestS11B_LaunchReplacementRejectsLateOldLaunchWrite(t *testing.T) {
	s := NewAgentStatusStore()
	sid := "controlled_pty:lr"
	l1 := int64(1)
	l2 := int64(2)

	// L1 working (streamGen 5).
	if rec := s.Update(AgentStatusUpdate{SessionID: sid, LaunchGen: l1, Generation: 5, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev(sid, agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}}); rec.Status != agent.StatusWorking {
		t.Fatalf("L1 status=%q, want working", rec.Status)
	}
	// L2 replacement installs a non-current high-water at the new launch (streamGen 0).
	s.Invalidate(sid, l2, 0, "launch binding replaced")
	if rec, _, _ := s.Current(sid); rec.Status != agent.StatusUnknown || !rec.Degraded || rec.LaunchGen != l2 {
		t.Fatalf("after L2 high-water: %+v, want unknown+degraded at launchGen 2", rec)
	}
	// DELAYED L1 update (even at a HIGHER streamGen 99) must be rejected — the
	// launch axis dominates, so a prior launch cannot resurrect status.
	if rec := s.Update(AgentStatusUpdate{SessionID: sid, LaunchGen: l1, Generation: 99, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev(sid, agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}}); rec.Status != agent.StatusUnknown {
		t.Errorf("late L1 update resurrected status=%q, want unknown (launch high-water reject)", rec.Status)
	}
	// DELAYED L1 revoke is likewise rejected; record stays at L2.
	s.Revoke(sid, l1, 99, "0.144.1", "late")
	if rec, _, _ := s.Current(sid); rec.LaunchGen != l2 {
		t.Errorf("late L1 revoke moved launchGen to %d, want 2", rec.LaunchGen)
	}
	// Only L2's fresh evidence produces a positive status.
	if rec := s.Update(AgentStatusUpdate{SessionID: sid, LaunchGen: l2, Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev(sid, agent.EventThinking, contract.ProvenanceNativeLog, 0.9)}}); rec.Status != agent.StatusThinking {
		t.Errorf("L2 fresh evidence status=%q, want thinking", rec.Status)
	}
}

// B-isolate: replacing session A's launch cannot affect session B.
func TestS11B_ReplacementIsolatedPerSession(t *testing.T) {
	s := NewAgentStatusStore()
	a, b := "controlled_pty:A", "controlled_pty:B"
	s.Update(AgentStatusUpdate{SessionID: a, LaunchGen: 1, Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev(a, agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	s.Update(AgentStatusUpdate{SessionID: b, LaunchGen: 1, Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev(b, agent.EventThinking, contract.ProvenanceNativeLog, 0.9)}})
	// Replace A's launch.
	s.Invalidate(a, 2, 0, "launch binding replaced")
	if rec, _, _ := s.Current(a); rec.Status != agent.StatusUnknown {
		t.Errorf("A after replacement: status=%q, want unknown", rec.Status)
	}
	if rec, _, _ := s.Current(b); rec.Status != agent.StatusThinking {
		t.Errorf("B affected by A's replacement: status=%q, want thinking (untouched)", rec.Status)
	}
}

// B-procinfo: controlled_pty exposes a real daemon-owned PID and a nonzero spawn
// StartedAt through the existing ProcessProvider (no new interface, no ps scrape).
func TestS11B_ControlledPTYExposesProcessIdentity(t *testing.T) {
	adapter := mux.NewControlledPTYAdapter()
	before := time.Now()
	id, err := adapter.(mux.SessionCreator).CreateSession(context.Background(), mux.CreateOptions{
		Name: "pi", Command: "sleep 5",
	})
	if err != nil {
		t.Skipf("PTY creation skipped: %v", err)
	}
	defer adapter.(mux.SessionTerminator).TerminateSession(context.Background(), id)

	var sess mux.Session
	for _, s := range mustSessions(t, adapter) {
		if s.ID() == id {
			sess = s
			break
		}
	}
	if sess == nil {
		t.Fatal("created session not found")
	}
	pp, ok := sess.(mux.ProcessProvider)
	if !ok {
		t.Fatal("controlled_pty must implement ProcessProvider (S1.1-B)")
	}
	info, err := pp.ProcessInfo(context.Background())
	if err != nil {
		t.Fatalf("ProcessInfo: %v", err)
	}
	if info.PID <= 0 {
		t.Errorf("PID=%d, want a real daemon-owned child PID", info.PID)
	}
	if info.StartedAt.IsZero() {
		t.Error("StartedAt is zero; want the spawn timestamp")
	}
	if info.StartedAt.Before(before.Add(-time.Second)) || info.StartedAt.After(time.Now().Add(time.Second)) {
		t.Errorf("StartedAt %v not within the create window", info.StartedAt)
	}
}

func mustSessions(t *testing.T, adapter mux.Adapter) []mux.Session {
	t.Helper()
	ss, err := adapter.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	return ss
}

// B-prodchain: the full production chain — a managed launch binding with a real
// PID+StartedAt, a matching process snapshot, and accepted events — correlates and
// produces a positive status; a snapshot whose start time DIFFERS (PID reuse) does
// NOT correlate and yields no positive status.
func TestS11B_ProductionCorrelationUsesRuntimeIdentity(t *testing.T) {
	start := time.Unix(1_700_100_000, 0)
	setup := func(t *testing.T, snapStart time.Time) (*TelemetryService, *procSessMock, string) {
		dir := t.TempDir()
		logPath := dir + "/codex.jsonl"
		writeLines(t, logPath, []string{
			`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","cli_version":"0.144.1"}}`,
			`{"timestamp":"2026-07-06T13:29:37.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","approval_id":"appr-1"}}`,
		})
		sid := "controlled_pty:rid"
		ts := transcript.NewService(transcript.DefaultStoreConfig())
		regAdapter := &stubRegAdapter{name: "controlled_pty"}
		reg := mux.MustNewRegistry(regAdapter)
		sess := &procSessMock{id: "rid", adapter: "controlled_pty"}
		regAdapter.sessions = []mux.Session{sess}
		svc := NewTelemetryService(reg, NewMemoryEventStore(), nil, nil,
			NewApprovalStore(), NewActivityBuffer(100), ts)
		svc.SetLogResolver(func(models.ProcessInfo) (LogRef, error) {
			return LogRef{Path: logPath, Agent: "codex", Session: sid}, nil
		})
		svc.mu.Lock()
		svc.sessions[sid] = &sessionStateData{LastActivity: time.Now(), State: "idle"}
		svc.mu.Unlock()
		// Managed launch binding claims PID 7777 + a specific start time.
		transcript.RegisterFirstLaunch(transcript.LaunchSpec{
			SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1",
			PID: 7777, StartedAt: start,
		})
		t.Cleanup(func() { transcript.RemoveLaunch(sid) })
		// The process snapshot the poll sees: matching PID, provided start time.
		snap := map[string]models.ProcessInfo{sid: {PID: 7777, Command: "codex", StartedAt: snapStart}}
		svc.processSession(context.Background(), sess, snap,
			map[string]bool{"controlled_pty": false}, map[string]bool{})
		return svc, sess, sid
	}

	t.Run("matching PID+start correlates", func(t *testing.T) {
		svc, _, sid := setup(t, start)
		rec, _, ok := svc.statusStore.Current(sid)
		if !ok || rec.Status != agent.StatusWaitingApproval || rec.Degraded {
			t.Errorf("matching identity: %+v ok=%v, want waiting_approval, not degraded", rec, ok)
		}
	})
	t.Run("PID reuse (different start) does not correlate", func(t *testing.T) {
		svc, _, sid := setup(t, start.Add(time.Hour)) // same PID, different start
		if rec, _, ok := svc.statusStore.Current(sid); ok && rec.Status == agent.StatusWaitingApproval {
			t.Errorf("PID reuse produced positive status: %+v", rec)
		}
	})
}

// R2 (production path): a launch binding recorded for one adapter must NOT
// correlate when the runtime session reports a DIFFERENT adapter, even though
// provider/version match. Drives processSession with a session whose canonical id
// and AdapterName() are a non-controlled_pty adapter while the binding claims
// controlled_pty → no positive status is produced.
func TestS11B_ProductionAdapterMismatchNoStatus(t *testing.T) {
	dir := t.TempDir()
	logPath := dir + "/codex.jsonl"
	writeLines(t, logPath, []string{
		`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","cli_version":"0.144.1"}}`,
		`{"timestamp":"2026-07-06T13:29:37.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","approval_id":"appr-1"}}`,
	})
	// The runtime session is a DIFFERENT adapter than the binding claims.
	sid := "tmux:mism"
	ts := transcript.NewService(transcript.DefaultStoreConfig())
	regAdapter := &stubRegAdapter{name: "tmux"}
	reg := mux.MustNewRegistry(regAdapter)
	sess := &procSessMock{id: "mism", adapter: "tmux"}
	regAdapter.sessions = []mux.Session{sess}
	svc := NewTelemetryService(reg, NewMemoryEventStore(), nil, nil,
		NewApprovalStore(), NewActivityBuffer(100), ts)
	svc.SetLogResolver(func(models.ProcessInfo) (LogRef, error) {
		return LogRef{Path: logPath, Agent: "codex", Session: sid}, nil
	})
	svc.mu.Lock()
	svc.sessions[sid] = &sessionStateData{LastActivity: time.Now(), State: "idle"}
	svc.mu.Unlock()
	// Binding claims controlled_pty, but the runtime adapter is tmux.
	transcript.RegisterFirstLaunch(transcript.LaunchSpec{
		SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1",
	})
	defer transcript.RemoveLaunch(sid)

	svc.processSession(context.Background(), sess,
		map[string]models.ProcessInfo{sid: {PID: 1234, Command: "codex"}},
		map[string]bool{"tmux": false}, map[string]bool{})

	if rec, _, ok := svc.statusStore.Current(sid); ok && rec.Status == agent.StatusWaitingApproval {
		t.Errorf("adapter mismatch produced positive status: %+v", rec)
	}
}

// B-wiring: the create.go registration helpers are exercised against a REAL
// controlled_pty session and a REAL TelemetryService — proving the production
// create→identity→register→replacement-invalidation chain, not just isolated
// helpers. managedProcessIdentity reads the session's own ProcessProvider; a
// same-ID relaunch goes through the atomic RegisterOrReplaceLaunch boundary which
// invalidates prior status at the reserved generation before publishing.
func TestS11B_CreateRegisterReplaceWiring(t *testing.T) {
	adapter := mux.NewControlledPTYAdapter()
	reg := mux.MustNewRegistry(adapter)
	ts := transcript.NewService(transcript.DefaultStoreConfig())
	svc := NewTelemetryService(reg, NewMemoryEventStore(), nil, nil,
		NewApprovalStore(), NewActivityBuffer(100), ts)

	id, err := adapter.(mux.SessionCreator).CreateSession(context.Background(), mux.CreateOptions{
		Name: "wire", Command: "sleep 5",
	})
	if err != nil {
		t.Skipf("PTY creation skipped: %v", err)
	}
	defer adapter.(mux.SessionTerminator).TerminateSession(context.Background(), id)
	sid := "controlled_pty:" + id
	defer transcript.RemoveLaunch(sid)

	// Production helper reads the real daemon-owned PID + spawn start.
	pid, startedAt := managedProcessIdentity(context.Background(), reg, sid)
	if pid <= 0 || startedAt.IsZero() {
		t.Fatalf("managedProcessIdentity returned pid=%d start=%v, want real identity", pid, startedAt)
	}
	spec := transcript.LaunchSpec{
		SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1",
		PID: pid, StartedAt: startedAt,
	}

	// First launch via the production boundary (no prior binding → not a replace).
	gen1, replaced1 := svc.RegisterOrReplaceLaunch(spec)
	if replaced1 {
		t.Error("first registration reported replaced=true")
	}
	// Seed a positive status at the first launch generation.
	svc.statusStore.Update(AgentStatusUpdate{SessionID: sid, LaunchGen: gen1, Generation: 1,
		Adapter: resolvingAdapter{}, Events: []agent.AgentEvent{ev(sid, agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	if rec, _, _ := svc.statusStore.Current(sid); rec.Status != agent.StatusWorking {
		t.Fatalf("seed: status=%q, want working", rec.Status)
	}

	// A same-ID relaunch through the atomic boundary: it reserves gen2, invalidates
	// the prior status at gen2, then publishes — all before returning.
	gen2, replaced2 := svc.RegisterOrReplaceLaunch(spec)
	if !replaced2 || gen2 <= gen1 {
		t.Fatalf("replacement: replaced=%v gen2=%d gen1=%d, want replaced+higher", replaced2, gen2, gen1)
	}

	// The prior positive status is already non-current (invalidation ran inside the
	// boundary, before publish), and a delayed prior-launch write cannot resurrect it.
	if rec, _, _ := svc.statusStore.Current(sid); rec.Status != agent.StatusUnknown || !rec.Degraded || rec.LaunchGen != gen2 {
		t.Errorf("after replacement: %+v, want unknown+degraded at gen2", rec)
	}
	svc.statusStore.Update(AgentStatusUpdate{SessionID: sid, LaunchGen: gen1, Generation: 99,
		Adapter: resolvingAdapter{}, Events: []agent.AgentEvent{ev(sid, agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	if rec, _, _ := svc.statusStore.Current(sid); rec.Status == agent.StatusWorking {
		t.Error("delayed prior-launch write resurrected working after replacement")
	}
	// The published binding carries gen2.
	if b := transcript.LookupLaunch(sid); b == nil || b.Generation != gen2 {
		t.Errorf("published binding gen = %v, want %d", b, gen2)
	}
}
