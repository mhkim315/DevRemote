package term

import (
	"sync"
	"time"
)

type AgentEvent struct {
	ID        string `json:"id"`
	Session   string `json:"session"`
	Type      string `json:"type"` // "file_edit", "approval_request", "error", "done"
	Summary   string `json:"summary"`
	Detail    string `json:"detail"`
	Timestamp string `json:"timestamp"`
}

var (
	eventsCache = make(map[string][]AgentEvent)
	eventsMu    sync.Mutex
)

// EmitEvent appends a new event for the given session.
func EmitEvent(session, eventType, summary, detail string) {
	eventsMu.Lock()
	defer eventsMu.Unlock()
	ev := AgentEvent{
		ID:        time.Now().Format("20060102150405.000000"),
		Session:   session,
		Type:      eventType,
		Summary:   summary,
		Detail:    detail,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	eventsCache[session] = append(eventsCache[session], ev)

	if len(eventsCache[session]) > 100 {
		eventsCache[session] = eventsCache[session][len(eventsCache[session])-100:]
	}
}

// GetEvents returns a copy of the events for a session.
func GetEvents(session string) []AgentEvent {
	eventsMu.Lock()
	defer eventsMu.Unlock()
	
	if evs, ok := eventsCache[session]; ok {
		copied := make([]AgentEvent, len(evs))
		copy(copied, evs)
		return copied
	}
	return []AgentEvent{}
}
