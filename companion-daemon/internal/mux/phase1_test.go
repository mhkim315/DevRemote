package mux

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Phase 1: Identity and discovery contract tests.

func TestSessionRef_Canonical(t *testing.T) {
	tests := []struct {
		adapter, localID, want string
	}{
		{"tmux", "ai", "tmux:ai"},
		{"tmux", "aider", "tmux:aider"},
		{"tmux", "tmux:aider", "tmux:tmux:aider"},
		{"cmux", "surface:1", "cmux:surface:1"},
		{"tmux", "한글", "tmux:한글"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			ref := SessionRef{Adapter: tt.adapter, LocalID: tt.localID}
			got := ref.Canonical()
			if got != tt.want {
				t.Errorf("Canonical() = %q, want %q", got, tt.want)
			}
			if ref.String() != tt.want {
				t.Errorf("String() = %q, want %q", ref.String(), tt.want)
			}
		})
	}
}

func TestSessionRef_Validate(t *testing.T) {
	valid := SessionRef{Adapter: "tmux", LocalID: "session"}
	if err := valid.Validate(); err != nil {
		t.Errorf("Validate() on valid ref: %v", err)
	}

	tests := []struct {
		name string
		ref  SessionRef
	}{
		{"empty adapter", SessionRef{Adapter: "", LocalID: "x"}},
		{"empty local", SessionRef{Adapter: "tmux", LocalID: ""}},
		{"control char in adapter", SessionRef{Adapter: "tm\x00ux", LocalID: "x"}},
		{"control char in local", SessionRef{Adapter: "tmux", LocalID: "x\x1fy"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.ref.Validate(); err == nil {
				t.Error("Validate() returned nil, want error")
			}
		})
	}
}

type testSession struct {
	id, adapter string
}

func (s *testSession) ID() string          { return s.id }
func (s *testSession) Title() string       { return s.id }
func (s *testSession) AdapterName() string { return s.adapter }

type testAdapter struct {
	name     string
	sessions []Session
	failWith error
}

func (a *testAdapter) Name() string { return a.name }
func (a *testAdapter) ListSessions(_ context.Context) ([]Session, error) {
	if a.failWith != nil {
		return nil, a.failWith
	}
	if a.sessions == nil {
		return nil, nil
	}
	return a.sessions, nil
}
func (a *testAdapter) GetSession(id string) (Session, error) {
	return nil, ErrSessionNotFound
}

func (a *testAdapter) CreateSession(_ context.Context, opts CreateOptions) (string, error) {
	return opts.Name, nil
}

func TestRegistry_RejectDuplicateAdapter(t *testing.T) {
	reg := MustNewRegistry()
	if err := reg.Register(&testAdapter{name: "dup"}); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := reg.Register(&testAdapter{name: "dup"})
	if err == nil {
		t.Fatal("Register(duplicate) returned nil, want error")
	}
	if !errors.Is(err, ErrDuplicateAdapter) {
		t.Errorf("error = %v, want ErrDuplicateAdapter", err)
	}
}

func TestSentinelErrors_Distinguishable(t *testing.T) {
	errs := []error{
		ErrSessionNotFound,
		ErrAdapterUnavailable,
		ErrUnsupported,
		ErrTimeout,
		ErrInvalidSessionID,
		ErrDuplicateAdapter,
	}
	for i, e1 := range errs {
		for j, e2 := range errs {
			if i != j && errors.Is(e1, e2) {
				t.Errorf("%v and %v should be distinct", e1, e2)
			}
		}
	}
}

// Phase 1 contract regression tests.

func TestValidateAdapterName(t *testing.T) {
	valid := []string{"tmux", "cmux", "test-adapter", "backend_2", "a"}
	for _, name := range valid {
		if err := ValidateAdapterName(name); err != nil {
			t.Errorf("ValidateAdapterName(%q) = %v, want nil", name, err)
		}
	}
	invalid := []string{"", "Tmux", "tmux:bad", "has space", "a\x00b", "9start"}
	for _, name := range invalid {
		if err := ValidateAdapterName(name); err == nil {
			t.Errorf("ValidateAdapterName(%q) = nil, want error", name)
		}
	}
}

func TestNewRegistry_DuplicateAdapter(t *testing.T) {
	a1 := &testAdapter{name: "dup"}
	a2 := &testAdapter{name: "dup"}
	_, err := NewRegistry(a1, a2)
	if err == nil {
		t.Fatal("NewRegistry with duplicate names: got nil, want error")
	}
	if !errors.Is(err, ErrDuplicateAdapter) {
		t.Errorf("error = %v, want ErrDuplicateAdapter", err)
	}
}

