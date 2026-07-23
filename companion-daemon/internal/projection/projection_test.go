package projection

import (
	"path/filepath"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	agentcontract "devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/timeline/contract"
	"devremote/companion-daemon/internal/timeline/writer"
	"devremote/companion-daemon/internal/transcript"
)

func testWriter(t *testing.T) *writer.Writer {
	t.Helper()
	w, err := writer.Open(writer.Config{Path: filepath.Join(t.TempDir(), "timeline.jsonl")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return w
}

func envelope(t *testing.T, id string, kind contract.EventKind, typ agent.AgentEventType, sid string, gen int64) contract.Envelope {
	t.Helper()
	scope := contract.Scope{SessionID: sid, RuntimeID: "runtime-" + sid, LaunchGeneration: gen}
	refs := contract.References{}
	switch kind {
	case contract.EventProviderInvocationStarted, contract.EventProviderInvocationFinished:
		refs.ProviderInvocation = &contract.TypedReference{Kind: contract.ReferenceProviderInvocation, ID: "provider-" + id, Scope: scope}
	case contract.EventToolCallStarted, contract.EventToolCallFinished:
		refs.ToolCall = &contract.TypedReference{Kind: contract.ReferenceToolCall, ID: "tool-" + id, Scope: scope}
	case contract.EventApprovalRequested, contract.EventApprovalResolved:
		refs.ApprovalRequest = &contract.TypedReference{Kind: contract.ReferenceApprovalRequest, ID: "approval-" + id, Scope: scope}
	case contract.EventStreamObserved:
		refs.Transport = &contract.TypedReference{Kind: contract.ReferenceTransport, ID: "transport-" + id, Scope: scope}
	case contract.EventEvidenceObserved:
		refs.Provenance = &contract.TypedReference{Kind: contract.ReferenceProvenance, ID: "provenance-" + id, Scope: scope}
	}
	t0 := agent.AgentEvent{ID: "agent-" + id, SessionID: sid, AgentKind: "codex", Type: typ, Provenance: "provider_protocol"}
	e, err := contract.NewEnvelope(contract.Envelope{
		SchemaVersion: contract.SchemaV1, PayloadVersion: contract.PayloadV1,
		EventKind: kind, SessionID: sid, RuntimeID: scope.RuntimeID, LaunchGeneration: gen, Provider: "codex",
		SourceIncarnation: "incarnation-" + id, SourceIdentity: contract.SourceIdentity{Kind: "fixture", ID: "source-" + id}, SourcePosition: "position-" + id,
		OccurredAt: time.Unix(int64(len(id)+1), 0), ObservedAt: time.Unix(int64(len(id)+2), 0), RedactionPolicyVersion: "redaction-v1",
		Payload: contract.Payload{Redacted: &contract.RedactedPayload{Summary: "safe " + id}}, EvidenceSources: contract.EvidenceSources{Provider: &contract.ProviderEvidenceRef{ID: "evidence-" + id, Scope: scope}}, References: refs,
		T0Event: t0,
	})
	if err != nil {
		t.Fatalf("envelope %s: %v", id, err)
	}
	return e
}

func appendEnv(t *testing.T, w *writer.Writer, e contract.Envelope) {
	t.Helper()
	if !w.Append(e) {
		t.Fatal("append rejected")
	}
}

func TestProjectionKnownSequenceAndImmutableOrder(t *testing.T) {
	w := testWriter(t)
	for _, id := range []string{"one", "two", "three"} {
		appendEnv(t, w, envelope(t, id, contract.EventToolCallStarted, agent.EventToolCallStarted, "s", 1))
	}
	p := NewProjector(w)
	a, b := p.Activity(), p.Activity()
	if len(a) != 3 || a[0].ProjectionOrder != 0 || b[0].ProjectionOrder != 0 || a[2].EventID != b[2].EventID {
		t.Fatalf("immutable order violated: %#v %#v", a, b)
	}
}

func TestProjectionReplayDedupAndCollision(t *testing.T) {
	w := testWriter(t)
	e := envelope(t, "replay", contract.EventApprovalRequested, agent.EventApprovalRequested, "s", 1)
	appendEnv(t, w, e)
	appendEnv(t, w, e)
	s := NewProjector(w).Snapshot(nil)
	if len(s.Activity) != 1 || s.Collisions != 0 {
		t.Fatalf("replay = %#v", s)
	}
	b := e
	b.Payload.Redacted = &contract.RedactedPayload{Summary: "different safe summary"}
	b.EventID = ""
	b, _ = contract.NewEnvelope(b)
	appendEnv(t, w, b)
	if got := NewProjector(w).Snapshot(nil).Collisions; got != 1 {
		t.Fatalf("collisions=%d", got)
	}
}

func TestProjectionRingOverwriteRequiresScopedGap(t *testing.T) {
	w := testWriter(t)
	ids := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		e := envelope(t, string(rune(0x1000+i)), contract.EventStreamObserved, agent.EventThinking, "s", 1)
		ids = append(ids, e.EventID)
		appendEnv(t, w, e)
	}
	b := FixtureEpochBinding{EpochOccurrence: 1, SessionID: "s", RuntimeID: "runtime-s", LaunchGeneration: 1, TimelineEventIDs: ids}
	s := NewProjector(w).Snapshot([]FixtureEpochBinding{b})
	if s.RingOverwritten != 72 || len(s.Gaps) != 1 || s.Gaps[0].Reason != "ring_overwrite" || s.Unexplained != 0 {
		t.Fatalf("ring snapshot=%#v", s)
	}
	if NewProjector(w).Snapshot(nil).Unexplained == 0 {
		t.Fatal("unscoped overwrite must fail")
	}
}

func TestProjectionUnknownKindIsRejected(t *testing.T) {
	w := testWriter(t)
	appendEnv(t, w, envelope(t, "unknown", contract.EventEvidenceObserved, agent.EventUnknown, "s", 1))
	s := NewProjector(w).Snapshot(nil)
	if s.Unknown != 1 || len(s.Activity) != 0 {
		t.Fatalf("unknown=%#v", s)
	}
}

func TestProjectionActualWriterDropHasSafeGap(t *testing.T) {
	before := writer.Stats{Dropped: 4}
	s := Snapshot{Stats: writer.Stats{Dropped: 5}, Degraded: true, DegradedReason: "write failure", Gaps: []GapMarker{{SessionID: "s", RuntimeID: "runtime-s", LaunchGeneration: 1, EpochOccurrence: 1, GlobalDroppedBefore: before.Dropped, GlobalDroppedAfter: 5, Reason: "writer_drop"}}}
	if !s.Degraded || s.Stats.Dropped == 0 || len(s.Gaps) != 1 || s.Gaps[0].Reason != "writer_drop" || s.Gaps[0].GlobalDroppedBefore != before.Dropped {
		t.Fatalf("drop snapshot=%#v", s)
	}
}

func TestDualFeedOracleAndRestoredEpoch(t *testing.T) {
	w := testWriter(t)
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	sid := "s"
	runEpoch := func(occ uint64, gen int64, events []agent.AgentEvent, envs []contract.Envelope) (transcript.TranscriptResponse, FixtureEpochBinding) {
		if occ == 1 {
			svc.EnableQueue(sid)
		} else {
			svc.ReplaceTranscript(sid)
		}
		svc.SetCorrelation(sid, transcript.CorrelationState{SessionID: sid, Correlation: agentcontract.CorrelationProven, Provider: "codex"})
		svc.ProjectAgentEvents(sid, events)
		for _, e := range envs {
			appendEnv(t, w, e)
		}
		resp := svc.BuildResponse(sid, svc.ListTranscript(sid))
		ids := make([]string, len(envs))
		for i := range envs {
			ids[i] = envs[i].EventID
		}
		return resp, FixtureEpochBinding{EpochOccurrence: occ, SessionID: sid, TranscriptGeneration: resp.Generation, RuntimeID: "runtime-s", LaunchGeneration: gen, TimelineEventIDs: ids}
	}
	e1 := envelope(t, "start", contract.EventProviderInvocationStarted, agent.EventAgentStarted, sid, 1)
	r1, b1 := runEpoch(1, 1, []agent.AgentEvent{e1.T0Event}, []contract.Envelope{e1})
	if report := Compare(r1, NewProjector(w).Snapshot([]FixtureEpochBinding{b1}), []FixtureEpochBinding{b1}); !report.Passed {
		t.Fatalf("epoch1 report=%#v", report)
	}
	// Restore deliberately reuses runtime ID and Timeline generation, while the
	// fixture occurrence and Transcript generation are both new.
	e2 := envelope(t, "restored", contract.EventProviderInvocationStarted, agent.EventAgentStarted, sid, 1)
	e2.SourceIncarnation = e1.SourceIncarnation // actual restored runtime tuple repeats this evidence field
	e2.EventID = ""
	e2, _ = contract.NewEnvelope(e2)
	r2, b2 := runEpoch(2, 1, []agent.AgentEvent{e2.T0Event}, []contract.Envelope{e2})
	if b2.EpochOccurrence == b1.EpochOccurrence || r2.Generation == r1.Generation {
		t.Fatal("restore epoch not distinct")
	}
	if report := Compare(r2, NewProjector(w).Snapshot([]FixtureEpochBinding{b2}), []FixtureEpochBinding{b2}); !report.Passed {
		t.Fatalf("restored report=%#v", report)
	}
}

func TestToolAndApprovalPairingAndMisboundVerdict(t *testing.T) {
	w := testWriter(t)
	sid := "s"
	toolStart := envelope(t, "tool-start", contract.EventToolCallStarted, agent.EventToolCallStarted, sid, 1)
	toolFinish := envelope(t, "tool-finish", contract.EventToolCallFinished, agent.EventToolCallFinished, sid, 1)
	toolFinish.References.ToolCall.ID = toolStart.References.ToolCall.ID
	toolFinish.EventID = ""
	toolFinish, _ = contract.NewEnvelope(toolFinish)
	approvalRequest := envelope(t, "approval-request", contract.EventApprovalRequested, agent.EventApprovalRequested, sid, 1)
	approvalResolve := envelope(t, "approval-resolve", contract.EventApprovalResolved, agent.EventApprovalResolved, sid, 1)
	approvalResolve.References.ApprovalRequest.ID = approvalRequest.References.ApprovalRequest.ID
	approvalResolve.EventID = ""
	approvalResolve, _ = contract.NewEnvelope(approvalResolve)
	for _, e := range []contract.Envelope{toolStart, toolFinish, approvalRequest, approvalResolve} {
		appendEnv(t, w, e)
	}
	items := NewProjector(w).Transcript()
	if err := ValidatePairOrder(items); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePairOrder([]TranscriptItem{items[1]}); err == nil {
		t.Fatal("finish without start accepted")
	}
	response := transcript.TranscriptResponse{SessionID: sid, Generation: 1, Semantic: []transcript.TranscriptSegment{{SessionID: "wrong", Kind: transcript.KindAgentEvent, Source: transcript.SourceAgentEvent, AgentEventRef: items[2].AgentEventRef, AgentKind: "codex", EventType: items[2].EventType, Text: items[2].Text, Seq: 1}}}
	b := FixtureEpochBinding{EpochOccurrence: 1, SessionID: sid, TranscriptGeneration: 1, RuntimeID: items[2].RuntimeID, LaunchGeneration: items[2].LaunchGeneration, TimelineEventIDs: []string{items[2].EventID}}
	if Compare(response, Snapshot{Transcript: []TranscriptItem{items[2]}}, []FixtureEpochBinding{b}).MisboundApprovals == 0 {
		t.Fatal("misbound approval not reported")
	}
}

func TestProjectionEmptyWriterIsExplicit(t *testing.T) {
	s := NewProjector(testWriter(t)).Snapshot(nil)
	if s.Activity == nil || s.Transcript == nil || len(s.Activity) != 0 || s.Unexplained != 0 {
		t.Fatalf("empty snapshot=%#v", s)
	}
}

func TestCompareMissingAndOrderingFail(t *testing.T) {
	response := transcript.TranscriptResponse{SessionID: "s", Generation: 1, Semantic: []transcript.TranscriptSegment{
		{SessionID: "s", Kind: transcript.KindAgentEvent, Source: transcript.SourceAgentEvent, AgentEventRef: "a", AgentKind: "codex", EventType: "agent_started", Text: "Agent started", Seq: 2},
		{SessionID: "s", Kind: transcript.KindAgentEvent, Source: transcript.SourceAgentEvent, AgentEventRef: "b", AgentKind: "codex", EventType: "agent_started", Text: "Agent started", Seq: 1},
		{SessionID: "s", Kind: transcript.KindAgentEvent, Source: transcript.SourceAgentEvent, AgentEventRef: "missing", AgentKind: "codex", EventType: "agent_started", Text: "Agent started", Seq: 3},
	}}
	b := FixtureEpochBinding{EpochOccurrence: 1, SessionID: "s", TranscriptGeneration: 1, RuntimeID: "r", LaunchGeneration: 1, TimelineEventIDs: []string{"e", "e2"}}
	snap := Snapshot{Transcript: []TranscriptItem{{EventID: "e", AgentEventRef: "a", SessionID: "s", AgentKind: "codex", EventType: "agent_started", Text: "Agent started", RuntimeID: "r", LaunchGeneration: 1, ProjectionOrder: 0}, {EventID: "e2", AgentEventRef: "b", SessionID: "s", AgentKind: "codex", EventType: "agent_started", Text: "Agent started", RuntimeID: "r", LaunchGeneration: 1, ProjectionOrder: -1}}}
	r := Compare(response, snap, []FixtureEpochBinding{b})
	if r.Passed || r.Missings != 1 || r.OrderingDivergences == 0 {
		t.Fatalf("report=%#v", r)
	}
}

func TestCompareGapRequiresTranscriptMarker(t *testing.T) {
	b := FixtureEpochBinding{EpochOccurrence: 1, SessionID: "s", TranscriptGeneration: 1, RuntimeID: "r", LaunchGeneration: 1}
	gap := GapMarker{SessionID: "s", RuntimeID: "r", LaunchGeneration: 1, EpochOccurrence: 1, Reason: "writer_drop"}
	without := Compare(transcript.TranscriptResponse{SessionID: "s", Generation: 1}, Snapshot{Gaps: []GapMarker{gap}}, []FixtureEpochBinding{b})
	if without.Passed || without.Unexplained == 0 {
		t.Fatalf("unmatched gap=%#v", without)
	}
	with := Compare(transcript.TranscriptResponse{SessionID: "s", Generation: 1, Semantic: []transcript.TranscriptSegment{{SessionID: "s", Kind: transcript.KindDegraded, DegradedReason: "writer_drop"}}}, Snapshot{Gaps: []GapMarker{gap}}, []FixtureEpochBinding{b})
	if !with.Passed || with.ToleratedGaps != 1 {
		t.Fatalf("matched gap=%#v", with)
	}
}

func TestNonAgentTranscriptIsClosedToleratedLoss(t *testing.T) {
	b := FixtureEpochBinding{EpochOccurrence: 1, SessionID: "s", TranscriptGeneration: 1}
	r := Compare(transcript.TranscriptResponse{SessionID: "s", Generation: 1, Semantic: []transcript.TranscriptSegment{{SessionID: "s", Kind: transcript.KindInputBoundary, Source: transcript.SourceByteStream}}}, Snapshot{}, []FixtureEpochBinding{b})
	if !r.Passed || r.ToleratedLosses != 1 {
		t.Fatalf("report=%#v", r)
	}
}

func TestBindingDuplicateAndRuntimeMismatchFail(t *testing.T) {
	b := FixtureEpochBinding{EpochOccurrence: 1, SessionID: "s", TranscriptGeneration: 1, RuntimeID: "expected", LaunchGeneration: 1, TimelineEventIDs: []string{"e"}}
	response := transcript.TranscriptResponse{SessionID: "s", Generation: 1, Semantic: []transcript.TranscriptSegment{{SessionID: "s", Kind: transcript.KindAgentEvent, Source: transcript.SourceAgentEvent, AgentEventRef: "a", AgentKind: "codex", EventType: "agent_started", Text: "Agent started", Seq: 1}}}
	snap := Snapshot{Transcript: []TranscriptItem{{EventID: "e", AgentEventRef: "a", SessionID: "s", AgentKind: "codex", EventType: "agent_started", Text: "Agent started", RuntimeID: "wrong", LaunchGeneration: 1}}}
	r := Compare(response, snap, []FixtureEpochBinding{b, b})
	if r.Passed || r.GenerationMismatches == 0 {
		t.Fatalf("report=%#v", r)
	}
}

func TestActivityIncludesSourceProvenance(t *testing.T) {
	w := testWriter(t)
	e := envelope(t, "provenance", contract.EventProviderInvocationStarted, agent.EventAgentStarted, "s", 1)
	appendEnv(t, w, e)
	it := NewProjector(w).Activity()[0]
	if it.SourceIncarnation != e.SourceIncarnation || it.SourcePosition != e.SourcePosition {
		t.Fatalf("activity=%#v", it)
	}
}

func TestComparatorWrongRuntimeIsGenerationMismatch(t *testing.T) {
	b := FixtureEpochBinding{EpochOccurrence: 1, SessionID: "s", TranscriptGeneration: 1, RuntimeID: "expected", LaunchGeneration: 1, TimelineEventIDs: []string{"event"}}
	response := transcript.TranscriptResponse{SessionID: "s", Generation: 1, Semantic: []transcript.TranscriptSegment{{SessionID: "s", Kind: transcript.KindAgentEvent, Source: transcript.SourceAgentEvent, AgentEventRef: "agent", AgentKind: "codex", EventType: "agent_started", Text: "Agent started", Seq: 1}}}
	snap := Snapshot{Transcript: []TranscriptItem{{EventID: "event", AgentEventRef: "agent", SessionID: "s", AgentKind: "codex", EventType: "agent_started", Text: "Agent started", RuntimeID: "wrong", LaunchGeneration: 1}}}
	if got := Compare(response, snap, []FixtureEpochBinding{b}); got.GenerationMismatches == 0 || got.Passed {
		t.Fatalf("report=%#v", got)
	}
}

func TestComparatorRejectsUnclosedFallback(t *testing.T) {
	b := FixtureEpochBinding{EpochOccurrence: 1, SessionID: "s", TranscriptGeneration: 1}
	r := Compare(transcript.TranscriptResponse{SessionID: "s", Generation: 1, Fallback: []transcript.TranscriptSegment{{SessionID: "s", Source: transcript.SourceUnknown}}}, Snapshot{}, []FixtureEpochBinding{b})
	if r.Passed || r.Unexplained == 0 {
		t.Fatalf("report=%#v", r)
	}
}
