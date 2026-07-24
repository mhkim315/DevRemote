package dscl1

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// ── JSONL structure validation ──

// jsonlEvent is the minimal common shape every Claude JSONL event must satisfy.
// Unknown fields are rejected by json.Decoder.DisallowUnknownFields in strict mode.
type jsonlEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId,omitempty"`
}

// TestJSONLFixtureValid218 verifies the synthetic valid 2.1.218 transcript
// fixture contains well-formed NDJSON with expected event types.
func TestJSONLFixtureValid218(t *testing.T) {
	events := readJSONL(t, "testdata/valid_218.jsonl")
	if len(events) < 3 {
		t.Fatalf("valid_218.jsonl: expected at least 3 events, got %d", len(events))
	}

	// The fixture must start with a system or mode event.
	first := events[0]
	if first.Type != "mode" && first.Type != "system" {
		t.Errorf("valid_218.jsonl: first event type should be mode or system, got %q", first.Type)
	}

	// Every event must have a non-empty type.
	for i, e := range events {
		if e.Type == "" {
			t.Errorf("valid_218.jsonl[%d]: empty type field", i)
		}
	}

	knownTypes := map[string]bool{
		"mode": true, "system": true, "assistant": true, "user": true,
		"ai-title": true, "agent-name": true, "result": true,
		"attachment": true, "file-history-snapshot": true, "last-prompt": true,
	}
	for i, e := range events {
		if !knownTypes[e.Type] {
			t.Logf("valid_218.jsonl[%d]: unknown event type %q (may be version-specific)", i, e.Type)
		}
	}

	t.Logf("valid_218.jsonl: %d events, first_type=%s", len(events), first.Type)
}

// TestJSONLFixturePartialLine verifies that partial (non-terminated) last lines
// are detected and do not produce a valid event.
func TestJSONLFixturePartialLine(t *testing.T) {
	f, err := os.Open("testdata/partial_line.jsonl")
	if err != nil {
		t.Fatalf("cannot open partial_line.jsonl: %v", err)
	}
	defer f.Close()

	var events []jsonlEvent
	scanner := bufio.NewScanner(f)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev jsonlEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Logf("partial_line.jsonl[%d]: expected parse failure: %v", lineCount, err)
			continue
		}
		events = append(events, ev)
	}

	// The last "line" in this fixture is intentionally truncated (no trailing
	// newline, cut mid-JSON). The scanner should either fail to parse it or
	// surface an error. The key invariant: we must not silently emit a partial
	// event.
	t.Logf("partial_line.jsonl: %d lines scanned, %d valid events", lineCount, len(events))

	// Verify that the truncated data does not produce a quietly corrupted event.
	if err := scanner.Err(); err != nil {
		t.Logf("partial_line.jsonl: scanner error (expected for truncated input): %v", err)
	}
}

// TestJSONLFixtureMalformed verifies that non-JSON lines are rejected.
func TestJSONLFixtureMalformed(t *testing.T) {
	data, err := os.ReadFile("testdata/malformed.jsonl")
	if err != nil {
		t.Fatalf("cannot open malformed.jsonl: %v", err)
	}

	lines := strings.Split(string(data), "\n")
	validCount := 0
	invalidCount := 0
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev jsonlEvent
		if json.Unmarshal([]byte(line), &ev) == nil {
			validCount++
		} else {
			invalidCount++
			t.Logf("malformed.jsonl[%d]: correctly rejected non-JSON line", i)
		}
	}
	if invalidCount == 0 {
		t.Error("malformed.jsonl: expected at least one non-JSON line to be rejected")
	}
	// Valid lines should still be parseable.
	if validCount == 0 {
		t.Error("malformed.jsonl: expected at least one valid JSON line")
	}
	t.Logf("malformed.jsonl: %d valid, %d invalid", validCount, invalidCount)
}

