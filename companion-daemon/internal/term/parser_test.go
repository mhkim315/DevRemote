package term

import (
	"io"
	"os"
	"testing"
)

func TestCodexParser(t *testing.T) {
	_, err := os.ReadFile("testdata/codex.jsonl")
	if err != nil {
		t.Fatalf("Failed to read codex.jsonl: %v", err)
	}

	parser := &CodexParser{Session: "test-session"}
	cursor := &LogCursor{Path: "testdata/codex.jsonl", Offset: 0}

	events, err := ReadNewEvents(cursor, parser, 500)
	if err != nil && err != io.EOF {
		t.Fatalf("Failed to read new events: %v", err)
	}

	if len(events) != 4 {
		t.Fatalf("Expected 4 events, got %d", len(events))
	}

	if events[0].Type != "user_message" || events[0].Detail != "Hello Codex" {
		t.Errorf("Unexpected user event: %+v", events[0])
	}
	if events[1].Type != "assistant_message" || events[1].Detail != "Hello User!" {
		t.Errorf("Unexpected message event: %+v", events[1])
	}
	if events[2].Type != "tool_call_started" || events[2].ToolCallID != "call-123" {
		t.Errorf("Unexpected tool_use event: %+v", events[2])
	}
	if events[3].Type != "tool_call_finished" || events[3].ToolCallID != "call-123" {
		t.Errorf("Unexpected tool_result event: %+v", events[3])
	}
}

func TestClaudeParser(t *testing.T) {
	parser := &ClaudeParser{Session: "test-session"}
	cursor := &LogCursor{Path: "testdata/claude.jsonl", Offset: 0}

	events, err := ReadNewEvents(cursor, parser, 500)
	if err != nil && err != io.EOF {
		t.Fatalf("Failed to read new events: %v", err)
	}

	if len(events) != 4 {
		t.Fatalf("Expected 4 events, got %d", len(events))
	}

	if events[0].Type != "user_message" {
		t.Errorf("Expected user_message, got %v", events[0].Type)
	}
	if events[1].Type != "assistant_message" || events[1].Detail != "Hello User!" {
		t.Errorf("Expected assistant_message, got %v", events[1])
	}
	if events[2].Type != "thinking" || events[2].Detail != "Hmm..." {
		t.Errorf("Expected thinking, got %v", events[2])
	}
	if events[3].Type != "tool_call_started" || events[3].ToolCallID != "call-456" {
		t.Errorf("Expected tool_call_started, got %v", events[3])
	}
}

func TestGeminiParser(t *testing.T) {
	parser := &GeminiParser{Session: "test-session"}
	cursor := &LogCursor{Path: "testdata/gemini.jsonl", Offset: 0}

	events, err := ReadNewEvents(cursor, parser, 500)
	if err != nil && err != io.EOF {
		t.Fatalf("Failed to read new events: %v", err)
	}

	if len(events) != 4 {
		t.Fatalf("Expected 4 events, got %d", len(events))
	}

	if events[0].Type != "user" {
		t.Errorf("Expected user, got %v", events[0].Type)
	}
	if events[1].Type != "message" || events[1].Detail != "Hello User!" {
		t.Errorf("Expected message, got %v", events[1])
	}
	if events[2].Type != "tool_use" {
		t.Errorf("Expected tool_use, got %v", events[2])
	}
	if events[3].Type != "tool_result" {
		t.Errorf("Expected tool_result, got %v", events[3])
	}
}

func TestOversizedRecord(t *testing.T) {
	// Create a temporary file with a normal line, an oversized line, and a normal line
	f, err := os.CreateTemp("", "oversized*.jsonl")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(f.Name())

	f.WriteString("{\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":\"Line 1\"}}\n")

	// Write a 6MB line (maxRecordSize is 5MB)
	f.WriteString("{\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":\"")
	largeData := make([]byte, 6*1024*1024)
	for i := range largeData {
		largeData[i] = 'A'
	}
	f.Write(largeData)
	f.WriteString("\"}}\n")

	f.WriteString("{\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":\"Line 3\"}}\n")
	f.Close()

	parser := &CodexParser{Session: "test-session"}
	cursor := &LogCursor{Path: f.Name(), Offset: 0}

	events, err := ReadNewEvents(cursor, parser, 500)
	if err != nil && err != io.EOF {
		t.Fatalf("ReadNewEvents error: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("Expected 2 events (skipping oversized), got %d", len(events))
	}
	if events[0].Detail != "Line 1" || events[1].Detail != "Line 3" {
		t.Error("Events did not match expected recovery")
	}
}
