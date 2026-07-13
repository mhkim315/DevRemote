package term

import (
	"context"
	"encoding/json"
	"testing"

	"devremote/companion-daemon/internal/agent"
	claude "devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202"
	codex "devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1"
	"devremote/companion-daemon/internal/agent/contract"
)

// ── Test record helpers ──

// testCodexSessionMeta returns a valid Codex session_meta record at version 0.144.1.
func testCodexSessionMeta() []byte {
	return []byte(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","id":"m1","timestamp":"2026-07-06T13:29:26.903Z","cwd":"/home/user/project","originator":"codex-tui","cli_version":"0.144.1","source":"cli","model_provider":"openai"}}`)
}

// testCodexTaskStarted returns a valid Codex task_started record.
func testCodexTaskStarted() []byte {
	return []byte(`{"timestamp":"2026-07-06T13:29:36.000Z","type":"task_started","payload":{"task_id":"t1","message":"test task"}}`)
}

// testCodexAssistantMessage returns a valid Codex assistant message record.
func testCodexAssistantMessage() []byte {
	return []byte(`{"timestamp":"2026-07-06T13:29:37.000Z","type":"assistant","payload":{"message":{"content":[{"type":"text","text":"hello world"}]}}}`)
}

// testClaudeVersionRecord returns a valid Claude record at version 2.1.202.
func testClaudeVersionRecord() []byte {
	return []byte(`{"version":"2.1.202","type":"user","message":{"role":"user","content":[{"type":"text","text":"test prompt"}]}}`)
}

// testClaudeAssistantRecord returns a valid Claude assistant record.
func testClaudeAssistantRecord() []byte {
	return []byte(`{"version":"2.1.202","type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hello from claude"}]}}`)
}

// testCodexSessionMetaWrongVersion returns a session_meta with unsupported version.
func testCodexSessionMetaWrongVersion() []byte {
	return []byte(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","id":"m1","cli_version":"9.9.9","source":"cli"}}`)
}

// testClaudeWrongVersion returns a Claude record with unsupported version.
func testClaudeWrongVersion() []byte {
	return []byte(`{"version":"9.9.9","type":"user","message":{"role":"user","content":[{"type":"text","text":"test"}]}}`)
}

// ── Codex bridge tests ──

func TestBridge_Codex_FirstPollAuthorityPlusEvents(t *testing.T) {
	// Simulate the first poll: session_meta + task_started.
	// Adapter must validate version from position 0 and return events.
	adapter := &codex.Adapter{}
	records := []contract.RawRecord{
		{Bytes: testCodexSessionMeta(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
		{Bytes: testCodexTaskStarted(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
	}

	result, err := adapter.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "controlled_pty:test"},
		Records:   records,
		Cursor:    "",
		MaxEvents: 500,
	})
	if err != nil {
		t.Fatalf("ReadEvents error: %v", err)
	}
	if len(result.Events) == 0 {
		t.Fatal("first poll: expected events, got 0")
	}
	if result.NextCursor == "" {
		t.Fatal("first poll: expected non-empty NextCursor")
	}
	// Verify event fields.
	for _, ev := range result.Events {
		if ev.SessionID != "controlled_pty:test" {
			t.Errorf("event session mismatch: got %q", ev.SessionID)
		}
		if ev.ID == "" {
			t.Error("event has empty ID")
		}
		if ev.Type == agent.EventUnknown {
			t.Logf("event type is unknown (may be expected for session_meta): %+v", ev)
		}
	}
	t.Logf("first poll: %d events, cursor=%q", len(result.Events), result.NextCursor)
}

func TestBridge_Codex_SecondPollAppendsEvents(t *testing.T) {
	adapter := &codex.Adapter{}

	// First poll: session_meta + task_started.
	records1 := []contract.RawRecord{
		{Bytes: testCodexSessionMeta(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
		{Bytes: testCodexTaskStarted(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
	}
	result1, err := adapter.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "controlled_pty:test"},
		Records:   records1,
		Cursor:    "",
		MaxEvents: 500,
	})
	if err != nil {
		t.Fatalf("first poll error: %v", err)
	}
	cursor1 := result1.NextCursor

	// Second poll: full prefix (same 2 + 1 new).
	records2 := []contract.RawRecord{
		{Bytes: testCodexSessionMeta(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
		{Bytes: testCodexTaskStarted(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
		{Bytes: testCodexAssistantMessage(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
	}
	result2, err := adapter.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "controlled_pty:test"},
		Records:   records2,
		Cursor:    cursor1,
		MaxEvents: 500,
	})
	if err != nil {
		t.Fatalf("second poll error: %v", err)
	}

	// Second poll must return only NEW events (not duplicates).
	if len(result2.Events) == 0 {
		t.Error("second poll: expected new events (assistant message), got 0")
	}
	// Verify no duplicate IDs.
	seenIDs := make(map[string]bool)
	for _, ev := range result1.Events {
		seenIDs[ev.ID] = true
	}
	for _, ev := range result2.Events {
		if seenIDs[ev.ID] {
			t.Errorf("second poll: duplicate event ID %q", ev.ID)
		}
	}
	t.Logf("second poll: %d new events, cursor=%q", len(result2.Events), result2.NextCursor)
}

func TestBridge_Codex_ThirdPollWithMoreEvents(t *testing.T) {
	adapter := &codex.Adapter{}

	// Poll 1.
	base := []contract.RawRecord{
		{Bytes: testCodexSessionMeta(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
		{Bytes: testCodexTaskStarted(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
	}
	r1, _ := adapter.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "controlled_pty:test"},
		Records:   base, Cursor: "", MaxEvents: 500,
	})

	// Poll 2: add assistant.
	base = append(base, contract.RawRecord{Bytes: testCodexAssistantMessage(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog})
	r2, _ := adapter.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "controlled_pty:test"},
		Records:   base, Cursor: r1.NextCursor, MaxEvents: 500,
	})

	// Poll 3: add another task_started.
	base = append(base, contract.RawRecord{Bytes: testCodexTaskStarted(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog})
	r3, err := adapter.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "controlled_pty:test"},
		Records:   base, Cursor: r2.NextCursor, MaxEvents: 500,
	})
	if err != nil {
		t.Fatalf("third poll error: %v", err)
	}
	if len(r3.Events) == 0 {
		t.Error("third poll: expected new events, got 0")
	}

	// Total events across all polls must be deterministic.
	total := len(r1.Events) + len(r2.Events) + len(r3.Events)
	t.Logf("three polls: %d + %d + %d = %d total events", len(r1.Events), len(r2.Events), len(r3.Events), total)

	// One-shot with all records must produce the same total events.
	allAtOnce, err := adapter.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "controlled_pty:test"},
		Records:   base, Cursor: "", MaxEvents: 500,
	})
	if err != nil {
		t.Fatalf("one-shot error: %v", err)
	}
	if len(allAtOnce.Events) != total {
		t.Errorf("one-shot/paged mismatch: one-shot=%d, paged=%d", len(allAtOnce.Events), total)
	}
}

func TestBridge_Codex_ZeroEventsAcrossPolls(t *testing.T) {
	adapter := &codex.Adapter{}

	base := []contract.RawRecord{
		{Bytes: testCodexSessionMeta(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
		{Bytes: testCodexTaskStarted(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
	}
	r1, _ := adapter.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "controlled_pty:test"},
		Records:   base, Cursor: "", MaxEvents: 500,
	})

	// Same records, same cursor → zero new events.
	r2, err := adapter.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "controlled_pty:test"},
		Records:   base, Cursor: r1.NextCursor, MaxEvents: 500,
	})
	if err != nil {
		t.Fatalf("second poll error: %v", err)
	}
	if len(r2.Events) != 0 {
		t.Errorf("re-poll with same records: expected 0 events, got %d", len(r2.Events))
	}
	// Cursor must still be valid (not empty) after zero-event result.
	if r2.NextCursor == "" {
		t.Error("zero-event poll: NextCursor must not be empty")
	}
}

