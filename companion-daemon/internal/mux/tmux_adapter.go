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

type tmuxAdapter struct {
	runner CommandRunner
}

const tmuxSessionFormat = "#{session_id}::POKIT::#{session_name}"

// tmuxExecRunner implements CommandRunner using exec.CommandContext.
// No binary discovery: tmux is a core macOS/Linux tool always in PATH.
// CombinedOutput captures both stdout and stderr for error diagnostics.
type tmuxExecRunner struct{}

func (r *tmuxExecRunner) Run(ctx context.Context, _ CommandOptions, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	return cmd.CombinedOutput()
}

type tmuxSession struct {
	id      string
	target  string
	adapter Adapter
}

func (s *tmuxSession) ID() string          { return s.id }
func (s *tmuxSession) AdapterName() string { return s.adapter.Name() }
func (s *tmuxSession) Title() string       { return s.id }

func (s *tmuxSession) OpenStream(ctx context.Context) (TerminalStream, error) {
	if _, err := s.ReadScreen(ctx); err != nil {
		return nil, fmt.Errorf("tmux session preflight failed: %w", err)
	}
	return SpawnPTY(s.id, "xterm-256color", "tmux", "attach", "-t", s.targetName())
}

func (s *tmuxSession) ProcessInfo(ctx context.Context) (models.ProcessInfo, error) {
	out, err := s.adapter.(*tmuxAdapter).runner.Run(ctx, CommandOptions{}, "tmux", "display-message", "-p", "-t", s.targetName(), "#{pane_start_time},#{pane_pid},#{pane_current_path}")
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
	return s.adapter.(*tmuxAdapter).runner.Run(ctx, CommandOptions{}, "tmux", "capture-pane", "-t", s.targetName(), "-p")
}

func (s *tmuxSession) ReadHistory(ctx context.Context, lines int) ([]byte, error) {
	if lines < 1 {
		lines = 1000
	} else if lines > 10000 {
		lines = 10000
	}
	return s.adapter.(*tmuxAdapter).runner.Run(ctx, CommandOptions{}, "tmux", "capture-pane", "-t", s.targetName(), "-p", "-S", fmt.Sprintf("-%d", lines))
}

func (s *tmuxSession) targetName() string {
	if s.target != "" {
		return s.target
	}
	return s.id
}

func (a *tmuxAdapter) CreateSession(ctx context.Context, opts CreateOptions) (string, error) {
	_, err := a.runner.Run(ctx, CommandOptions{Dir: opts.CWD}, "tmux", "new-session", "-d", "-s", opts.Name)
	return opts.Name, err
}

func (a *tmuxAdapter) TerminateSession(ctx context.Context, id string) error {
	target := id
	if resolved, err := resolveTmuxTarget(ctx, id); err == nil {
		target = resolved
	}
	_, err := a.runner.Run(ctx, CommandOptions{}, "tmux", "kill-session", "-t", target)
	return err
}

func NewTmuxAdapter() Adapter {
	return &tmuxAdapter{runner: &tmuxExecRunner{}}
}

// NewTmuxAdapterWithRunner creates a tmux adapter with an injected CommandRunner.
func NewTmuxAdapterWithRunner(runner CommandRunner) Adapter {
	return &tmuxAdapter{runner: runner}
}

func (a *tmuxAdapter) Name() string {
	return "tmux"
}

func (a *tmuxAdapter) ListSessions(ctx context.Context) ([]Session, error) {
	out, err := a.runner.Run(ctx, CommandOptions{}, "tmux", "list-sessions", "-F", tmuxSessionFormat)
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			return nil, fmt.Errorf("tmux list-sessions: %w", err)
		}
		return nil, fmt.Errorf("tmux list-sessions: %w: %s", err, detail)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	sessions := make([]Session, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		target, name, ok := parseTmuxListSessionLine(line)
		if !ok {
			continue
		}
		sessions = append(sessions, &tmuxSession{id: name, target: target, adapter: a})
	}
	if len(sessions) == 0 && strings.TrimSpace(string(out)) != "" {
		return nil, fmt.Errorf("tmux list-sessions returned no parseable sessions: %q", strings.TrimSpace(string(out)))
	}
	return sessions, nil
}

func (a *tmuxAdapter) GetSession(id string) (Session, error) {
	target, _ := resolveTmuxTarget(context.Background(), id)
	return &tmuxSession{id: id, target: target, adapter: a}, nil
}

func parseTmuxListSessionLine(line string) (target string, name string, ok bool) {
	parts := strings.SplitN(line, "::POKIT::", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func resolveTmuxTarget(ctx context.Context, name string) (string, error) {
	cmd := exec.CommandContext(ctx, "tmux", "list-sessions", "-F", tmuxSessionFormat)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		target, sessionName, ok := parseTmuxListSessionLine(line)
		if ok && sessionName == name {
			return target, nil
		}
	}
	return "", fmt.Errorf("tmux session %q not found", name)
}
