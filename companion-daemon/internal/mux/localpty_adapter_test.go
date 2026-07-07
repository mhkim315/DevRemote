package mux

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// --- Mock session (no real PTY needed for contract tests) ---

type mockLocalPTYSession struct {
	id    string
	title string
}

func (s *mockLocalPTYSession) ID() string          { return s.id }
func (s *mockLocalPTYSession) Title() string       { return s.title }
func (s *mockLocalPTYSession) AdapterName() string { return "localpty" }
func (s *mockLocalPTYSession) OpenStream(_ context.Context) (TerminalStream, error) {
	return &mockLocalPTYStream{pr: strings.NewReader("localpty stream content")}, nil
}

type mockLocalPTYStream struct {
	pr     *strings.Reader
	mu     sync.Mutex
	closed bool
}

func (s *mockLocalPTYStream) Read(p []byte) (int, error) { return s.pr.Read(p) }
func (s *mockLocalPTYStream) Write(p []byte) (int, error) { return len(p), nil }
func (s *mockLocalPTYStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}
func (s *mockLocalPTYStream) Resize(rows, cols int) error { return nil }

// --- Mock adapter ---

type mockLocalPTYAdapter struct {
	mu       sync.Mutex
	sessions map[string]*mockLocalPTYSession
	nextID   int
}

func newMockLocalPTYAdapter() Adapter {
	return &mockLocalPTYAdapter{
		sessions: map[string]*mockLocalPTYSession{
			"lp1": {id: "lp1", title: "localpty-shell"},
			"lp2": {id: "lp2", title: "localpty-build"},
		},
	}
}

func (a *mockLocalPTYAdapter) Name() string { return "localpty" }
func (a *mockLocalPTYAdapter) ListSessions(ctx context.Context) ([]Session, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Session, 0, len(a.sessions))
	for _, s := range a.sessions {
		out = append(out, s)
	}
	return out, nil
}
func (a *mockLocalPTYAdapter) CreateSession(_ context.Context, opts CreateOptions) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextID++
	id := opts.Name
	if id == "" {
		id = "new"
	}
	a.sessions[id] = &mockLocalPTYSession{id: id, title: id}
	return id, nil
}
func (a *mockLocalPTYAdapter) TerminateSession(_ context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.sessions, id)
	return nil
}

// --- Contract test ---

func TestLocalPTYAdapter_Contract(t *testing.T) {
	cfg := &ContractConfig{
		ExpectStreamOpener:      true,
		ExpectSessionCreator:    true,
		ExpectSessionTerminator: true,
		// ScreenReader, HistoryReader intentionally unsupported.
	}

	factory := func(t *testing.T) Adapter { return newMockLocalPTYAdapter() }

	RunAdapterContract(t, "localpty", factory)
	RunCreateDiscoverTerminateContract(t, cfg, factory)
	RunLiveStreamContract(t, cfg, factory)

	// ScreenHistory skipped — localpty does not support screen capture.
	t.Run("ScreenHistory_unsupported", func(t *testing.T) {
		adapter := factory(t)
		sessions, _ := adapter.ListSessions(context.Background())
		if len(sessions) > 0 {
			s := sessions[0]
			if _, ok := s.(ScreenReader); ok {
				t.Error("localpty session should NOT implement ScreenReader")
			}
			if _, ok := s.(HistoryReader); ok {
				t.Error("localpty session should NOT implement HistoryReader")
			}
		}
	})
}
