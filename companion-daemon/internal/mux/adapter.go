package mux

import (
	"context"
	"errors"
	"io"

	"devremote/companion-daemon/internal/models"
)

// Sentinel errors for adapter operations. Handlers use errors.Is to distinguish
// not-found, unavailable, unsupported, and timeout conditions.
var (
	ErrSessionNotFound    = errors.New("session not found")
	ErrAdapterUnavailable = errors.New("adapter unavailable")
	ErrUnsupported        = errors.New("operation not supported by adapter")
	ErrTimeout            = errors.New("operation timed out")
	ErrInvalidSessionID   = errors.New("invalid session ID")
	ErrDuplicateAdapter   = errors.New("duplicate adapter name")
)

// Session represents an abstract terminal session that can be viewed and controlled remotely.
type Session interface {
	// ID returns the unique identifier for the session
	ID() string

	// AdapterName returns the name of the adapter managing this session
	AdapterName() string

	// Title returns the display name or title of the session
	Title() string
}

// TerminalStream represents an open PTY connection to a session
type TerminalStream interface {
	io.ReadWriteCloser
	Resize(rows, cols int) error
}

// StreamOpener represents the ability to open a live PTY stream to the session
type StreamOpener interface {
	OpenStream(ctx context.Context) (TerminalStream, error)
}

// InputWriter represents the ability to send keystrokes/input to the session.
// Distinct from TerminalStream.Write which writes raw bytes to a PTY stream.
type InputWriter interface {
	WriteInput(ctx context.Context, data []byte) error
}

// ScreenReader represents the ability to take a one-shot snapshot of the terminal screen
type ScreenReader interface {
	ReadScreen(ctx context.Context) ([]byte, error)
}

// ProcessProvider represents the ability to resolve the actual shell PID running inside the session
type ProcessProvider interface {
	ProcessInfo(ctx context.Context) (models.ProcessInfo, error)
}

// ProcessSnapshotProvider represents the ability to fetch all process info in a single batch
type ProcessSnapshotProvider interface {
	ProcessSnapshot(ctx context.Context) (map[string]models.ProcessInfo, error)
}

// HistoryReader represents the ability to read the scrollback buffer of the session
type HistoryReader interface {
	ReadHistory(ctx context.Context, lines int) ([]byte, error)
}

// KeyWriter represents the ability to send special keys to the terminal
type KeyWriter interface {
	WriteKey(ctx context.Context, key string) error
}

// SessionCreator represents an adapter that can spawn new sessions
type SessionCreator interface {
	CreateSession(ctx context.Context, opts CreateOptions) (string, error)
}

// SessionTerminator represents an adapter that can kill sessions
type SessionTerminator interface {
	TerminateSession(ctx context.Context, id string) error
}

type CreateOptions struct {
	Name        string
	WorkspaceID string
	CWD         string
	Command     string
}

// RegistryHealth is the minimal callback interface an adapter needs to
// report session list changes without depending on the concrete Registry type.
// Phase 3 deferred: replace with InvalidationSender (one-way signal) to
// fully decouple adapter from Registry. See ADAPTER_EXPANSION_PLAN.md §5 Phase 3.
type RegistryHealth interface {
	Refresh(ctx context.Context, name string, force bool) (AdapterSnapshot, error)
}

// InvalidationSender is a one-way signal that an adapter's session list has changed.
// Unlike RegistryHealth, the adapter does not wait for or depend on the refresh result.
// Phase 3: cmux adapter will use this instead of RegistryHealth.
type InvalidationSender interface {
	Invalidate()
}

// Adapter defines the interface for different session backends (Native, cmux, tmux).
// Phase 1: ListSessions accepts context. GetSession is removed from mandatory
// contract; lookup uses snapshot refresh or optional SessionLookup capability.
type Adapter interface {
	Name() string
	ListSessions(ctx context.Context) ([]Session, error)
}
