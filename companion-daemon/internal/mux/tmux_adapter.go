package mux

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"devremote/companion-daemon/internal/models"
)

type tmuxAdapter struct{}

type tmuxSession struct {
	id      string
	adapter Adapter
}

func (s *tmuxSession) ID() string { return s.id }
func (s *tmuxSession) AdapterName() string { return s.adapter.Name() }
func (s *tmuxSession) Title() string { return s.id } // tmux pane title not parsed yet
func (s *tmuxSession) Read(p []byte) (n int, err error) { return 0, fmt.Errorf("not connected") }
func (s *tmuxSession) Write(p []byte) (n int, err error) { return 0, fmt.Errorf("not connected") }
func (s *tmuxSession) Close() error { return nil }
func (s *tmuxSession) Resize(rows, cols int) error { return nil }

func (s *tmuxSession) ProcessInfo(ctx context.Context) (models.ProcessInfo, error) {
	cmd := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", s.id, "#{pane_start_time},#{pane_pid},#{pane_current_path}")
	out, err := cmd.Output()
	if err != nil {
		return models.ProcessInfo{}, err
	}
	parts := strings.Split(strings.TrimSpace(string(out)), ",")
	if len(parts) != 3 {
		return models.ProcessInfo{}, fmt.Errorf("invalid tmux pane output")
	}
	startTimeUnix, _ := strconv.ParseInt(parts[0], 10, 64)
	shellPID, _ := strconv.Atoi(parts[1])
	return models.ProcessInfo{
		PID:       shellPID,
		CWD:       parts[2],
		StartedAt: time.Unix(startTimeUnix, 0),
		PaneID:    s.id,
	}, nil
}

func (s *tmuxSession) ReadScreen(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "tmux", "capture-pane", "-t", s.id, "-p")
	return cmd.Output()
}

func (s *tmuxSession) WriteInput(ctx context.Context, data []byte) error {
	cmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", s.id, string(data))
	return cmd.Run()
}

func NewTmuxAdapter() Adapter {
	return &tmuxAdapter{}
}

func (a *tmuxAdapter) Name() string {
	return "tmux"
}

func (a *tmuxAdapter) ListSessions() ([]Session, error) {
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
	out, err := cmd.Output()
	if err != nil {
		return nil, nil // Return empty if no sessions or tmux not installed
	}

	var sessions []Session
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if line != "" {
			sessions = append(sessions, &tmuxSession{id: line, adapter: a})
		}
	}
	return sessions, nil
}

func (a *tmuxAdapter) GetSession(id string) (Session, error) {
	// Check if session exists first
	cmd := exec.Command("tmux", "has-session", "-t", id)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("session not found")
	}

	// Simple and elegant Phase 4 approach:
	// Allocate a Native PTY and run the command `tmux attach -t <session_id>` inside it.
	return SpawnPTY(id, "xterm-256color", "tmux", "attach", "-t", id)
}

func init() {
	RegisterAdapter(NewTmuxAdapter())
}
