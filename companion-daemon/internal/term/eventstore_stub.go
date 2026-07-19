package term

import "devremote/companion-daemon/internal/models"

// PA3 Step6: EventStore physically deleted. Stub retained for compilation
// compatibility; all consumers cut over to Transcript in Steps 1-3.

type EventStore interface {
	Emit(session, eventType, summary, detail string)
	Append(session string, events []models.AgentEvent)
	List(session string) []models.AgentEvent
	Clear(session string)
}

type memoryEventStore struct {
	events map[string][]models.AgentEvent
}

func NewMemoryEventStore() EventStore {
	return &memoryEventStore{events: make(map[string][]models.AgentEvent)}
}

func (s *memoryEventStore) Emit(session, eventType, summary, detail string) {}
func (s *memoryEventStore) Append(session string, evs []models.AgentEvent) {}
func (s *memoryEventStore) List(session string) []models.AgentEvent { return nil }
func (s *memoryEventStore) Clear(session string) {}
