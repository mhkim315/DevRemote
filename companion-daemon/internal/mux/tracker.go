package mux

import (
	"fmt"
	"os/exec"
	"strings"
)


// TrackCmuxPanels is a helper to run cmux list-panels and parse output
func TrackCmuxPanels() ([]CmuxPanelInfo, error) {
	// In a real implementation, cmux list-panels --json might be used
	// Or a fallback to tmux
	cmd := exec.Command("tmux", "list-panes", "-a", "-F", "#{pane_id}\t#{pane_title}\t#{pane_tty}\t#{pane_pid}")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("tmux list-panes failed: %w", err)
	}

	var panels []CmuxPanelInfo
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) >= 4 {
			var pid int
			fmt.Sscanf(parts[3], "%d", &pid)
			panels = append(panels, CmuxPanelInfo{
				ID:    parts[0],
				Title: parts[1],
				TTY:   parts[2],
				PID:   pid,
			})
		}
	}
	return panels, nil
}
