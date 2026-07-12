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

// codexRec builds a RawRecord from a Codex JSONL line with native-log provenance
// and JSONL source (preserved per-record, not overridden by batch).
func codexRec(line string) contract.RawRecord {
	return contract.RawRecord{
		Bytes:      []byte(line),
		Source:     agent.SourceJSONL,
		Provenance: contract.ProvenanceNativeLog,
	}
}

// sessionMeta0_144_1 is the version-confirming session_meta record required at
// the start of valid record batches.
func sessionMeta0_144_1() contract.RawRecord {
	return codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","id":"m1","timestamp":"2026-07-06T13:29:26.903Z","cwd":"<HOME>/<PROJECT>","originator":"codex-tui","cli_version":"0.144.1","source":"cli","model_provider":"<PROVIDER>"}}`)
}

func codexFixtures() contract.ConformanceFixtures {
	// Valid records: session_meta (with correct cli_version) + task_started + user message.
	// The session_meta is required so the version gate passes.
	validRecords := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1","started_at":1783344575,"model_context_window":353400,"collaboration_mode_kind":"default"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:38.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<PROMPT>"}]}}`),
	}

	// Approval records: waiting_for_approval + approval_resolved, each carrying
	// the same approval_id so the approval lifecycle is linked.  Must include
	// session_meta so the version gate passes.
	approvalRecords := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:30:00.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","turn_id":"t2","approval_id":"appr-001","message":"<REDACTED_APPROVAL_MESSAGE>"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:30:05.000Z","type":"event_msg","payload":{"type":"approval_resolved","turn_id":"t2","approval_id":"appr-001","resolution":"approved"}}`),
	}

	// Near-miss: a response_item (user message) mentioning "approve".
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
		ExpectTypes:  []contract.AgentEventType{agent.EventAgentStarted},

		// Distinct records for bounds/dedupe/cursor checks. Each has a unique
		// nanosecond timestamp so Seq values are all distinct.
		DistinctRecord: func(i int) contract.RawRecord {
			ts := "2026-07-06T13:29:35." + nanoPad(i) + "Z"
			return codexRec(`{"timestamp":"` + ts + `","type":"event_msg","payload":{"type":"task_started","turn_id":"distinct-t` + strconv.Itoa(i) + `","started_at":1783344575}}`)
		},

		// SizedRecord for batch-byte boundary test.
		SizedRecord: func(size int) contract.RawRecord {
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
	if len(res.Events) < 2 {
		t.Fatalf("expected ≥2 events from valid records, got %d", len(res.Events))
	}

	got := toJSON(t, res.Events)
	checks := []string{
		`"type":"agent_started"`,
		`"type":"user_message"`,
		`"provenance":"native_log"`,
		`"sessionId":"pokit:host-a"`,
		`"agentKind":"codex"`,
	}
	for _, ck := range checks {
		if !contains(got, ck) {
			t.Errorf("DTO missing %s", ck)
		}
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

// ── BLOCKER 1: Version gate ──

func TestCodexAdapter_VersionGate_ExactMatch(t *testing.T) {
	a := &Adapter{}
	rec := sessionMeta0_144_1()
	ev, deg := a.NormalizeEvent(context.Background(), rec)
	if deg.Degraded {
		t.Errorf("exact version 0.144.1 must not degrade: %+v", deg)
	}
	if ev.Type != agent.EventAgentStarted {
		t.Errorf("exact version: got %q, want agent_started", ev.Type)
	}
	if ev.Confidence < 0.8 {
		t.Errorf("exact version confidence too low: %.2f", ev.Confidence)
	}
}

func TestCodexAdapter_VersionGate_NewerVersion(t *testing.T) {
	a := &Adapter{}
	rec := codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"cli_version":"9.9.9","session_id":"s1"}}`)
	ev, deg := a.NormalizeEvent(context.Background(), rec)
	if ev.Type != agent.EventUnknown {
		t.Errorf("newer version: got %q, want EventUnknown", ev.Type)
	}
	if !deg.Degraded {
		t.Error("newer version must degrade")
	}
}

func TestCodexAdapter_VersionGate_MissingVersion(t *testing.T) {
	a := &Adapter{}
	rec := codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1"}}`)
	ev, deg := a.NormalizeEvent(context.Background(), rec)
	if ev.Type != agent.EventUnknown {
		t.Errorf("missing version: got %q, want EventUnknown", ev.Type)
	}
	if !deg.Degraded {
		t.Error("missing version must degrade")
	}
}

