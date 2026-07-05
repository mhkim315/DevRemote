package term

import (
	"context"
	"regexp"
	"time"
)

var uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// LogRef holds the resolved path and agent metadata for parsing
type LogRef struct {
	Path    string
	Agent   string // "codex", "claude", "gemini"
	Session string
}

// ProcessInfo holds runtime information about the active agent process
type ProcessInfo struct {
	PID       int
	CWD       string
	Command   string
	StartedAt time.Time
	PaneID    string
}

// AgentLogResolver maps a system process to its actual chat log file
type AgentLogResolver interface {
	Resolve(ctx context.Context, p ProcessInfo) (LogRef, error)
}
