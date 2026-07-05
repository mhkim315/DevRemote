package term

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type GeminiResolver struct{}

func (r *GeminiResolver) Resolve(ctx context.Context, p ProcessInfo) (LogRef, error) {
	if p.PaneID == "" {
		return LogRef{}, fmt.Errorf("gemini resolver requires pane ID to query environment variables")
	}

	cmd := exec.Command("tmux", "show-environment", "-t", p.PaneID, "POKIT_AGENT_SESSION_ID")
	out, err := cmd.Output()
	if err != nil {
		return LogRef{}, fmt.Errorf("failed to get POKIT_AGENT_SESSION_ID from pane %s: %w", p.PaneID, err)
	}

	envStr := strings.TrimSpace(string(out))
	if envStr == "" || !strings.HasPrefix(envStr, "POKIT_AGENT_SESSION_ID=") {
		return LogRef{}, fmt.Errorf("POKIT_AGENT_SESSION_ID not found in pane %s", p.PaneID)
	}

	sessionID := strings.TrimPrefix(envStr, "POKIT_AGENT_SESSION_ID=")
	
	// Valid UUID check
	if !uuidRegex.MatchString(sessionID) {
		return LogRef{}, fmt.Errorf("invalid gemini session ID format: %s", sessionID)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return LogRef{}, fmt.Errorf("failed to get home dir: %w", err)
	}

	expectedBase := filepath.Clean(filepath.Join(homeDir, ".gemini", "antigravity", "brain"))
	logPath := filepath.Clean(filepath.Join(expectedBase, sessionID, ".system_generated", "logs", "transcript.jsonl"))
	
	if err := ValidateLogPath(expectedBase, logPath); err != nil {
		return LogRef{}, fmt.Errorf("gemini resolver path validation failed: %w", err)
	}

	return LogRef{
		Path:    logPath,
		Agent:   "gemini",
		Session: sessionID,
	}, nil
}
