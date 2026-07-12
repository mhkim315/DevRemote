package v0_144_1

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// newAdapter builds a healthy codex adapter for conformance testing.
func newAdapter(t *testing.T) contract.AgentAdapter {
	t.Helper()
	return &Adapter{}
}

// failingAdapter builds an adapter whose ReadEvents always returns degraded.
func failingAdapter(t *testing.T) contract.AgentAdapter {
	t.Helper()
	return &Adapter{failRead: true}
}

// ── Fixtures ──

// codexRec builds a RawRecord from a Codex JSONL line with native-log provenance.
func codexRec(line string) contract.RawRecord {
	return contract.RawRecord{
		Bytes:      []byte(line),
		Source:     agent.SourceJSONL,
		Provenance: contract.ProvenanceNativeLog,
	}
}

func codexFixtures() contract.ConformanceFixtures {
	// Valid records: session_meta + task_started + user message (from session_task.jsonl structure).
	validRecords := []contract.RawRecord{
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","id":"m1","timestamp":"2026-07-06T13:29:26.903Z","cwd":"<HOME>/<PROJECT>","originator":"codex-tui","cli_version":"0.144.1","source":"cli","model_provider":"<PROVIDER>"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1","started_at":1783344575,"model_context_window":353400,"collaboration_mode_kind":"default"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:38.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<PROMPT>"}]}}`),
	}

	// Approval records: waiting_for_approval (from approval_waiting.jsonl structure).
	approvalRecords := []contract.RawRecord{
		codexRec(`{"timestamp":"2026-07-06T13:30:00.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","turn_id":"t2","approval_id":"appr-001","message":"<REDACTED_APPROVAL_MESSAGE>"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:30:05.000Z","type":"event_msg","payload":{"type":"approval_resolved","turn_id":"t2","approval_id":"appr-001","resolution":"approved"}}`),
	}

	// Near-miss: a user message mentioning "approve" must NOT trigger approval.
	nearMissRecords := []contract.RawRecord{
		codexRec(`{"timestamp":"2026-07-06T13:30:01.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"please approve this change"}]}}`),
	}

	// Malformed records: unknown type, missing fields, garbage.
	malformedRecords := []contract.RawRecord{
		codexRec(`{"timestamp":"2026-07-06T13:29:35.500Z","type":"unknown_msg_type","payload":{}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:36.000Z"}`),
		codexRec(`{"type":"response_item"}`),
		{},
		{Bytes: []byte("{"), Source: agent.SourceJSONL},
		{Bytes: []byte("\x00\xff not json"), Source: agent.SourceJSONL},
		{Bytes: nil},
	}

	return contract.ConformanceFixtures{
		DetectContext:    contract.SessionContext{SessionID: "pokit:host-a", ProcessName: "codex", CWD: "/Users/dev/.codex/sessions/2026/07/06"},
		ExpectDetectKind: "codex",

		ValidRecords: validRecords,
		ExpectTypes:  []contract.AgentEventType{agent.EventAgentStarted, agent.EventUserMessage},

		// Distinct records for bounds/dedupe/cursor checks.  Each record gets a
		// unique Seq via the index in the timestamp nanosecond field so the
		// harness can generate thousands of distinct records without collisions.
		DistinctRecord: func(i int) contract.RawRecord {
			// RFC 3339 timestamp with unique nanosecond per i.
			ts := "2026-07-06T13:29:35." + nanoPad(i) + "Z"
			return codexRec(`{"timestamp":"` + ts + `","type":"event_msg","payload":{"type":"task_started","turn_id":"distinct-t` + strconv.Itoa(i) + `","started_at":1783344575}}`)
		},

		// SizedRecord for batch-byte boundary test. Each record is roughly size bytes.
		SizedRecord: func(size int) contract.RawRecord {
			// Reserve ~80 bytes for JSON envelope: {"timestamp":"...","type":"event_msg","payload":{"type":"task_started","turn_id":"sz-...","started_at":0,"_pad":"..."}}
			pad := size - 90
			if pad < 0 {
				pad = 0
			}
			b := make([]byte, pad)
			for i := range b {
				b[i] = byte('a' + (i % 26))
			}
			return codexRec(`{"timestamp":"2026-07-06T13:29:35.00Z","type":"event_msg","payload":{"type":"task_started","turn_id":"sz-` + strconv.Itoa(size) + `","started_at":0,"_pad":"` + string(b) + `"}}`)
		},

		FailingFactory:   failingAdapter,
		ApprovalRecords:  approvalRecords,
		NearMissRecords:  nearMissRecords,
		MalformedRecords: malformedRecords,
	}
}

// nanoPad formats i as a 9-digit nanosecond field with zero-padding so every
// record gets a unique RFC 3339 timestamp and therefore a distinct Seq.
func nanoPad(i int) string {
	s := strconv.Itoa(i)
	for len(s) < 9 {
		s = "0" + s
	}
	return s
}

// ── Contract conformance ──

func TestCodexAdapter_Conformance(t *testing.T) {
	contract.RunAgentContract(t, "codex", func(t *testing.T) contract.AgentAdapter {
		return &Adapter{}
	}, codexFixtures())
}

// ── DTO snapshot stability ──