func TestCodexAdapter_VersionGate_MalformedPayload(t *testing.T) {
	a := &Adapter{}
	// Version is a number, not a string — must not match "0.144.1".
	rec := codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"cli_version":1}}`)
	ev, deg := a.NormalizeEvent(context.Background(), rec)
	if ev.Type != agent.EventUnknown {
		t.Errorf("non-string version: got %q, want EventUnknown", ev.Type)
	}
	if !deg.Degraded {
		t.Error("non-string version must degrade")
	}
}

func TestCodexAdapter_VersionGate_BatchMismatch(t *testing.T) {
	a := &Adapter{}
	// First record: session_meta with wrong version. All subsequent records
	// in this batch must be EventUnknown.
	records := []contract.RawRecord{
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"cli_version":"9.9.9"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:38.000Z","type":"response_item","payload":{"type":"message","role":"user"}}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	for _, e := range res.Events {
		if e.Type != agent.EventUnknown {
			t.Errorf("version-mismatch batch: event %q, want EventUnknown", e.Type)
		}
	}
	if !res.Degraded.Degraded {
		t.Error("version-mismatch batch must be degraded")
	}
}

func TestCodexAdapter_VersionGate_NoSessionMeta_StillWorksDegraded(t *testing.T) {
	a := &Adapter{}
	// Without session_meta, no version-specific typed events may be emitted.
	// The event is still returned, but forced to EventUnknown + degraded.
	rec := codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`)
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: []contract.RawRecord{rec},
	})
	if len(res.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(res.Events))
	}
	if res.Events[0].Type != agent.EventUnknown {
		t.Errorf("no-meta: type=%q, want EventUnknown", res.Events[0].Type)
	}
	if !res.Degraded.Degraded {
		t.Error("no session_meta must produce degraded (version not confirmed)")
	}
}

// ── BLOCKER 2: No invented correlation ──

func TestCodexAdapter_Discovery_AlwaysEmpty(t *testing.T) {
	a := &Adapter{}

	// Empty context.
	got, _ := a.DiscoverSessions(context.Background(), contract.DiscoveryInput{
		Session: contract.SessionContext{},
	})
	if len(got) != 0 {
		t.Error("empty context must return empty")
	}

	// Codex process name alone does NOT grant correlation.
	got, _ = a.DiscoverSessions(context.Background(), contract.DiscoveryInput{
		Session: contract.SessionContext{SessionID: "s", ProcessName: "codex", CWD: "/home/dev/.codex"},
	})
	if len(got) != 0 {
		t.Errorf("process-name-only must not invent discovery: got %d sessions", len(got))
	}
}

// ── BLOCKER 3: Same-timestamp records preserved ──

func TestCodexAdapter_SameTimestamp_BothPreserved(t *testing.T) {
	a := &Adapter{}
	// Real fixture: session_meta and task_started share the exact same timestamp
	// (13:29:35.399). Both must appear in the result.
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1","started_at":1783344575}}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if len(res.Events) != 2 {
		t.Fatalf("same-timestamp: got %d events, want 2 (both preserved)", len(res.Events))
	}
	if res.Events[0].Seq >= res.Events[1].Seq {
		t.Errorf("Seq must be strictly increasing: %d >= %d", res.Events[0].Seq, res.Events[1].Seq)
	}
}

func TestCodexAdapter_SameTimestamp_Dedupe(t *testing.T) {
	a := &Adapter{}
	// Same record twice => 1 event.
	rec := sessionMeta0_144_1()
	records := []contract.RawRecord{rec, rec}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if len(res.Events) != 1 {
		t.Errorf("duplicate same-timestamp: got %d events, want 1", len(res.Events))
	}
}

func TestCodexAdapter_SameTimestamp_RereadNoReemit(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if len(res.Events) != 2 {
		t.Fatalf("first read: got %d events, want 2", len(res.Events))
	}
	// Re-read with cursor — must re-emit nothing.
	again, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
		Cursor:  res.NextCursor,
	})
	if len(again.Events) != 0 {
		t.Errorf("cursor re-read re-emitted %d events, want 0", len(again.Events))
	}
}

// ── BLOCKER 4: Degradation accumulation + deterministic timestamps ──

