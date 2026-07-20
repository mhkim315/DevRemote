package term

import (
	"context"
	"regexp"

	"devremote/companion-daemon/internal/models"
)

var uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// LogRef holds the resolved path and agent metadata for parsing
type LogRef struct {
	Path    string
	Agent   string // "codex", "claude", "gemini", "antigravity"
	Session string
}

// ProcessInfo moved to models package

// AgentLogResolver maps a system process to its actual chat log file
type AgentLogResolver interface {
	Resolve(ctx context.Context, p models.ProcessInfo) (LogRef, error)
}

// AntigravityResolver resolves the log path for an explicit Gemini-Antigravity UUID
type AntigravityResolver struct{}
