package watcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExtractToolUse(t *testing.T) {
	line := `{"parentUuid":"0f727faa-239b-46ad-b49c-57df383bdde0","isSidechain":false,"message":{"id":"a75a7616-6418-4e96-9cc3-4b048e7aa78f","type":"message","role":"assistant","model":"deepseek-v4-pro","content":[{"type":"tool_use","id":"call_00_yI3acvVFgi5kb3TSU3Vw9902","name":"Bash","input":{"command":"git clone https://github.com/mhkim315/DevRemote .","description":"Clone DevRemote repository into current directory","timeout":120000}}],"stop_reason":"tool_use","stop_sequence":null},"type":"assistant","uuid":"29c1faa7-d355-469e-a7db-7cd434687aae","timestamp":"2026-06-29T21:23:50.796Z"}`

	var ev RawEvent
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if ev.Type != "assistant" {
		t.Fatalf("expected type 'assistant', got '%s'", ev.Type)
	}

	tu := ExtractToolUse(ev)
	if tu == nil {
		t.Fatal("expected tool_use, got nil")
	}
	if tu.Name != "Bash" {
		t.Fatalf("expected 'Bash', got '%s'", tu.Name)
	}
	if tu.ID != "call_00_yI3acvVFgi5kb3TSU3Vw9902" {
		t.Fatalf("wrong ID: %s", tu.ID)
	}
}

func TestTailFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	received := make(chan RawEvent, 10)
	tailer, err := New(dir, func(ev RawEvent) {
		received <- ev
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tailer.Close()

	if err := tailer.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Write file after starting so fsnotify catches the CREATE.
	if err := os.WriteFile(path, []byte(`{"type":"test-init","sessionId":"s1","timestamp":"2026-01-01T00:00:00Z"}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-received:
		if ev.Type != "test-init" {
			t.Fatalf("expected 'test-init', got '%s'", ev.Type)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for initial event")
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"type":"test-append","sessionId":"s1","timestamp":"2026-01-01T00:00:01Z"}` + "\n")
	f.Close()

	select {
	case ev := <-received:
		if ev.Type != "test-append" {
			t.Fatalf("expected 'test-append', got '%s'", ev.Type)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for appended event")
	}
}
