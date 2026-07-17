package term

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestActivityBuffer_SeqIncrements(t *testing.T) {
	b := NewActivityBuffer(10)
	// Two adjacent terminal_output events merge into one block.
	b.Append(ActivityEvent{SessionID: "s1", Type: ActivityTerminalOutput, Text: "a"})
	b.Append(ActivityEvent{SessionID: "s1", Type: ActivityTerminalOutput, Text: "b"})
	// terminal_input breaks the merge.
	b.Append(ActivityEvent{SessionID: "s1", Type: ActivityTerminalInput, Text: "", Bytes: 1})
	// Another output starts a new block.
	b.Append(ActivityEvent{SessionID: "s1", Type: ActivityTerminalOutput, Text: "c"})

	list := b.List("s1")
	// "a"+"b" merged, then input, then "c" = 3 events
	if len(list) != 3 {
		t.Fatalf("len=%d, want 3 (output merged)", len(list))
	}
	if list[0].Seq != 1 || list[0].Text != "ab" {
		t.Errorf("merged output seq=%d text=%q, want seq=1 text=ab", list[0].Seq, list[0].Text)
	}
	if list[1].Seq != 2 || list[1].Text != "" {
		t.Errorf("input seq=%d text=%q", list[1].Seq, list[1].Text)
	}
	if list[2].Seq != 3 || list[2].Text != "c" {
		t.Errorf("output seq=%d text=%q, want c", list[2].Seq, list[2].Text)
	}
}

func TestActivityBuffer_CapacityDrop(t *testing.T) {
	b := NewActivityBuffer(3)
	// Merge: all adjacent output events combine into one.
	for i := 0; i < 5; i++ {
		b.Append(ActivityEvent{SessionID: "s1", Type: ActivityTerminalOutput, Text: "x"})
	}
	list := b.List("s1")
	// All merged into one block.
	if len(list) != 1 {
		t.Errorf("len=%d, want 1 (all merged)", len(list))
	}
	if list[0].Text != "xxxxx" {
		t.Errorf("merged text=%q, want xxxxx", list[0].Text)
	}
}

func TestActivityBuffer_SessionIsolation(t *testing.T) {
	b := NewActivityBuffer(10)
	b.Append(ActivityEvent{SessionID: "s1", Type: ActivityTerminalOutput, Text: "a"})
	b.Append(ActivityEvent{SessionID: "s2", Type: ActivityTerminalOutput, Text: "b"})

	if len(b.List("s1")) != 1 || b.List("s1")[0].Text != "a" {
		t.Error("s1 isolation broken")
	}
	if len(b.List("s2")) != 1 || b.List("s2")[0].Text != "b" {
		t.Error("s2 isolation broken")
	}
}

func TestActivityBuffer_ListOrder(t *testing.T) {
	b := NewActivityBuffer(10)
	b.Append(ActivityEvent{SessionID: "s1", Type: ActivityTerminalOutput, Text: "first"})
	// Break merge with input event.
	b.Append(ActivityEvent{SessionID: "s1", Type: ActivityTerminalInput, Text: "", Bytes: 1})
	b.Append(ActivityEvent{SessionID: "s1", Type: ActivityTerminalOutput, Text: "second"})

	list := b.List("s1")
	if len(list) != 3 {
		t.Fatalf("len=%d, want 3", len(list))
	}
	if list[0].Text != "first" || list[2].Text != "second" {
		t.Errorf("order: got %q / %q, want first / second", list[0].Text, list[2].Text)
	}
}

func TestActivityBuffer_Clear(t *testing.T) {
	b := NewActivityBuffer(10)
	b.Append(ActivityEvent{SessionID: "s1", Type: ActivityTerminalOutput, Text: "a"})
	b.Clear("s1")
	if len(b.List("s1")) != 0 {
		t.Error("Clear did not remove events")
	}
	b.Append(ActivityEvent{SessionID: "s1", Type: ActivityTerminalOutput, Text: "b"})
	if b.List("s1")[0].Seq != 1 {
		t.Error("Clear did not reset seq counter")
	}
}

func TestActivityEndpoint_Empty(t *testing.T) {
	h := &Handlers{Activity: NewActivityBuffer(10)}
	req := httptest.NewRequest("GET", "/api/sessions?activity=nonexistent", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsV2(rec, req)

	if rec.Code != 200 {
		t.Errorf("status=%d, want 200", rec.Code)
	}
	var events []ActivityEvent
	json.Unmarshal(rec.Body.Bytes(), &events)
	if len(events) != 0 {
		t.Errorf("got %d events, want 0", len(events))
	}
}

func TestActivityEndpoint_WithData(t *testing.T) {
	b := NewActivityBuffer(10)
	b.Append(ActivityEvent{SessionID: "s1", Type: ActivityTerminalOutput, Text: "hello", Bytes: 5})
	b.Append(ActivityEvent{SessionID: "s1", Type: ActivityTerminalInput, Text: "", Bytes: 2})

	h := &Handlers{Activity: b}
	req := httptest.NewRequest("GET", "/api/sessions?activity=s1", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsV2(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	var events []ActivityEvent
	json.Unmarshal(rec.Body.Bytes(), &events)
	if len(events) != 2 {
		t.Fatalf("len=%d, want 2", len(events))
	}
	if events[0].Type != ActivityTerminalOutput || events[0].Seq != 1 {
		t.Errorf("first event: type=%s seq=%d", events[0].Type, events[0].Seq)
	}
	if events[1].Type != ActivityTerminalInput || events[1].Seq != 2 {
		t.Errorf("second event: type=%s seq=%d", events[1].Type, events[1].Seq)
	}
}

func TestActivityEndpoint_NilBuffer(t *testing.T) {
	h := &Handlers{Activity: nil}
	req := httptest.NewRequest("GET", "/api/sessions?activity=s1", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsV2(rec, req)

	if rec.Code != 200 {
		t.Errorf("status=%d, want 200", rec.Code)
	}
	var events []ActivityEvent
	json.Unmarshal(rec.Body.Bytes(), &events)
	if len(events) != 0 {
		t.Errorf("nil buffer: got %d events, want 0", len(events))
	}
}

func TestActivityInput_NoRawText(t *testing.T) {
	b := NewActivityBuffer(10)
	b.Append(ActivityEvent{SessionID: "s1", Type: ActivityTerminalInput, Text: "", Bytes: 5})
	b.Append(ActivityEvent{SessionID: "s1", Type: ActivityTerminalOutput, Text: "output", Bytes: 6})

	list := b.List("s1")
	if list[0].Text != "" {
		t.Errorf("terminal_input Text=%q, want empty (no raw input)", list[0].Text)
	}
	if list[1].Text != "output" {
		t.Errorf("terminal_output Text=%q, want 'output'", list[1].Text)
	}
}
