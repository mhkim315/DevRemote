package mux

import (
	"os/exec"
	"testing"
	"time"
)

func TestTmuxIntegration(t *testing.T) {
	// 1. Start a tmux session with a dummy process that has a UUID in arguments
	sessionName := "test-uuid-integration-go"
	uuid := "abcdef12-3456-7890-abcd-ef1234567890"

	// Ensure we clean up before and after
	exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	defer exec.Command("tmux", "kill-session", "-t", sessionName).Run()

	cmd := exec.Command("tmux", "new-session", "-d", "-s", sessionName, "sleep 1000 --session-id "+uuid)
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to start tmux session: %v", err)
	}

	// Give it a tiny bit of time to spawn the process
	time.Sleep(500 * time.Millisecond)

	// 2. Track panels
	panels, err := TrackCmuxPanels()
	if err != nil {
		t.Fatalf("TrackCmuxPanels failed: %v", err)
	}

	var foundPanel *CmuxPanelInfo
	for _, p := range panels {
		if p.TTY != "" && p.TTY != "not a tty" && p.TTY != "?" {
			// FindSessionUUIDByTTY handles the parsing
			extractedUUID, err := FindSessionUUIDByTTY(p.TTY)
			if err == nil && extractedUUID == uuid {
				pCopy := p
				foundPanel = &pCopy
				break
			}
		}
	}

	if foundPanel == nil {
		t.Fatalf("Could not find pane with matching UUID %s among %d panels", uuid, len(panels))
	}

	// 3. Test the adapter
	adapter := NewTmuxAdapter()
	session, err := adapter.GetSession(sessionName)
	if err != nil {
		t.Fatalf("Failed to get session from adapter: %v", err)
	}
	defer session.Close()

	if session.AdapterName() != "tmux" {
		t.Errorf("Expected adapter name 'tmux', got '%s'", session.AdapterName())
	}
}
