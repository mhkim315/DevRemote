package mux

import (
	"context"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"

	"devremote/companion-daemon/internal/models"
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

// ManagedLifecycle marks controlled_pty as a Pokit-managed runtime: Pokit owns
// the process/process group and its Stop/Kill cleanup. This is the only MVP
// adapter that opts in — external (tmux) and observer (cmux) adapters do not,
// even though they accept input/control.
func (a *controlledPTYAdapter) ManagedLifecycle() bool { return true }

// TerminateGroup signals the session's process group. pty.Start set Setsid, so
// the child is a session/process-group leader (pgid == child pid); signalling
// the negative pid reaches every member of THAT process group (the child and
// processes it starts within the same group, e.g. claude inside the controlled
// bash). A descendant that creates its own session/process group (setsid) is
// not covered — MVP Stop terminates the daemon-owned group, not arbitrary
// re-parented trees.
func (s *controlledPTYSession) TerminateGroup(force bool) error {
	if s.native == nil || s.native.Cmd == nil || s.native.Cmd.Process == nil {
		return fmt.Errorf("controlled_pty: no process to signal")
	}
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	return syscall.Kill(-s.native.Cmd.Process.Pid, sig)
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

func (a *controlledPTYAdapter) CreateSession(ctx context.Context, opts CreateOptions) (string, error) {
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

	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	var native *NativeSession
	var err error
	if opts.Executable != "" {
		// M1 safe path: exec the resolved binary directly with argv, no shell
		// interpretation — prevents shell-string injection.
		native, err = SpawnPTYWithDir(id, "xterm-256color", opts.CWD, opts.Executable, opts.Args...)
	} else {
		// Legacy path (CLI `pokit run`): run the command under bash -c.
		native, err = SpawnPTYWithDir(id, "xterm-256color", opts.CWD, "bash", "-c", command)
	}
	if err != nil {
		return "", fmt.Errorf("controlled_pty SpawnPTY: %w", err)
	}

	s := &controlledPTYSession{
		id:        id,
		title:     id,
		native:    native,
		startedAt: time.Now(),
	}
	a.sessions[id] = s

	// Watch for natural process exit.
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

// CreateSessionAndCapture implements SessionCreatorWithIdentity (PA3 Step 6a).
// Allocates the session ID under a.mu, releases the lock BEFORE SpawnPTY I/O,
// then re-acquires to install the session in the map. No adapter lock is held
// across external I/O — PA2d §5 invariant preserved.
func (a *controlledPTYAdapter) CreateSessionAndCapture(ctx context.Context, opts CreateOptions) (string, Session, error) {
	a.mu.Lock()
	a.nextID++
	id := opts.Name
	if id == "" {
		id = fmt.Sprintf("pty%d", a.nextID)
	}
	// Release a.mu BEFORE SpawnPTY — no adapter lock across I/O.
	a.mu.Unlock()

	command := opts.Command
	if command == "" {
		command = os.Getenv("SHELL")
		if command == "" {
			command = "bash"
		}
	}

	if ctx.Err() != nil {
		return "", nil, ctx.Err()
	}

	var native *NativeSession
	var err error
	if opts.Executable != "" {
		native, err = SpawnPTYWithDir(id, "xterm-256color", opts.CWD, opts.Executable, opts.Args...)
	} else {
		native, err = SpawnPTYWithDir(id, "xterm-256color", opts.CWD, "bash", "-c", command)
	}
	if err != nil {
		return "", nil, fmt.Errorf("controlled_pty SpawnPTY: %w", err)
	}

	s := &controlledPTYSession{
		id:        id,
		title:     id,
		native:    native,
		startedAt: time.Now(),
	}

	// Re-acquire to install the session atomically.
	a.mu.Lock()
	a.sessions[id] = s
	a.mu.Unlock()

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

	return id, s, nil
}

// PA2c-R6: shared detach primitive.  Locates and removes the session under
// a.mu, sets exited, and returns the native I/O handle.  Caller must call
// native.Close() outside every lock.  Returns nil on not-found or stale
// identity (nil native means nothing to close).
func (a *controlledPTYAdapter) detachLocked(localID string, expected Session) *NativeSession {
	s, ok := a.sessions[localID]
	if !ok {
		return nil
	}
	if expected != nil && s != expected {
		return nil
	}
	s.exited = true
	delete(a.sessions, localID)
	return s.native
}

func (a *controlledPTYAdapter) TerminateSession(_ context.Context, id string) error {
	a.mu.Lock()
	native := a.detachLocked(id, nil) // unconditional delete by local id
	a.mu.Unlock()

	if native == nil {
		return fmt.Errorf("%w: controlled_pty session %q not found", ErrSessionNotFound, id)
	}
	// All lifecycle locks released before native process I/O (PA2c-R6).
	if err := native.Close(); err != nil {
		return fmt.Errorf("controlled_pty terminate: %w", err)
	}
	return nil
}

// testCloseBarrier is a nil-in-production deterministic seam exercised only
// by the PA2c-R6 production-adapter tests. When non-nil it is called after
// detach (lock released) but before native.Close(). Production code path:
// nil → no-op; zero overhead.
var testCloseBarrier func()

// CompareAndTerminate implements SessionIdentityTerminator.  The identity
// comparison and session detachment happen under a single a.mu.Lock() — the
// PA2c-R6 atomic boundary — verifying the expected immutable session
// pointer is still the current entry, removing it from the map, and
// releasing the lock BEFORE native process/PTY Close I/O.  The detached
// session cannot receive new operations; a replacement inserted under the
// same local id between detach and Close is never touched.
func (a *controlledPTYAdapter) CompareAndTerminate(_ context.Context, localID string, expected Session) error {
	a.mu.Lock()
	s, ok := a.sessions[localID]
	if !ok {
		a.mu.Unlock()
		return ErrSessionNotFound
	}
	if s != expected {
		a.mu.Unlock()
		return ErrStaleSessionIdentity
	}
	native := a.detachLocked(localID, expected)
	a.mu.Unlock()

	if native == nil {
		// Should not happen: s matched expected and existed.
		return ErrSessionNotFound
	}
	// Deterministic test seam: nil in production, channel-barrier in tests.
	if testCloseBarrier != nil {
		testCloseBarrier()
	}
	if err := native.Close(); err != nil {
		return fmt.Errorf("controlled_pty atomic-terminate: %w", err)
	}
	return nil
}

// --- controlledPTYSession ---

type controlledPTYSession struct {
	id        string
	title     string
	exited    bool
	native    *NativeSession
	startedAt time.Time // spawn time, recorded at CreateSession (S1.1-B runtime identity)
}

func (s *controlledPTYSession) ID() string          { return s.id }
func (s *controlledPTYSession) Title() string       { return s.title }
func (s *controlledPTYSession) AdapterName() string { return "controlled_pty" }

// ProcessInfo exposes the daemon-owned child PID and the spawn StartedAt so the
// managed-launch correlation (S1.1-B) can bind agent status to the exact runtime
// instance. Both values come from state the adapter already owns — the child
// *exec.Cmd it started and the timestamp recorded at CreateSession — NOT from any
// ps/lsof scraping. This is supporting runtime identity only; it never selects
// agent status. Returns an error (no fabricated identity) if the process is gone.
func (s *controlledPTYSession) ProcessInfo(_ context.Context) (models.ProcessInfo, error) {
	if s.native == nil || s.native.Cmd == nil || s.native.Cmd.Process == nil {
		return models.ProcessInfo{}, fmt.Errorf("controlled_pty: no process")
	}
	return models.ProcessInfo{
		PID:       s.native.Cmd.Process.Pid,
		StartedAt: s.startedAt,
	}, nil
}

func (s *controlledPTYSession) OpenStream(_ context.Context) (TerminalStream, error) {
	return s.native, nil
}

func (s *controlledPTYSession) WriteInput(_ context.Context, data []byte) error {
	_, err := s.native.PTY.Write(data)
	return err
}
