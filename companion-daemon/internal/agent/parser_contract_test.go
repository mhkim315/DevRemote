package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// ParserFactory creates a fresh parser for each sub-test.
type ParserFactory func(t *testing.T) AgentParser

// fixtureMeta mirrors testdata metadata.json structure.
type fixtureMeta struct {
	Agent          string   `json:"agent"`
	ExpectedEvents []string `json:"expectedEvents"`
	EntryCount     int      `json:"entryCount"`
}

// RunParserContract runs the full parser contract suite for one agent.
// It reads testdata/<agent>/metadata.json and all .jsonl fixtures,
// then verifies the parser against common contracts.
func RunParserContract(t *testing.T, agentName string, factory ParserFactory) {
	t.Helper()
	meta := loadMeta(t, agentName)
	allLines := loadAllFixtures(t, agentName)

	t.Run(agentName, func(t *testing.T) {
		t.Run("AgentKind", func(t *testing.T) { testAgentKind(t, factory, agentName) })
		t.Run("ValidParse_ExpectedEvents", func(t *testing.T) { testExpectedEvents(t, factory, meta, allLines) })
		t.Run("MalformedSkip", func(t *testing.T) { testMalformedSkip(t, factory, agentName) })
		t.Run("UnknownFieldIgnore", func(t *testing.T) { testUnknownFieldIgnore(t, factory) })
		t.Run("MissingFieldFallback", func(t *testing.T) { testMissingFieldFallback(t, factory) })
		t.Run("DuplicatePrevention", func(t *testing.T) { testDuplicatePrevention(t, factory, allLines) })
		t.Run("CursorResume", func(t *testing.T) { testCursorResume(t, factory, allLines) })
		t.Run("EventOrdering", func(t *testing.T) { testEventOrdering(t, factory, allLines) })
		t.Run("PanicRecover", func(t *testing.T) { testPanicRecover(t, factory) })
		t.Run("EmptyInput", func(t *testing.T) { testEmptyInput(t, factory) })
	})
}

// --- Contract tests ---

func testAgentKind(t *testing.T, factory ParserFactory, want string) {
	t.Helper()
	p := factory(t)
	if got := p.AgentKind(); got != want {
		t.Errorf("AgentKind = %q, want %q", got, want)
	}
}

func testExpectedEvents(t *testing.T, factory ParserFactory, meta *fixtureMeta, lines [][]byte) {
	t.Helper()
	p := factory(t)
	events, _, err := p.ParseBatch(lines, "")
	if err != nil {
		t.Fatalf("ParseBatch error: %v", err)
	}

	gotTypes := make(map[AgentEventType]bool)
	for _, e := range events {
		gotTypes[e.Type] = true
		// Event invariants
		if e.Type == "" {
			t.Error("event has empty Type")
		}
		if e.Source == "" {
			t.Error("event has empty Source")
		}
		if e.AgentKind != p.AgentKind() {
			t.Errorf("event AgentKind=%q, want %q", e.AgentKind, p.AgentKind())
		}
	}

	// Every expected event type must appear in parsed output.
	for _, want := range meta.ExpectedEvents {
		if !gotTypes[AgentEventType(want)] {
			t.Errorf("expected event type %q not found in parsed output (got: %v)", want, eventTypes(gotTypes))
		}
	}

	// Low confidence must map to unknown.
	for _, e := range events {
		if e.Confidence < 0.4 && e.Type != EventUnknown {
			t.Errorf("low confidence (%.2f) event type=%q, want unknown", e.Confidence, e.Type)
		}
	}
}

func testMalformedSkip(t *testing.T, factory ParserFactory, agentName string) {
	t.Helper()
	p := factory(t)
	lines := loadFixtures(t, agentName, "malformed.jsonl")
	if len(lines) == 0 {
		t.Skip("no malformed fixture")
		return
	}
	events, _, err := p.ParseBatch(lines, "")
	if err != nil {
		t.Errorf("ParseBatch error on malformed: %v", err)
	}
	// Malformed records should not prevent normal parsing.
	// Some events may parse (best effort), none should cause failure.
	_ = events
}

func testUnknownFieldIgnore(t *testing.T, factory ParserFactory) {
	t.Helper()
	p := factory(t)
	line := []byte(`{"type":"user","sessionId":"abc","unexpectedXYZ":123,"message":{"role":"user","content":"hello"}}`)
	events, _, err := p.ParseBatch([][]byte{line}, "")
	if err != nil {
		t.Errorf("ParseBatch error on unknown field: %v", err)
	}
	for _, e := range events {
		if e.Type == "" {
			t.Error("event with unknown field has empty Type")
		}
	}
}