func TestCodexAdapter_MixedBatch_DegradedAccumulates(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.500Z","type":"unknown_msg_type","payload":{}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:36.000Z"}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	// The session_meta should still produce a valid event.
	foundStarted := false
	for _, e := range res.Events {
		if e.Type == agent.EventAgentStarted {
			foundStarted = true
		}
	}
	if !foundStarted {
		t.Error("mixed batch lost the valid session_meta event")
	}
	if !res.Degraded.Degraded {
		t.Error("mixed valid+malformed batch must be degraded")
	}
}

func TestCodexAdapter_InvalidTimestamp_Deterministic(t *testing.T) {
	a := &Adapter{}
	// Missing/invalid timestamp → zero time (never time.Now()).
	rec := codexRec(`{"type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`)
	ev1, _ := a.NormalizeEvent(context.Background(), rec)
	ev2, _ := a.NormalizeEvent(context.Background(), rec)
	if ev1.Seq != ev2.Seq {
		t.Errorf("non-deterministic Seq for same record: %d vs %d", ev1.Seq, ev2.Seq)
	}
	if toJSON(t, ev1) != toJSON(t, ev2) {
		t.Error("non-deterministic normalization for same record")
	}
}

// ── BLOCKER 5: Approval identity preserved on resolved ──

func TestCodexAdapter_Approval_ResolvedCarriesApprovalID(t *testing.T) {
	a := &Adapter{}
	// ReadEvents version gate: include session_meta with correct version so the
	// approval_resolved event is classified confidently.
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:30:05.000Z","type":"event_msg","payload":{"type":"approval_resolved","turn_id":"t2","approval_id":"appr-001","resolution":"approved"}}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	// Find the approval_resolved event.
	var got *contract.AgentEvent
	for i := range res.Events {
		if res.Events[i].Type == agent.EventApprovalResolved {
			got = &res.Events[i]
			break
		}
	}
	if got == nil {
		t.Fatal("approval_resolved event not found in batch")
	}
	if got.ApprovalID != "appr-001" {
		t.Errorf("approval_resolved ApprovalID=%q, want appr-001", got.ApprovalID)
	}
}

func TestCodexAdapter_Approval_ResolvedMissingID(t *testing.T) {
	a := &Adapter{}
	rec := codexRec(`{"timestamp":"2026-07-06T13:30:05.000Z","type":"event_msg","payload":{"type":"approval_resolved","turn_id":"t2","resolution":"approved"}}`)
	ev, deg := a.NormalizeEvent(context.Background(), rec)
	if ev.Type != agent.EventUnknown {
		t.Errorf("approval_resolved without approval_id: got %q, want EventUnknown", ev.Type)
	}
	if !deg.Degraded {
		t.Error("approval_resolved without approval_id must degrade")
	}
}

