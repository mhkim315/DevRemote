package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ParserFactory creates a fresh parser for each sub-test.
type ParserFactory func(t *testing.T) AgentParser

// RunParserContract runs the full parser contract suite.
func RunParserContract(t *testing.T, name string, factory ParserFactory) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		t.Run("ValidRecord", func(t *testing.T) { testValidRecord(t, factory) })
		t.Run("MalformedSkip", func(t *testing.T) { testMalformedSkip(t, factory) })
		t.Run("UnknownFieldIgnore", func(t *testing.T) { testUnknownFieldIgnore(t, factory) })
		t.Run("MissingFieldFallback", func(t *testing.T) { testMissingFieldFallback(t, factory) })
		t.Run("PanicRecover", func(t *testing.T) { testPanicRecover(t, factory) })
		t.Run("EmptyInput", func(t *testing.T) { testEmptyInput(t, factory) })
	})
}

func testValidRecord(t *testing.T, factory ParserFactory) {
	t.Helper()
	p := factory(t)

	fixtures := loadFixtures(t, "claude", "user_assistant.jsonl")
	if len(fixtures) == 0 {
		t.Skip("no fixture available")
	}

	parsed := 0
	for _, line := range fixtures {
		event, err := p.Parse(line)
		if err != nil {
			t.Errorf("Parse error on valid fixture: %v", err)
			continue
		}
		if event == nil {
			continue // parser may skip some records
		}
		parsed++
		// Common model invariants.
		if event.Type == "" {
			t.Error("parsed event has empty Type")
		}
		if event.Source == "" {
			t.Error("parsed event has empty Source")
		}
		if event.AgentKind == "" {
			t.Error("parsed event has empty AgentKind")
		}
	}
	if parsed == 0 {
		t.Error("parser returned 0 events from fixture")
	}
}

func testMalformedSkip(t *testing.T, factory ParserFactory) {
	t.Helper()
	p := factory(t)

	malformed := loadFixtures(t, "claude", "malformed.jsonl")
	for _, line := range malformed {
		event, err := p.Parse(line)
		if err != nil {
			t.Errorf("Parse error on malformed record (should skip, not error): %v", err)
		}
		if event != nil {
			// Parser may return unknown event — that's acceptable.
			// But it must not panic or error.
		}
	}
}

func testUnknownFieldIgnore(t *testing.T, factory ParserFactory) {
	t.Helper()
	p := factory(t)

	// Line with an unexpected field must not break parsing.
	line := []byte(`{"type":"user","sessionId":"abc","unexpectedField":"xyz","message":{"role":"user","content":"hello"}}`)
	event, err := p.Parse(line)
	if err != nil {
		t.Errorf("Parse error on unknown field: %v", err)
	}
	if event == nil {
		return // skipping unknown is acceptable
	}
	if event.Type != EventUnknown {
		// Successful parse with unknown field is fine.
		_ = event
	}
}

func testMissingFieldFallback(t *testing.T, factory ParserFactory) {
	t.Helper()
	p := factory(t)

	// Record missing the 'message' field — parser must not crash.
	line := []byte(`{"type":"user","sessionId":"abc"}`)
	event, err := p.Parse(line)
	if err != nil {
		t.Errorf("Parse error on missing field: %v", err)
	}
	if event == nil {
		return // skipping is acceptable
	}
}

func testPanicRecover(t *testing.T, factory ParserFactory) {
	t.Helper()
	p := factory(t)

	// Parser must not panic on any input. We test with nil, empty, garbage.
	inputs := [][]byte{
		nil,
		{},
		[]byte("not json"),
		[]byte(`{"type":`), // truncated JSON
	}
	for _, input := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("parser panicked on input %q: %v", string(input), r)
				}
			}()
			_, _ = p.Parse(input)
		}()
	}
}

func testEmptyInput(t *testing.T, factory ParserFactory) {
	t.Helper()
	p := factory(t)

	event, err := p.Parse([]byte{})
	if err != nil {
		t.Logf("Parse empty: %v (acceptable)", err)
	}
	if event != nil {
		t.Error("Parse empty returned non-nil event")
	}
}

// loadFixtures reads JSONL fixture lines from testdata/.
func loadFixtures(t *testing.T, agent, file string) [][]byte {
	t.Helper()
	path := filepath.Join("testdata", agent, file)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fixture %s: %v", path, err)
	}
	var lines [][]byte
	for _, line := range parseJSONLLines(data) {
		if len(line) > 0 {
			lines = append(lines, line)
		}
	}
	return lines
}

func parseJSONLLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			trimmed := trimSpace(data[start:i])
			if len(trimmed) > 0 {
				lines = append(lines, trimmed)
			}
			start = i + 1
		}
	}
	if start < len(data) {
		trimmed := trimSpace(data[start:])
		if len(trimmed) > 0 {
			lines = append(lines, trimmed)
		}
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

// --- Mock parser for self-testing the harness ---

type mockParser struct {
	agentKind string
}

func (p *mockParser) Parse(line []byte) (*AgentEvent, error) {
	if len(line) == 0 {
		return nil, nil
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil, nil // malformed → skip
	}
	eventType := stringField(raw, "type")
	event := &AgentEvent{
		AgentKind: p.agentKind,
		Source:    SourceJSONL,
	}

	switch eventType {
	case "user":
		event.Type = EventUserMessage
	case "assistant":
		if msg, ok := raw["message"].(map[string]interface{}); ok {
			if content, ok := msg["content"].([]interface{}); ok {
				for _, c := range content {
					if cm, ok := c.(map[string]interface{}); ok {
						switch cm["type"] {
						case "thinking":
							event.Type = EventThinking
						case "tool_use":
							event.Type = EventToolCallStarted
						case "text":
							event.Type = EventAssistantMessage
						}
					}
				}
			}
		}
		if event.Type == "" {
			event.Type = EventAssistantMessage
		}
	case "attachment":
		event.Type = EventUnknown // attachments are informational, not agent activity
	default:
		event.Type = EventUnknown
	}

	if event.Type == EventUnknown {
		event.Confidence = 0.3
	} else {
		event.Confidence = 0.9
	}
	return event, nil
}

func stringField(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func TestMockParser_Contract(t *testing.T) {
	RunParserContract(t, "mock-claude", func(t *testing.T) AgentParser {
		return &mockParser{agentKind: "claude"}
	})
}
