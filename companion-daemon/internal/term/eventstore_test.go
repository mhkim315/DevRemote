package term

import (
	"sync"
	"testing"

	"devremote/companion-daemon/internal/models"
)

func TestEventStore_EmitAndList(t *testing.T) {
	t.Parallel()
	s := NewMemoryEventStore()
	s.Emit("s1", "file_edit", "Write", "/tmp/x")
	evs := s.List("s1")
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1", len(evs))
	}
	if evs[0].Type != "file_edit" {
		t.Errorf("type = %s, want file_edit", evs[0].Type)
	}
}

func TestEventStore_AppendAndList(t *testing.T) {
	t.Parallel()
	s := NewMemoryEventStore()
	s.Append("s1", []models.AgentEvent{
		{Type: "message"}, {Type: "tool_use"},
	})
	evs := s.List("s1")
	if len(evs) != 2 {
		t.Fatalf("got %d events, want 2", len(evs))
	}
}

func TestEventStore_Clear(t *testing.T) {
	t.Parallel()
	s := NewMemoryEventStore()
	s.Emit("s1", "x", "y", "z")
	s.Clear("s1")
	if len(s.List("s1")) != 0 {
		t.Error("events not cleared")
	}
}

func TestEventStore_ListReturnsCopy(t *testing.T) {
	t.Parallel()
	s := NewMemoryEventStore()
	s.Emit("s1", "x", "y", "z")
	evs1 := s.List("s1")
	evs1[0].Type = "mutated"
	evs2 := s.List("s1")
	if evs2[0].Type == "mutated" {
		t.Error("List does not return a copy — internal state mutated")
	}
}

func TestEventStore_InstanceIsolation(t *testing.T) {
	t.Parallel()
	s1 := NewMemoryEventStore()
	s2 := NewMemoryEventStore()
	s1.Emit("s", "x", "y", "z")
	if len(s2.List("s")) != 0 {
		t.Error("events leaked between instances")
	}
}

func TestEventStore_CapEnforced(t *testing.T) {
	t.Parallel()
	s := NewMemoryEventStore()
	// Insert 600 events via Append, should be capped at 500.
	var batch []models.AgentEvent
	for i := 0; i < 600; i++ {
		batch = append(batch, models.AgentEvent{Type: "msg"})
	}
	s.Append("s", batch)
	evs := s.List("s")
	if len(evs) > maxEventsPerSession {
		t.Errorf("got %d events, want ≤ %d", len(evs), maxEventsPerSession)
	}
}

func TestEventStore_ConcurrentAccess(t *testing.T) {
	s := NewMemoryEventStore()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Emit("s", "x", "y", "z")
			s.List("s")
		}()
	}
	wg.Wait()
}