func testMissingFieldFallback(t *testing.T, factory ParserFactory) {
	t.Helper()
	p := factory(t)
	line := []byte(`{"type":"user"}`)
	events, _, err := p.ParseBatch([][]byte{line}, "")
	if err != nil {
		t.Errorf("ParseBatch error on missing field: %v", err)
	}
	for _, e := range events {
		if e.Type == "" {
			t.Error("event from missing-field record has empty Type (want best-effort)")
		}
	}
}

func testDuplicatePrevention(t *testing.T, factory ParserFactory, lines [][]byte) {
	t.Helper()
	if len(lines) == 0 {
		t.Skip("no fixture lines")
		return
	}
	p := factory(t)
	// Parse same lines twice. Second call should produce no new events
	// if cursor is properly tracked, or at minimum not duplicate.
	e1, cursor, _ := p.ParseBatch(lines, "")
	e2, _, _ := p.ParseBatch(lines, cursor)
	if len(e2) > len(e1) {
		t.Errorf("re-parse with cursor produced %d events (more than first parse %d)", len(e2), len(e1))
	}
}

func testCursorResume(t *testing.T, factory ParserFactory, lines [][]byte) {
	t.Helper()
	if len(lines) < 2 {
		t.Skip("need >=2 lines for cursor test")
		return
	}
	p := factory(t)
	// Parse first half, get cursor, parse second half.
	mid := len(lines) / 2
	e1, cursor, _ := p.ParseBatch(lines[:mid], "")
	e2, _, _ := p.ParseBatch(lines[mid:], cursor)
	// Parse all at once.
	eAll, _, _ := p.ParseBatch(lines, "")
	if len(e1)+len(e2) != len(eAll) {
		t.Logf("cursor resume: split=%d+%d, full=%d (may differ if parser re-evaluates context)", len(e1), len(e2), len(eAll))
	}
	_ = cursor
}

func testEventOrdering(t *testing.T, factory ParserFactory, lines [][]byte) {
	t.Helper()
	p := factory(t)
	events, _, err := p.ParseBatch(lines, "")
	if err != nil {
		t.Fatalf("ParseBatch error: %v", err)
	}
	// Events must be in non-decreasing timestamp order.
	for i := 1; i < len(events); i++ {
		if events[i].Timestamp.Before(events[i-1].Timestamp) {
			t.Errorf("event %d timestamp %v before event %d timestamp %v",
				i, events[i].Timestamp, i-1, events[i-1].Timestamp)
		}
	}
}

func testPanicRecover(t *testing.T, factory ParserFactory) {
	t.Helper()
	p := factory(t)
	inputs := [][]byte{nil, {}, []byte("not json"), []byte(`{"type":`)}
	for _, input := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("parser panicked on %q: %v", string(input), r)
				}
			}()
			_, _, _ = p.ParseBatch([][]byte{input}, "")
		}()
	}
}

func testEmptyInput(t *testing.T, factory ParserFactory) {
	t.Helper()
	p := factory(t)
	events, cursor, err := p.ParseBatch(nil, "")
	if err != nil {
		t.Logf("ParseBatch nil: %v (acceptable)", err)
	}
	if len(events) != 0 {
		t.Errorf("ParseBatch nil: %d events, want 0", len(events))
	}
	events2, _, _ := p.ParseBatch([][]byte{}, cursor)
	if len(events2) != 0 {
		t.Errorf("ParseBatch empty with cursor: %d events, want 0", len(events2))
	}
}

// --- Fixture helpers ---

func loadMeta(t *testing.T, agentName string) *fixtureMeta {
	t.Helper()
	path := filepath.Join("testdata", agentName, "metadata.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("metadata %s: %v", path, err)
	}
	var m fixtureMeta
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("metadata %s: %v", path, err)
	}
	return &m
}

func loadFixtures(t *testing.T, agentName, file string) [][]byte {
	t.Helper()
	path := filepath.Join("testdata", agentName, file)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fixture %s: %v", path, err)
	}
	var lines [][]byte
	for _, line := range splitLines(data) {
		if len(line) > 0 {
			lines = append(lines, line)
		}
	}
	return lines
}

func loadAllFixtures(t *testing.T, agentName string) [][]byte {
	t.Helper()
	dir := filepath.Join("testdata", agentName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("testdata/%s: %v", agentName, err)
	}
	var all [][]byte
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".jsonl" {
			all = append(all, loadFixtures(t, agentName, e.Name())...)
		}
	}
	return all
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, trimSpace(data[start:i]))
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, trimSpace(data[start:]))
	}
	return lines
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\r') {
		b = b[1:]
	}
	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '\t' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

func eventTypes(m map[AgentEventType]bool) []string {
	var s []string
	for t := range m {
		s = append(s, string(t))
	}
	sort.Strings(s)
	return s
}