// ── Claude bridge tests ──

func TestBridge_Claude_FirstPollAuthorityPlusEvents(t *testing.T) {
	adapter := &claude.Adapter{}
	records := []contract.RawRecord{
		{Bytes: testClaudeVersionRecord(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
		{Bytes: testClaudeAssistantRecord(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
	}

	result, err := adapter.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "controlled_pty:test"},
		Records:   records,
		Cursor:    "",
		MaxEvents: 500,
	})
	if err != nil {
		t.Fatalf("ReadEvents error: %v", err)
	}
	if len(result.Events) == 0 {
		t.Fatal("first poll: expected events, got 0")
	}
	if result.NextCursor == "" {
		t.Fatal("first poll: expected non-empty NextCursor")
	}
	t.Logf("claude first poll: %d events, cursor=%q", len(result.Events), result.NextCursor)
}

func TestBridge_Claude_SecondPollAppendsEvents(t *testing.T) {
	adapter := &claude.Adapter{}

	base := []contract.RawRecord{
		{Bytes: testClaudeVersionRecord(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
	}
	r1, _ := adapter.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "controlled_pty:test"},
		Records:   base, Cursor: "", MaxEvents: 500,
	})

	base = append(base, contract.RawRecord{Bytes: testClaudeAssistantRecord(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog})
	r2, err := adapter.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "controlled_pty:test"},
		Records:   base, Cursor: r1.NextCursor, MaxEvents: 500,
	})
	if err != nil {
		t.Fatalf("second poll error: %v", err)
	}
	if len(r2.Events) == 0 {
		t.Error("second poll: expected new events, got 0")
	}

	// Check no duplicate IDs.
	seen := make(map[string]bool)
	for _, ev := range r1.Events {
		seen[ev.ID] = true
	}
	for _, ev := range r2.Events {
		if seen[ev.ID] {
			t.Errorf("duplicate event ID: %q", ev.ID)
		}
	}
}

