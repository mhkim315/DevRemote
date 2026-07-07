package term

import (
	"encoding/json"
	"fmt"
	"strings"

	"devremote/companion-daemon/internal/models"
)

type ClaudeParser struct {
	Session string
}

func (p *ClaudeParser) Parse(record json.RawMessage) ([]models.AgentEvent, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(record, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal claude jsonl: %w", err)
	}

	typ, _ := payload["type"].(string)
	var ts string
	switch v := payload["timestamp"].(type) {
	case float64:
		ts = fmt.Sprintf("%.0f", v)
	case string:
		ts = v
	}

	var events []models.AgentEvent

	if typ == "message" || typ == "assistant" {
		msg, ok := payload["message"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("missing message payload")
		}

		contentArr, ok := msg["content"].([]interface{})
		if !ok {
			return nil, fmt.Errorf("missing content array")
		}

		for _, item := range contentArr {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			itemType, _ := itemMap["type"].(string)

			if itemType == "text" {
				text, _ := itemMap["text"].(string)
				events = append(events, models.AgentEvent{
					Session:   p.Session,
					Agent:     "claude",
					Type:      "message",
					Summary:   "Claude",
					Detail:    text,
					Timestamp: ts,
				})
			} else if itemType == "thinking" {
				thinking, _ := itemMap["thinking"].(string)
				events = append(events, models.AgentEvent{
					Session:   p.Session,
					Agent:     "claude",
					Type:      "message",
					Summary:   "Claude (Thinking)",
					Detail:    thinking,
					Timestamp: ts,
				})
			} else if itemType == "tool_use" {
				toolName, _ := itemMap["name"].(string)
				toolID, _ := itemMap["id"].(string)
				inputData, _ := itemMap["input"]
				b, _ := json.MarshalIndent(inputData, "", "  ")

				events = append(events, models.AgentEvent{
					Session:    p.Session,
					Agent:      "claude",
					Type:       "tool_use",
					Summary:    fmt.Sprintf("Tool: %s", toolName),
					Detail:     string(b),
					Timestamp:  ts,
					ToolCallID: toolID,
				})
			}
		}
	} else if typ == "tool_result" {
		content, _ := payload["content"].(string)
		toolID, _ := payload["tool_use_id"].(string)
		events = append(events, models.AgentEvent{
			Session:    p.Session,
			Agent:      "claude",
			Type:       "tool_result",
			Summary:    "Result",
			Detail:     content,
			Timestamp:  ts,
			ToolCallID: toolID,
		})
	} else if typ == "user" {
		msg, ok := payload["message"].(map[string]interface{})
		if ok {
			switch content := msg["content"].(type) {
			case string:
				events = append(events, models.AgentEvent{
					Session:   p.Session,
					Agent:     "claude",
					Type:      "user",
					Summary:   "User",
					Detail:    content,
					Timestamp: ts,
				})
			case []interface{}:
				var detailStr strings.Builder
				for _, item := range content {
					itemMap, ok := item.(map[string]interface{})
					if ok {
						itemType, _ := itemMap["type"].(string)
						if itemType == "text" {
							if text, ok := itemMap["text"].(string); ok {
								detailStr.WriteString(text)
								detailStr.WriteString("\n")
							}
						} else if itemType == "tool_result" {
							toolID, _ := itemMap["tool_use_id"].(string)
							contentStr, _ := itemMap["content"].(string)
							events = append(events, models.AgentEvent{
								Session:    p.Session,
								Agent:      "claude",
								Type:       "tool_result",
								Summary:    "Result",
								Detail:     contentStr,
								Timestamp:  ts,
								ToolCallID: toolID,
							})
						}
					}
				}
				trimmedText := strings.TrimSpace(detailStr.String())
				if trimmedText != "" {
					events = append(events, models.AgentEvent{
						Session:   p.Session,
						Agent:     "claude",
						Type:      "user",
						Summary:   "User",
						Detail:    trimmedText,
						Timestamp: ts,
					})
				}
			}
		}
	} else if typ == "permission-mode" {
		mode, _ := payload["permissionMode"].(string)
		if mode == "ask" {
			events = append(events, models.AgentEvent{
				Session:   p.Session,
				Agent:     "claude",
				Type:      "approval_requested",
				Summary:   "Approval Required",
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