func TestCodexAdapter_Approval_RequestAndResolvedLinked(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:30:00.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","turn_id":"t2","approval_id":"appr-001","message":"approve?"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:30:05.000Z","type":"event_msg","payload":{"type":"approval_resolved","turn_id":"t2","approval_id":"appr-001","resolution":"approved"}}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if len(res.Events) < 2 {
		t.Fatalf("got %d events, want ≥2", len(res.Events))
	}
	// Both carry the same ApprovalID.
	var reqEv, resEv *contract.AgentEvent
	for i := range res.Events {
		switch res.Events[i].Type {
		case agent.EventApprovalRequested:
			reqEv = &res.Events[i]
		case agent.EventApprovalResolved:
			resEv = &res.Events[i]
		}
	}
	if reqEv == nil || resEv == nil {
		t.Fatal("missing approval_requested or approval_resolved in linked pair")
	}
	if reqEv.ApprovalID != "appr-001" {
		t.Errorf("request ApprovalID=%q", reqEv.ApprovalID)
	}
	if resEv.ApprovalID != "appr-001" {
		t.Errorf("resolved ApprovalID=%q", resEv.ApprovalID)
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

// ── Detection ──

func TestCodexAdapter_Detect_EmptyIsUnknown(t *testing.T) {
	a := &Adapter{}
	id, err := a.Detect(context.Background(), contract.SessionContext{})
	if err != nil {
		t.Fatal(err)
	}
	if id.Kind != "unknown" || id.Confidence >= 0.5 {
		t.Errorf("empty context: kind=%q conf=%.2f", id.Kind, id.Confidence)
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

// ── Additional: CapLogDetection removed ──

func TestCodexAdapter_Descriptor_NoCapLogDetection(t *testing.T) {
	d := (&Adapter{}).Descriptor()
	for _, c := range d.Capabilities {
		if c == contract.CapLogDetection {
			t.Error("CapLogDetection must not be declared (no file scanning / LogRef discovery)")
		}
	}
}

// ── Additional: source per-record preserved ──

func TestCodexAdapter_PerRecordSource(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		{Bytes: []byte(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"cli_version":"0.144.1"}}`), Source: agent.SourceJSONL},
		{Bytes: []byte(`{"timestamp":"2026-07-06T13:29:38.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`), Source: agent.SourceLogFile},
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if len(res.Events) < 2 {
		t.Fatalf("got %d events, want 2", len(res.Events))
	}
	// Each event keeps its own source; batch-first-record override removed.
	sources := map[contract.AgentEventSource]int{}
	for _, e := range res.Events {
		sources[e.Source]++
	}
	if sources[agent.SourceJSONL] == 0 || sources[agent.SourceLogFile] == 0 {
		t.Errorf("per-record source not preserved: %v", sources)
	}
}

// ── Cursor: compact position+anchor, strict validation ──
//
// Input policy: ordered full-prefix snapshots.  The cursor encodes the
// absolute position and content-hash anchor of the last emitted record.
// Mismatch → 0 events + degraded.

func TestCodexAdapter_Cursor_LargeFullPrefix_ZeroReemission(t *testing.T) {
	a := &Adapter{}
	var records []contract.RawRecord
	records = append(records, sessionMeta0_144_1())
	for i := 0; i < 299; i++ {
		records = append(records, codexRec(`{"timestamp":"2026-07-06T13:29:35.`+nanoPad(i)+`Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t`+strconv.Itoa(i)+`"}}`))
	}
	res1, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if len(res1.Events) != 300 {
		t.Fatalf("first read: got %d events, want 300", len(res1.Events))
	}
	if len(res1.NextCursor) > contract.MaxCursorBytes {
		t.Errorf("cursor too large: %d bytes", len(res1.NextCursor))
	}

	// Re-read identical prefix → anchor validated at pos-1 → 0 events.
	res2, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
		Cursor:  res1.NextCursor,
	})
	if len(res2.Events) != 0 {
		t.Fatalf("re-read: got %d events, want 0", len(res2.Events))
	}
}

func TestCodexAdapter_Cursor_MaxBatch_ZeroReemission(t *testing.T) {
	a := &Adapter{}
	var records []contract.RawRecord
	records = append(records, sessionMeta0_144_1())
	for i := 0; i < contract.MaxEventsPerRead-1; i++ {
		records = append(records, codexRec(`{"timestamp":"2026-07-06T13:29:35.`+nanoPad(i)+`Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t`+strconv.Itoa(i)+`"}}`))
	}
	res1, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if len(res1.Events) != contract.MaxEventsPerRead {
		t.Fatalf("first read: got %d events", len(res1.Events))
	}
	if len(res1.NextCursor) > contract.MaxCursorBytes {
		t.Errorf("cursor too large: %d bytes", len(res1.NextCursor))
	}
	res2, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
		Cursor:  res1.NextCursor,
	})
	if len(res2.Events) != 0 {
		t.Fatalf("max-batch re-read: got %d events, want 0", len(res2.Events))
	}
}

func TestCodexAdapter_Cursor_CallerLimitPagination(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.001Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.002Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t2"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.003Z","type":"response_item","payload":{"type":"message","role":"user"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.004Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t3"}}`),
	}
	seen := map[string]bool{}
	var cursor contract.Cursor
	for {
		res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
			Session:   contract.SessionContext{SessionID: "pokit:host-a"},
			Records:   records,
			Cursor:    cursor,
			MaxEvents: 2,
		})
		for _, e := range res.Events {
			if seen[e.ID] {
				t.Errorf("duplicate emission: %s", e.ID)
			}
			seen[e.ID] = true
		}
		cursor = res.NextCursor
		if len(res.Events) < 2 {
			break
		}
	}
	if len(seen) != 5 {
		t.Errorf("pagination: got %d unique events, want 5", len(seen))
	}
}

