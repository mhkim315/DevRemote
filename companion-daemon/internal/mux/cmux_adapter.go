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
//   - surface:1  terminal  [focused]  "My Panel Title"
//     surface:5  terminal  "Another"
var panelRe = regexp.MustCompile(`(surface:\d+)\s+\S+(?:\s+\[.*?\])?\s+"(.*?)"`)

func (a *cmuxAdapter) ListSessions() ([]Session, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "cmux", "tree", "--all").Output()
	if err != nil {
		return nil, fmt.Errorf("cmux tree failed: %w", err)
	}

	return parseCmuxTree(out)
}

var (
	workspaceRe = regexp.MustCompile(`\bworkspace\s+(workspace:[^\s]+)`)
	surfaceRe   = regexp.MustCompile(`\bsurface\s+(surface:[^\s]+)\s+\[([^\]]+)\]\s+"([^"]*)"`)
)

func parseCmuxTree(out []byte) ([]Session, error) {
	var sessions []Session
	lines := strings.Split(string(out), "\n")

	var currentWorkspace string
	var sawTreeStructure bool
	var sawTerminalSurfaceLine bool

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if strings.Contains(line, "window") || strings.Contains(line, "workspace") {
			sawTreeStructure = true
		}
		if strings.Contains(line, "surface") {
			if strings.Contains(line, "[terminal]") {
				sawTerminalSurfaceLine = true
			}
		}

		if m := workspaceRe.FindStringSubmatch(line); m != nil {
			currentWorkspace = m[1]
			continue
		}

		if m := surfaceRe.FindStringSubmatch(line); m != nil {
			surfaceID := m[1]
			surfaceType := m[2]
			title := m[3]

			if surfaceType != "terminal" {
				continue
			}

			sessions = append(sessions, &CmuxSession{
				id:          surfaceID,
				title:       title,
				surfaceID:   surfaceID,
				workspaceID: currentWorkspace,
			})
		}
	}

	// We only error out if we explicitly saw a terminal surface line but failed to parse it.
	// If we only saw [browser] surfaces (or no surfaces at all), we return the empty slice without error.
	if sawTreeStructure && sawTerminalSurfaceLine && len(sessions) == 0 {
		return nil, fmt.Errorf("unexpected format: saw terminal surface lines but failed to parse them")
	}

	return sessions, nil
}

func (a *cmuxAdapter) GetSession(id string) (Session, error) {
	if !strings.HasPrefix(id, "surface:") {
		return nil, fmt.Errorf("invalid cmux session id format: %s", id)
	}
	numStr := strings.TrimPrefix(id, "surface:")
	if _, err := strconv.Atoi(numStr); err != nil {
		return nil, fmt.Errorf("invalid cmux session id format: %s", id)
	}

	return &CmuxSession{
		id:        id,
		title:     "cmux panel",
		surfaceID: id,
	}, nil
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
	// Preflight validation to ensure the surface is alive
	if _, err := s.ReadScreen(ctx); err != nil {
		return nil, fmt.Errorf("preflight read-screen failed: %w", err)
	}

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

func (s *CmuxSession) AdapterName() string { return "cmux" }
func (s *CmuxSession) ID() string          { return s.id }
func (s *CmuxSession) Title() string       { return s.title }

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

// CmuxTopSnapshot represents the raw parsed data from cmux top
type CmuxTopSnapshot struct {
	Processes []TopProcess
	Tags      []TopTag
}

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

type AgentProcess struct {
	PID      int
	Provider string
}

func parseCmuxTop(out []byte) (CmuxTopSnapshot, error) {
	lines := strings.Split(string(out), "\n")
	var snap CmuxTopSnapshot

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, "\t")

		for i, p := range parts {
			p = strings.TrimSpace(p)
			if p == "tag" && i+3 < len(parts) {
				snap.Tags = append(snap.Tags, TopTag{
					Ref:       strings.TrimSpace(parts[i+1]),
					Workspace: strings.TrimSpace(parts[i+2]),
					Provider:  strings.TrimSpace(parts[i+3]),
				})
				break
			} else if p == "process" && i+3 < len(parts) {
				if pid, err := strconv.Atoi(strings.TrimSpace(parts[i+1])); err == nil {
					snap.Processes = append(snap.Processes, TopProcess{
						PID:    pid,
						Parent: strings.TrimSpace(parts[i+2]),
						Name:   strings.TrimSpace(parts[i+3]),
					})
				}
				break
			}
		}
	}
	return snap, nil
}

