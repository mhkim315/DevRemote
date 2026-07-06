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

func (s *tmuxSession) ID() string          { return s.id }
func (s *tmuxSession) AdapterName() string { return s.adapter.Name() }
func (s *tmuxSession) Title() string       { return s.id }

func (s *tmuxSession) OpenStream(ctx context.Context) (TerminalStream, error) {
	return SpawnPTY(s.id, "xterm-256color", "tmux", "attach", "-t", s.id)
}

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

func (s *tmuxSession) ReadHistory(ctx context.Context, lines int) ([]byte, error) {
	if lines < 1 {
		lines = 1000
	} else if lines > 10000 {
		lines = 10000
	}
	cmd := exec.CommandContext(ctx, "tmux", "capture-pane", "-t", s.id, "-p", "-S", fmt.Sprintf("-%d", lines))
	return cmd.Output()
}

func (a *tmuxAdapter) CreateSession(ctx context.Context, opts CreateOptions) (string, error) {
	cmd := exec.CommandContext(ctx, "tmux", "new-session", "-d", "-s", opts.Name)
	if opts.CWD != "" {
		cmd.Dir = opts.CWD
	}
	err := cmd.Run()
	if err == nil {
		Default.Invalidate()
	}
	return opts.Name, err
}

func (a *tmuxAdapter) TerminateSession(ctx context.Context, id string) error {
	cmd := exec.CommandContext(ctx, "tmux", "kill-session", "-t", id)
	err := cmd.Run()
	if err == nil {
		Default.Invalidate()
	}
	return err
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

	return &tmuxSession{
		id:      id,
		adapter: a,
	}, nil
}

