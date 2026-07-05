package mux

import (
	"context"
	"fmt"
	"io"
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

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
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
			sessionID := surfaceID
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
	// The id passed in is the raw surface ID (e.g., "38")
	surfaceID := id

	// Verify the surface exists by listing panels
	sessions, err := a.ListSessions()
	if err != nil {
		return nil, err
	}
	
	for _, s := range sessions {
		if s.ID() == id {
			pr, pw := io.Pipe()
			cs := &CmuxSession{
				id:        id,
				title:     "cmux panel",
				surfaceID: surfaceID,
				pr:        pr,
				pw:        pw,
				done:      make(chan struct{}),
			}
			go cs.pollScreen()
			return cs, nil
		}
	}
	return nil, fmt.Errorf("session %s not found in cmux", id)
}

// CmuxSession implements the Session interface for a cmux panel
type CmuxSession struct {
	id        string
	title     string
	surfaceID string
	pid       int

	pr   *io.PipeReader
	pw   *io.PipeWriter
	done chan struct{}
}

func (s *CmuxSession) pollScreen() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastContent string
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			out, err := exec.CommandContext(ctx, "cmux", "read-screen", "--surface", s.surfaceID).Output()
			cancel()

			if err == nil {
				currentContent := string(out)
				if currentContent != lastContent {
					// Clear screen and redraw for xterm.js
					payload := "\033[2J\033[H" + currentContent
					// Ensure CRLF for xterm.js line breaks
					payload = strings.ReplaceAll(payload, "\n", "\r\n")

					s.pw.Write([]byte(payload))
					lastContent = currentContent
				}
			}
		}
	}
}

func (s *CmuxSession) AdapterName() string                 { return "cmux" }
func (s *CmuxSession) ID() string                        { return s.id }
func (s *CmuxSession) Title() string                     { return s.title }

func (s *CmuxSession) Read(p []byte) (n int, err error) {
	return s.pr.Read(p)
}

func (s *CmuxSession) Write(p []byte) (n int, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Use send-panel to send arbitrary byte sequences/text
	text := string(p)
	err = exec.CommandContext(ctx, "cmux", "send-panel", "--panel", s.surfaceID, text).Run()
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (s *CmuxSession) Close() error {
	close(s.done)
	s.pw.Close()
	s.pr.Close()
	return nil
}

func (s *CmuxSession) Resize(rows, cols int) error {
	return nil
}

func (s *CmuxSession) ReadScreen(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "cmux", "read-screen", "--surface", s.surfaceID)
	return cmd.Output()
}

func (s *CmuxSession) WriteInput(ctx context.Context, data []byte) error {
	cmd := exec.CommandContext(ctx, "cmux", "send-panel", "--panel", s.surfaceID, string(data))
	return cmd.Run()
}

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
