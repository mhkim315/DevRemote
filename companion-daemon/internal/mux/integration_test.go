package mux

import (
	"os/exec"
	"testing"
)

func TestTmuxAdapter_ListSessions(t *testing.T) {
	// Create a dummy session
	exec.Command("tmux", "new-session", "-d", "-s", "test-tmux-adapter-session").Run()
	defer exec.Command("tmux", "kill-session", "-t", "test-tmux-adapter-session").Run()

	adapter := NewTmuxAdapter()
	sessions, err := adapter.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}

	found := false
	for _, s := range sessions {
		if s.ID() == "test-tmux-adapter-session" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Expected to find 'test-tmux-adapter-session' in tmux sessions list")
	}
}
