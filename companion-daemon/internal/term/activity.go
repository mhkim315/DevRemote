package term

import (
	"sync"
	"time"
)

// ActivityType classifies captured terminal activity.
type ActivityType string

const (
	ActivityTerminalOutput ActivityType = "terminal_output"
	ActivityTerminalInput  ActivityType = "terminal_input"
	ActivitySystem         ActivityType = "system"
	ActivityStatus         ActivityType = "status"
)

// ActivityEvent is a captured terminal activity record.
// Plain text only — no VT100/ANSI emulation, no structured tool/approval events.
type ActivityEvent struct {
	ID        string       `json:"id"`
	SessionID string       `json:"sessionId"`
	Type      ActivityType `json:"type"`
	Text      string       `json:"text"`            // redacted-safe plain text
	Bytes     int          `json:"bytes,omitempty"` // original byte count
	Timestamp time.Time    `json:"timestamp"`
}

// ActivityBuffer stores captured terminal activity in memory.
type ActivityBuffer struct {
	mu       sync.Mutex
	events   map[string][]ActivityEvent // sessionID → events
	capacity int
}

// NewActivityBuffer creates an in-memory activity buffer.
func NewActivityBuffer(capacity int) *ActivityBuffer {
	if capacity <= 0 {
		capacity = 1000
	}
	return &ActivityBuffer{
		events:   make(map[string][]ActivityEvent),
		capacity: capacity,
	}
}

// Append adds an activity event for a session. Oldest events are dropped when over capacity.
func (b *ActivityBuffer) Append(event ActivityEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	sessionID := event.SessionID
	if event.ID == "" {
		event.ID = time.Now().Format("20060102T150405.000000")
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	events := b.events[sessionID]
	events = append(events, event)
	if len(events) > b.capacity {
		events = events[len(events)-b.capacity:]
	}
	b.events[sessionID] = events
}

// List returns activity events for a session, newest first.
func (b *ActivityBuffer) List(sessionID string) []ActivityEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	events := b.events[sessionID]
	if len(events) == 0 {
		return nil
	}

	// Return a copy, newest first.
	result := make([]ActivityEvent, len(events))
	for i, e := range events {
		result[len(events)-1-i] = e
	}
	return result
}

// Clear removes all events for a session.
func (b *ActivityBuffer) Clear(sessionID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.events, sessionID)
}

// TruncateText limits text length for safe storage.
func TruncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}
