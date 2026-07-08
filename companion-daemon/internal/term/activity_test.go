package term

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestActivityBuffer_SeqIncrements(t *testing.T) {
	b := NewActivityBuffer(10)
	b.Append(ActivityEvent{SessionID: "s1", Type: ActivityTerminalOutput, Text: "a"})
	b.Append(ActivityEvent{SessionID: "s1", Type: ActivityTerminalOutput, Text: "b"})
	b.Append(ActivityEvent{SessionID: "s1", Type: ActivityTerminalInput, Text: "c"})

	list := b.List("s1")
	if len(list) != 3 {
		t.Fatalf("len=%d, want 3", len(list))
	}
	if list[0].Seq != 1 || list[0].Text != "a" {
		t.Errorf("seq0=%d text=%s, want seq=1 text=a", list[0].Seq, list[0].Text)
	}
	if list[1].Seq != 2 || list[1].Text != "b" {
		t.Errorf("seq1=%d text=%s, want seq=2 text=b", list[1].Seq, list[1].Text)
	}
	if list[2].Seq != 3 || list[2].Text != "c" {
		t.Errorf("seq2=%d text=%s, want seq=3 text=c", list[2].Seq, list[2].Text)
	}
}

func TestActivityBuffer_CapacityDrop(t *testing.T) {
	b := NewActivityBuffer(3)
	for i := 0; i < 5; i++ {
		b.Append(ActivityEvent{SessionID: "s1", Type: ActivityTerminalOutput, Text: "x"})
	}
	list := b.List("s1")
	if len(list) != 3 {
		t.Errorf("len=%d, want 3 (capacity capped)", len(list))
	}
	// Oldest dropped: seq should be 3,4,5
	if list[0].Seq != 3 {
		t.Errorf("oldest seq=%d, want 3", list[0].Seq)
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
	b.Append(ActivityEvent{SessionID: "s1", Type: ActivityTerminalOutput, Text: "second"})

	list := b.List("s1")
	if list[0].Text != "first" || list[1].Text != "second" {
		t.Error("List order wrong — want oldest first")
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
	b.Append(ActivityEvent{SessionID: "s1", Type: ActivityTerminalInput, Text: "ls", Bytes: 2})

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
