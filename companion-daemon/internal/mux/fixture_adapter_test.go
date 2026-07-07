package mux

import (
	"context"
	"io"
	"sync"
	"testing"
)

// --- Fixture Adapter ---
// In-memory adapter with no external binary dependencies. Used to prove
// Phase 4 contract harness + Phase 5 zero-modification extension.

type fixtureAdapter struct {
	mu       sync.Mutex
	sessions map[string]*fixtureSession
	nextID   int
}

func NewFixtureAdapter() Adapter {
	return &fixtureAdapter{
		sessions: map[string]*fixtureSession{
			"f1": {id: "f1", title: "fixture-shell", adapterName: "fixture"},
			"f2": {id: "f2", title: "fixture-build", adapterName: "fixture"},
		},
		nextID: 100,
	}
}

func (a *fixtureAdapter) Name() string { return "fixture" }

func (a *fixtureAdapter) ListSessions(ctx context.Context) ([]Session, error) {
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

func (a *fixtureAdapter) CreateSession(_ context.Context, opts CreateOptions) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextID++
	id := opts.Name
	if id == "" {
		id = "f" + string(rune('0'+a.nextID%10))
	}
	s := &fixtureSession{id: id, title: id, adapterName: a.Name()}
	a.sessions[id] = s
	return id, nil
}

func (a *fixtureAdapter) TerminateSession(_ context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.sessions, id)
	return nil
}

// --- Fixture Session ---

type fixtureSession struct {
	id          string
	title       string
	adapterName string
}

func (s *fixtureSession) ID() string          { return s.id }
func (s *fixtureSession) Title() string       { return s.title }
func (s *fixtureSession) AdapterName() string { return s.adapterName }

func (s *fixtureSession) ReadScreen(_ context.Context) ([]byte, error) {
	return []byte("fixture screen content"), nil
}

func (s *fixtureSession) WriteInput(_ context.Context, data []byte) error {
	// Accept all input; no-op.
	return nil
}

func (s *fixtureSession) OpenStream(_ context.Context) (TerminalStream, error) {
	ctx, cancel := context.WithCancel(context.Background())
	pr, pw := io.Pipe()
	fs := &fixtureStream{
		pr:     pr,
		pw:     pw,
		ctx:    ctx,
		cancel: cancel,
	}
	go fs.pushFrames()
	return fs, nil
}

// --- Fixture Stream ---

type fixtureStream struct {
	pr     *io.PipeReader
	pw     *io.PipeWriter
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
}

func (s *fixtureStream) pushFrames() {
	// Push one frame so Read contract receives content.
	payload := "\033[2J\033[Hfixture stream content\r\n"
	if _, err := s.pw.Write([]byte(payload)); err != nil {
		return
	}
	// Keep stream alive until Close cancels ctx.
	<-s.ctx.Done()
}

func (s *fixtureStream) Read(p []byte) (int, error) {
	return s.pr.Read(p)
}

func (s *fixtureStream) Write(p []byte) (int, error) {
	return len(p), nil // accept all writes
}

func (s *fixtureStream) Close() error {
	s.once.Do(func() {
		s.cancel()
		s.pw.Close()
		s.pr.Close()
	})
	return nil
}

func (s *fixtureStream) Resize(rows, cols int) error {
	// No-op: in-memory stream has no real PTY.
	return nil
}

// --- Contract Harness ---

func TestFixtureAdapter_Contract(t *testing.T) {
	cfg := &ContractConfig{
		ExpectScreenReader:      true,
		ExpectStreamOpener:      true,
		ExpectSessionCreator:    true,
		ExpectSessionTerminator: true,
		// HistoryReader, ProcessProvider, ProcessSnapshot intentionally
		// unsupported — proves unsupported path does not break harness.
	}

	factory := func(t *testing.T) Adapter { return NewFixtureAdapter() }

	RunAdapterContract(t, "fixture", factory)
	RunScreenHistoryContract(t, cfg, factory)
	RunCreateDiscoverTerminateContract(t, cfg, factory)
	RunLiveStreamContract(t, cfg, factory)
}
