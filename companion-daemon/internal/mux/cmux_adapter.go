package mux

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"devremote/companion-daemon/internal/models"
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
var panelRe = regexp.MustCompile(`(surface:\d+)\s+\S+(?:\s+\[.*?\])?\s+"(.*?)"`)

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
		wsRe := regexp.MustCompile(`workspace:[a-zA-Z0-9\-]+`)
		matches := wsRe.FindAllStringSubmatch(string(wsOut), -1)
		for _, m := range matches {
			workspaces = append(workspaces, m[0])
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
				id:          sessionID,
				title:       title,
				surfaceID:   surfaceID,
				workspaceID: workspaceID,
				pid:         0, // TODO: resolve PID via cmux or ps
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
			cs := &CmuxSession{
				id:        id,
				title:     "cmux panel",
				surfaceID: surfaceID,
			}
			return cs, nil
		}
	}
	return nil, fmt.Errorf("session %s not found in cmux", id)
}

// CmuxSession implements the Session interface for a cmux panel
type CmuxSession struct {
	id          string
	title       string
	surfaceID   string
	workspaceID string
	pid         int
}

type CmuxStream struct {
	session *CmuxSession
	pr      *io.PipeReader
	pw      *io.PipeWriter
	done    chan struct{}
	once    sync.Once
}

func (s *CmuxStream) pollScreen() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastContent string
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			out, err := exec.CommandContext(ctx, "cmux", "read-screen", "--surface", s.session.surfaceID).Output()
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

func (s *CmuxStream) Read(p []byte) (n int, err error) {
	return s.pr.Read(p)
}

func (s *CmuxStream) Write(p []byte) (n int, err error) {
	return 0, fmt.Errorf("CmuxStream.Write not implemented, use InputWriter capability directly")
}

func (s *CmuxStream) Close() error {
	s.once.Do(func() {
		close(s.done)
		s.pw.Close()
		s.pr.Close()
	})
	return nil
}

func (s *CmuxStream) Resize(rows, cols int) error {
	return nil // cmux doesn't natively support dynamic resizing without changing the backend tty
}

func (s *CmuxSession) OpenStream(ctx context.Context) (TerminalStream, error) {
	pr, pw := io.Pipe()
	stream := &CmuxStream{
		session: s,
		pr:      pr,
		pw:      pw,
		done:    make(chan struct{}),
	}
	go stream.pollScreen()
	return stream, nil
}

func (s *CmuxSession) AdapterName() string                 { return "cmux" }
func (s *CmuxSession) ID() string                        { return s.id }
func (s *CmuxSession) Title() string                     { return s.title }

func (s *CmuxSession) ReadScreen(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "cmux", "read-screen", "--surface", s.surfaceID)
	return cmd.Output()
}

func (s *CmuxSession) ReadHistory(ctx context.Context, lines int) ([]byte, error) {
	if lines < 1 {
		lines = 1000
	} else if lines > 10000 {
		lines = 10000
	}
	cmd := exec.CommandContext(ctx, "cmux", "read-screen", "--surface", s.surfaceID, "--scrollback", "--lines", fmt.Sprintf("%d", lines))
	return cmd.Output()
}

func (s *CmuxSession) WriteInput(ctx context.Context, data []byte) error {
	// Parse special keys for cmux since `cmux send` doesn't handle raw ANSI well
	str := string(data)
	if str == "\x03" {
		return s.WriteKey(ctx, "ctrl-c")
	} else if str == "\x1b" {
		return s.WriteKey(ctx, "escape")
	} else if str == "\r" || str == "\n" || str == "\r\n" {
		return s.WriteKey(ctx, "enter")
	} else if str == "\x1b[A" {
		return s.WriteKey(ctx, "up")
	} else if str == "\x1b[B" {
		return s.WriteKey(ctx, "down")
	} else if str == "\x1b[C" {
		return s.WriteKey(ctx, "right")
	} else if str == "\x1b[D" {
		return s.WriteKey(ctx, "left")
	}

	cmd := exec.CommandContext(ctx, "cmux", "send", "--surface", s.surfaceID, str)
	return cmd.Run()
}

func (s *CmuxSession) WriteKey(ctx context.Context, key string) error {
	cmd := exec.CommandContext(ctx, "cmux", "send-key", "--surface", s.surfaceID, key)
	return cmd.Run()
}

