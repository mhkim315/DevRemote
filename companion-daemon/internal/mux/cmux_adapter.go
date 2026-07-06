package mux

import (
	"context"
	"fmt"
	"log"

	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"devremote/companion-daemon/internal/models"
)

// CmuxError represents a structured error returned by the cmux runner
type CmuxError struct {
	Path     string
	Args     []string
	ExitCode int
	Stderr   string
	Duration time.Duration
	Err      error
}

func (e *CmuxError) Error() string {
	// Provide a sanitized summary for clients.
	// The detailed log (with Stderr, Path) should be written to the server log by the caller.
	operation := "command"
	if len(e.Args) > 0 {
		operation = e.Args[0]
	}
	if e.ExitCode == -1 {
		return fmt.Sprintf("cmux %s timed out or failed to start", operation)
	}
	return fmt.Sprintf("cmux %s failed (exit %d)", operation, e.ExitCode)
}

func (e *CmuxError) Unwrap() error {
	return e.Err
}

type CommandOptions struct {
	Dir string
}

// CommandRunner allows injecting a mock executor for testing
type CommandRunner interface {
	Run(ctx context.Context, opts CommandOptions, args ...string) ([]byte, error)
}

// serialCommandRunner protects cmux's single Unix socket from command bursts.
// Every CmuxSession created by an adapter shares this runner, so discovery,
// telemetry, and live-screen polling cannot write to the socket concurrently.
type serialCommandRunner struct {
	mu       sync.Mutex
	delegate CommandRunner
}

func (r *serialCommandRunner) Run(ctx context.Context, opts CommandOptions, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.delegate.Run(ctx, opts, args...)
}

type execCommandRunner struct {
	binaryPath string
	lookupErr  error
}

func newExecCommandRunner() *execCommandRunner {
	path, err := exec.LookPath("cmux")
	if err != nil {
		path = "cmux"
	}
	return &execCommandRunner{binaryPath: path, lookupErr: err}
}

func (r *execCommandRunner) Run(ctx context.Context, opts CommandOptions, args ...string) ([]byte, error) {
	start := time.Now()
	// Copy args to prevent modification
	argsCopy := make([]string, len(args))
	copy(argsCopy, args)

	if r.lookupErr != nil {
		ce := &CmuxError{
			Path:     r.binaryPath,
			Args:     argsCopy,
			Err:      r.lookupErr,
			Duration: time.Since(start),
			ExitCode: -1,
		}
		logCmuxError(ce)
		return nil, ce
	}

	cmd := exec.CommandContext(ctx, r.binaryPath, argsCopy...)
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}

	out, err := cmd.Output() // We use Output(), which gives stdout. If it fails, error often is ExitError holding Stderr
	duration := time.Since(start)

	if err != nil {
		ce := &CmuxError{
			Path:     r.binaryPath,
			Args:     argsCopy,
			Err:      err,
			Duration: duration,
			ExitCode: -1, // default to -1 for timeout/not started
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			ce.ExitCode = exitErr.ExitCode()
			ce.Stderr = strings.TrimSpace(string(exitErr.Stderr))
		}
		logCmuxError(ce)
		return nil, ce
	}
	return out, nil
}

func logCmuxError(err *CmuxError) {
	log.Printf("[Cmux Runner] ERROR: path=%s args=%v exit=%d duration=%v err=%v stderr=%q", err.Path, err.Args, err.ExitCode, err.Duration, err.Err, err.Stderr)
}

type cmuxAdapter struct {
	runner CommandRunner
	health RegistryHealth
}

func NewCmuxAdapter(health RegistryHealth) (Adapter, error) {
	if health == nil {
		return nil, fmt.Errorf("cmux adapter requires RegistryHealth")
	}
	return &cmuxAdapter{
		runner: &serialCommandRunner{delegate: newExecCommandRunner()},
		health: health,
	}, nil
}

func (a *cmuxAdapter) Name() string {
	return "cmux"
}

// parsePanelLine extracts surface ID and title from cmux output like:
//
//   - surface:1  terminal  [focused]  "My Panel Title"
//     surface:5  terminal  "Another"
var panelRe = regexp.MustCompile(`(surface:\d+)\s+\S+(?:\s+\[.*?\])?\s+"(.*?)"`)

func (a *cmuxAdapter) ListSessions(ctx context.Context) ([]Session, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	out, err := a.runner.Run(ctx, CommandOptions{}, "tree", "--all")
	if err != nil {
		return nil, fmt.Errorf("cmux tree failed: %w", err)
	}

	return parseCmuxTree(out, a.runner, a.health)
}

var (
	workspaceRe = regexp.MustCompile(`\bworkspace\s+(workspace:[^\s]+)`)
	surfaceRe   = regexp.MustCompile(`\bsurface\s+(surface:[^\s]+)\s+\[([^\]]+)\]\s+"([^"]*)"`)
)