func TestCodexAdapter_DTOSnapshotStable(t *testing.T) {
	a := &Adapter{}
	res, err := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: codexFixtures().ValidRecords,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Degraded.Degraded && len(res.Events) < 2 {
		t.Fatalf("expected ≥2 events from valid records, got %d", len(res.Events))
	}

	// Snapshot: verify stable JSON shape.
	got := toJSON(t, res.Events)
	// Expected types must appear.
	if !strings.Contains(got, `"type":"agent_started"`) {
		t.Error("DTO missing agent_started event")
	}
	if !strings.Contains(got, `"type":"user_message"`) {
		t.Error("DTO missing user_message event")
	}
	// Events must carry provenance.
	if !strings.Contains(got, `"provenance":"native_log"`) {
		t.Error("DTO missing provenance field")
	}
	// Session binding.
	if !strings.Contains(got, `"sessionId":"pokit:host-a"`) {
		t.Error("DTO missing session binding")
	}
	// Agent kind.
	if !strings.Contains(got, `"agentKind":"codex"`) {
		t.Error("DTO missing codex agent kind")
	}

	// Second read must produce identical JSON.
	res2, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: codexFixtures().ValidRecords,
	})
	if toJSON(t, res.Events) != toJSON(t, res2.Events) {
		t.Error("normalization is not deterministic across fresh reads")
	}
}

// ── Secret leak prevention ──

func TestCodexAdapter_NoSecretLeak(t *testing.T) {
	hostile := `{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1","message":"token ` + "sk" + `-abcd1234567890abcd at /Users/victim/x"}}`
	rec := contract.RawRecord{Bytes: []byte(hostile), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog}

	a := &Adapter{}
	ev, deg := a.NormalizeEvent(context.Background(), rec)

	if contract.ContainsSensitive(ev.Text) {
		t.Errorf("event Text leaked sensitive content: %q", ev.Text)
	}
	for _, v := range ev.Metadata {
		if contract.ContainsSensitive(v) {
			t.Errorf("event Metadata leaked sensitive content: %q", v)
		}
	}
	if contract.ContainsSensitive(deg.Reason) {
		t.Errorf("degraded reason leaked sensitive content: %q", deg.Reason)
	}
	for _, d := range deg.Diagnostics {
		if contract.ContainsSensitive(d) {
			t.Errorf("degraded diagnostic leaked sensitive content: %q", d)
		}
	}

	// Read path must also sanitize.
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: []contract.RawRecord{rec},
	})
	for _, diag := range res.Degraded.Diagnostics {
		if contract.ContainsSensitive(diag) {
			t.Errorf("ReadEvents diagnostic leaked sensitive content: %q", diag)
		}
	}
}

// ── Unsupported version / shape ──

func TestCodexAdapter_UnsupportedVersion_UnknownShape(t *testing.T) {
	a := &Adapter{}

	// A completely unknown JSON shape must NOT produce typed events.
	unknown := codexRec(`{"version":"9.9.9","kind":"future_event","data":{"extra":"field"}}`)
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: []contract.RawRecord{unknown},
	})
	for _, e := range res.Events {
		if e.Type != agent.EventUnknown {
			t.Errorf("unsupported version produced typed event %q, want EventUnknown", e.Type)
		}
		if e.Confidence >= 0.5 {
			t.Errorf("unsupported version event has confidence %.2f, want < 0.5", e.Confidence)
		}
	}
}

// ── Detection boundaries ──

func TestCodexAdapter_Detect_EmptyIsUnknown(t *testing.T) {
	a := &Adapter{}
	id, err := a.Detect(context.Background(), contract.SessionContext{})
	if err != nil {
		t.Fatal(err)
	}
	if id.Kind != "unknown" {
		t.Errorf("empty context kind=%q, want unknown", id.Kind)
	}
	if id.Confidence >= 0.5 {
		t.Errorf("empty context confidence=%.2f, want < 0.5", id.Confidence)
	}
}

func TestCodexAdapter_Detect_UnknownProcess(t *testing.T) {
	a := &Adapter{}
	id, err := a.Detect(context.Background(), contract.SessionContext{
		SessionID:   "s",
		ProcessName: "bash",
		CWD:         "/tmp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id.Kind != "unknown" {
		t.Errorf("bash process kind=%q, want unknown", id.Kind)
	}
}

func TestCodexAdapter_Discovery_EmptyReturnsEmpty(t *testing.T) {
	a := &Adapter{}
	got, err := a.DiscoverSessions(context.Background(), contract.DiscoveryInput{
		Session: contract.SessionContext{},
		Limit:   10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("empty context returned %d sessions, want 0", len(got))
	}
}

func TestCodexAdapter_Discovery_Bounded(t *testing.T) {
	a := &Adapter{}
	got, err := a.DiscoverSessions(context.Background(), contract.DiscoveryInput{
		Session: contract.SessionContext{SessionID: "s", ProcessName: "codex", CWD: "/home/dev/.codex"},
		Limit:   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 1 {
		t.Errorf("Limit=1 got %d sessions", len(got))
	}
	if len(got) == 1 {
		if got[0].Correlation != contract.CorrelationManagedLaunch {
			t.Errorf("correlation=%q, want managed_launch", got[0].Correlation)
		}
		if got[0].Provider != "codex" {
			t.Errorf("provider=%q, want codex", got[0].Provider)
		}
	}
}

func TestCodexAdapter_Discovery_NonCodexEmpty(t *testing.T) {
	a := &Adapter{}
	got, _ := a.DiscoverSessions(context.Background(), contract.DiscoveryInput{
		Session: contract.SessionContext{SessionID: "s", ProcessName: "claude"},
		Limit:   10,
	})
	if len(got) != 0 {
		t.Errorf("claude process returned %d sessions, want 0", len(got))
	}
}

// ── GetStatus delegation ──

func TestCodexAdapter_GetStatus_NoEvidence(t *testing.T) {
	a := &Adapter{}
	res, err := a.GetStatus(context.Background(), contract.StatusInput{
		Session: contract.SessionContext{SessionID: "s"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != agent.StatusUnknown {
		t.Errorf("no evidence status=%q, want unknown", res.Status)
	}
	if !res.Degraded.Degraded {
		t.Error("no evidence must produce degraded result")
	}
}

// ── Helpers ──

func toJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}
