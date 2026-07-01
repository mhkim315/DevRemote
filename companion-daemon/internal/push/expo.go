package push

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// Message sent to Expo Push API.
type Message struct {
	To    string `json:"to"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Data  Data   `json:"data,omitempty"`
}

type Data struct {
	SessionID string `json:"sessionId,omitempty"`
	ToolName  string `json:"toolName,omitempty"`
}

// Send sends a push notification via Expo Push API to all registered tokens.
func Send(tokens []string, title, body, sessionID, toolName string) {
	if len(tokens) == 0 {
		return
	}

	for _, tok := range tokens {
		go sendOne(tok, title, body, sessionID, toolName)
	}
}

func sendOne(token, title, body, sessionID, toolName string) {
	msg := Message{
		To:    token,
		Title: title,
		Body:  body,
		Data: Data{
			SessionID: sessionID,
			ToolName:  toolName,
		},
	}

	b, err := json.Marshal([]Message{msg})
	if err != nil {
		log.Printf("push: marshal: %v", err)
		return
	}

	resp, err := http.Post(
		"https://exp.host/--/api/v2/push/send",
		"application/json",
		bytes.NewReader(b),
	)
	if err != nil {
		log.Printf("push: http: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("push: status %d for %s", resp.StatusCode, token)
		return
	}
	log.Printf("push: sent to %s (title=%q)", token, title)
}

// DescriptionFor creates a human-readable push body from tool_use info.
func DescriptionFor(toolName, description string) string {
	switch toolName {
	case "AskUserQuestion":
		return fmt.Sprintf("질문: %s", description)
	case "Bash":
		return fmt.Sprintf("명령어 실행: %s", description)
	default:
		return fmt.Sprintf("%s: %s", toolName, description)
	}
}
