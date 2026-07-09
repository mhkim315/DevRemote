package mux

import (
	"context"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"
)

// controlledPTYAdapter manages child PTY sessions owned by the daemon.
// This is the first-class Control Adapter for Pokit-owned PTY runtime.
// Sessions are created by the app, not discovered from external managers.
type controlledPTYAdapter struct {
	mu       sync.Mutex
	sessions map[string]*controlledPTYSession
	nextID   int
}

// NewControlledPTYAdapter creates a Controlled PTY adapter.
func NewControlledPTYAdapter() Adapter {
	return &controlledPTYAdapter{
		sessions: make(map[string]*controlledPTYSession),
	}
}

func (a *controlledPTYAdapter) Name() string { return "controlled_pty" }

func (a *controlledPTYAdapter) TranscriptCaptureMode() TranscriptCaptureMode {
	return CaptureModeByteStream
}

func (a *controlledPTYAdapter) ListSessions(ctx context.Context) ([]Session, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Session, 0, len(a.sessions))
	for _, s := range a.sessions {
		if s.exited {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func (a *controlledPTYAdapter) CreateSession(_ context.Context, opts CreateOptions) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.nextID++
	id := opts.Name
	if id == "" {
		id = fmt.Sprintf("pty%d", a.nextID)
	}

	command := opts.Command
	if command == "" {
		command = os.Getenv("SHELL")
		if command == "" {
			command = "bash"
		}
	}

	// Support cwd if provided.
	if opts.CWD != "" {
		command = fmt.Sprintf("cd %s && exec %s", opts.CWD, command)
	}

	native, err := SpawnPTY(id, "xterm-256color", "bash", "-c", command)
	if err != nil {
		return "", fmt.Errorf("controlled_pty SpawnPTY: %w", err)
	}

	s := &controlledPTYSession{
		id:     id,
		title:  id,
		native: native,
	}
	a.sessions[id] = s

	// Watch for natural process exit via Signal(0) polling.
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			if native.Cmd.Process == nil {
				break
			}
			if err := native.Cmd.Process.Signal(syscall.Signal(0)); err != nil {
				a.mu.Lock()
				s.exited = true
				a.mu.Unlock()
				return
			}
		}
	}()

	return id, nil
}

func (a *controlledPTYAdapter) TerminateSession(_ context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	s, ok := a.sessions[id]
	if !ok {
		return fmt.Errorf("%w: controlled_pty session %q not found", ErrSessionNotFound, id)
	}
	s.exited = true
	if err := s.native.Close(); err != nil {
		return fmt.Errorf("controlled_pty terminate: %w", err)
	}
	delete(a.sessions, id)
	return nil
}

// --- controlledPTYSession ---

type controlledPTYSession struct {
	id     string
	title  string
	exited bool
	native *NativeSession
}

func (s *controlledPTYSession) ID() string          { return s.id }
func (s *controlledPTYSession) Title() string       { return s.title }
func (s *controlledPTYSession) AdapterName() string { return "controlled_pty" }

func (s *controlledPTYSession) OpenStream(_ context.Context) (TerminalStream, error) {
	return s.native, nil
}

func (s *controlledPTYSession) WriteInput(_ context.Context, data []byte) error {
	_, err := s.native.PTY.Write(data)
	return err
}
