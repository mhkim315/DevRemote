package term

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// FindAgentLogRef finds the LogRef for Claude, Codex, or Gemini running in the given pane.
func FindAgentLogRef(ctx context.Context, paneID string, cwd string) (LogRef, error) {
	// 1. Get shell PID for paneID
	cmd := exec.Command("tmux", "display-message", "-p", "-t", paneID, "#{pane_start_time},#{pane_pid}")
	out, err := cmd.Output()
	if err != nil {
		return LogRef{}, fmt.Errorf("failed to get pane pid: %w", err)
	}

	parts := strings.Split(strings.TrimSpace(string(out)), ",")
	if len(parts) != 2 {
		return LogRef{}, fmt.Errorf("invalid tmux pane output")
	}

	startTimeUnix, _ := strconv.ParseInt(parts[0], 10, 64)
	shellPID, _ := strconv.Atoi(parts[1])

	processInfo := ProcessInfo{
		PID:       shellPID, // initially shell PID
		CWD:       cwd,
		StartedAt: time.Unix(startTimeUnix, 0),
		PaneID:    paneID,
	}

	// Try Gemini via env var (wrapper mode)
	geminiRes := &GeminiResolver{}
	if ref, err := geminiRes.Resolve(ctx, processInfo); err == nil {
		return ref, nil
	}

	// 2. Find child processes to identify claude, codex, or aider
	agentType, agentPID, err := findAgentProcess(shellPID)
	if err != nil {
		return LogRef{}, fmt.Errorf("failed to find agent process: %w", err)
	}

	if agentType == "" {
		return LogRef{}, fmt.Errorf("no supported agent found in pane %s", paneID)
	}

	// Fetch actual agent start time using ps
	cmdTime := exec.Command("ps", "-p", strconv.Itoa(agentPID), "-o", "lstart=")
	outTime, err := cmdTime.Output()
	if err == nil {
		if t, err := time.ParseInLocation(time.ANSIC, strings.TrimSpace(string(outTime)), time.Local); err == nil {
			processInfo.StartedAt = t
		}
	}

	processInfo.PID = agentPID

	var resolver AgentLogResolver
	if agentType == "claude" {
		resolver = &ClaudeResolver{}
	} else if agentType == "codex" {
		resolver = &CodexResolver{}
	} else {
		return LogRef{}, fmt.Errorf("unsupported agent type: %s", agentType)
	}

	return resolver.Resolve(ctx, processInfo)
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
		if strings.Contains(commLower, "codex") {
			return "codex", pid
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

// ValidateLogPath safely evaluates symlinks and ensures target stays within base.
func ValidateLogPath(base, target string) error {
	evalBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		evalBase = filepath.Clean(base) // fallback if base doesn't exist yet
	}
	
	evalTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		// Target might not exist yet, resolve its directory
		evalDir, errDir := filepath.EvalSymlinks(filepath.Dir(target))
		if errDir == nil {
			evalTarget = filepath.Join(evalDir, filepath.Base(target))
		} else {
			evalTarget = filepath.Clean(target)
		}
	}

	rel, err := filepath.Rel(evalBase, evalTarget)
	if err != nil {
		return fmt.Errorf("failed to compute relative path: %w", err)
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path traversal detected: %s", target)
	}
	
	if !strings.HasSuffix(target, ".jsonl") && !strings.HasSuffix(target, ".json") {
		return fmt.Errorf("invalid log file extension: %s", target)
	}

	return nil
}


