package term

import (
	"encoding/json"
	"fmt"

	"devremote/companion-daemon/internal/models"
)

// AntigravityParser parses Antigravity CLI JSONL transcripts into common AgentEvent models.
// It implements AgentLogParser for the production telemetry path.
type AntigravityParser struct {
	Session string
}

type antigravityRecord struct {
	StepIndex int              `json:"step_index"`
	Source    string           `json:"source"`
	Type      string           `json:"type"`
	Content   string           `json:"content"`
	Thinking  string           `json:"thinking"`
	CreatedAt string           `json:"created_at"`
	ToolCalls []antigravityToolCall `json:"tool_calls"`
}

type antigravityToolCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

func (p *AntigravityParser) Parse(record json.RawMessage) ([]models.AgentEvent, error) {
	var payload antigravityRecord
	if err := json.Unmarshal(record, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse antigravity jsonl: %w", err)
	}

	var events []models.AgentEvent
	ts := payload.CreatedAt
	step := fmt.Sprintf("%d", payload.StepIndex)

	if payload.Type == "EPHEMERAL_MESSAGE" {
		return nil, nil // Skip ephemerals
	}

	if payload.Source == "USER_EXPLICIT" && payload.Type == "USER_INPUT" {
		events = append(events, models.AgentEvent{
			ID:        fmt.Sprintf("antigravity-usr-%s", step),
			Session:   p.Session,
			Agent:     "antigravity",
			Type:      "user",
			Summary:   "User",
			Detail:    payload.Content,
			Timestamp: ts,
		})
	} else if payload.Source == "MODEL" && payload.Type == "PLANNER_RESPONSE" {
		if payload.Thinking != "" {
			events = append(events, models.AgentEvent{
				ID:        fmt.Sprintf("antigravity-thk-%s", step),
				Session:   p.Session,
				Agent:     "antigravity",
				Type:      "message",
				Summary:   "Antigravity (Thinking)",
				Detail:    payload.Thinking,
				Timestamp: ts,
			})
		}
		if payload.Content != "" {
			events = append(events, models.AgentEvent{
				ID:        fmt.Sprintf("antigravity-msg-%s", step),
				Session:   p.Session,
				Agent:     "antigravity",
				Type:      "message",
				Summary:   "Antigravity",
				Detail:    payload.Content,
				Timestamp: ts,
			})
		}
		for i, call := range payload.ToolCalls {
			b, _ := json.MarshalIndent(call.Args, "", "  ")
			events = append(events, models.AgentEvent{
				ID:         fmt.Sprintf("antigravity-tool-%s-%d", step, i),
				Session:    p.Session,
				Agent:      "antigravity",
				Type:       "tool_use",
				Summary:    fmt.Sprintf("Tool: %s", call.Name),
				Detail:     string(b),
				Timestamp:  ts,
				ToolCallID: fmt.Sprintf("antigravity-call-%s-%d", step, i),
			})
		}
	} else if payload.Source == "MODEL" && (payload.Type == "VIEW_FILE" || payload.Type == "SEARCH_WEB" || payload.Type == "LIST_DIRECTORY") {
		// Model-initiated tool actions: produce tool_use events with tool name.
		events = append(events, models.AgentEvent{
			ID:         fmt.Sprintf("antigravity-tool-%s", step),
			Session:    p.Session,
			Agent:      "antigravity",
			Type:       "tool_use",
			Summary:    fmt.Sprintf("Tool: %s", payload.Type),
			Detail:     payload.Content,
			Timestamp:  ts,
			ToolCallID: fmt.Sprintf("antigravity-call-%s", step),
		})
	} else if payload.Type != "" {
		// CHECKPOINT, ERROR_MESSAGE, GENERIC, UNKNOWN_TYPE, or any other type.
		if payload.Content != "" {
			events = append(events, models.AgentEvent{
				ID:        fmt.Sprintf("antigravity-res-%s", step),
				Session:   p.Session,
				Agent:     "antigravity",
				Type:      "tool_result",
				Summary:   fmt.Sprintf("Result: %s", payload.Type),
				Detail:    payload.Content,
				Timestamp: ts,
			})
		}
	}

	// Normalize to common AgentEvent types before returning.
	for i := range events {
		normalizeEventType(&events[i])
	}

	return events, nil
}