func mapAgentProcesses(snap CmuxTopSnapshot) (map[string]AgentProcess, error) {
	agentTags := make(map[string]string)
	for _, t := range snap.Tags {
		lowerRef := strings.ToLower(t.Ref)
		lowerProvider := strings.ToLower(t.Provider)
		if strings.Contains(lowerRef, "claude_code") || strings.Contains(lowerProvider, "claude_code") {
			agentTags[t.Ref] = "claude"
		} else if strings.Contains(lowerRef, "codex") || strings.Contains(lowerProvider, "codex") {
			agentTags[t.Ref] = "codex"
		} else if strings.Contains(lowerRef, "gemini") || strings.Contains(lowerProvider, "gemini") {
			agentTags[t.Ref] = "gemini"
		}
	}

	pidParents := make(map[int][]string)
	pidNames := make(map[int]string)
	for _, p := range snap.Processes {
		pidParents[p.PID] = append(pidParents[p.PID], p.Parent)
		pidNames[p.PID] = p.Name
	}

	result := make(map[string]AgentProcess)
	surfaceCandidates := make(map[string][]int) // surfaceID -> PIDs

	for pid, parents := range pidParents {
		var surfaceID string
		var provider string

		for _, parent := range parents {
			if strings.HasPrefix(parent, "surface:") {
				surfaceID = parent
			}
			if p, ok := agentTags[parent]; ok {
				provider = p
			}
		}

		if provider == "" {
			name := strings.ToLower(pidNames[pid])
			if strings.Contains(name, "claude") {
				provider = "claude"
			} else if strings.Contains(name, "codex") {
				provider = "codex"
			} else if strings.Contains(name, "gemini") {
				provider = "gemini"
			}
		}

		if surfaceID != "" {
			surfaceCandidates[surfaceID] = append(surfaceCandidates[surfaceID], pid)
			if provider != "" {
				if existing, ok := result[surfaceID]; ok {
					return nil, fmt.Errorf("ambiguous agent processes on surface %s: %v and %v", surfaceID, existing.PID, pid)
				}
				result[surfaceID] = AgentProcess{
					PID:      pid,
					Provider: provider,
				}
			}
		}
	}

	// Fallback to min PID if no agent tag was found
	for surfaceID, pids := range surfaceCandidates {
		if _, ok := result[surfaceID]; !ok && len(pids) > 0 {
			minPID := pids[0]
			for _, pid := range pids {
				if pid < minPID {
					minPID = pid
				}
			}
			result[surfaceID] = AgentProcess{PID: minPID}
		}
	}

	return result, nil
}

func (s *CmuxSession) ProcessInfo(ctx context.Context) (models.ProcessInfo, error) {
	cmd := exec.CommandContext(ctx, "cmux", "top", "--all", "--processes", "--format", "tsv")
	out, err := cmd.Output()
	if err != nil {
		return models.ProcessInfo{}, fmt.Errorf("cmux top failed: %w", err)
	}

	snap, err := parseCmuxTop(out)
	if err != nil {
		return models.ProcessInfo{}, err
	}

	mapped, err := mapAgentProcesses(snap)
	if err != nil {
		return models.ProcessInfo{}, err
	}

	if ap, ok := mapped[s.surfaceID]; ok {
		return s.resolveProcessInfo(ap.PID)
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

func (a *cmuxAdapter) ProcessSnapshot(ctx context.Context) (map[string]models.ProcessInfo, error) {
	cmd := exec.CommandContext(ctx, "cmux", "top", "--all", "--processes", "--format", "tsv")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("cmux top failed: %w", err)
	}

	snap, err := parseCmuxTop(out)
	if err != nil {
		return nil, err
	}

	mapped, err := mapAgentProcesses(snap)
	if err != nil {
		return nil, err
	}

	result := make(map[string]models.ProcessInfo)
	for surfaceID, ap := range mapped {
		dummy := &CmuxSession{surfaceID: surfaceID}
		if info, err := dummy.resolveProcessInfo(ap.PID); err == nil {
			result[surfaceID] = info
		}
	}
	return result, nil
}