func TestCodexAdapter_Cursor_AppendAfterRead(t *testing.T) {
	// Full-prefix with appended record: cursor skips previously emitted,
	// new record gets typed event with advancing Seq.
	a := &Adapter{}
	first := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"first"}}`),
	}
	res1, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: first,
	})
	if len(res1.Events) != 2 {
		t.Fatalf("first read: got %d events, want 2", len(res1.Events))
	}

	// Full prefix with new record appended.
	second := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"first"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:40.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"second"}}`),
	}
	res2, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: second,
		Cursor:  res1.NextCursor,
	})
	if len(res2.Events) != 1 {
		t.Fatalf("append: got %d events, want 1", len(res2.Events))
	}
	if res2.Events[0].Type != agent.EventAgentStarted {
		t.Errorf("append: type=%q, want agent_started", res2.Events[0].Type)
	}
	prevMax := res1.Events[len(res1.Events)-1].Seq
	if res2.Events[0].Seq <= prevMax {
		t.Errorf("append Seq %d <= previous max %d", res2.Events[0].Seq, prevMax)
	}
}

func TestCodexAdapter_Cursor_Deterministic(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`),
	}
	res1, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	res2, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if string(res1.NextCursor) != string(res2.NextCursor) {
		t.Errorf("non-deterministic cursor:\n  %s\n  %s", res1.NextCursor, res2.NextCursor)
	}
	if toJSON(t, res1.Events) != toJSON(t, res2.Events) {
		t.Error("non-deterministic events")
	}
}

// ── Fail-closed: 0 events + degraded on any cursor anomaly ──

func TestCodexAdapter_Cursor_AnchorTamper_ZeroEvents(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`),
	}
	res1, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	// Tamper: replace anchor with random hex.
	cur, _ := parseCursor(res1.NextCursor)
	cur.anchor = "deadbeefcafebabe"
	tampered := encodeCursor(cur)

	res2, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
		Cursor:  tampered,
	})
	if len(res2.Events) != 0 {
		t.Errorf("tampered anchor: got %d events, want 0 (fail-closed)", len(res2.Events))
	}
	if !res2.Degraded.Degraded {
		t.Error("tampered anchor must degrade")
	}
}

func TestCodexAdapter_Cursor_AnchorLoss_ZeroEvents(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`),
	}
	res1, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	cur, _ := parseCursor(res1.NextCursor)
	// Construct a cursor with pos>0 but empty anchor directly (parseCursor
	// rejects this as invalid syntax, so we build the string manually).
	badCursor := contract.Cursor(strconv.FormatInt(cur.nextPos, 10) + ":")
	res3, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
		Cursor:  badCursor,
	})
	if len(res3.Events) != 0 {
		t.Errorf("anchor loss: got %d events, want 0 (fail-closed)", len(res3.Events))
	}
	if !res3.Degraded.Degraded {
		t.Error("anchor loss must degrade")
	}
}

func TestCodexAdapter_Cursor_StreamRotation_ZeroEvents(t *testing.T) {
	a := &Adapter{}
	streamA := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"streamA"}}`),
	}
	resA, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: streamA,
	})

	// Apply stream-A cursor to completely different stream-B prefix.
	streamB := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T14:00:00.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"streamB-1"}}`),
	}
	resB, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: streamB,
		Cursor:  resA.NextCursor,
	})
	if len(resB.Events) != 0 {
		t.Errorf("stream rotation: got %d events, want 0 (fail-closed)", len(resB.Events))
	}
	if !resB.Degraded.Degraded {
		t.Error("stream rotation must degrade")
	}
}

func TestCodexAdapter_Cursor_PositionAnchorMismatch_ZeroEvents(t *testing.T) {
	// Cursor with correct anchor but wrong position → fail-closed.
	a := &Adapter{}
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:36.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t2"}}`),
	}
	res1, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	// Cursor has pos=2, anchor=t1_hash (last emitted).
	// Tamper: set pos=1 but keep anchor as t1_hash → anchor is at pos 0, not pos-1=0.
	cur, _ := parseCursor(res1.NextCursor)
	cur.nextPos = 1 // mismatched: anchor (record 1's hash) should be at pos 0 if pos=1
	tampered := encodeCursor(cur)

	res2, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
		Cursor:  tampered,
	})
	if len(res2.Events) != 0 {
		t.Errorf("position/anchor mismatch: got %d events, want 0", len(res2.Events))
	}
	if !res2.Degraded.Degraded {
		t.Error("position/anchor mismatch must degrade")
	}
}

