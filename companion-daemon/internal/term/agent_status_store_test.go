package term

import (
	"context"
	"strconv"
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

// evSeq is ev with an explicit Seq (for tie-ordering tests).
func evSeq(sessionID string, t agent.AgentEventType, prov contract.Provenance, conf float64, seq int64) agent.AgentEvent {
	e := ev(sessionID, t, prov, conf)
	e.Seq = seq
	return e
}

// noCapAdapter advertises NO CapStatus; GetStatus must never be called on it.
type noCapAdapter struct{ resolvingAdapter }

func (noCapAdapter) Descriptor() contract.AgentAdapterDescriptor {
	return contract.AgentAdapterDescriptor{Name: "nocap"} // no CapStatus
}
func (noCapAdapter) GetStatus(context.Context, contract.StatusInput) (contract.StatusResult, error) {
	panic("GetStatus must not be called on an adapter without CapStatus")
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

// 2. cross-session event → rejected (no record created; no foreign status leaks).
func TestAgentStatusStore_CrossSessionRejected(t *testing.T) {
	s := NewAgentStatusStore()
	s.Update(AgentStatusUpdate{
		SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev("b:2", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)},
	})
	if _, _, ok := s.Current("a:1"); ok {
		t.Error("cross-session event created a record for a:1 (evidence must be filtered out)")
	}
}

// 2b. a poll with no status evidence must NOT overwrite a valid prior result.
func TestAgentStatusStore_NoEvidencePollLeavesPrior(t *testing.T) {
	s := NewAgentStatusStore()
	s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	// Poll delivering only non-status events (agent_started) and a cross-session event.
	s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{
			ev("a:1", agent.EventAgentStarted, contract.ProvenanceNativeLog, 0.9),
			ev("b:2", agent.EventCompleted, contract.ProvenanceNativeLog, 0.9),
		}})
	cur, _, ok := s.Current("a:1")
	if !ok || cur.Status != agent.StatusWorking {
		t.Errorf("no-evidence poll altered prior: status=%q ok=%v, want working (unchanged)", cur.Status, ok)
	}
}

// 3. unknown/invalid provenance → dropped, no typed status (B1).
func TestAgentStatusStore_UnknownProvenanceSafeUnknown(t *testing.T) {
	s := NewAgentStatusStore()
	s.Update(AgentStatusUpdate{
		SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev("a:1", agent.EventToolCallStarted, contract.Provenance("bogus_prov"), 0.9)},
	})
	if _, _, ok := s.Current("a:1"); ok {
		t.Error("unknown-provenance event created a typed status (must be dropped)")
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

// Revoke forces unknown+degraded (authority loss) and obeys the generation rule.
func TestAgentStatusStore_RevokeDowngrades(t *testing.T) {
	s := NewAgentStatusStore()
	s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 2, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	rec := s.Revoke("a:1", 2, "2.1.202", "accepted version conflict")
	if rec.Status != agent.StatusUnknown || !rec.Degraded {
		t.Errorf("revoke: status=%q degraded=%v, want unknown/degraded", rec.Status, rec.Degraded)
	}
	// An older-generation revoke cannot overwrite a newer record.
	s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 5, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev("a:1", agent.EventThinking, contract.ProvenanceNativeLog, 0.9)}})
	if got := s.Revoke("a:1", 3, "", "late"); got.Status != agent.StatusThinking || got.Generation != 5 {
		t.Errorf("stale revoke overwrote newer generation: %+v", got)
	}
}
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

// B1: empty provenance is NOT upgraded to native_log; it creates no typed status.
func TestAgentStatusStore_EmptyProvenanceNotUpgraded(t *testing.T) {
	s := NewAgentStatusStore()
	s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{{SessionID: "a:1", Type: agent.EventCompleted, Confidence: 0.9}}}) // no provenance
	if _, _, ok := s.Current("a:1"); ok {
		t.Error("empty-provenance completed event created a status (must not be promoted to native_log)")
	}
}

// --- B1-B6 remediation tests ---

// B2: an empty SessionID event is discarded (never treated as the current session).
func TestAgentStatusStore_EmptySessionIDDiscarded(t *testing.T) {
	s := NewAgentStatusStore()
	s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev("", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	if _, _, ok := s.Current("a:1"); ok {
		t.Error("empty-SessionID event created a record (must be discarded)")
	}
}

// B3: a non-authoritative approval never becomes waiting_approval.
func TestAgentStatusStore_NonAuthoritativeApproval(t *testing.T) {
	mk := func(prov contract.Provenance, conf float64, id string) agent.AgentEvent {
		e := ev("a:1", agent.EventApprovalRequested, prov, conf)
		e.ApprovalID = id
		return e
	}
	cases := map[string]agent.AgentEvent{
		"heuristic provenance": mk(contract.ProvenanceHeuristic, 0.9, "ap1"),
		"missing approval id":  mk(contract.ProvenanceNativeLog, 0.9, ""),
		"low confidence":       mk(contract.ProvenanceNativeLog, 0.3, "ap1"),
	}
	for name, e := range cases {
		t.Run(name, func(t *testing.T) {
			s := NewAgentStatusStore()
			s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
				Events: []agent.AgentEvent{e}})
			if rec, _, ok := s.Current("a:1"); ok && rec.Status == agent.StatusWaitingApproval {
				t.Errorf("non-authoritative approval produced waiting_approval: %+v", rec)
			}
		})
	}
}

