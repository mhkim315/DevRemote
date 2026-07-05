package models

import (
	"sync"
	"time"
)

type AgentEvent struct {
	ID         string `json:"id"`
	Session    string `json:"session"`
	Agent      string `json:"agent"`       // "codex", "claude", "gemini"
	Type       string `json:"type"`        // "message", "tool_use", "tool_result", "user"
	Timestamp  string `json:"timestamp"`   // Original timestamp from the log
	ToolCallID string `json:"toolCallId"`  // Used to link tool_use and tool_result blocks
	Summary    string `json:"summary"`
	Detail     string `json:"detail"`
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

// AppendEvents adds multiple parsed events, maintaining a maximum size of 500.
func AppendEvents(session string, evs []AgentEvent) {
	eventsMu.Lock()
	defer eventsMu.Unlock()
	
	eventsCache[session] = append(eventsCache[session], evs...)
	if len(eventsCache[session]) > 500 {
		eventsCache[session] = eventsCache[session][len(eventsCache[session])-500:]
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