func TestCodexAdapter_Cursor_VersionForgery_Rejected(t *testing.T) {
	// A cursor claiming version confirmation without actual 0.144.1
	// session_meta in the batch must NOT activate typed events.
	// (Old cursor format v:1:0: — pos=0 anchor="" but ver=1 was forgeable.
	// New format has no version field — version is always from current batch.)
	a := &Adapter{}
	// Craft a cursor at pos=0 (empty anchor) — forged "confirmed" equivalent
	// in the old format would be v:1:0:. In new format, just start from 0.
	records := []contract.RawRecord{
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	// No session_meta → version not confirmed → all events EventUnknown.
	if len(res.Events) != 1 {
		t.Fatalf("got %d events, want 1", len(res.Events))
	}
	if res.Events[0].Type != agent.EventUnknown {
		t.Errorf("no session_meta: type=%q, want EventUnknown (no forged version authority)", res.Events[0].Type)
	}
}

func TestCodexAdapter_Cursor_DuplicateRecords_AnchorAtCorrectPosition(t *testing.T) {
	// Cursor anchor is validated at exact position nextPos-1.
	// Even when records are identical at different positions, the anchor
	// at the specific position must match.
	a := &Adapter{}
	uniq := codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"uniq"}}`)
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		uniq,
		uniq, // duplicate at position 2 (batch-dedup'd in read 1)
	}
	res1, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	// Cursor records pos=3 (3 input records processed).
	if len(res1.Events) < 1 {
		t.Fatal("no events emitted")
	}

	// Re-read: cursor pos=3, anchor should be the hash of uniq.
	// validateCursorAnchor checks all[2].ID == anchor. all[2] is uniq
	// (the duplicate copy), which has the same ID as all[1]. ✓
	res2, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
		Cursor:  res1.NextCursor,
	})
	// pos=3 means skip all 3 records → 0 events.
	if len(res2.Events) != 0 {
		t.Errorf("re-read: got %d events, want 0 (all positions skipped)", len(res2.Events))
	}
}

func TestCodexAdapter_Cursor_SeqFromAbsolutePosition(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:36.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t2"}}`),
	}
	res1, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "pokit:host-a"},
		Records:   records,
		MaxEvents: 1,
	})
	if len(res1.Events) != 1 {
		t.Fatalf("limited read: got %d events, want 1", len(res1.Events))
	}
	firstSeq := res1.Events[0].Seq

	// Continuation full-prefix: cursor pos=1, anchor=record[0].ID.
	// Skip pos=1 records, emit records 1-2 with Seq=1,2.
	res2, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
		Cursor:  res1.NextCursor,
	})
	if len(res2.Events) != 2 {
		t.Fatalf("continuation: got %d events, want 2", len(res2.Events))
	}
	if res2.Events[0].Seq != firstSeq+1 {
		t.Errorf("first continuation Seq=%d, want %d", res2.Events[0].Seq, firstSeq+1)
	}
	if res2.Events[1].Seq != firstSeq+2 {
		t.Errorf("second continuation Seq=%d, want %d", res2.Events[1].Seq, firstSeq+2)
	}
}

// ── Cross-page duplicate suppression ──

