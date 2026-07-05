package term

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"devremote/companion-daemon/internal/models"
)

var uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// LogRef holds the resolved path and agent metadata for parsing
type LogRef struct {
	Path    string
	Agent   string // "codex", "claude", "gemini"
	Session string
}

// ProcessInfo moved to models package

// AgentLogResolver maps a system process to its actual chat log file
type AgentLogResolver interface {
	Resolve(ctx context.Context, p models.ProcessInfo) (LogRef, error)
}

// LinkedLogResolver maps an explicit link to its actual chat log file
type LinkedLogResolver interface {
	ResolveLink(ctx context.Context, externalSessionID string) (LogRef, error)
}

// AntigravityResolver resolves the log path for an explicit Gemini-Antigravity UUID
type AntigravityResolver struct{}

func (r *AntigravityResolver) ResolveLink(ctx context.Context, externalSessionID string) (LogRef, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return LogRef{}, err
	}
	// Antigravity saves JSONL transcripts under: ~/.gemini/antigravity/brain/<uuid>/.system_generated/logs/transcript.jsonl
	// OR sometimes directly under brain/<uuid>/transcript.jsonl depending on version. We'll check both.
	
	path1 := filepath.Join(homeDir, ".gemini", "antigravity", "brain", externalSessionID, ".system_generated", "logs", "transcript.jsonl")
	path2 := filepath.Join(homeDir, ".gemini", "antigravity", "brain", externalSessionID, "transcript.jsonl")
	
	if _, err := os.Stat(path1); err == nil {
		return LogRef{Path: path1, Agent: "gemini", Session: externalSessionID}, nil
	}
	if _, err := os.Stat(path2); err == nil {
		return LogRef{Path: path2, Agent: "gemini", Session: externalSessionID}, nil
	}
	
	// Fallback to searching, though explicit links should ideally exist.
	return LogRef{}, fmt.Errorf("antigravity log not found for uuid %s", externalSessionID)
}
