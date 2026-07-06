package term

import (
	"encoding/json"
	"fmt"

	"devremote/companion-daemon/internal/models"
)

type GeminiParser struct {
	Session string
}

type geminiToolCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type geminiRecord struct {
	StepIndex int              `json:"step_index"`
	Source    string           `json:"source"`
	Type      string           `json:"type"`
	Content   string           `json:"content"`
	Thinking  string           `json:"thinking"`
	CreatedAt string           `json:"created_at"`
	ToolCalls []geminiToolCall `json:"tool_calls"`
}

func (p *GeminiParser) Parse(record json.RawMessage) ([]models.AgentEvent, error) {
	var payload geminiRecord
	if err := json.Unmarshal(record, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse gemini jsonl: %w", err)
	}

	var events []models.AgentEvent
	ts := payload.CreatedAt
	step := fmt.Sprintf("%d", payload.StepIndex)

	if payload.Type == "EPHEMERAL_MESSAGE" {
		return nil, nil // Skip ephemerals
	}

	if payload.Source == "USER_EXPLICIT" && payload.Type == "USER_INPUT" {
		events = append(events, models.AgentEvent{
			ID:        fmt.Sprintf("gemini-usr-%s", step),
			Session:   p.Session,
			Agent:     "gemini",
			Type:      "user",
			Summary:   "User",
			Detail:    payload.Content,
			Timestamp: ts,
		})
	} else if payload.Source == "MODEL" && payload.Type == "PLANNER_RESPONSE" {
		if payload.Thinking != "" {
			events = append(events, models.AgentEvent{
				ID:        fmt.Sprintf("gemini-thk-%s", step),
				Session:   p.Session,
				Agent:     "gemini",
				Type:      "message",
				Summary:   "Gemini (Thinking)",
				Detail:    payload.Thinking,
				Timestamp: ts,
			})
		}
		if payload.Content != "" {
			events = append(events, models.AgentEvent{
				ID:        fmt.Sprintf("gemini-msg-%s", step),
				Session:   p.Session,
				Agent:     "gemini",
				Type:      "message",
				Summary:   "Gemini",
				Detail:    payload.Content,
				Timestamp: ts,
			})
		}
		for i, call := range payload.ToolCalls {
			b, _ := json.MarshalIndent(call.Args, "", "  ")
			events = append(events, models.AgentEvent{
				ID:         fmt.Sprintf("gemini-tool-%s-%d", step, i),
				Session:    p.Session,
				Agent:      "gemini",
				Type:       "tool_use",
				Summary:    fmt.Sprintf("Tool: %s", call.Name),
				Detail:     string(b),
				Timestamp:  ts,
				ToolCallID: fmt.Sprintf("gemini-call-%s-%d", step, i),
			})
		}
	} else if payload.Type != "" {
		// Tool results or other events (RUN_COMMAND, VIEW_FILE, etc.)
		if payload.Content != "" {
			events = append(events, models.AgentEvent{
				ID:        fmt.Sprintf("gemini-res-%s", step),
				Session:   p.Session,
				Agent:     "gemini",
				Type:      "tool_result",
				Summary:   fmt.Sprintf("Result: %s", payload.Type),
				Detail:    payload.Content,
				Timestamp: ts,
				// ToolCallID omitted for Gemini tool results since they are not strongly linked in JSONL
			})
		}
	}

	return events, nil
}
