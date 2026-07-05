package mux

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"os"
)

// FindSessionUUIDByTTY scans the process tree for the given TTY and extracts the --session-id argument
func FindSessionUUIDByTTY(tty string) (string, error) {
	// macOS ps command to list all processes and their arguments on a specific tty
	// Ensure tty doesn't have /dev/ prefix if using -t
	ttyName := strings.TrimPrefix(tty, "/dev/")
	
	cmd := exec.Command("ps", "-t", ttyName, "-o", "command")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to run ps on tty %s: %w", ttyName, err)
	}

	// Regex to match --session-id <uuid>
	re := regexp.MustCompile(`--session-id\s+([a-fA-F0-9\-]{36})`)
	
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		matches := re.FindStringSubmatch(line)
		if len(matches) > 1 {
			return matches[1], nil
		}
	}

	return "", fmt.Errorf("session-id not found for tty %s", tty)
}

// GetLogPathForUUID constructs the transcript.jsonl path for a given UUID
func GetLogPathForUUID(uuid string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, ".gemini", "antigravity", "brain", uuid, ".system_generated", "logs", "transcript.jsonl")
	
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("transcript log not found at %s", path)
	}
	return path, nil
}

// TrackCmuxPanels is a helper to run cmux list-panels and parse output
func TrackCmuxPanels() ([]CmuxPanelInfo, error) {
	// In a real implementation, cmux list-panels --json might be used
	// Or a fallback to tmux
	cmd := exec.Command("tmux", "list-panes", "-a", "-F", "#{pane_id}\t#{pane_title}\t#{pane_tty}\t#{pane_pid}")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("tmux list-panes failed: %w", err)
	}

	var panels []CmuxPanelInfo
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) >= 4 {
			var pid int
			fmt.Sscanf(parts[3], "%d", &pid)
			panels = append(panels, CmuxPanelInfo{
				ID:    parts[0],
				Title: parts[1],
				TTY:   parts[2],
				PID:   pid,
			})
		}
	}
	return panels, nil
}