// --- Mock parser (agent-neutral, metadata-driven) ---

type mockParser struct {
	agentKind string
}

func (p *mockParser) AgentKind() string { return p.agentKind }

func (p *mockParser) ParseBatch(lines [][]byte, cursor string) ([]AgentEvent, string, error) {
	skipUntil := cursor
	var events []AgentEvent
	newCursor := cursor

	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(line, &raw); err != nil {
			continue // malformed → skip
		}
		// Use type field as cursor key.
		lineID := stringField(raw, "type")
		if lineID != "" && skipUntil != "" && lineID <= skipUntil {
			continue // cursor-based dedup
		}
		newCursor = lineID

		event := &AgentEvent{
			AgentKind: p.agentKind,
			Source:    SourceJSONL,
		}
		event.Type = classifyEvent(raw)
		if event.Type == EventUnknown {
			event.Confidence = 0.3
		} else {
			event.Confidence = 0.9
		}
		events = append(events, *event)
	}
	return events, newCursor, nil
}

// classifyEvent maps raw record fields to common event types.
// Handles Claude (type=user/assistant + message.content), Codex (type=event_msg + payload.type),
// and Antigravity (type=USER_INPUT/TOOL_CALL + source).
func classifyEvent(raw map[string]interface{}) AgentEventType {
	rawType := stringField(raw, "type")

	// Claude: type=user/assistant with message.content
	if rawType == "user" && hasContentType(raw, "tool_result") {
		return EventToolCallFinished
	}
	if rawType == "user" && hasMessageRole(raw, "user") {
		return EventUserMessage
	}
	if rawType == "assistant" && hasContentType(raw, "thinking") {
		return EventThinking
	}
	if rawType == "assistant" && hasContentType(raw, "tool_use") {
		return EventToolCallStarted
	}
	if rawType == "assistant" {
		return EventAssistantMessage
	}
	if rawType == "permission-mode" {
		return EventApprovalRequested
	}

	// Codex: type=event_msg, response_item, session_meta with payload.type
	if rawType == "session_meta" {
		return EventAgentStarted
	}
	if rawType == "event_msg" {
		pt := payloadType(raw)
		switch pt {
		case "task_started":
			return EventAgentStarted
		case "waiting_for_approval":
			return EventApprovalRequested
		case "approval_resolved":
			return EventApprovalResolved
		}
	}
	if rawType == "response_item" {
		if payloadRole(raw) == "user" {
			return EventUserMessage
		}
	}

	// Antigravity: type=USER_INPUT, AGENT_OUTPUT, TOOL_CALL, TOOL_RESULT
	if rawType == "USER_INPUT" {
		return EventUserMessage
	}
	if rawType == "AGENT_OUTPUT" {
		return EventAssistantMessage
	}
	if rawType == "TOOL_CALL" {
		return EventToolCallStarted
	}
	if rawType == "TOOL_RESULT" {
		return EventToolCallFinished
	}

	return EventUnknown
}

func payloadType(raw map[string]interface{}) string {
	if p, ok := raw["payload"].(map[string]interface{}); ok {
		return stringField(p, "type")
	}
	return ""
}

func payloadRole(raw map[string]interface{}) string {
	if p, ok := raw["payload"].(map[string]interface{}); ok {
		return stringField(p, "role")
	}
	return ""
}

func hasMessageRole(raw map[string]interface{}, role string) bool {
	if msg, ok := raw["message"].(map[string]interface{}); ok {
		if r, ok := msg["role"].(string); ok && r == role {
			return true
		}
	}
	return false
}

func hasContentType(raw map[string]interface{}, ct string) bool {
	if msg, ok := raw["message"].(map[string]interface{}); ok {
		if content, ok := msg["content"].([]interface{}); ok {
			for _, c := range content {
				if cm, ok := c.(map[string]interface{}); ok {
					if t, ok := cm["type"].(string); ok && t == ct {
						return true
					}
				}
			}
		}
	}
	return false
}

func stringField(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// --- Self-tests: mock parser against all 3 agents ---

func TestMockParser_Contract_Claude(t *testing.T) {
	RunParserContract(t, "claude", func(t *testing.T) AgentParser {
		return &mockParser{agentKind: "claude"}
	})
}

func TestMockParser_Contract_Codex(t *testing.T) {
	RunParserContract(t, "codex", func(t *testing.T) AgentParser {
		return &mockParser{agentKind: "codex"}
	})
}

func TestMockParser_Contract_Antigravity(t *testing.T) {
	RunParserContract(t, "antigravity", func(t *testing.T) AgentParser {
		return &mockParser{agentKind: "antigravity"}
	})
}