func TestRegister_InvalidName(t *testing.T) {
	reg := MustNewRegistry()
	err := reg.Register(&testAdapter{name: "BAD"})
	if err == nil {
		t.Fatal("Register with uppercase name: got nil, want error")
	}
	if !errors.Is(err, ErrInvalidSessionID) {
		t.Errorf("error = %v, want ErrInvalidSessionID", err)
	}
}

func TestFindSession_InvalidID(t *testing.T) {
	reg := MustNewRegistry()
	ctx := context.Background()
	_, err := reg.FindSession(ctx, "")
	if err == nil {
		t.Fatal("FindSession with empty ID: got nil, want error")
	}
	if !errors.Is(err, ErrInvalidSessionID) {
		t.Errorf("error = %v, want ErrInvalidSessionID", err)
	}
	_, err = reg.FindSession(ctx, "bad\x1fid")
	if err == nil {
		t.Fatal("FindSession with control char: got nil, want error")
	}
}

func TestFindSession_AdapterUnavailable(t *testing.T) {
	reg := MustNewRegistry()
	ctx := context.Background()
	_, err := reg.FindSession(ctx, "nonexistent:session")
	if err == nil {
		t.Fatal("FindSession with missing adapter: got nil, want error")
	}
	if !errors.Is(err, ErrAdapterUnavailable) {
		t.Errorf("error = %v, want ErrAdapterUnavailable", err)
	}
}

func TestRefresh_TimeoutPreservation(t *testing.T) {
	// Verify that context.DeadlineExceeded is wrapped with ErrTimeout.
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	// The context is already expired, so the adapter will see DeadlineExceeded.
	reg := MustNewRegistry(&testAdapter{name: "test"})
	_, err := reg.Refresh(ctx, "test", true)
	if err == nil {
		t.Fatal("Refresh with expired context: got nil, want error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want context.DeadlineExceeded", err)
	}
}