func (a *cmuxAdapter) CreateSession(ctx context.Context, opts CreateOptions) (string, error) {
	ws := opts.WorkspaceID
	if ws == "" {
		ws = "workspace:1" // Fallback
	}
	// Verify workspace ref is valid
	if !strings.HasPrefix(ws, "workspace:") {
		return "", fmt.Errorf("invalid workspace ID format (must be workspace:<id>)")
	}

	cmd := exec.CommandContext(ctx, "cmux", "new-surface", "--type", "terminal", "--workspace", ws)
	if opts.CWD != "" {
		cmd.Dir = opts.CWD
	}
	
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("cmux new-surface failed: %v, out: %s", err, string(out))
	}
	
	InvalidateCache()
	
	// Parse output to find "surface:NN"
	outStr := string(out)
	re := regexp.MustCompile(`surface:\s*(\d+)`)
	if m := re.FindStringSubmatch(outStr); m != nil {
		return "cmux:surface:" + m[1], nil
	}
	
	// Fallback if parsing fails but command succeeded
	return "cmux:unknown", nil
}

func (a *cmuxAdapter) TerminateSession(ctx context.Context, rawID string) error {
	if !strings.HasPrefix(rawID, "surface:") {
		return fmt.Errorf("invalid surface ID format (must be surface:<id>)")
	}
	cmd := exec.CommandContext(ctx, "cmux", "close-surface", "--surface", rawID)
	err := cmd.Run()
	if err == nil {
		InvalidateCache()
	}
	return err
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

func (s *CmuxSession) ProcessInfo(ctx context.Context) (models.ProcessInfo, error) {
	cmd := exec.CommandContext(ctx, "cmux", "top", "--all", "--processes", "--format", "tsv")
	out, err := cmd.Output()
	if err != nil {
		return models.ProcessInfo{}, fmt.Errorf("cmux top failed: %w", err)
	}

	lines := strings.Split(string(out), "\n")
	
	type TopProcess struct {
		PID    int
		Parent string
		Name   string
	}
	type TopTag struct {
		Ref       string
		Workspace string
		Provider  string
	}
	
	var processes []TopProcess
	var tags []TopTag
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" { continue }
		
		parts := strings.Split(line, "\t")
		
		for i, p := range parts {
			p = strings.TrimSpace(p)
			if p == "tag" && i+3 < len(parts) {
				tags = append(tags, TopTag{
					Ref:       strings.TrimSpace(parts[i+1]),
					Workspace: strings.TrimSpace(parts[i+2]),
					Provider:  strings.TrimSpace(parts[i+3]),
				})
				break
			} else if p == "process" && i+3 < len(parts) {
				if pid, err := strconv.Atoi(strings.TrimSpace(parts[i+1])); err == nil {
					processes = append(processes, TopProcess{
						PID:    pid,
						Parent: strings.TrimSpace(parts[i+2]),
						Name:   strings.TrimSpace(parts[i+3]),
					})
				}
				break
			}
		}
	}

	// Build relations
	// PID -> []Parents
	pidParents := make(map[int][]string)
	for _, p := range processes {
		pidParents[p.PID] = append(pidParents[p.PID], p.Parent)
	}

	// Find the tagRef containing claude_code or codex
	var targetTagRef string
	for _, t := range tags {
		lowerRef := strings.ToLower(t.Ref)
		if strings.Contains(lowerRef, "claude_code") || strings.Contains(lowerRef, "codex") || strings.Contains(lowerRef, "gemini") {
			targetTagRef = t.Ref
			break
		}
	}

	var candidatePIDs []int
	var agentPID int

	for pid, parents := range pidParents {
		isOnSurface := false
		hasAgentTag := false
		
		for _, parent := range parents {
			if parent == s.surfaceID {
				isOnSurface = true
			}
			if targetTagRef != "" && parent == targetTagRef {
				hasAgentTag = true
			}
		}
		
		if isOnSurface {
			candidatePIDs = append(candidatePIDs, pid)
			if hasAgentTag {
				agentPID = pid
			}
		}
	}

	if agentPID > 0 {
		return s.resolveProcessInfo(agentPID)
	}

	if len(candidatePIDs) > 0 {
		minPID := candidatePIDs[0]
		for _, pid := range candidatePIDs {
			if pid < minPID {
				minPID = pid
			}
		}
		return s.resolveProcessInfo(minPID)
	}

	return models.ProcessInfo{}, fmt.Errorf("no processes found for surface %s", s.surfaceID)
}

func (s *CmuxSession) resolveProcessInfo(pid int) (models.ProcessInfo, error) {
	info := models.ProcessInfo{
		PID:    pid,
		PaneID: s.surfaceID,
	}

	// Fetch started at
	cmdTime := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "lstart=")
	outTime, err := cmdTime.Output()
	if err == nil {
		if t, err := time.ParseInLocation(time.ANSIC, strings.TrimSpace(string(outTime)), time.Local); err == nil {
			info.StartedAt = t
		}
	}

	// Fetch CWD
	cmdCwd := exec.Command("lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn")
	outCwd, err := cmdCwd.Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(outCwd)), "\n")
		for _, l := range lines {
			if strings.HasPrefix(l, "n") {
				info.CWD = strings.TrimPrefix(l, "n")
				break
			}
		}
	}

	return info, nil
}