func parseCmuxTree(out []byte, runner CommandRunner, health RegistryHealth) ([]Session, error) {
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
				runner:      runner,
				health:      health,
			})
		}
	}

	// If the output is completely empty or doesn't even contain "window" or "workspace", it's an error.
	if !sawTreeStructure {
		return nil, fmt.Errorf("unexpected format: output is completely empty or missing window/workspace structure")
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
		runner:    a.runner,
		health:    a.health,
	}, nil
}

// CmuxSession implements the Session interface for a cmux panel
type CmuxSession struct {
	id          string
	title       string
	surfaceID   string
	workspaceID string
	pid         int
	runner      CommandRunner
	health      RegistryHealth
}

type CmuxStream struct {
	session *CmuxSession
	pr      *io.PipeReader
	pw      *io.PipeWriter
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	once    sync.Once
}

func (s *CmuxStream) pollScreen(initialFrame []byte) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastContent string
	consecutiveErrs := 0
	maxErrs := 3

	// Send initial frame immediately if valid
	if len(initialFrame) > 0 {
		currentContent := string(initialFrame)
		payload := "\033[2J\033[H" + currentContent
		payload = strings.ReplaceAll(payload, "\n", "\r\n")
		if _, err := s.pw.Write([]byte(payload)); err != nil {
			return
		}
		lastContent = currentContent
	}

	// Helper to run one poll iteration
	pollOnce := func() error {
		ctx, cancel := context.WithTimeout(s.ctx, 1500*time.Millisecond)
		defer cancel()

		out, err := s.session.runner.Run(ctx, CommandOptions{}, "read-screen", "--surface", s.session.surfaceID)
		if err != nil {
			return err
		}

		currentContent := string(out)
		if currentContent != lastContent {
			// Clear screen and redraw for xterm.js
			payload := "\033[2J\033[H" + currentContent
			// Ensure CRLF for xterm.js line breaks
			payload = strings.ReplaceAll(payload, "\n", "\r\n")

			if _, err := s.pw.Write([]byte(payload)); err != nil {
				return err
			}
			lastContent = currentContent
		}
		return nil
	}

	// First iteration logic is removed since initial frame is handled above

	for {
		select {
		case <-s.ctx.Done(): // Context cancellation check
			return
		case <-ticker.C:
			if err := pollOnce(); err != nil {
				consecutiveErrs++
				if consecutiveErrs >= maxErrs {
					// Force adapter refresh to update health status
					_, refreshErr := s.session.health.Refresh(s.ctx, "cmux", true)
					if refreshErr != nil {
						log.Printf("cmux health refresh failed: %v", refreshErr)
					}
					// Close with error to notify reader
					s.pw.CloseWithError(fmt.Errorf("cmux read-screen failed %d times: %v", maxErrs, err))
					return
				}
			} else {
				consecutiveErrs = 0
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
		s.cancel() // Cancel context to stop polling
		s.pw.Close()
		s.pr.Close()
	})
	return nil
}

func (s *CmuxStream) Resize(rows, cols int) error {
	return nil // cmux doesn't natively support dynamic resizing without changing the backend tty
}

func (s *CmuxSession) OpenStream(ctx context.Context) (TerminalStream, error) {
	// Preflight validation to ensure the surface is alive and to capture the first frame
	initialFrame, err := s.ReadScreen(ctx)
	if err != nil {
		return nil, fmt.Errorf("preflight read-screen failed: %w", err)
	}

	streamCtx, cancel := context.WithCancel(ctx)
	pr, pw := io.Pipe()
	stream := &CmuxStream{
		session: s,
		pr:      pr,
		pw:      pw,
		ctx:     streamCtx,
		cancel:  cancel,
	}
	go stream.pollScreen(initialFrame)
	return stream, nil
}

func (s *CmuxSession) AdapterName() string { return "cmux" }
func (s *CmuxSession) ID() string          { return s.id }
func (s *CmuxSession) Title() string       { return s.title }

func (s *CmuxSession) ReadScreen(ctx context.Context) ([]byte, error) {
	return s.runner.Run(ctx, CommandOptions{}, "read-screen", "--surface", s.surfaceID)
}

func (s *CmuxSession) ReadHistory(ctx context.Context, lines int) ([]byte, error) {
	if lines < 1 {
		lines = 1000
	} else if lines > 10000 {
		lines = 10000
	}
	return s.runner.Run(ctx, CommandOptions{}, "read-screen", "--surface", s.surfaceID, "--scrollback", "--lines", fmt.Sprintf("%d", lines))
}

