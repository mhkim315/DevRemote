package term

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"devremote/companion-daemon/internal/models"
)

type CodexParser struct {
	Session string
}

func (p *CodexParser) Parse(record json.RawMessage) ([]models.AgentEvent, error) {
	var wrapper map[string]interface{}
	if err := json.Unmarshal(record, &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse codex jsonl: %w", err)
	}

	typ, _ := wrapper["type"].(string)
	tsRaw, _ := wrapper["timestamp"].(string)

	// Normalize timestamp format for UI
	ts := tsRaw
	if t, err := time.Parse(time.RFC3339Nano, tsRaw); err == nil {
		ts = t.Format(time.RFC3339)
	}

	payload, ok := wrapper["payload"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("missing payload object")
	}

	var events []models.AgentEvent

	if typ == "event_msg" {
		pType, _ := payload["type"].(string)
		if pType == "user_message" {
			msg, _ := payload["message"].(string)
			events = append(events, models.AgentEvent{
				ID:        fmt.Sprintf("codex-usr-%s", tsRaw),
				Session:   p.Session,
				Agent:     "codex",
				Type:      "user",
				Summary:   "User",
				Detail:    msg,
				Timestamp: ts,
			})
		}
	} else if typ == "response_item" {
		role, _ := payload["role"].(string)
		if role == "assistant" {
			contentArr, ok := payload["content"].([]interface{})
			if ok {
				for i, item := range contentArr {
					itemMap, ok := item.(map[string]interface{})
					if !ok {
						continue
					}
					itemType, _ := itemMap["type"].(string)

					if itemType == "output_text" {
						text, _ := itemMap["text"].(string)
						events = append(events, models.AgentEvent{
							ID:        fmt.Sprintf("codex-msg-%s-%d", tsRaw, i),
							Session:   p.Session,
							Agent:     "codex",
							Type:      "message",
							Summary:   "Codex",
							Detail:    text,
							Timestamp: ts,
						})
					}
				}
			}
		}
	} else if typ == "tool_use" || typ == "tool_result" {
		// Wait, the schema from reviewer said:
		// tool 호출: payload.type=function_call
		// tool 결과: payload.type=function_call_output
		// reasoning: 별도의 payload.type=reasoning
	}

	// For other codex payloads
	pType, _ := payload["type"].(string)
	if pType == "function_call" {
		name, _ := payload["name"].(string)
		args := payload["arguments"]
		callID, _ := payload["call_id"].(string)

		var b []byte
		if argsStr, ok := args.(string); ok {
			var dummy interface{}
			if err := json.Unmarshal([]byte(argsStr), &dummy); err == nil {
				b, _ = json.MarshalIndent(dummy, "", "  ")
			} else {
				b = []byte(argsStr)
			}
		} else {
			b, _ = json.MarshalIndent(args, "", "  ")
		}

		events = append(events, models.AgentEvent{
			ID:         fmt.Sprintf("codex-%s-%s", tsRaw, callID),
			Session:    p.Session,
			Agent:      "codex",
			Type:       "tool_use",
			Summary:    fmt.Sprintf("Tool: %s", name),
			Detail:     string(b),
			Timestamp:  ts,
			ToolCallID: callID,
		})
	} else if pType == "function_call_output" {
		output, _ := payload["output"].(string)
		callID, _ := payload["call_id"].(string)
		events = append(events, models.AgentEvent{
			ID:         fmt.Sprintf("codex-res-%s-%s", tsRaw, callID),
			Session:    p.Session,
			Agent:      "codex",
			Type:       "tool_result",
			Summary:    "Tool Result",
			Detail:     output,
			Timestamp:  ts,
			ToolCallID: callID,
		})
	} else if pType == "reasoning" {
		summaryArr, ok := payload["summary"].([]interface{})
		if ok && len(summaryArr) > 0 {
			var detailStr strings.Builder
			for _, s := range summaryArr {
				if str, ok := s.(string); ok {
					detailStr.WriteString(str)
					detailStr.WriteString("\n")
				}
			}
			events = append(events, models.AgentEvent{
				ID:        fmt.Sprintf("codex-rsn-%s", tsRaw),
				Session:   p.Session,
				Agent:     "codex",
				Type:      "message",
				Summary:   "Codex (Reasoning)",
				Detail:    strings.TrimSpace(detailStr.String()),
				Timestamp: ts,
			})
		}
	}

	return events, nil
}
