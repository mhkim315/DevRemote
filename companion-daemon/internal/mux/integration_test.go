//go:build integration

package mux

import (
	"os/exec"
	"testing"
)

func TestTmuxAdapter_ListSessions(t *testing.T) {
	// Skip if tmux is not installed
	if err := exec.Command("tmux", "-V").Run(); err != nil {
		t.Skip("tmux is not installed, skipping integration test")
	}

	// Create a dummy session. Since tmux is installed, failure here is a real error.
	if err := exec.Command("tmux", "new-session", "-d", "-s", "test-tmux-adapter-session").Run(); err != nil {
		t.Fatalf("Failed to create tmux session: %v", err)
	}
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
