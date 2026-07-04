package parsers

import (
	"encoding/json"
	"strings"

	"devremote/companion-daemon/internal/term"
)

type AntigravityParser struct{}

func NewAntigravityParser() *AntigravityParser {
	return &AntigravityParser{}
}

func (p *AntigravityParser) ParseLine(line string) (*term.AgentEvent, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		// Not a JSON line, ignore safely
		return nil, nil
	}

	typ, _ := payload["type"].(string)
	if typ == "PLANNER_RESPONSE" {
		// Parse tool_calls
		if toolCalls, ok := payload["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
			firstCall, ok := toolCalls[0].(map[string]interface{})
			if ok {
				name, _ := firstCall["name"].(string)
				eventType := "approval_request"
				nameLower := strings.ToLower(name)
				if strings.Contains(nameLower, "edit") || strings.Contains(nameLower, "write") || strings.Contains(nameLower, "replace") {
					eventType = "file_edit"
				}

				var detail string
				if args, ok := firstCall["arguments"]; ok {
					if b, err := json.MarshalIndent(args, "", "  "); err == nil {
						detail = string(b)
					}
				}

				return &term.AgentEvent{
					Type:    eventType,
					Summary: "Antigravity Tool: " + name,
					Detail:  detail,
				}, nil
			}
		}

		// Parse thinking
		if thinking, ok := payload["thinking"].(string); ok && thinking != "" {
			return &term.AgentEvent{
				Type:    "message",
				Summary: "Antigravity Thinking",
				Detail:  thinking,
			}, nil
		}
	}

	return nil, nil
}
