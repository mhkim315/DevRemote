package mux

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type cmuxAdapter struct{}

func NewCmuxAdapter() Adapter {
	return &cmuxAdapter{}
}

func (a *cmuxAdapter) Name() string {
	return "cmux"
}

// parsePanelLine extracts surface ID and title from cmux output like:
//
//	* surface:1  terminal  [focused]  "My Panel Title"
//	 surface:5  terminal  "Another"
var panelRe = regexp.MustCompile(`surface:(\d+)\s+\S+(?:\s+\[.*?\])?\s+"(.*?)"`)

func (a *cmuxAdapter) ListSessions() ([]Session, error) {
	seen := make(map[string]bool)
	var all []Session

	// Collect workspace IDs to scan
	var workspaces []string
	workspaces = append(workspaces, "") // Default workspace context

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	wsOut, err := exec.CommandContext(ctx, "cmux", "workspace", "list").Output()

	if err == nil {
		// Extract all workspace IDs, including UUIDs like workspace:33 or workspace:a1b2...
		wsRe := regexp.MustCompile(`workspace:([a-zA-Z0-9\-]+)`)
		matches := wsRe.FindAllStringSubmatch(string(wsOut), -1)
		for _, m := range matches {
			workspaces = append(workspaces, m[1])
		}
	} else {
		// Fallback to stable indices if list command fails
		for i := 1; i <= 5; i++ {
			workspaces = append(workspaces, fmt.Sprintf("%d", i))
		}
	}

	for _, ws := range workspaces {
		sessions, err := a.listPanels(ws)
		if err != nil {
			continue
		}
		for _, s := range sessions {
			if !seen[s.ID()] {
				seen[s.ID()] = true
				all = append(all, s)
			}
		}
	}
	return all, nil
}

func (a *cmuxAdapter) listPanels(workspaceID string) ([]Session, error) {
	args := []string{"list-panels"}
	if workspaceID != "" {
		args = append(args, "--workspace", workspaceID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	
	out, err := exec.CommandContext(ctx, "cmux", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("cmux list-panels: %w", err)
	}

	var sessions []Session
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		matches := panelRe.FindStringSubmatch(line)
		if len(matches) == 3 {
			surfaceID := matches[1]
			title := matches[2]
			sessionID := fmt.Sprintf("cmux-%s", surfaceID)
			sessions = append(sessions, &CmuxSession{
				id:    sessionID,
				title: title,
				pid:   0, // TODO: resolve PID via cmux or ps
			})
		}
	}

	return sessions, nil
}

func (a *cmuxAdapter) GetSession(id string) (Session, error) {
	// Extract surface ID from session ID (e.g., "cmux-39" → "39")
	surfaceID := strings.TrimPrefix(id, "cmux-")

	// Verify the surface exists by listing panels
	sessions, err := a.ListSessions()
	if err != nil {
		return nil, err
	}
	for _, s := range sessions {
		if s.ID() == id {
			// Use SpawnPTY with cmux read-screen + send-panel
			return SpawnPTY(id, "xterm-256color", "cmux", "attach-surface", "--surface", surfaceID)
		}
	}
	return nil, fmt.Errorf("session %s not found in cmux", id)
}

// CmuxSession implements the Session interface for a cmux panel
type CmuxSession struct {
	id    string
	title string
	pid   int
}

func (s *CmuxSession) AdapterName() string                 { return "cmux" }
func (s *CmuxSession) ID() string                        { return s.id }
func (s *CmuxSession) Read(p []byte) (n int, err error)   { return 0, fmt.Errorf("not connected") }
func (s *CmuxSession) Write(p []byte) (n int, err error)  { return 0, fmt.Errorf("not connected") }
func (s *CmuxSession) Close() error                        { return nil }
func (s *CmuxSession) Resize(rows, cols int) error         { return nil }


func init() {
	RegisterAdapter(NewCmuxAdapter())
}

// CmuxPanelInfo represents the data we get from cmux list-panels
type CmuxPanelInfo struct {
	ID    string
	Title string
	TTY   string
	PID   int
}
