package term

import (
	"context"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// --- test adapters ---

// resolvingAdapter mirrors the accepted T1/T2 adapters: its GetStatus delegates
// to the frozen contract.ResolveStatus (empty evidence → unknown+degraded). The
// other five operations are inert zero-value stubs.
type resolvingAdapter struct{}

func (resolvingAdapter) Descriptor() contract.AgentAdapterDescriptor {
	return contract.AgentAdapterDescriptor{Name: "fake", Capabilities: []contract.AdapterCapability{contract.CapStatus}}
}
func (resolvingAdapter) Detect(context.Context, contract.SessionContext) (contract.AgentIdentity, error) {
	return contract.AgentIdentity{}, nil
}
func (resolvingAdapter) DiscoverSessions(context.Context, contract.DiscoveryInput) ([]contract.DiscoveredSession, error) {
	return nil, nil
}
func (resolvingAdapter) ReadEvents(context.Context, contract.ReadInput) (contract.ReadResult, error) {
	return contract.ReadResult{}, nil
}
func (resolvingAdapter) NormalizeEvent(context.Context, contract.RawRecord) (contract.AgentEvent, contract.DegradedInfo) {
	return contract.AgentEvent{}, contract.DegradedInfo{}
}
func (resolvingAdapter) DetectApproval(context.Context, []contract.AgentEvent) ([]contract.AgentApproval, error) {
	return nil, nil
}
func (resolvingAdapter) GetStatus(_ context.Context, in contract.StatusInput) (contract.StatusResult, error) {
	if len(in.Evidence) == 0 {
		return contract.StatusResult{
			Status: agent.StatusUnknown, Provenance: contract.ProvenanceUnknown,
			Degraded: contract.Degrade("no status evidence"),
		}, nil
	}
	return contract.ResolveStatus(in.Evidence), nil
}

// panicAdapter panics inside GetStatus to prove failure isolation.
type panicAdapter struct{ resolvingAdapter }

func (panicAdapter) GetStatus(context.Context, contract.StatusInput) (contract.StatusResult, error) {
	panic("boom")
}

// errorAdapter returns an error from GetStatus.
type errorAdapter struct{ resolvingAdapter }

func (errorAdapter) GetStatus(context.Context, contract.StatusInput) (contract.StatusResult, error) {
	return contract.StatusResult{}, context.DeadlineExceeded
}

// ev builds an accepted event bound to sessionID with a provenance/confidence.
func ev(sessionID string, t agent.AgentEventType, prov contract.Provenance, conf float64) agent.AgentEvent {
	return agent.AgentEvent{SessionID: sessionID, Type: t, Provenance: string(prov), Confidence: conf}
}

// --- 8 required authority negatives + supporting cases ---

// 1. advisory terminal claim → unknown + degraded (frozen ResolveStatus downgrade).
func TestAgentStatusStore_AdvisoryTerminalDowngraded(t *testing.T) {
	s := NewAgentStatusStore()
	rec := s.Update(AgentStatusUpdate{
		SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev("a:1", agent.EventCompleted, contract.ProvenanceHeuristic, 0.9)},
	})
	if rec.Status != agent.StatusUnknown {
		t.Errorf("advisory completed: status=%q, want unknown (terminal downgrade)", rec.Status)
	}
	if !rec.Degraded {
		t.Error("advisory terminal claim must be degraded")
	}
	if rec.Confidence > 0.5 {
		t.Errorf("advisory confidence=%v, want ≤ 0.5 ceiling", rec.Confidence)
	}
}

// 2. cross-session event → rejected (no foreign status leaks in).
func TestAgentStatusStore_CrossSessionRejected(t *testing.T) {
	s := NewAgentStatusStore()
	rec := s.Update(AgentStatusUpdate{
		SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev("b:2", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)},
	})
	if rec.Status != agent.StatusUnknown {
		t.Errorf("cross-session event leaked: status=%q, want unknown", rec.Status)
	}
}

// 3. unknown provenance/status → safe unknown.
func TestAgentStatusStore_UnknownProvenanceSafeUnknown(t *testing.T) {
	s := NewAgentStatusStore()
	rec := s.Update(AgentStatusUpdate{
		SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev("a:1", agent.EventToolCallStarted, contract.Provenance("bogus_prov"), 0.9)},
	})
	if rec.Status != agent.StatusUnknown {
		t.Errorf("unknown-provenance evidence: status=%q, want unknown", rec.Status)
	}
}

// 4. old generation result cannot overwrite the current session.
func TestAgentStatusStore_OldGenerationCannotOverwrite(t *testing.T) {
	s := NewAgentStatusStore()
	s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 5, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev("a:1", agent.EventThinking, contract.ProvenanceNativeLog, 0.9)}})
	got := s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 2, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	if got.Status != agent.StatusThinking || got.Generation != 5 {
		t.Errorf("older generation overwrote: got status=%q gen=%d, want thinking/5", got.Status, got.Generation)
	}
	cur, _, _ := s.Current("a:1")
	if cur.Status != agent.StatusThinking || cur.Generation != 5 {
		t.Errorf("stored current status=%q gen=%d, want thinking/5", cur.Status, cur.Generation)
	}
}

