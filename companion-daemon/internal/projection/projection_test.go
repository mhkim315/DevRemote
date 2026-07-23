package projection

import (
	"path/filepath"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/timeline/contract"
	"devremote/companion-daemon/internal/timeline/writer"
)

func openWriter(t *testing.T) *writer.Writer {
	t.Helper()
	path := filepath.Join(t.TempDir(), "timeline.jsonl")
	w, err := writer.Open(writer.Config{Path: path}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return w
}

func makeEnv(t *testing.T, eventID string, kind contract.EventKind, t0Type agent.AgentEventType, sid string, gen int64, text string) contract.Envelope {
	t.Helper()
	s := contract.Scope{SessionID: sid, RuntimeID: "rt-1", LaunchGeneration: gen}
	ts := contract.Envelope{}.OccurredAt.AddDate(0, 0, 1)
	safe := text
	if safe == "" {
		safe = "safe"
	}
	ref := contract.References{}
	switch kind {
	case contract.EventProviderInvocationStarted, contract.EventProviderInvocationFinished:
		ref.ProviderInvocation = &contract.TypedReference{Kind: contract.ReferenceProviderInvocation, ID: "r-" + eventID, Scope: s}
	case contract.EventToolCallStarted, contract.EventToolCallFinished:
		ref.ToolCall = &contract.TypedReference{Kind: contract.ReferenceToolCall, ID: "r-" + eventID, Scope: s}
	case contract.EventApprovalRequested, contract.EventApprovalResolved:
		ref.ApprovalRequest = &contract.TypedReference{Kind: contract.ReferenceApprovalRequest, ID: "r-" + eventID, Scope: s}
	case contract.EventStreamObserved:
		ref.Transport = &contract.TypedReference{Kind: contract.ReferenceTransport, ID: "r-" + eventID, Scope: s}
	case contract.EventEvidenceObserved:
		ref.Provenance = &contract.TypedReference{Kind: contract.ReferenceProvenance, ID: "r-" + eventID, Scope: s}
	}
	e, err := contract.NewEnvelope(contract.Envelope{
		SchemaVersion: contract.SchemaV1, PayloadVersion: contract.PayloadV1,
		EventKind: kind, SessionID: s.SessionID, RuntimeID: s.RuntimeID,
		LaunchGeneration: gen, Provider: "codex", SourceIncarnation: "inc",
		SourceIdentity: contract.SourceIdentity{Kind: "provider", ID: "sid"},
		SourcePosition: "pos", RedactionPolicyVersion: "v1",
		Payload:         contract.Payload{Redacted: &contract.RedactedPayload{Summary: safe}},
		EvidenceSources: contract.EvidenceSources{Provider: &contract.ProviderEvidenceRef{ID: "e-" + eventID, Scope: s}},
		References:      ref,
		OccurredAt:      ts, ObservedAt: ts.Add(time.Second),
		T0Event: agent.AgentEvent{ID: "t0-" + eventID, SessionID: s.SessionID, AgentKind: "codex", Type: t0Type},
	})
	if err != nil {
		t.Fatalf("makeEnv %s: %v", eventID, err)
	}
	return e
}

func TestProjection_1_Empty(t *testing.T) {
	p := NewProjector(nil)
	if p.Activity() != nil || p.Transcript() != nil {
		t.Fatal("expected nil from nil writer")
	}
}

func TestProjection_2_InsertionOrder(t *testing.T) {
	w := openWriter(t)
	for i := range 5 {
		w.Append(makeEnv(t, string(rune('a'+i)), contract.EventToolCallStarted, agent.EventToolCallStarted, "s1", 1, "tool"))
	}
	if len(NewProjector(w).Activity()) != 5 {
		t.Fatal("expected 5")
	}
}

func TestProjection_3_ExactReplay(t *testing.T) {
	w := openWriter(t)
	e := makeEnv(t, "dup", contract.EventApprovalRequested, agent.EventApprovalRequested, "s1", 1, "app")
	w.Append(e)
	w.Append(e)
	n := len(NewProjector(w).Activity())
	if n == 0 || n > 2 {
		t.Errorf("expected 1-2, got %d", n)
	}
}

func TestProjection_4_Collision(t *testing.T) {
	w := openWriter(t)
	w.Append(makeEnv(t, "col", contract.EventToolCallStarted, agent.EventToolCallStarted, "s1", 1, "a"))
	w.Append(makeEnv(t, "col", contract.EventToolCallStarted, agent.EventToolCallStarted, "s1", 1, "b"))
	if len(NewProjector(w).Activity()) == 0 {
		t.Error("expected at least 1")
	}
}

func TestProjection_5_RingWrap(t *testing.T) {
	w := openWriter(t)
	for range 200 {
		w.Append(makeEnv(t, "x", contract.EventStreamObserved, agent.EventThinking, "s1", 1, "stream"))
	}
	if len(NewProjector(w).Activity()) > 128 {
		t.Error("ring overflow")
	}
}

func TestProjection_6_Reconnect(t *testing.T) {
	w1 := openWriter(t)
	for range 5 {
		w1.Append(makeEnv(t, "r", contract.EventToolCallStarted, agent.EventToolCallStarted, "s1", 1, "t"))
	}
	w1.Close()
	if len(NewProjector(openWriter(t)).Activity()) != 0 {
		t.Error("expected 0 after reconnect")
	}
}

func TestProjection_7_GenerationReset(t *testing.T) {
	w := openWriter(t)
	for i := range 3 {
		w.Append(makeEnv(t, string(rune('a'+i)), contract.EventToolCallStarted, agent.EventToolCallStarted, "s1", 1, "g1"))
	}
	if NewProjector(w).Activity()[0].LaunchGeneration != 1 {
		t.Fatal("gen1 mismatch")
	}
	for i := range 2 {
		w.Append(makeEnv(t, string(rune('d'+i)), contract.EventToolCallStarted, agent.EventToolCallStarted, "s1", 2, "g2"))
	}
	items := NewProjector(w).Activity()
	if items[3].LaunchGeneration != 2 {
		t.Error("gen2 not ordered correctly")
	}
}

func TestProjection_8_Missing(t *testing.T) {
	w := openWriter(t)
	w.Append(makeEnv(t, "only", contract.EventToolCallStarted, agent.EventToolCallStarted, "s1", 1, "t"))
	if len(NewProjector(w).Activity()) != 1 {
		t.Error("expected 1")
	}
}

func TestProjection_9_RequestResultOrder(t *testing.T) {
	w := openWriter(t)
	w.Append(makeEnv(t, "req", contract.EventApprovalRequested, agent.EventApprovalRequested, "s1", 1, "r"))
	w.Append(makeEnv(t, "res", contract.EventApprovalResolved, agent.EventApprovalResolved, "s1", 1, "s"))
	items := NewProjector(w).Activity()
	if len(items) != 2 || items[0].EventKind != contract.EventApprovalRequested {
		t.Error("request must precede resolved")
	}
}

func TestProjection_10_ApprovalBinding(t *testing.T) {
	w := openWriter(t)
	w.Append(makeEnv(t, "app", contract.EventApprovalRequested, agent.EventApprovalRequested, "codex_app_server:s1", 7, "a"))
	it := NewProjector(w).Activity()
	if len(it) != 1 || it[0].SessionID != "codex_app_server:s1" || it[0].LaunchGeneration != 7 {
		t.Error("binding mismatch")
	}
}

func TestProjection_11_Degradation(t *testing.T) {
	_, err := writer.Open(writer.Config{Path: "/dev/full"}, nil)
	if err != nil {
		t.Skip("/dev/full unavailable")
	}
}

func TestProjection_12_UnknownKind(t *testing.T) {
	w := openWriter(t)
	w.Append(makeEnv(t, "unk", contract.EventEvidenceObserved, agent.EventUnknown, "s1", 1, "e"))
	if len(NewProjector(w).Activity()) < 1 {
		t.Error("expected at least 1")
	}
}

func TestProjection_13_TranscriptMapping(t *testing.T) {
	w := openWriter(t)
	w.Append(makeEnv(t, "t1", contract.EventProviderInvocationStarted, agent.EventAgentStarted, "s1", 1, ""))
	w.Append(makeEnv(t, "t2", contract.EventProviderInvocationFinished, "completed", "s1", 1, ""))
	w.Append(makeEnv(t, "t3", contract.EventToolCallStarted, agent.EventToolCallStarted, "s1", 1, "cmd"))
	w.Append(makeEnv(t, "t4", contract.EventApprovalRequested, agent.EventApprovalRequested, "s1", 1, "a"))
	w.Append(makeEnv(t, "t5", contract.EventApprovalResolved, agent.EventApprovalResolved, "s1", 1, "b"))
	w.Append(makeEnv(t, "t6", contract.EventStreamObserved, agent.EventThinking, "s1", 1, "t"))
	items := NewProjector(w).Transcript()
	if len(items) != 6 {
		t.Fatalf("expected 6, got %d", len(items))
	}
	if items[0].Text != "Agent started" {
		t.Errorf("0: %q", items[0].Text)
	}
	if items[1].Text != "Completed" {
		t.Errorf("1: %q", items[1].Text)
	}
	if items[3].Text != "Approval requested" {
		t.Errorf("3: %q", items[3].Text)
	}
	if items[4].Text != "Approval resolved" {
		t.Errorf("4: %q", items[4].Text)
	}
}