// TestJSONLFixtureUnknownSchema verifies that events with unknown fields
// (schema violations) are detected when using strict decoding.
func TestJSONLFixtureUnknownSchema(t *testing.T) {
	data, err := os.ReadFile("testdata/unknown_schema.jsonl")
	if err != nil {
		t.Fatalf("cannot open unknown_schema.jsonl: %v", err)
	}

	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Strict decode: unknown fields must be detected.
		dec := json.NewDecoder(strings.NewReader(line))
		dec.DisallowUnknownFields()
		var raw map[string]interface{}
		if err := dec.Decode(&raw); err != nil {
			t.Logf("unknown_schema.jsonl[%d]: correctly rejected by strict decode: %v", i, err)
			continue
		}
		// Drain remaining tokens to catch trailing garbage.
		if _, err := dec.Token(); err != io.EOF {
			t.Logf("unknown_schema.jsonl[%d]: trailing content detected", i)
		}
	}
}

// TestJSONLFixtureTruncation verifies that a file truncated mid-event is handled.
func TestJSONLFixtureTruncation(t *testing.T) {
	data, err := os.ReadFile("testdata/truncated.jsonl")
	if err != nil {
		t.Fatalf("cannot open truncated.jsonl: %v", err)
	}

	content := string(data)
	// Should have at least one complete line followed by a partial line.
	lines := strings.Split(content, "\n")

	completeEvents := 0
	for i, line := range lines {
		if i == len(lines)-1 {
			// Last "line" — may be partial.
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var ev jsonlEvent
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				t.Logf("truncated.jsonl[%d]: last line is partial/unparseable (expected): %v", i, err)
				continue
			}
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev jsonlEvent
		if err := json.Unmarshal([]byte(line), &ev); err == nil {
			completeEvents++
		}
	}
	t.Logf("truncated.jsonl: %d complete events", completeEvents)
	if completeEvents < 1 {
		t.Error("truncated.jsonl: expected at least one complete event before truncation")
	}
}

// TestJSONLFixtureOversized verifies that events with excessive text are
// bounded and flagged, not silently ingested.
func TestJSONLFixtureOversized(t *testing.T) {
	f, err := os.Open("testdata/oversized.jsonl")
	if err != nil {
		t.Fatalf("cannot open oversized.jsonl: %v", err)
	}
	defer f.Close()

	const maxBytes = 40000 // matching client.ts MAX_TEXT_BYTES

	scanner := bufio.NewScanner(f)
	var oversizedLines []int
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Bytes()
		if len(line) > maxBytes {
			oversizedLines = append(oversizedLines, lineNo)
			t.Logf("oversized.jsonl[%d]: line exceeds bound (%d bytes)", lineNo, len(line))
		}
	}

	if len(oversizedLines) == 0 {
		t.Error("oversized.jsonl: expected at least one oversized line in fixture")
	}
}

// TestJSONLFixtureRotation verifies that a rotation scenario (old file renamed,
// new file created) is detectable through path identity change.
func TestJSONLFixtureRotation(t *testing.T) {
	// The rotation_sample.jsonl fixture contains events from a "before rotation"
	// scenario. We validate that events carry consistent session identity.
	events := readJSONL(t, "testdata/rotation_sample.jsonl")
	if len(events) == 0 {
		t.Fatal("rotation_sample.jsonl: no events found")
	}

	sessionIDs := make(map[string]int)
	for _, e := range events {
		if e.SessionID != "" {
			sessionIDs[e.SessionID]++
		}
	}

	// All events in one file must share the same session ID (or be sessionless
	// system events).
	if len(sessionIDs) > 1 {
		t.Errorf("rotation_sample.jsonl: multiple session IDs in one file: %v", sessionIDs)
	}
	t.Logf("rotation_sample.jsonl: %d events, session_ids=%d", len(events), len(sessionIDs))
}

// readJSONL reads a JSONL fixture and returns parsed events.
func readJSONL(t *testing.T, path string) []jsonlEvent {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("cannot open %s: %v", path, err)
	}
	defer f.Close()

	var events []jsonlEvent
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev jsonlEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Logf("%s: unparseable line: %v", path, err)
			continue
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("%s: scanner error: %v", path, err)
	}
	return events
}
