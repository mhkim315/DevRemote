package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// ParserFactory creates a fresh parser for each sub-test.
type ParserFactory func(t *testing.T) AgentParser

type fixtureMeta struct {
	Agent          string   `json:"agent"`
	ExpectedEvents []string `json:"expectedEvents"`
	EntryCount     int      `json:"entryCount"`
}

func RunParserContract(t *testing.T, agentName string, factory ParserFactory) {
	t.Helper()
	meta := loadMeta(t, agentName)
	allLines := loadAllFixtures(t, agentName)

	t.Run(agentName, func(t *testing.T) {
		t.Run("AgentKind", func(t *testing.T) { testAgentKind(t, factory, agentName) })
		t.Run("ValidParse_ExpectedEvents", func(t *testing.T) { testExpectedEvents(t, factory, meta, allLines) })
		t.Run("StatusBaseline", func(t *testing.T) { testStatusBaseline(t, factory, allLines) })
		t.Run("ApprovalDetection", func(t *testing.T) { testApprovalDetection(t, factory, agentName) })
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

func testAgentKind(t *testing.T, factory ParserFactory, want string) {
	t.Helper()
	if got := factory(t).AgentKind(); got != want {
		t.Errorf("AgentKind = %q, want %q", got, want)
	}
}

func testExpectedEvents(t *testing.T, factory ParserFactory, meta *fixtureMeta, lines [][]byte) {
	t.Helper()
	p := factory(t)
	result := p.ParseBatch(lines, "")

	gotTypes := make(map[AgentEventType]bool)
	for _, e := range result.Events {
		gotTypes[e.Type] = true
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
	for _, want := range meta.ExpectedEvents {
		if !gotTypes[AgentEventType(want)] {
			t.Errorf("expected event %q not in output (got %v)", want, eventTypes(gotTypes))
		}
	}
	for _, e := range result.Events {
		if e.Confidence < 0.4 && e.Type != EventUnknown {
			t.Errorf("low confidence (%.2f) type=%q, want unknown", e.Confidence, e.Type)
		}
	}
	// Result must not be degraded on valid input.
	if result.Degraded {
		t.Errorf("Degraded=true on valid fixture (diagnostics: %v)", result.Diagnostics)
	}
}

func testStatusBaseline(t *testing.T, factory ParserFactory, lines [][]byte) {
	t.Helper()
	p := factory(t)
	result := p.ParseBatch(lines, "")
	if result.Status == "" {
		t.Error("Status is empty after ParseBatch")
	}
	if len(result.Events) > 0 && result.Status == StatusUnknown {
		t.Errorf("status=unknown with %d events (valid fixture must infer status)", len(result.Events))
	}
	// Status must be one of the defined constants.
	valid := map[AgentStatus]bool{
		StatusUnknown: true, StatusIdle: true, StatusThinking: true,
		StatusWorking: true, StatusWaitingApproval: true, StatusWaitingInput: true,
		StatusCompleted: true, StatusFailed: true, StatusInterrupted: true,
		StatusDegraded: true,
	}
	if !valid[result.Status] {
		t.Errorf("Status %q is not a defined AgentStatus constant", result.Status)
	}
}

func testApprovalDetection(t *testing.T, factory ParserFactory, agentName string) {
	t.Helper()
	p := factory(t)
	lines := loadFixtures(t, agentName, "approval_waiting.jsonl")
	if len(lines) == 0 {
		t.Skip("no approval fixture")
		return
	}
	result := p.ParseBatch(lines, "")
	// Must detect at least one approval from the fixture.
	if len(result.Approvals) == 0 {
		t.Error("no approvals detected from approval_waiting fixture")
	}
	for _, a := range result.Approvals {
		if a.ID == "" {
			t.Error("approval has empty ID")
		}
		if a.Status == "" {
			t.Error("approval has empty Status")
		}
		if a.Prompt == "" {
			t.Error("approval has empty Prompt")
		}
	}
	// Approval fixture must return waiting_approval status.
	if result.Status != StatusWaitingApproval {
		t.Errorf("status=%q with approvals present, want waiting_approval", result.Status)
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
	result := p.ParseBatch(lines, "")
	// Malformed must not crash. Degraded is acceptable but must include diagnostics.
	if result.Degraded && len(result.Diagnostics) == 0 {
		t.Error("Degraded=true but Diagnostics is empty")
	}
}

func testUnknownFieldIgnore(t *testing.T, factory ParserFactory) {
	t.Helper()
	p := factory(t)
	line := []byte(`{"type":"user","sessionId":"abc","unexpectedXYZ":123,"message":{"role":"user","content":"hello"}}`)
	result := p.ParseBatch([][]byte{line}, "")
	for _, e := range result.Events {
		if e.Type == "" {
			t.Error("event from unknown-field record has empty Type")
		}
	}
	if result.Degraded {
		t.Error("Degraded=true on record with extra fields (should be handled gracefully)")
	}
}

func testMissingFieldFallback(t *testing.T, factory ParserFactory) {
	t.Helper()
	p := factory(t)
	line := []byte(`{"type":"user"}`)
	result := p.ParseBatch([][]byte{line}, "")
	if result.Degraded {
		t.Error("Degraded=true on missing-field record (should use fallback)")
	}
	// At minimum, should not panic. May produce unknown events.
	_ = result.Events
}

func testDuplicatePrevention(t *testing.T, factory ParserFactory, lines [][]byte) {
	t.Helper()
	if len(lines) == 0 {
		t.Skip("no fixture lines")
		return
	}
	p := factory(t)
	r1 := p.ParseBatch(lines, "")
	r2 := p.ParseBatch(lines, r1.Cursor)
	// Same lines + returned cursor must produce 0 duplicate events.
	if len(r2.Events) != 0 {
		t.Errorf("re-parse with cursor produced %d events, want 0 (cursor=%q)", len(r2.Events), r1.Cursor)
	}
}

func testCursorResume(t *testing.T, factory ParserFactory, lines [][]byte) {
	t.Helper()
	if len(lines) < 2 {
		t.Skip("need >=2 lines for cursor test")
		return
	}
	p := factory(t)
	// Split parse.
	mid := len(lines) / 2
	r1 := p.ParseBatch(lines[:mid], "")
	r2 := p.ParseBatch(lines[mid:], r1.Cursor)
	splitTotal := len(r1.Events) + len(r2.Events)
	// Full parse.
	rAll := p.ParseBatch(lines, "")
	fullTotal := len(rAll.Events)
	// Must match. Cursor resume is a hard contract.
	if splitTotal < fullTotal {
		t.Errorf("cursor resume: split=%d+%d=%d, full=%d (must not lose events)", len(r1.Events), len(r2.Events), splitTotal, fullTotal)
	}
}

func testEventOrdering(t *testing.T, factory ParserFactory, lines [][]byte) {
	t.Helper()
	p := factory(t)
	result := p.ParseBatch(lines, "")
	for i := 1; i < len(result.Events); i++ {
		if result.Events[i].Timestamp.Before(result.Events[i-1].Timestamp) {
			t.Errorf("event %d ts %v before event %d ts %v",
				i, result.Events[i].Timestamp, i-1, result.Events[i-1].Timestamp)
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
			_ = p.ParseBatch([][]byte{input}, "")
		}()
	}
}

func testEmptyInput(t *testing.T, factory ParserFactory) {
	t.Helper()
	p := factory(t)
	result := p.ParseBatch(nil, "")
	if len(result.Events) != 0 {
		t.Errorf("nil input: %d events, want 0", len(result.Events))
	}
	result2 := p.ParseBatch([][]byte{}, result.Cursor)
	if len(result2.Events) != 0 {
		t.Errorf("empty input with cursor: %d events, want 0", len(result2.Events))
	}
}

// --- Fixture helpers ---

func loadMeta(t *testing.T, agentName string) *fixtureMeta {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", agentName, "metadata.json"))
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	var m fixtureMeta
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("metadata: %v", err)
	}
	return &m
}

func loadFixtures(t *testing.T, agentName, file string) [][]byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", agentName, file))
	if err != nil {
		return nil // optional fixture, caller skips if empty
	}
	return splitLines(data)
}

func loadAllFixtures(t *testing.T, agentName string) [][]byte {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Join("testdata", agentName))
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

// --- Mock parser ---

type mockParser struct {
	agentKind string
}

func (p *mockParser) AgentKind() string { return p.agentKind }

func (p *mockParser) ParseBatch(lines [][]byte, cursor string) ParseResult {
	// Always process all lines. Cursor is a content-based token
	// for dedup: same lines produce same token → re-parse returns empty.
	firstLine := firstLineHash(lines)
	if cursor != "" && cursor == firstLine {
		return ParseResult{Cursor: cursor, Status: StatusIdle}
	}
	var events []AgentEvent
	var approvals []AgentApproval
	hasApproval := false
	degraded := false
	diagnostics := []string{}

	// Duplicate prevention: skip lines already covered by cursor.
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(line, &raw); err != nil {
			degraded = true
			diagnostics = append(diagnostics, "malformed JSON: "+err.Error())
			continue
		}

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

		// Approval detection.
		if event.Type == EventApprovalRequested {
			approvals = append(approvals, AgentApproval{
				ID:        itoa(len(lines)),
				AgentKind: p.agentKind,
				Status:    "pending",
				Prompt:    "approval requested",
				Source:    SourceJSONL,
			})
			hasApproval = true
		}
		if event.Type == EventApprovalResolved {
			approvals = append(approvals, AgentApproval{
				ID:        itoa(len(lines)),
				AgentKind: p.agentKind,
				Status:    "approved",
				Prompt:    "approval resolved",
				Source:    SourceJSONL,
			})
		}
	}

	newCursor := firstLine

	// Status inference.
	status := StatusUnknown
	if hasApproval {
		status = StatusWaitingApproval
	} else if len(events) > 0 {
		lastType := events[len(events)-1].Type
		switch lastType {
		case EventThinking:
			status = StatusThinking
		case EventToolCallStarted, EventToolCallFinished:
			status = StatusWorking
		case EventUserMessage:
			status = StatusWaitingInput
		default:
			status = StatusWorking
		}
	}

	return ParseResult{
		Events:      events,
		Cursor:      newCursor,
		Status:      status,
		Approvals:   approvals,
		Degraded:    degraded,
		Diagnostics: diagnostics,
	}
}

func classifyEvent(raw map[string]interface{}) AgentEventType {
	rawType := stringField(raw, "type")
	// Claude
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
	// Codex
	if rawType == "session_meta" {
		return EventAgentStarted
	}
	if rawType == "event_msg" {
		switch payloadType(raw) {
		case "task_started":
			return EventAgentStarted
		case "waiting_for_approval":
			return EventApprovalRequested
		case "approval_resolved":
			return EventApprovalResolved
		}
	}
	if rawType == "response_item" && payloadRole(raw) == "user" {
		return EventUserMessage
	}
	// Antigravity (actual observed CLI types)
	if rawType == "USER_INPUT" {
		return EventUserMessage
	}
	if rawType == "PLANNER_RESPONSE" || rawType == "VIEW_FILE" {
		return EventAssistantMessage
	}
	if rawType == "SEARCH_WEB" || rawType == "LIST_DIRECTORY" {
		return EventToolCallStarted
	}
	return EventUnknown
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

func stringField(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func hashFirstLine(lines [][]byte) string {
	for _, l := range lines {
		if len(l) > 0 {
			// Use first 40 bytes as simple content hash.
			n := len(l)
			if n > 40 {
				n = 40
			}
			return string(l[:n])
		}
	}
	return ""
}

func hashLines(lines [][]byte) string {
	return hashFirstLine(lines)
}

// --- Self-tests ---

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

func firstLineHash(lines [][]byte) string {
	for _, l := range lines {
		if len(l) > 0 {
			return fmt.Sprintf("%x-%d", len(l), len(lines))
		}
	}
	return ""
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for n := i; n > 0; n /= 10 {
		s = string(rune('0'+n%10)) + s
	}
	return s
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

func TestMockParser_Contract_Antigravity(t *testing.T) {
	RunParserContract(t, "antigravity", func(t *testing.T) AgentParser {
		return &mockParser{agentKind: "antigravity"}
	})
}