// 5. adapter failure (panic/error) → only that status degrades; store keeps working.
func TestAgentStatusStore_AdapterFailureIsolates(t *testing.T) {
	s := NewAgentStatusStore()
	pr := s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: panicAdapter{},
		Events: []agent.AgentEvent{ev("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	if pr.Status != agent.StatusUnknown || !pr.Degraded {
		t.Errorf("panic adapter: status=%q degraded=%v, want unknown/degraded", pr.Status, pr.Degraded)
	}
	er := s.Update(AgentStatusUpdate{SessionID: "a:2", Generation: 1, Adapter: errorAdapter{},
		Events: []agent.AgentEvent{ev("a:2", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	if er.Status != agent.StatusUnknown || !er.Degraded {
		t.Errorf("error adapter: status=%q degraded=%v, want unknown/degraded", er.Status, er.Degraded)
	}
	// A healthy session is unaffected by the failing ones.
	ok := s.Update(AgentStatusUpdate{SessionID: "a:3", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev("a:3", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	if ok.Status != agent.StatusWorking {
		t.Errorf("healthy session degraded by others: status=%q, want working", ok.Status)
	}
}

// 6. lifecycle "exited" and activity "working" remain separate: the store has no
// lifecycle input at all, so a working activity is stored purely from evidence.
func TestAgentStatusStore_LifecycleExitedActivityWorkingSeparate(t *testing.T) {
	s := NewAgentStatusStore()
	rec := s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	if rec.Status != agent.StatusWorking {
		t.Errorf("activity status=%q, want working (independent of any lifecycle)", rec.Status)
	}
	if rec.Provenance != contract.ProvenanceNativeLog {
		t.Errorf("provenance=%q, want native_log", rec.Provenance)
	}
	// AgentActivityRecord carries NO lifecycle field — activity cannot become lifecycle.
}

// 7. agent "completed" and lifecycle "running" remain separate: strong evidence
// records completed activity without any lifecycle coupling.
func TestAgentStatusStore_AgentCompletedLifecycleRunningSeparate(t *testing.T) {
	s := NewAgentStatusStore()
	rec := s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev("a:1", agent.EventCompleted, contract.ProvenanceNativeLog, 0.9)}})
	if rec.Status != agent.StatusCompleted {
		t.Errorf("strong completed: status=%q, want completed (not downgraded; native_log is strong)", rec.Status)
	}
}

// 8. delete clears status; a recreated session cannot inherit the old status.
func TestAgentStatusStore_DeleteClearsAndNoInherit(t *testing.T) {
	s := NewAgentStatusStore()
	s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 5, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	s.Clear("a:1")
	if _, _, ok := s.Current("a:1"); ok {
		t.Fatal("Clear did not remove the record")
	}
	// Recreation at a fresh (even lower) generation must NOT inherit working.
	rec := s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev("a:1", agent.EventThinking, contract.ProvenanceNativeLog, 0.9)}})
	if rec.Status != agent.StatusThinking || rec.Generation != 1 {
		t.Errorf("recreated session status=%q gen=%d, want thinking/1 (no inheritance)", rec.Status, rec.Generation)
	}
}

// stale policy: elapsed time flags stale but never fabricates/changes the status.
func TestAgentStatusStore_StaleDoesNotFabricate(t *testing.T) {
	base := time.Unix(1_000_000, 0)
	clock := base
	s := NewAgentStatusStore()
	s.now = func() time.Time { return clock }
	s.staleAfter = 30 * time.Second

	s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev("a:1", agent.EventThinking, contract.ProvenanceNativeLog, 0.9)}})

	// Fresh read.
	if rec, stale, ok := s.Current("a:1"); !ok || stale || rec.Status != agent.StatusThinking {
		t.Fatalf("fresh read: status=%q stale=%v ok=%v", rec.Status, stale, ok)
	}
	// Advance past the horizon.
	clock = base.Add(31 * time.Second)
	rec, stale, ok := s.Current("a:1")
	if !ok || !stale {
		t.Fatalf("expected stale after horizon: stale=%v ok=%v", stale, ok)
	}
	if rec.Status != agent.StatusThinking {
		t.Errorf("stale changed the stored status to %q; must remain thinking (no fabrication)", rec.Status)
	}
}

// happy path: working resolves and stores with native_log strong provenance.
func TestAgentStatusStore_HappyPathWorking(t *testing.T) {
	s := NewAgentStatusStore()
	rec := s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 3, Version: "2.1.202", Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.8)}})
	if rec.Status != agent.StatusWorking || rec.Version != "2.1.202" || rec.Generation != 3 {
		t.Errorf("happy path: %+v, want working/2.1.202/gen3", rec)
	}
	if rec.ObservedAt.IsZero() {
		t.Error("observedAt not set")
	}
}

// empty-provenance events default to the accepted native_log path (never upgraded).
func TestAgentStatusStore_EmptyProvenanceDefaultsNativeLog(t *testing.T) {
	s := NewAgentStatusStore()
	rec := s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{{SessionID: "a:1", Type: agent.EventThinking, Confidence: 0.7}}}) // no provenance
	if rec.Status != agent.StatusThinking || rec.Provenance != contract.ProvenanceNativeLog {
		t.Errorf("empty-provenance default: status=%q prov=%q, want thinking/native_log", rec.Status, rec.Provenance)
	}
}
