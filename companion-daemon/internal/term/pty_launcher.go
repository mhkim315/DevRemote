package term

import (
	"context"
	"fmt"

	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/sessionid"
)

// PB.5a: ManagedPTYLauncher is the narrow platform-neutral PTY spawn boundary.
// It exposes only the operations OwnedPTYRuntime needs for managed session
// creation and termination. It does NOT expose registry, discovery, adapter
// selection, or legacy list/get APIs.
//
// OwnedPTYRuntime uses this interface instead of directly depending on
// mux.Adapter, which carries legacy baggage (ListSessions, GetSession,
// capability interfaces, etc.).

// ManagedPTYLauncher is the retained managed PTY creation + termination boundary.
type ManagedPTYLauncher interface {
	// Name returns the adapter name ("controlled_pty").
	Name() string

	// CreateSessionAndCapture atomically creates a PTY session and captures
	// its identity. Returns the adapter-local ID and the session handle.
	CreateSessionAndCapture(ctx context.Context, opts mux.CreateOptions) (localID string, sess mux.Session, err error)

	// CreateSession creates a PTY session (pre-PA3 legacy path).
	CreateSession(ctx context.Context, opts mux.CreateOptions) (string, error)

	// ListSessions returns all launcher-owned sessions.
	ListSessions(ctx context.Context) ([]mux.Session, error)

	// CompareAndTerminate atomically terminates the session if its identity
	// matches the expected session. Returns ErrSessionNotFound if not found,
	// or ErrSessionIdentityMismatch if identity doesn't match.
	CompareAndTerminate(ctx context.Context, canonicalID string, expected mux.Session) error

	// TerminateSession terminates a session by adapter-local ID.
	TerminateSession(ctx context.Context, localID string) error
}

// controlledPTYLauncher wraps the mux controlled_pty adapter behind the
// narrow ManagedPTYLauncher boundary. It delegates to the adapter but
// limits its surface to only the methods OwnedPTYRuntime needs.
type controlledPTYLauncher struct {
	adapter mux.Adapter
}

// NewControlledPTYLauncher creates a ManagedPTYLauncher backed by the
// controlled_pty adapter. The adapter must satisfy SessionCreator,
// SessionCreatorWithIdentity, and SessionIdentityTerminator.
func NewControlledPTYLauncher(adapter mux.Adapter) (ManagedPTYLauncher, error) {
	if _, ok := adapter.(mux.SessionCreator); !ok {
		return nil, fmt.Errorf("adapter does not support SessionCreator")
	}
	if _, ok := adapter.(mux.SessionCreatorWithIdentity); !ok {
		return nil, fmt.Errorf("adapter does not support SessionCreatorWithIdentity")
	}
	if _, ok := adapter.(mux.SessionIdentityTerminator); !ok {
		return nil, fmt.Errorf("adapter does not support SessionIdentityTerminator")
	}
	return &controlledPTYLauncher{adapter: adapter}, nil
}

func (l *controlledPTYLauncher) Name() string {
	return l.adapter.Name()
}

func (l *controlledPTYLauncher) CreateSessionAndCapture(ctx context.Context, opts mux.CreateOptions) (string, mux.Session, error) {
	sci := l.adapter.(mux.SessionCreatorWithIdentity)
	return sci.CreateSessionAndCapture(ctx, opts)
}

func (l *controlledPTYLauncher) CreateSession(ctx context.Context, opts mux.CreateOptions) (string, error) {
	sc := l.adapter.(mux.SessionCreator)
	return sc.CreateSession(ctx, opts)
}

func (l *controlledPTYLauncher) ListSessions(ctx context.Context) ([]mux.Session, error) {
	return l.adapter.ListSessions(ctx)
}

func (l *controlledPTYLauncher) CompareAndTerminate(ctx context.Context, canonicalID string, expected mux.Session) error {
	sit := l.adapter.(mux.SessionIdentityTerminator)
	ref := sessionid.ParseSessionID(canonicalID)
	return sit.CompareAndTerminate(ctx, ref.LocalID, expected)
}

func (l *controlledPTYLauncher) TerminateSession(ctx context.Context, localID string) error {
	st := l.adapter.(mux.SessionTerminator)
	return st.TerminateSession(ctx, localID)
}
