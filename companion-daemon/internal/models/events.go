package models

// AgentEvent represents a single parsed event from an AI agent JSONL log.
type AgentEvent struct {
	ID         string `json:"id"`
	Session    string `json:"session"`
	Agent      string `json:"agent"`      // "codex", "claude", "gemini"
	Type       string `json:"type"`       // "message", "tool_use", "tool_result", "user"
	Timestamp  string `json:"timestamp"`  // Original timestamp from the log
	ToolCallID string `json:"toolCallId"` // Used to link tool_use and tool_result blocks
	Summary    string `json:"summary"`
	Detail     string `json:"detail"`
}
