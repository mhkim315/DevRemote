package parsers

import (
	"encoding/json"
	"strings"
)

type ClaudeParser struct{}

func NewClaudeParser() *ClaudeParser {
	return &ClaudeParser{}
}

func (p *ClaudeParser) ParseLine(line string) (eventType, summary, detail string, err error) {
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		// Not a JSON line, ignore safely
		return "", "", "", nil
	}

	typ, _ := payload["type"].(string)

	if typ == "tool_use" {
		name, _ := payload["name"].(string)
		eventType := "approval_request"
		nameLower := strings.ToLower(name)
		if strings.Contains(nameLower, "edit") || strings.Contains(nameLower, "write") || strings.Contains(nameLower, "replace") {
			eventType = "file_edit"
		}

		var detail string
		if input, ok := payload["input"]; ok {
			if b, err := json.MarshalIndent(input, "", "  "); err == nil {
				detail = string(b)
			}
		}

		return eventType, "Claude Tool: " + name, detail, nil

	} else if typ == "assistant" {
		msgMap, ok := payload["message"].(map[string]interface{})
		if !ok {
			return "", "", "", nil
		}
		contentArr, ok := msgMap["content"].([]interface{})
		if !ok {
			return "", "", "", nil
		}
		for _, item := range contentArr {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if itemMap["type"] == "text" {
				text, _ := itemMap["text"].(string)
				return "message", "Claude Message", text, nil
			}
		}
	}

	return "", "", "", nil
}
