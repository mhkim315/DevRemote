package term

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"devremote/companion-daemon/internal/models"
)

type ClaudeResolver struct{}

type claudeSessionMeta struct {
	SessionID string `json:"sessionId"`
}

func (r *ClaudeResolver) Resolve(ctx context.Context, p models.ProcessInfo) (LogRef, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return LogRef{}, fmt.Errorf("failed to get home dir: %w", err)
	}

	sessionFile := filepath.Join(homeDir, ".claude", "sessions", fmt.Sprintf("%d.json", p.PID))
	data, err := os.ReadFile(sessionFile)
	if err != nil {
		return LogRef{}, fmt.Errorf("failed to read claude session file: %w", err)
	}

	var session claudeSessionMeta
	if err := json.Unmarshal(data, &session); err != nil {
		return LogRef{}, fmt.Errorf("failed to parse claude session file: %w", err)
	}

	if session.SessionID == "" {
		return LogRef{}, fmt.Errorf("no sessionId found in claude session file")
	}

	// UUID Validation to prevent path traversal
	if !uuidRegex.MatchString(session.SessionID) {
		return LogRef{}, fmt.Errorf("invalid sessionId format: %s", session.SessionID)
	}

	encodedCwd := strings.ReplaceAll(p.CWD, "/", "-")
	encodedCwd = strings.ReplaceAll(encodedCwd, ".", "-")
	
	// Clean and strictly join
	logPath := filepath.Clean(filepath.Join(homeDir, ".claude", "projects", encodedCwd, fmt.Sprintf("%s.jsonl", session.SessionID)))
	
	// Ensure containment
	expectedBase := filepath.Clean(filepath.Join(homeDir, ".claude", "projects"))
	if err := ValidateLogPath(expectedBase, logPath); err != nil {
		return LogRef{}, fmt.Errorf("claude resolver path validation failed: %w", err)
	}

	return LogRef{
		Path:    logPath,
		Agent:   "claude",
		Session: session.SessionID,
	}, nil
}
