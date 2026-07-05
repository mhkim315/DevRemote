package term

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type CodexResolver struct{}

type codexSessionMeta struct {
	Timestamp string `json:"timestamp"`
	Payload   struct {
		SessionID string `json:"session_id"`
		Cwd       string `json:"cwd"`
	} `json:"payload"`
}

func (r *CodexResolver) Resolve(ctx context.Context, p ProcessInfo) (LogRef, error) {
	// 1. Try lsof on PID and its children
	pidsToScan := append([]int{p.PID}, getChildPIDs(p.PID)...)

	var foundPath string
	for _, pid := range pidsToScan {
		// execute lsof -a -p <pid> -Fn
		cmd := exec.Command("lsof", "-a", "-p", fmt.Sprintf("%d", pid), "-Fn")
		out, err := cmd.Output()
		if err == nil {
			lines := strings.Split(string(out), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "n") && strings.HasSuffix(line, ".jsonl") && strings.Contains(line, ".codex") {
					foundPath = strings.TrimPrefix(line, "n")
					break
				}
			}
		}
		if foundPath != "" {
			break
		}
	}

	if foundPath != "" {
		// Ensure containment
		homeDir, _ := os.UserHomeDir()
		expectedBase := filepath.Clean(filepath.Join(homeDir, ".codex", "sessions"))
		cleanPath := filepath.Clean(foundPath)
		if err := ValidateLogPath(expectedBase, cleanPath); err != nil {
			return LogRef{}, fmt.Errorf("codex resolver path validation failed (lsof): %w", err)
		}
		return LogRef{Path: cleanPath, Agent: "codex"}, nil
	}

	// 2. Fallback: Parse recent rollout-*.jsonl files
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return LogRef{}, fmt.Errorf("failed to get home dir: %w", err)
	}

	sessionsDir := filepath.Join(homeDir, ".codex", "sessions")
	var candidates []string

	// Walk the sessions dir to find jsonl files
	err = filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".jsonl") && strings.HasPrefix(info.Name(), "rollout-") {
			candidates = append(candidates, path)
		}
		return nil
	})
	
	if err != nil {
		return LogRef{}, fmt.Errorf("failed to walk codex sessions: %w", err)
	}

	var matchedPaths []string
	var matchedSessionID string

	for _, path := range candidates {
		// Read just the first line (metadata)
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		
		// Create a small buffer for the first line
		buf := make([]byte, 4096)
		n, _ := f.Read(buf)
		f.Close()

		firstLine := strings.Split(string(buf[:n]), "\n")[0]
		
		var meta codexSessionMeta
		if err := json.Unmarshal([]byte(firstLine), &meta); err != nil {
			continue
		}

		// Match CWD
		if meta.Payload.Cwd != p.CWD {
			continue
		}

		// Match Timestamp (within 5 seconds)
		metaTime, err := time.Parse(time.RFC3339Nano, meta.Timestamp)
		if err != nil {
			continue
		}

		diff := p.StartedAt.Sub(metaTime)
		if diff < 0 {
			diff = -diff
		}

		if diff <= 5*time.Second {
			matchedPaths = append(matchedPaths, path)
			matchedSessionID = meta.Payload.SessionID
		}
	}

	if len(matchedPaths) == 0 {
		return LogRef{}, fmt.Errorf("no codex log found for pid %d", p.PID)
	}

	if len(matchedPaths) > 1 {
		return LogRef{}, fmt.Errorf("ambiguous codex logs found: %v", matchedPaths)
	}

	cleanPath := filepath.Clean(matchedPaths[0])
	expectedBase := filepath.Clean(filepath.Join(homeDir, ".codex", "sessions"))
	if err := ValidateLogPath(expectedBase, cleanPath); err != nil {
		return LogRef{}, fmt.Errorf("codex resolver path validation failed (fallback): %w", err)
	}

	return LogRef{Path: cleanPath, Agent: "codex", Session: matchedSessionID}, nil
}

func getChildPIDs(parent int) []int {
	cmd := exec.Command("ps", "-A", "-o", "pid,ppid")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	children := make(map[int][]int)
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		var pid, ppid int
		fmt.Sscanf(fields[0], "%d", &pid)
		fmt.Sscanf(fields[1], "%d", &ppid)
		children[ppid] = append(children[ppid], pid)
	}

	var res []int
	var walk func(p int)
	walk = func(p int) {
		for _, child := range children[p] {
			res = append(res, child)
			walk(child)
		}
	}
	walk(parent)
	return res
}