func TestBridge_Claude_ThreePollOneShotMatch(t *testing.T) {
	adapter := &claude.Adapter{}

	makeAssistant := func(n int) []byte {
		b, _ := json.Marshal(map[string]any{
			"version": "2.1.202",
			"type":    "assistant",
			"message": map[string]any{
				"role":    "assistant",
				"content": []map[string]any{{"type": "text", "text": "msg " + string(rune('0'+n))}},
			},
		})
		return b
	}

	base := []contract.RawRecord{
		{Bytes: testClaudeVersionRecord(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
	}
	r1, _ := adapter.ReadEvents(context.Background(), contract.ReadInput{Session: contract.SessionContext{SessionID: "s"}, Records: base, Cursor: "", MaxEvents: 500})

	base = append(base, contract.RawRecord{Bytes: makeAssistant(1), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog})
	r2, _ := adapter.ReadEvents(context.Background(), contract.ReadInput{Session: contract.SessionContext{SessionID: "s"}, Records: base, Cursor: r1.NextCursor, MaxEvents: 500})

	base = append(base, contract.RawRecord{Bytes: makeAssistant(2), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog})
	r3, _ := adapter.ReadEvents(context.Background(), contract.ReadInput{Session: contract.SessionContext{SessionID: "s"}, Records: base, Cursor: r2.NextCursor, MaxEvents: 500})

	paged := len(r1.Events) + len(r2.Events) + len(r3.Events)
	allAtOnce, _ := adapter.ReadEvents(context.Background(), contract.ReadInput{Session: contract.SessionContext{SessionID: "s"}, Records: base, Cursor: "", MaxEvents: 500})

	if len(allAtOnce.Events) != paged {
		t.Errorf("Claude one-shot/paged mismatch: one-shot=%d, paged=%d", len(allAtOnce.Events), paged)
	}
}

// ── Version authority tests ──

func TestBridge_Codex_UnsupportedVersionRevokesAuthority(t *testing.T) {
	adapter := &codex.Adapter{}
	records := []contract.RawRecord{
		{Bytes: testCodexSessionMetaWrongVersion(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
		{Bytes: testCodexTaskStarted(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
	}

	result, err := adapter.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "s"},
		Records:   records,
		Cursor:    "",
		MaxEvents: 500,
	})
	if err != nil {
		t.Fatalf("ReadEvents error: %v", err)
	}
	// All events must be EventUnknown (version not confirmed).
	for _, ev := range result.Events {
		if ev.Type != agent.EventUnknown {
			t.Errorf("unsupported version: event type=%q, want EventUnknown", ev.Type)
		}
	}
	if !result.Degraded.Degraded {
		t.Error("unsupported version: expected degraded result")
	}
}

func TestBridge_Claude_UnsupportedVersionRevokesAuthority(t *testing.T) {
	adapter := &claude.Adapter{}
	records := []contract.RawRecord{
		{Bytes: testClaudeWrongVersion(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
	}

	result, err := adapter.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "s"},
		Records:   records,
		Cursor:    "",
		MaxEvents: 500,
	})
	if err != nil {
		t.Fatalf("ReadEvents error: %v", err)
	}
	for _, ev := range result.Events {
		if ev.Type != agent.EventUnknown {
			t.Errorf("unsupported Claude version: event type=%q, want EventUnknown", ev.Type)
		}
	}
	if !result.Degraded.Degraded {
		t.Error("unsupported Claude version: expected degraded result")
	}
}

func TestBridge_Codex_MissingVersionAuthority(t *testing.T) {
	adapter := &codex.Adapter{}
	// No session_meta at all — version cannot be confirmed.
	records := []contract.RawRecord{
		{Bytes: testCodexTaskStarted(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
	}

	result, err := adapter.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "s"},
		Records:   records,
		Cursor:    "",
		MaxEvents: 500,
	})
	if err != nil {
		t.Fatalf("ReadEvents error: %v", err)
	}
	for _, ev := range result.Events {
		if ev.Type != agent.EventUnknown {
			t.Errorf("missing version: event type=%q, want EventUnknown", ev.Type)
		}
	}
}

// ── Cursor and error handling tests ──

func TestBridge_MalformedCursorFailsClosed(t *testing.T) {
	adapter := &codex.Adapter{}
	records := []contract.RawRecord{
		{Bytes: testCodexSessionMeta(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
	}

	// Oversized cursor (> 4096 bytes).
	bigCursor := make([]byte, 5000)
	for i := range bigCursor {
		bigCursor[i] = 'x'
	}

	result, err := adapter.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "s"},
		Records:   records,
		Cursor:    contract.Cursor(string(bigCursor)),
		MaxEvents: 500,
	})
	if err != nil {
		t.Fatalf("ReadEvents should not panic: %v", err)
	}
	if !result.Degraded.Degraded {
		t.Error("malformed cursor: expected degraded result")
	}
}