func TestCodexAdapter_Cursor_CrossPageDuplicate_NotReemitted(t *testing.T) {
	// Duplicate record appears at position 0 and position 2.
	// Paginate with MaxEvents=1 — the duplicate at pos 2 must NOT be
	// re-emitted because the prefix dedup set includes it from pos 0.
	a := &Adapter{}
	b := codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"dup"}}`)
	records := []contract.RawRecord{
		sessionMeta0_144_1(), // position 0
		b,                    // position 1
		b,                    // position 2 (duplicate of pos 1)
		codexRec(`{"timestamp":"2026-07-06T13:29:36.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t2"}}`), // position 3
	}

	seen := map[string]bool{}
	var cursor contract.Cursor
	for {
		res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
			Session:   contract.SessionContext{SessionID: "pokit:host-a"},
			Records:   records,
			Cursor:    cursor,
			MaxEvents: 1,
		})
		for _, e := range res.Events {
			if seen[e.ID] {
				t.Errorf("cross-page duplicate re-emitted: %s", e.ID)
			}
			seen[e.ID] = true
		}
		cursor = res.NextCursor
		if len(res.Events) == 0 {
			break
		}
	}
	if len(seen) != 3 {
		t.Errorf("got %d unique events, want 3 (session_meta + dup + t2)", len(seen))
	}
}

func TestCodexAdapter_Cursor_MaxBatchRecordsPlusOne_PaginatedFully(t *testing.T) {
	// Full prefix of MaxBatchRecords+1 records. Paginate through all.
	// Verify every record emitted exactly once, no loss, no duplicates.
	a := &Adapter{}
	n := contract.MaxBatchRecords + 1
	var records []contract.RawRecord
	records = append(records, sessionMeta0_144_1())
	for i := 1; i < n; i++ {
		records = append(records, codexRec(`{"timestamp":"2026-07-06T13:29:35.`+nanoPad(i)+`Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t`+strconv.Itoa(i)+`"}}`))
	}

	seen := map[string]bool{}
	var cursor contract.Cursor
	var seqs []int64
	for {
		res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
			Session:   contract.SessionContext{SessionID: "pokit:host-a"},
			Records:   records,
			Cursor:    cursor,
			MaxEvents: 200,
		})
		for _, e := range res.Events {
			if seen[e.ID] {
				t.Errorf("duplicate emission: %s", e.ID)
			}
			seen[e.ID] = true
			seqs = append(seqs, e.Seq)
		}
		cursor = res.NextCursor
		if len(res.Events) == 0 {
			break
		}
	}
	if len(seen) != n {
		t.Errorf("MaxBatchRecords+1 pagination: got %d unique events, want %d", len(seen), n)
	}
	// All Seq strictly increasing across pages.
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Errorf("cross-page Seq not strictly increasing: %d <= %d", seqs[i], seqs[i-1])
		}
	}
	// Final cursor re-read → 0 events.
	final, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
		Cursor:  cursor,
	})
	if len(final.Events) != 0 {
		t.Errorf("final re-read: got %d events, want 0", len(final.Events))
	}
}

func TestCodexAdapter_Cursor_ByteBound_LargeSuffix(t *testing.T) {
	// Individually-valid records (~500 KB each) that together exceed
	// MaxBatchBytes (8 MiB). 20 records × 500 KB = 10 MB > 8 MB bound.
	a := &Adapter{}
	recSize := 500_000 // well under MaxRecordBytes (1 MiB)
	jsonOverhead := 120
	padPerRec := recSize - jsonOverhead
	if padPerRec < 0 {
		padPerRec = 0
	}
	nRecords := 22 // 22 × 500 KB ≈ 11 MB > 8 MB MaxBatchBytes
	var records []contract.RawRecord
	records = append(records, sessionMeta0_144_1())
	for i := 0; i < nRecords; i++ {
		records = append(records, codexRec(`{"timestamp":"2026-07-06T13:29:35.`+nanoPad(i)+`Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t`+strconv.Itoa(i)+`","_pad":"`+strings.Repeat("x", padPerRec)+`"}}`))
	}

	res1, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if len(res1.Events) == 0 {
		t.Fatal("first read produced 0 events")
	}
	if !res1.Degraded.Degraded {
		t.Error("byte-bound truncation must degrade")
	}
	cur, _ := parseCursor(res1.NextCursor)
	if cur.nextPos == 0 {
		t.Error("cursor did not advance past byte-bound truncation")
	}

	// Continuation: remaining records processed from cursor forward.
	if cur.nextPos < int64(len(records)) {
		res2, _ := a.ReadEvents(context.Background(), contract.ReadInput{
			Session: contract.SessionContext{SessionID: "pokit:host-a"},
			Records: records,
			Cursor:  res1.NextCursor,
		})
		if len(res1.Events)+len(res2.Events) <= len(res1.Events) {
			t.Error("continuation did not make progress after byte-bound truncation")
		}
	}
}

func TestCodexAdapter_Cursor_CombinedLimitDuplicateBound(t *testing.T) {
	// Combined: caller limit + within-batch content dedup + batch bound.
	// Within-batch content dedup prevents same-ID emission in one page.
	// Cross-page, identical content at a different source position is a
	// distinct position-based event (bounded window does not dedup across
	// pages by content — that would require unbounded historical ID storage).
	a := &Adapter{}
	dup := codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"dup"}}`)
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		dup,
		codexRec(`{"timestamp":"2026-07-06T13:29:36.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`),
		dup, // position 3: same content as position 1, distinct source position
		codexRec(`{"timestamp":"2026-07-06T13:29:37.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t2"}}`),
	}

	emitted := 0
	var cursor contract.Cursor
	var seqs []int64
	for {
		res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
			Session:   contract.SessionContext{SessionID: "pokit:host-a"},
			Records:   records,
			Cursor:    cursor,
			MaxEvents: 1,
		})
		emitted += len(res.Events)
		for _, e := range res.Events {
			seqs = append(seqs, e.Seq)
		}
		cursor = res.NextCursor
		if len(res.Events) == 0 {
			break
		}
	}
	// 5 position-based events emitted: meta + dup(pos1) + t1 + dup(pos3) + t2.
	if emitted != 5 {
		t.Errorf("combined: got %d emissions, want 5 (position-based identity)", emitted)
	}
	// Seq strictly increasing.
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Errorf("combined cross-page Seq: %d <= %d", seqs[i], seqs[i-1])
		}
	}
}

