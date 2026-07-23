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
	w, err := writer.Open(writer.Config{Path: "/dev/full"}, nil)
	if err != nil {
		t.Skip("/dev/full unavailable")
	}
	defer w.Close()
	if w.Append(envelope(t, "drop", contract.EventToolCallStarted, agent.EventToolCallStarted, "s", 1)) {
		t.Fatal("/dev/full unexpectedly accepted write")
	}
	b := FixtureEpochBinding{EpochOccurrence: 1, SessionID: "s", RuntimeID: "runtime-s", LaunchGeneration: 1}
	s := NewProjector(w).Snapshot([]FixtureEpochBinding{b})
	if !s.Degraded || s.Stats.Dropped == 0 || len(s.Gaps) != 1 || s.Gaps[0].Reason != "writer_drop" {
		t.Fatalf("drop snapshot=%#v", s)
	}
	if got := NewProjector(w).Activity(); len(got) != 0 {
		t.Fatalf("degraded empty activity must not panic or fabricate: %#v", got)
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
	for _, e := range []contract.Envelope{toolStart, toolFinish, envelope(t, "approval-request", contract.EventApprovalRequested, agent.EventApprovalRequested, sid, 1), envelope(t, "approval-resolve", contract.EventApprovalResolved, agent.EventApprovalResolved, sid, 1)} {
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
	b := FixtureEpochBinding{EpochOccurrence: 1, SessionID: sid, TranscriptGeneration: 1, TimelineEventIDs: []string{items[2].EventID}}
	if Compare(response, Snapshot{Transcript: []TranscriptItem{items[2]}}, []FixtureEpochBinding{b}).MisboundApprovals == 0 {
		t.Fatal("misbound approval not reported")
	}
}
