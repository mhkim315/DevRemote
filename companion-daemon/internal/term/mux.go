package term

import (
	"os/exec"
	"strings"
)

// Multiplexer defines the interface for interacting with terminal multiplexers
// like tmux, zellij, or cumx.
type Multiplexer interface {
	// ListSessions returns a list of active session names.
	ListSessions() ([]string, error)
	// AttachCmd returns the exec.Cmd that will connect to (or create) the session.
	AttachCmd(sessionName string) *exec.Cmd
}

// TmuxMux implements the Multiplexer interface for tmux.
type TmuxMux struct{}

func (t *TmuxMux) ListSessions() ([]string, error) {
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
	out, err := cmd.Output()
	if err != nil {
		// If tmux server is not running, it returns an error. Treat as empty list.
		return []string{}, nil
	}
	
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var sessions []string
	for _, line := range lines {
		if line != "" {
			sessions = append(sessions, line)
		}
	}
	return sessions, nil
}

func (t *TmuxMux) AttachCmd(sessionName string) *exec.Cmd {
	if sessionName == "" {
		sessionName = "devremote"
	}
	return exec.Command("tmux", "new-session", "-A", "-s", sessionName)
}

// DefaultMux is the globally active multiplexer.
var DefaultMux Multiplexer = &TmuxMux{}