func TestCodexAdapter_Seq_MissingTimestampEmitted(t *testing.T) {
	a := &Adapter{}
	rec := codexRec(`{"type":"event_msg","payload":{"type":"task_started","turn_id":"no-ts"}}`)
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: []contract.RawRecord{rec},
	})
	if len(res.Events) != 1 {
		t.Fatalf("missing-timestamp: got %d events, want 1", len(res.Events))
	}
	if res.Events[0].Type != agent.EventUnknown {
		t.Errorf("missing-timestamp: type=%q, want EventUnknown", res.Events[0].Type)
	}
	if err := contract.ValidateEvent(res.Events[0]); err != nil {
		t.Errorf("emitted event fails ValidateEvent: %v", err)
	}
}

func TestCodexAdapter_ValidateEvent_AllReturnedEvents(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:38.000Z","type":"response_item","payload":{"type":"message","role":"user"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:30:00.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","turn_id":"t2","approval_id":"appr-001"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:30:05.000Z","type":"event_msg","payload":{"type":"approval_resolved","turn_id":"t2","approval_id":"appr-001"}}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if len(res.Events) != 5 {
		t.Fatalf("got %d events, want 5", len(res.Events))
	}
	for _, e := range res.Events {
		if err := contract.ValidateEvent(e); err != nil {
			t.Errorf("ValidateEvent failed for %s (type=%s): %v", e.ID, e.Type, err)
		}
	}
}

// ── BLOCKER 2 (round 2): Version gate validates entire batch ──

func TestCodexAdapter_VersionGate_MixedMeta_BatchAllUnknown(t *testing.T) {
	// session_meta 0.144.1 followed by session_meta 9.9.9 — the second
	// meta poisons the ENTIRE batch. All events must be EventUnknown.
	a := &Adapter{}
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:36.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`),
		codexRec(`{"timestamp":"2026-07-06T14:00:00.000Z","type":"session_meta","payload":{"cli_version":"9.9.9"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:38.000Z","type":"response_item","payload":{"type":"message","role":"user"}}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	for _, e := range res.Events {
		if e.Type != agent.EventUnknown {
			t.Errorf("mixed meta batch: event %s type=%q, want EventUnknown for all", e.ID, e.Type)
		}
	}
	if !res.Degraded.Degraded {
		t.Error("mixed meta batch must be degraded")
	}
}

func TestCodexAdapter_VersionGate_NoMeta_AllUnknown(t *testing.T) {
	// No session_meta at all — version unconfirmed. No version-specific
	// typed events may be emitted. Everything must be EventUnknown.
	a := &Adapter{}
	records := []contract.RawRecord{
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:38.000Z","type":"response_item","payload":{"type":"message","role":"user"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:30:00.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","turn_id":"t2","approval_id":"appr-001"}}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	for _, e := range res.Events {
		if e.Type != agent.EventUnknown {
			t.Errorf("no-meta batch: event %s type=%q, want EventUnknown for all", e.ID, e.Type)
		}
	}
	if !res.Degraded.Degraded {
		t.Error("no-meta batch must be degraded")
	}
}

// ── BLOCKER 3 (round 2): Degradation reason preservation ──

func TestCodexAdapter_MixedBatch_ReasonPreserved(t *testing.T) {
	// Mixed valid+malformed batch must preserve a specific degradation reason,
	// not just an empty degraded flag.
	a := &Adapter{}
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.500Z","type":"unknown_msg_type","payload":{}}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if !res.Degraded.Degraded {
		t.Fatal("batch with unknown type must be degraded")
	}
	// The reason must contain something about the unknown type, not just be empty.
	if res.Degraded.Reason == "" {
		t.Error("degraded reason is empty — per-record reason was lost")
	}
	if !contains(res.Degraded.Reason, "unknown") && !contains(res.Degraded.Reason, "unknown_msg_type") {
		t.Errorf("degraded reason missing specific cause: %q", res.Degraded.Reason)
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

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchSubstring(s, sub)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