func (s *CmuxSession) WriteInput(ctx context.Context, data []byte) error {
	// Parse special keys for cmux since `cmux send` doesn't handle raw ANSI well
	str := string(data)
	if str == "\x03" {
		return s.WriteKey(ctx, "ctrl-c")
	} else if str == "\x1b" {
		return s.WriteKey(ctx, "escape")
	} else if str == "\x1b[A" {
		return s.WriteKey(ctx, "up")
	} else if str == "\x1b[B" {
		return s.WriteKey(ctx, "down")
	} else if str == "\x1b[C" {
		return s.WriteKey(ctx, "right")
	} else if str == "\x1b[D" {
		return s.WriteKey(ctx, "left")
	}

	// Mobile clients commonly send a submitted command and its newline in one
	// WebSocket message (for example, "ls\n"). cmux `send` treats that newline
	// as text rather than an Enter key, so split line endings into send-key
	// calls. Treat CRLF as one Enter while preserving repeated independent
	// newlines.
	textStart := 0
	for i := 0; i < len(str); i++ {
		if str[i] != '\r' && str[i] != '\n' {
			continue
		}
		if textStart < i {
			if _, err := s.runner.Run(ctx, CommandOptions{}, "send", "--surface", s.surfaceID, str[textStart:i]); err != nil {
				return err
			}
		}
		if err := s.WriteKey(ctx, "enter"); err != nil {
			return err
		}
		if str[i] == '\r' && i+1 < len(str) && str[i+1] == '\n' {
			i++
		}
		textStart = i + 1
	}
	if textStart < len(str) {
		_, err := s.runner.Run(ctx, CommandOptions{}, "send", "--surface", s.surfaceID, str[textStart:])
		return err
	}
	return nil
}

func (s *CmuxSession) WriteKey(ctx context.Context, key string) error {
	_, err := s.runner.Run(ctx, CommandOptions{}, "send-key", "--surface", s.surfaceID, key)
	return err
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

	// CommandRunner Options: pass Dir via CommandOptions
	out, err := a.runner.Run(ctx, CommandOptions{Dir: opts.CWD}, "new-surface", "--type", "terminal", "--workspace", ws)
	if err != nil {
		return "", fmt.Errorf("cmux new-surface failed: %v, out: %s", err, string(out))
	}

	// Parse output to find "surface:NN" — return local ID only.
	// Phase 1: handler canonicalizes to cmux:surface:NN.
	outStr := string(out)
	re := regexp.MustCompile(`surface:\s*(\d+)`)
	if m := re.FindStringSubmatch(outStr); m != nil {
		return "surface:" + m[1], nil
	}

	return "", fmt.Errorf("cmux new-surface: could not parse surface ID from output: %s", outStr)
}

func (a *cmuxAdapter) TerminateSession(ctx context.Context, rawID string) error {
	if !strings.HasPrefix(rawID, "surface:") {
		return fmt.Errorf("invalid surface ID format (must be surface:<id>)")
	}
	_, err := a.runner.Run(ctx, CommandOptions{}, "close-surface", "--surface", rawID)
	return err
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

type ProcessSnapshotResult struct {
	Processes map[string]AgentProcess
	Errors    map[string]error
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

func mapAgentProcesses(snap CmuxTopSnapshot) (ProcessSnapshotResult, error) {
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

	result := ProcessSnapshotResult{
		Processes: make(map[string]AgentProcess),
		Errors:    make(map[string]error),
	}
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
				if existing, ok := result.Processes[surfaceID]; ok {
					result.Errors[surfaceID] = fmt.Errorf("ambiguous agent processes on surface %s: %v and %v", surfaceID, existing.PID, pid)
					delete(result.Processes, surfaceID) // Remove the ambiguous entry
				} else if _, hasErr := result.Errors[surfaceID]; !hasErr {
					result.Processes[surfaceID] = AgentProcess{
						PID:      pid,
						Provider: provider,
					}
				}
			}
		}
	}

	// Fallback to min PID if no agent tag was found and no error occurred
	for surfaceID, pids := range surfaceCandidates {
		if _, hasErr := result.Errors[surfaceID]; hasErr {
			continue
		}
		if _, ok := result.Processes[surfaceID]; !ok && len(pids) > 0 {
			minPID := pids[0]
			for _, pid := range pids {
				if pid < minPID {
					minPID = pid
				}
			}
			result.Processes[surfaceID] = AgentProcess{PID: minPID}
		}
	}

	return result, nil
}

func (s *CmuxSession) ProcessInfo(ctx context.Context) (models.ProcessInfo, error) {
	out, err := s.runner.Run(ctx, CommandOptions{}, "top", "--all", "--processes", "--format", "tsv")
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

	if err, ok := mapped.Errors[s.surfaceID]; ok {
		return models.ProcessInfo{}, err
	}

	if ap, ok := mapped.Processes[s.surfaceID]; ok {
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
	out, err := a.runner.Run(ctx, CommandOptions{}, "top", "--all", "--processes", "--format", "tsv")
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
	for surfaceID, ap := range mapped.Processes {
		dummy := &CmuxSession{surfaceID: surfaceID}
		if info, err := dummy.resolveProcessInfo(ap.PID); err == nil {
			result[surfaceID] = info
		}
	}
	return result, nil
}