// B3: a fully-authoritative approval does produce waiting_approval.
func TestAgentStatusStore_AuthoritativeApproval(t *testing.T) {
	s := NewAgentStatusStore()
	e := ev("a:1", agent.EventApprovalRequested, contract.ProvenanceNativeLog, 0.9)
	e.ApprovalID = "ap1"
	rec := s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{e}})
	if rec.Status != agent.StatusWaitingApproval {
		t.Errorf("authoritative approval: status=%q, want waiting_approval", rec.Status)
	}
}

// B4: on equal precedence, the latest validated Seq wins (thinking → working).
func TestAgentStatusStore_TieLatestSeq_ThinkingWorking(t *testing.T) {
	s := NewAgentStatusStore()
	rec := s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{
			evSeq("a:1", agent.EventThinking, contract.ProvenanceNativeLog, 0.85, 1),
			evSeq("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.85, 2),
		}})
	if rec.Status != agent.StatusWorking {
		t.Errorf("tie thinking(seq1)→working(seq2): status=%q, want working (latest)", rec.Status)
	}
}

// B4: latest Seq wins even when the batch is presented out of order (working → completed).
func TestAgentStatusStore_TieLatestSeq_WorkingCompleted(t *testing.T) {
	s := NewAgentStatusStore()
	rec := s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{
			evSeq("a:1", agent.EventCompleted, contract.ProvenanceNativeLog, 0.85, 5), // later, listed first
			evSeq("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.85, 4),
		}})
	if rec.Status != agent.StatusCompleted {
		t.Errorf("tie working(seq4)→completed(seq5): status=%q, want completed (latest)", rec.Status)
	}
}

// B4: one-shot and incremental polling converge on the same final status.
func TestAgentStatusStore_OneShotEqualsIncremental(t *testing.T) {
	one := NewAgentStatusStore()
	oneRec := one.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{
			evSeq("a:1", agent.EventThinking, contract.ProvenanceNativeLog, 0.85, 1),
			evSeq("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.85, 2),
		}})
	inc := NewAgentStatusStore()
	inc.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{evSeq("a:1", agent.EventThinking, contract.ProvenanceNativeLog, 0.85, 1)}})
	incRec := inc.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{evSeq("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.85, 2)}})
	if oneRec.Status != agent.StatusWorking || incRec.Status != agent.StatusWorking {
		t.Errorf("one-shot=%q incremental=%q, want both working", oneRec.Status, incRec.Status)
	}
}

// B6b: an adapter without CapStatus is never queried; result is unknown+degraded.
func TestAgentStatusStore_NoCapStatusNotQueried(t *testing.T) {
	s := NewAgentStatusStore()
	rec := s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: noCapAdapter{},
		Events: []agent.AgentEvent{ev("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	if rec.Status != agent.StatusUnknown || !rec.Degraded {
		t.Errorf("no-CapStatus adapter: status=%q degraded=%v, want unknown/degraded", rec.Status, rec.Degraded)
	}
}

// B5: the store is size-bounded; churn beyond the cap evicts deterministically (oldest-first).
func TestAgentStatusStore_BoundedEviction(t *testing.T) {
	base := time.Unix(2_000_000, 0)
	s := NewAgentStatusStore()
	tick := int64(0)
	s.now = func() time.Time { tick++; return base.Add(time.Duration(tick) * time.Second) }

	total := maxSessions + 50
	for i := 0; i < total; i++ {
		sid := "a:" + strconv.Itoa(i)
		s.Update(AgentStatusUpdate{SessionID: sid, Generation: 1, Adapter: resolvingAdapter{},
			Events: []agent.AgentEvent{ev(sid, agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	}
	if got := s.Len(); got > maxSessions {
		t.Errorf("store size=%d exceeds bound %d", got, maxSessions)
	}
	if _, _, ok := s.Current("a:" + strconv.Itoa(total-1)); !ok {
		t.Error("most recent session evicted (eviction not oldest-first)")
	}
	if _, _, ok := s.Current("a:0"); ok {
		t.Error("oldest session should have been evicted under churn")
	}
}

// B5: an absurdly long SessionID is rejected (fail-closed).
func TestAgentStatusStore_AbsurdSessionIDRejected(t *testing.T) {
	s := NewAgentStatusStore()
	huge := make([]byte, maxSessionIDLen+1)
	for i := range huge {
		huge[i] = 'x'
	}
	rec := s.Update(AgentStatusUpdate{SessionID: string(huge), Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev(string(huge), agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	if rec.Status != "" || s.Len() != 0 {
		t.Errorf("absurd session id not rejected: rec=%+v len=%d", rec, s.Len())
	}
}

// B6a: RevokeIfPresent downgrades an existing record but never creates one.
func TestAgentStatusStore_RevokeIfPresent(t *testing.T) {
	s := NewAgentStatusStore()
	if rec, ok := s.RevokeIfPresent("a:1", 1, "", "lost"); ok || rec.Status != "" {
		t.Errorf("RevokeIfPresent on absent created a record: %+v ok=%v", rec, ok)
	}
	if _, _, ok := s.Current("a:1"); ok {
		t.Error("RevokeIfPresent must not create a record for an absent session")
	}
	s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	rec, ok := s.RevokeIfPresent("a:1", 1, "", "correlation unavailable")
	if !ok || rec.Status != agent.StatusUnknown || !rec.Degraded {
		t.Errorf("RevokeIfPresent on present: %+v ok=%v, want unknown/degraded", rec, ok)
	}
}