func TestCreateSession_ReturnsLocalID(t *testing.T) {
	reg := MustNewRegistry(&testAdapter{name: "test"})
	id, err := reg.CreateSession(context.Background(), "test", CreateOptions{Name: "session"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// Creator must return local ID (not canonical prefix).
	if strings.Contains(id, "test:") {
		t.Errorf("CreateSession returned %q, want local ID without adapter prefix", id)
	}
}

func TestRefresh_TimeoutWrapsErrTimeout(t *testing.T) {
	// Use a blocking adapter so the singleflight closure respects ctx.Done().
	blocker := &blockingAdapter{name: "test"}
	reg := MustNewRegistry(blocker)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := reg.Refresh(ctx, "test", true)
	if err == nil {
		t.Fatal("Refresh with expired context: got nil, want error")
	}
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("error does not wrap ErrTimeout: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error does not wrap DeadlineExceeded: %v", err)
	}
}

type blockingAdapter struct{ name string }

func (a *blockingAdapter) Name() string { return a.name }
func (a *blockingAdapter) ListSessions(ctx context.Context) ([]Session, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (a *blockingAdapter) GetSession(id string) (Session, error) {
	return nil, ErrSessionNotFound
}

func TestRefresh_CancelNotTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reg := MustNewRegistry(&blockingAdapter{name: "test"})
	_, err := reg.Refresh(ctx, "test", true)
	if err == nil {
		t.Fatal("Refresh with cancelled context: got nil, want error")
	}
	if errors.Is(err, ErrTimeout) {
		t.Errorf("cancel should NOT wrap ErrTimeout: %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error does not wrap Canceled: %v", err)
	}
}

func TestFindSession_EndedSession(t *testing.T) {
	// Simulate: cache has session, but forced refresh shows it's gone.
	s1 := &testSession{id: "gone", adapter: "test"}
	a := &testAdapter{name: "test", sessions: []Session{s1}}
	reg := MustNewRegistry(a)
	// Force cache population.
	_ = reg.Sessions(context.Background())
	// Remove session from adapter — now it's "ended".
	a.sessions = nil
	_, err := reg.FindSession(context.Background(), "test:gone")
	if err == nil {
		t.Fatal("FindSession for ended session: got nil, want error")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("error = %v, want ErrSessionNotFound", err)
	}
}

func TestCreateSession_TmuxCanonicalContract(t *testing.T) {
	// tmux adapter creates session by name and returns local ID.
	// Handler wraps with adapter prefix exactly once.
	reg := MustNewRegistry(&testAdapter{name: "tmux"})
	localID, err := reg.CreateSession(context.Background(), "tmux", CreateOptions{Name: "test-session"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// Local ID must NOT contain adapter prefix.
	if strings.Contains(localID, "tmux:") {
		t.Errorf("create returned %q, want local ID 'test-session' (not canonical)", localID)
	}
	// Handler canonicalizes: adapter + ":" + localID.
	canonical := SessionRef{Adapter: "tmux", LocalID: localID}.Canonical()
	if canonical != "tmux:test-session" {
		t.Errorf("canonical = %q, want tmux:test-session", canonical)
	}
}

func TestCanonicalID_URLRoundTrip(t *testing.T) {
	// Colon in local ID must survive encode/decode round-trip.
	localID := "session:with:colons"
	ref := SessionRef{Adapter: "tmux", LocalID: localID}
	canonical := ref.Canonical()
	parsed := ParseSessionID(canonical)
	if parsed.Adapter != "tmux" || parsed.LocalID != localID {
		t.Errorf("round-trip: %q -> Parse -> adapter=%q local=%q", canonical, parsed.Adapter, parsed.LocalID)
	}
}

func TestFindSession_StaleCacheAndRefreshFailure(t *testing.T) {
	s1 := &testSession{id: "s1", adapter: "test"}
	a := &testAdapter{name: "test", sessions: []Session{s1}}
	reg := MustNewRegistry(a)
	// Populate cache.
	_ = reg.Sessions(context.Background())
	snap, _ := reg.Snapshot("test")
	if len(snap.Sessions) != 1 {
		t.Fatalf("cache has %d sessions, want 1", len(snap.Sessions))
	}
	a.failWith = errors.New("adapter down")
	s, err := reg.FindSession(context.Background(), "test:s1")
	if err == nil {
		t.Fatal("FindSession got nil error, want ErrAdapterUnavailable")
	}
	if s != nil {
		t.Errorf("FindSession returned non-nil session on adapter failure: %v", s)
	}
	if !errors.Is(err, ErrAdapterUnavailable) {
		t.Errorf("error = %v, want ErrAdapterUnavailable", err)
	}
	// Stale snapshot still present.
	snap2, _ := reg.Snapshot("test")
	if len(snap2.Sessions) != 1 {
		t.Errorf("stale snapshot has %d sessions after failed refresh, want 1", len(snap2.Sessions))
	}
}

func TestCanonicalID_URLEncodeRoundTrip(t *testing.T) {
	// Colon + Unicode in local ID must survive URL query encode/decode.
	// Mobile clients encode session IDs in query parameters.
	localID := "session:with:colons_한글"
	ref := SessionRef{Adapter: "tmux", LocalID: localID}
	canonical := ref.Canonical()
	// URL query-encode the canonical ID (as mobile client does for ?session=...)
	encoded := url.QueryEscape(canonical)
	// Decode back
	decoded, err := url.QueryUnescape(encoded)
	if err != nil {
		t.Fatalf("QueryUnescape: %v", err)
	}
	parsed := ParseSessionID(decoded)
	if parsed.Adapter != "tmux" {
		t.Errorf("adapter = %q, want tmux", parsed.Adapter)
	}
	if parsed.LocalID != localID {
		t.Errorf("localID = %q, want %q", parsed.LocalID, localID)
	}
	// Percent-encoded : and _ should be present in encoded form.
	if !strings.Contains(encoded, "%") {
		t.Errorf("no percent encoding in %q", encoded)
	}
}

func TestCmuxAdapter_CreateSessionReturnsLocalID(t *testing.T) {
	// cmux adapter's CreateSession must return local ID "surface:42",
	// not canonical "cmux:surface:42". Handler wraps exactly once.
	mockRunner := &mockCmuxRunner{
		runFunc: func(_ context.Context, _ CommandOptions, args ...string) ([]byte, error) {
			return []byte("Created new surface: surface:42 (workspace: workspace:1)"), nil
		},
	}
	adapter := &cmuxAdapter{runner: mockRunner}
	id, err := adapter.CreateSession(context.Background(), CreateOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if id != "surface:42" {
		t.Errorf("CreateSession returned %q, want local ID surface:42", id)
	}
	// Handler canonicalization (exactly once).
	canonical := SessionRef{Adapter: "cmux", LocalID: id}.Canonical()
	if canonical != "cmux:surface:42" {
		t.Errorf("canonical = %q, want cmux:surface:42", canonical)
	}
}

func TestCmuxAdapter_CreateSession_MalformedOutput(t *testing.T) {
	// Malformed output must return error, not a fake ID.
	mockRunner := &mockCmuxRunner{
		runFunc: func(_ context.Context, _ CommandOptions, args ...string) ([]byte, error) {
			return []byte("garbage output with no surface id"), nil
		},
	}
	adapter := &cmuxAdapter{runner: mockRunner}
	id, err := adapter.CreateSession(context.Background(), CreateOptions{})
	if err == nil {
		t.Fatalf("CreateSession with malformed output: got nil error, id=%q", id)
	}
	if id != "" {
		t.Errorf("CreateSession on parse failure returned id=%q, want empty", id)
	}
}
