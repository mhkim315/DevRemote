package mux

import (
	"fmt"
	"os/exec"
)

type tmuxAdapter struct{}

func NewTmuxAdapter() Adapter {
	return &tmuxAdapter{}
}

func (a *tmuxAdapter) Name() string {
	return "tmux"
}

func (a *tmuxAdapter) ListSessions() ([]Session, error) {
	// Implement if needed
	return nil, nil
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
