package term

import (
	"sync"
	"time"

	"devremote/companion-daemon/internal/models"
)

// EventStore is the interface for agent event storage.
// Tests can inject a fake implementation.
type EventStore interface {
	// Emit appends a single event (capped at 500 same as Append).
	Emit(session, eventType, summary, detail string)
	// Append adds multiple parsed events (capped at 500).
	Append(session string, events []models.AgentEvent)
	// List returns a copy of all events for a session.
	List(session string) []models.AgentEvent
	// Clear removes all cached events for a session.
	Clear(session string)
}

const maxEventsPerSession = 500

// NewMemoryEventStore creates an in-memory EventStore.
func NewMemoryEventStore() EventStore {
	return &memoryEventStore{
		events: make(map[string][]models.AgentEvent),
	}
}

type memoryEventStore struct {
	mu     sync.Mutex
	events map[string][]models.AgentEvent
}

func (s *memoryEventStore) Emit(session, eventType, summary, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ev := models.AgentEvent{
		ID:        time.Now().Format("20060102150405.000000"),
		Session:   session,
		Type:      eventType,
		Summary:   summary,
		Detail:    detail,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	s.events[session] = append(s.events[session], ev)
	if len(s.events[session]) > maxEventsPerSession {
		s.events[session] = s.events[session][len(s.events[session])-maxEventsPerSession:]
	}
}

func (s *memoryEventStore) Append(session string, evs []models.AgentEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events[session] = append(s.events[session], evs...)
	if len(s.events[session]) > maxEventsPerSession {
		s.events[session] = s.events[session][len(s.events[session])-maxEventsPerSession:]
	}
}

func (s *memoryEventStore) List(session string) []models.AgentEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	if evs, ok := s.events[session]; ok {
		copied := make([]models.AgentEvent, len(evs))
		copy(copied, evs)
		return copied
	}
	return []models.AgentEvent{}
}

func (s *memoryEventStore) Clear(session string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.events, session)
}
