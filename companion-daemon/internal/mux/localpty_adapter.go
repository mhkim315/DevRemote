package mux

import (
	"context"
	"fmt"
	"sync"
	"syscall"
	"time"
)

// localptyAdapter manages child PTY sessions owned by the daemon.
// Unlike tmux/cmux, sessions are created by the app (not discovered
// from an external session manager). Only active in-memory sessions
// appear in ListSessions.
type localptyAdapter struct {
	mu       sync.Mutex
	sessions map[string]*localptySession
	nextID   int
}

// NewLocalPTYAdapter creates a LocalPTY adapter with an empty session map.
func NewLocalPTYAdapter() Adapter {
	return &localptyAdapter{
		sessions: make(map[string]*localptySession),
	}
}

func (a *localptyAdapter) Name() string { return "localpty" }

func (a *localptyAdapter) ListSessions(ctx context.Context) ([]Session, error) {
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

func (a *localptyAdapter) CreateSession(_ context.Context, opts CreateOptions) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.nextID++
	id := opts.Name
	if id == "" {
		id = fmt.Sprintf("s%d", a.nextID)
	}

	command := opts.Command
	if command == "" {
		command = "bash"
	}

	native, err := SpawnPTY(id, "xterm-256color", command)
	if err != nil {
		return "", fmt.Errorf("localpty SpawnPTY: %w", err)
	}

	s := &localptySession{
		id:     id,
		title:  id,
		native: native,
	}
	a.sessions[id] = s

	// Watch for natural process exit via Signal(0) polling.
	// Avoids data race from dual cmd.Wait() (SpawnPTY also calls Wait).
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

func (a *localptyAdapter) TerminateSession(_ context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	s, ok := a.sessions[id]
	if !ok {
		return fmt.Errorf("%w: localpty session %q not found", ErrSessionNotFound, id)
	}
	s.exited = true
	if err := s.native.Close(); err != nil {
		return fmt.Errorf("localpty terminate: %w", err)
	}
	delete(a.sessions, id)
	return nil
}

// --- localptySession ---

type localptySession struct {
	id     string
	title  string
	exited bool
	native *NativeSession
}

func (s *localptySession) ID() string          { return s.id }
func (s *localptySession) Title() string       { return s.title }
func (s *localptySession) AdapterName() string { return "localpty" }

func (s *localptySession) OpenStream(_ context.Context) (TerminalStream, error) {
	return s.native, nil
}

// WriteInput sends keystrokes to the PTY stdin.
func (s *localptySession) WriteInput(_ context.Context, data []byte) error {
	_, err := s.native.PTY.Write(data)
	return err
}
