package term

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"devremote/companion-daemon/internal/models"
)

// mockParser just returns an event if line is valid JSON
type mockParser struct{}

func (p *mockParser) Parse(record json.RawMessage) ([]models.AgentEvent, error) {
	var m map[string]interface{}
	if err := json.Unmarshal(record, &m); err != nil {
		return nil, err
	}
	return []models.AgentEvent{{Detail: "mock event"}}, nil
}

func TestReadNewEvents_PartialRead(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.jsonl")

	// Write a complete line and a partial line
	f, _ := os.Create(logPath)
	f.WriteString(`{"valid": true}` + "\n")
	f.WriteString(`{"partial": `) // no newline
	f.Close()

	cursor := &LogCursor{Path: logPath}
	parser := &mockParser{}

	events, err := ReadNewEvents(cursor, parser, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	expectedOffset := int64(len(`{"valid": true}` + "\n"))
	if cursor.Offset != expectedOffset {
		t.Fatalf("expected offset %d, got %d", expectedOffset, cursor.Offset)
	}

	// Now append the rest of the partial line
	f, _ = os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString(`true}` + "\n")
	f.Close()

	events2, err := ReadNewEvents(cursor, parser, 100)
	if err != nil {
		t.Fatalf("unexpected error on second read: %v", err)
	}
	if len(events2) != 1 {
		t.Fatalf("expected 1 event on second read, got %d", len(events2))
	}
}

func TestReadNewEvents_OversizedRecord(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.jsonl")

	// Write a 6MB string without newline
	f, _ := os.Create(logPath)
	f.WriteString(`{"huge": "`)
	// 6 MB of data
	chunk := make([]byte, 1024*1024)
	for i := 0; i < 1024*1024; i++ {
		chunk[i] = 'A'
	}
	for i := 0; i < 6; i++ {
		f.Write(chunk)
	}
	f.WriteString(`"}` + "\n")
	f.WriteString(`{"valid": true}` + "\n")
	f.Close()

	cursor := &LogCursor{Path: logPath}
	parser := &mockParser{}

	events, err := ReadNewEvents(cursor, parser, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event (the valid one after oversized), got %d", len(events))
	}

	// The offset should be advanced past both lines
	if cursor.Offset == 0 {
		t.Fatalf("cursor offset did not advance")
	}
	if cursor.Discarding {
		t.Fatalf("cursor is still in Discarding state")
	}
}
