package term

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// FindAgentLogPath finds the log path for Claude or Aider running in the given pane.
func FindAgentLogPath(paneID string, cwd string) (string, error) {
	// 1. Get shell PID for paneID
	cmd := exec.Command("tmux", "display-message", "-p", "-t", paneID, "#{pane_pid}")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get pane pid: %w", err)
	}

	shellPIDStr := strings.TrimSpace(string(out))
	if shellPIDStr == "" {
		return "", fmt.Errorf("empty pane pid returned")
	}

	shellPID, err := strconv.Atoi(shellPIDStr)
	if err != nil {
		return "", fmt.Errorf("invalid pane pid %q: %w", shellPIDStr, err)
	}

	// 2. Find child processes to identify claude or aider
	agentType, agentPID, err := findAgentProcess(shellPID)
	if err != nil {
		return "", fmt.Errorf("failed to find agent process: %w", err)
	}

	if agentType == "" {
		return "", fmt.Errorf("no supported agent (claude/aider) found in pane %s", paneID)
	}

	// 3. If claude, read session file
	if agentType == "claude" {
		return getClaudeLogPath(agentPID, cwd)
	}

	if agentType == "aider" {
		return getAiderLogPath(agentPID, cwd)
	}

	return "", fmt.Errorf("unsupported agent type: %s", agentType)
}

func findAgentProcess(parentPID int) (string, int, error) {
	// Parse ps output to find children
	// ps -A -o pid,ppid,comm works on mac and linux
	cmd := exec.Command("ps", "-A", "-o", "pid,ppid,comm")
	out, err := cmd.Output()
	if err != nil {
		return "", 0, fmt.Errorf("failed to run ps: %w", err)
	}

	lines := strings.Split(string(out), "\n")
	
	// Map ppid -> []pid
	// Map pid -> comm
	children := make(map[int][]int)
	commands := make(map[int]string)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "PID") { // skip header
			continue
		}
		
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		
		pid, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		
		commPath := strings.Join(parts[2:], " ")
		comm := filepath.Base(commPath)

		children[ppid] = append(children[ppid], pid)
		commands[pid] = comm
	}

	// recursively search for claude or aider from parentPID
	var search func(int) (string, int)
	search = func(pid int) (string, int) {
		comm := commands[pid]
		commLower := strings.ToLower(comm)
		
		if strings.Contains(commLower, "claude") {
			return "claude", pid
		}
		if strings.Contains(commLower, "aider") {
			return "aider", pid
		}
		
		for _, childPID := range children[pid] {
			if t, p := search(childPID); t != "" {
				return t, p
			}
		}
		return "", 0
	}

	for _, childPID := range children[parentPID] {
		if t, p := search(childPID); t != "" {
			return t, p, nil
		}
	}

	return "", 0, nil
}

type claudeSession struct {
	SessionID string `json:"sessionId"`
}

func getClaudeLogPath(pid int, cwd string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home dir: %w", err)
	}

	sessionFile := filepath.Join(homeDir, ".claude", "sessions", fmt.Sprintf("%d.json", pid))
	data, err := os.ReadFile(sessionFile)
	if err != nil {
		return "", fmt.Errorf("failed to read claude session file: %w", err)
	}

	var session claudeSession
	if err := json.Unmarshal(data, &session); err != nil {
		return "", fmt.Errorf("failed to parse claude session file: %w", err)
	}

	if session.SessionID == "" {
		return "", fmt.Errorf("no sessionId found in claude session file")
	}

	encodedCwd := strings.ReplaceAll(cwd, "/", "-")
	encodedCwd = strings.ReplaceAll(encodedCwd, ".", "-")
	logPath := filepath.Join(homeDir, ".claude", "projects", encodedCwd, fmt.Sprintf("%s.jsonl", session.SessionID))

	return logPath, nil
}

func getAiderLogPath(pid int, cwd string) (string, error) {
	// Stub for Aider implementation
	return "", fmt.Errorf("aider tracking not yet implemented")
}