func TestBridge_Codex_CursorAnchorMismatch(t *testing.T) {
	adapter := &codex.Adapter{}
	records := []contract.RawRecord{
		{Bytes: testCodexSessionMeta(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
		{Bytes: testCodexTaskStarted(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
	}

	// Tampered cursor with wrong anchor.
	tamperedCursor := contract.Cursor("1:badbadbadbadbadbadbadbadbadbadbadbad")

	result, err := adapter.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "s"},
		Records:   records,
		Cursor:    tamperedCursor,
		MaxEvents: 500,
	})
	if err != nil {
		t.Fatalf("ReadEvents error: %v", err)
	}
	// Must fail closed — no events from tampered cursor.
	if len(result.Events) > 0 {
		t.Errorf("tampered cursor: expected 0 events, got %d", len(result.Events))
	}
	if !result.Degraded.Degraded {
		t.Error("tampered cursor: expected degraded result")
	}
}

func TestBridge_Codex_StreamConflictSessionMetaAtNonZero(t *testing.T) {
	adapter := &codex.Adapter{}
	// Two session_meta records — the second at position 1 is a stream conflict.
	records := []contract.RawRecord{
		{Bytes: testCodexSessionMeta(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
		{Bytes: testCodexSessionMeta(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
		{Bytes: testCodexTaskStarted(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
	}

	result, err := adapter.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "s"},
		Records:   records,
		Cursor:    "",
		MaxEvents: 500,
	})
	if err != nil {
		t.Fatalf("ReadEvents error: %v", err)
	}
	// Stream conflict must be degraded and not process records after conflict.
	if !result.Degraded.Degraded {
		t.Error("stream conflict: expected degraded result")
	}
}

// ── Session/wrong-session tests ──

func TestBridge_SessionBindingPreserved(t *testing.T) {
	adapter := &codex.Adapter{}
	records := []contract.RawRecord{
		{Bytes: testCodexSessionMeta(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
		{Bytes: testCodexTaskStarted(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
	}

	result, err := adapter.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "controlled_pty:my-session"},
		Records:   records,
		Cursor:    "",
		MaxEvents: 500,
	})
	if err != nil {
		t.Fatalf("ReadEvents error: %v", err)
	}
	// All events must be bound to the provided session ID.
	for _, ev := range result.Events {
		if ev.SessionID != "controlled_pty:my-session" {
			t.Errorf("session binding: event has SessionID=%q, want %q", ev.SessionID, "controlled_pty:my-session")
		}
	}
}

// ── Long-stream / overflow tests ──

func TestBridge_Codex_LongStream2001Plus(t *testing.T) {
	adapter := &codex.Adapter{}

	// Build 2001 records: session_meta + 2000 task_started records.
	records := make([]contract.RawRecord, 2001)
	records[0] = contract.RawRecord{Bytes: testCodexSessionMeta(), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog}
	for i := 1; i < 2001; i++ {
		// Use user messages to avoid event limit issues (session_meta + task produce EventUnknown).
		records[i] = contract.RawRecord{
			Bytes:      []byte(`{"timestamp":"2026-07-06T13:29:36.000Z","type":"user","payload":{"message":{"role":"user","content":[{"type":"text","text":"msg ` + itoa(i) + `"}]}}}`),
			Source:     agent.SourceJSONL,
			Provenance: contract.ProvenanceNativeLog,
		}
	}

	// This must not panic and must produce bounded results.
	result, err := adapter.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "s"},
		Records:   records,
		Cursor:    "",
		MaxEvents: 500,
	})
	if err != nil {
		t.Fatalf("long stream error: %v", err)
	}
	if len(result.Events) > 500 {
		t.Errorf("event limit exceeded: %d events", len(result.Events))
	}
	// The result cursor must be valid for continuation.
	if result.NextCursor == "" {
		t.Error("long stream: NextCursor is empty")
	}
	t.Logf("long stream: %d events (first page), cursor=%q", len(result.Events), result.NextCursor)

	// Continue with second page.
	result2, err := adapter.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "s"},
		Records:   records,
		Cursor:    result.NextCursor,
		MaxEvents: 500,
	})
	if err != nil {
		t.Fatalf("long stream page 2 error: %v", err)
	}
	if len(result2.Events) == 0 {
		t.Error("long stream page 2: expected events, got 0")
	}
	t.Logf("long stream page 2: %d events", len(result2.Events))

	// Total events across both pages must not exceed the event limit per read.
	total := len(result.Events) + len(result2.Events)
	t.Logf("long stream total across 2 pages: %d events", total)
}
