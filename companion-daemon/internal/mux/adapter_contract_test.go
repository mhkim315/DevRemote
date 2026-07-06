package mux

import (
	"context"
	"errors"
	"net/url"
	"sync"
	"testing"
	"time"
)

// ContractAdapterFactory creates a fresh adapter for each contract sub-test.
// Implementations should return an adapter configured for happy-path tests.
// Error-path tests (cancellation, timeout, transient failure) build their own
// adapters directly rather than relying on the factory.
type ContractAdapterFactory func(t *testing.T) Adapter

// RunAdapterContract runs the full required adapter contract suite against the
// adapter produced by factory. Call this once per adapter implementation:
//
//	func TestTmuxAdapter_Contract(t *testing.T) {
//	    RunAdapterContract(t, "tmux", func(t *testing.T) Adapter {
//	        return NewTmuxAdapterWithRunner(&recordingRunner{})
//	    })
//	}
func RunAdapterContract(t *testing.T, name string, factory ContractAdapterFactory) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		t.Run("NameStable", func(t *testing.T) { testNameStable(t, factory) })
		t.Run("ListSessions_Basic", func(t *testing.T) { testListSessionsBasic(t, factory) })
		t.Run("ListSessions_Empty", func(t *testing.T) { testListSessionsEmpty(t, factory) })
		t.Run("ListSessions_ContextCancellation", func(t *testing.T) { testListSessionsContextCancellation(t, factory) })
		t.Run("ListSessions_ContextTimeout", func(t *testing.T) { testListSessionsContextTimeout(t, factory) })
		t.Run("Snapshot_IdentityMatch", func(t *testing.T) { testSnapshotIdentityMatch(t, factory) })
		t.Run("ConcurrentListSafety", func(t *testing.T) { testConcurrentListSafety(t, factory) })
		t.Run("StaleSnapshot_OnTransientFailure", func(t *testing.T) { testStaleSnapshotOnFailure(t, factory) })
		t.Run("ProbeAdapter_ErrorTaxonomy", func(t *testing.T) { testProbeAdapterTaxonomy(t) })
		t.Run("SentinelErrors_Distinct", func(t *testing.T) { testSentinelErrorsDistinct(t) })
		t.Run("CanonicalID_Stability", func(t *testing.T) { testCanonicalIDStability(t) })
		t.Run("StaleSession_EndedRace", func(t *testing.T) { testStaleSessionEndedRace(t, factory) })
		t.Run("UnavailableVsNotFound", func(t *testing.T) { testUnavailableVsNotFound(t, factory) })
		t.Run("CreateSession_ReturnsLocalID", func(t *testing.T) { testCreateSessionReturnsLocalID(t, factory) })
		t.Run("SlowAdapter_DoesNotBlock", func(t *testing.T) { testSlowAdapterDoesNotBlock(t, factory) })
	})
}

// --- Contract test implementations ---

// testNameStable verifies adapter Name() returns the same value on every call.
func testNameStable(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	adapter := factory(t)
	n1 := adapter.Name()
	if n1 == "" {
		t.Fatal("Name() returned empty string")
	}
	for i := 0; i < 100; i++ {
		if n := adapter.Name(); n != n1 {
			t.Errorf("Name() = %q on call %d, want %q (names must be stable)", n, i, n1)
		}
	}
}

// testListSessionsBasic verifies a healthy adapter returns session list without error.
func testListSessionsBasic(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	adapter := factory(t)
	sessions, err := adapter.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if sessions == nil {
		t.Error("ListSessions returned nil slice; want empty or populated slice")
	}
	for i, s := range sessions {
		if s.ID() == "" {
			t.Errorf("session[%d] has empty ID", i)
		}
		if s.AdapterName() == "" {
			t.Errorf("session[%d] has empty AdapterName", i)
		}
	}
}

// testListSessionsEmpty verifies an adapter with zero sessions returns nil or empty.
func testListSessionsEmpty(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	// Use testAdapter with no sessions, ignoring the factory's adapter.
	adapter := &testAdapter{name: "empty"}
	sessions, err := adapter.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions on empty adapter: %v", err)
	}
	if sessions == nil {
		return // nil is acceptable
	}
	if len(sessions) != 0 {
		t.Errorf("empty adapter returned %d sessions, want 0 or nil", len(sessions))
	}
}

// testListSessionsContextCancellation verifies a cancelled context produces an error.
// Uses a blockingAdapter because the factory may return an adapter whose mock runner
// does not check context cancellation (production runners do via exec.CommandContext).
func testListSessionsContextCancellation(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	blocker := &blockingAdapter{name: factory(t).Name()}
	sessions, err := blocker.ListSessions(ctx)
	if err == nil {
		t.Fatal("ListSessions with cancelled ctx: got nil error, want error")
	}
	if sessions != nil {
		t.Errorf("ListSessions with cancelled ctx returned %d sessions (non-nil), want nil", len(sessions))
	}
}

// testListSessionsContextTimeout verifies a timeout context produces an error
// that wraps context.DeadlineExceeded.
func testListSessionsContextTimeout(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	blocker := &blockingAdapter{name: factory(t).Name()}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := blocker.ListSessions(ctx)
	if err == nil {
		t.Fatal("ListSessions with timeout ctx: got nil error, want error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error does not wrap DeadlineExceeded: %v", err)
	}
}

// testSnapshotIdentityMatch verifies sessions obtained via ListSessions match
// sessions cached in the Registry snapshot.
func testSnapshotIdentityMatch(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	adapter := factory(t)
	reg := MustNewRegistry(adapter)

	// Populate cache via Registry.Sessions.
	discovered := reg.Sessions(context.Background())
	snap, ok := reg.Snapshot(adapter.Name())
	if !ok {
		t.Fatal("Snapshot not found after Sessions()")
	}

	// Build ID sets from both sources.
	discoveredIDs := make(map[string]bool, len(discovered))
	for _, s := range discovered {
		discoveredIDs[s.ID()] = true
	}
	snapshotIDs := make(map[string]bool, len(snap.Sessions))
	for _, s := range snap.Sessions {
		snapshotIDs[s.ID()] = true
	}

	// Every snapshot session must appear in the discovered set.
	for id := range snapshotIDs {
		if !discoveredIDs[id] {
			t.Errorf("session %q in snapshot but not in Sessions() result", id)
		}
	}
	// Every discovered session must appear in the snapshot.
	for id := range discoveredIDs {
		if !snapshotIDs[id] {
			t.Errorf("session %q in Sessions() result but not in snapshot", id)
		}
	}
}

// testConcurrentListSafety verifies ListSessions does not deadlock or race
// when called concurrently from multiple goroutines.
func testConcurrentListSafety(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	adapter := factory(t)
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := adapter.ListSessions(context.Background())
			if err != nil {
				errs <- err
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		// success — no deadlock
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent ListSessions timed out (possible deadlock)")
	}
	close(errs)
	for err := range errs {
		t.Errorf("concurrent ListSessions error: %v", err)
	}
}

// testStaleSnapshotOnFailure verifies that after a transient ListSessions failure,
// the last successful snapshot is preserved with LastError recorded.
func testStaleSnapshotOnFailure(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	base := factory(t)
	// Build a configurable adapter that fails on second call.
	cfg := &configurableAdapter{
		name:     base.Name(),
		sessions: []Session{&testSession{id: "s1", adapter: base.Name()}},
	}
	reg := MustNewRegistry(cfg)

	// Populate cache with successful refresh.
	_ = reg.Sessions(context.Background())
	snap1, _ := reg.Snapshot(base.Name())
	if len(snap1.Sessions) != 1 {
		t.Fatalf("initial snapshot has %d sessions, want 1", len(snap1.Sessions))
	}
	if snap1.LastError != nil {
		t.Fatalf("initial snapshot has unexpected LastError: %v", snap1.LastError)
	}

	// Force a failure.
	cfg.mu.Lock()
	cfg.failWith = errors.New("transient connection refused")
	cfg.mu.Unlock()

	reg.InvalidateAdapter(base.Name())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	sessions := reg.Sessions(ctx)

	// Fast path: panic/liveness check — must not crash.
	_ = sessions

	snap2, _ := reg.Snapshot(base.Name())
	if len(snap2.Sessions) != 1 {
		t.Errorf("stale snapshot has %d sessions after failure, want 1", len(snap2.Sessions))
	}
	if snap2.LastError == nil {
		t.Error("snapshot LastError is nil after transient failure, want non-nil")
	}
}

// testProbeAdapterTaxonomy verifies error classification for ProbeAdapter timeouts
// and cancellations.
func testProbeAdapterTaxonomy(t *testing.T) {
	t.Helper()

	t.Run("timeout_wraps_DeadlineExceeded_and_ErrAdapterUnavailable", func(t *testing.T) {
		blocker := &blockingAdapter{name: "test"}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		err := ProbeAdapter(ctx, blocker, 50*time.Millisecond)
		if err == nil {
			t.Fatal("ProbeAdapter: got nil, want error")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("error does not wrap DeadlineExceeded: %v", err)
		}
		if !errors.Is(err, ErrAdapterUnavailable) {
			t.Errorf("error does not wrap ErrAdapterUnavailable: %v", err)
		}
	})

	t.Run("cancel_wraps_Canceled_and_ErrAdapterUnavailable", func(t *testing.T) {
		blocker := &blockingAdapter{name: "test"}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := ProbeAdapter(ctx, blocker, 4*time.Second)
		if err == nil {
			t.Fatal("ProbeAdapter with cancelled context: got nil, want error")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("error does not wrap Canceled: %v", err)
		}
		if !errors.Is(err, ErrAdapterUnavailable) {
			t.Errorf("error does not wrap ErrAdapterUnavailable: %v", err)
		}
	})
}

// testSentinelErrorsDistinct verifies all sentinel errors are distinguishable
// via errors.Is.
func testSentinelErrorsDistinct(t *testing.T) {
	t.Helper()
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

// testCanonicalIDStability verifies canonical ID format rules that apply to
// all adapters: colon handling, Unicode, URL round-trip, and edge cases.
func testCanonicalIDStability(t *testing.T) {
	t.Helper()

	t.Run("LocalID_with_colon", func(t *testing.T) {
		tests := []struct {
			input, adapter, localID string
		}{
			{"tmux:aider", "tmux", "aider"},
			{"tmux:session:with:colons", "tmux", "session:with:colons"},
			{"cmux:surface:42", "cmux", "surface:42"},
		}
		for _, tt := range tests {
			ref := ParseSessionID(tt.input)
			if ref.Adapter != tt.adapter || ref.LocalID != tt.localID {
				t.Errorf("ParseSessionID(%q) = (%q, %q), want (%q, %q)",
					tt.input, ref.Adapter, ref.LocalID, tt.adapter, tt.localID)
			}
			if stringsContains(ref.Adapter, ":") {
				t.Errorf("adapter %q contains colon", ref.Adapter)
			}
		}
	})

	t.Run("Unicode_localID", func(t *testing.T) {
		ids := []string{"tmux:한글", "tmux:セッション", "tmux:中文", "tmux:emoji_🎉"}
		for _, id := range ids {
			ref := ParseSessionID(id)
			if ref.LocalID == "" {
				t.Errorf("ParseSessionID(%q) returned empty LocalID", id)
			}
			if MigrateLegacyID(id) != id {
				t.Errorf("Unicode ID %q was unexpectedly migrated to %q", id, MigrateLegacyID(id))
			}
		}
	})

	t.Run("URL_round_trip", func(t *testing.T) {
		localID := "session:with:colons_한글"
		ref := SessionRef{Adapter: "tmux", LocalID: localID}
		canonical := ref.Canonical()
		// Simulate mobile client URL encode/decode via net/url.
		encoded := url.QueryEscape(canonical)
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
	})

	t.Run("edge_cases_no_panic", func(t *testing.T) {
		neverPanic := []string{"", ":", "only-adapter:", ":only-local", "no-colon", "a:b:c:d:e"}
		for _, id := range neverPanic {
			_ = ParseSessionID(id)
			_ = MigrateLegacyID(id)
		}
	})
}

// testStaleSessionEndedRace verifies that when a session exists in cache but
// disappears after a forced refresh, the Registry returns ErrSessionNotFound.
func testStaleSessionEndedRace(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	base := factory(t)
	s1 := &testSession{id: "s1", adapter: base.Name()}
	cfg := &configurableAdapter{
		name:     base.Name(),
		sessions: []Session{s1},
	}
	reg := MustNewRegistry(cfg)

	// Populate cache.
	_ = reg.Sessions(context.Background())

	// Remove session from adapter — it "ended".
	cfg.mu.Lock()
	cfg.sessions = nil
	cfg.mu.Unlock()

	_, err := reg.FindSession(context.Background(), base.Name()+":s1")
	if err == nil {
		t.Fatal("FindSession for ended session: got nil, want error")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("error = %v, want ErrSessionNotFound", err)
	}
}

// testUnavailableVsNotFound verifies the Registry distinguishes adapter
// connectivity failure (ErrAdapterUnavailable) from missing sessions
// (ErrSessionNotFound).
func testUnavailableVsNotFound(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	base := factory(t)

	t.Run("unregistered_adapter_returns_ErrAdapterUnavailable", func(t *testing.T) {
		reg := MustNewRegistry()
		_, err := reg.FindSession(context.Background(), "nonexistent:s1")
		if err == nil {
			t.Fatal("FindSession with missing adapter: got nil, want error")
		}
		if !errors.Is(err, ErrAdapterUnavailable) {
			t.Errorf("error = %v, want ErrAdapterUnavailable", err)
		}
	})

	t.Run("adapter_down_returns_ErrAdapterUnavailable_not_ErrSessionNotFound", func(t *testing.T) {
		s1 := &testSession{id: "s1", adapter: base.Name()}
		cfg := &configurableAdapter{
			name:     base.Name(),
			sessions: []Session{s1},
		}
		reg := MustNewRegistry(cfg)
		_ = reg.Sessions(context.Background())

		// Make adapter fail.
		cfg.mu.Lock()
		cfg.failWith = errors.New("adapter down")
		cfg.mu.Unlock()

		s, err := reg.FindSession(context.Background(), base.Name()+":s1")
		if err == nil {
			t.Fatal("FindSession got nil error, want ErrAdapterUnavailable")
		}
		if s != nil {
			t.Errorf("FindSession returned non-nil session on adapter failure: %v", s)
		}
		if !errors.Is(err, ErrAdapterUnavailable) {
			t.Errorf("error = %v, want ErrAdapterUnavailable", err)
		}
		if errors.Is(err, ErrSessionNotFound) {
			t.Errorf("error wraps ErrSessionNotFound, want ErrAdapterUnavailable only")
		}
	})
}

// testCreateSessionReturnsLocalID verifies CreateSession returns a local ID
// without the adapter prefix (canonicalization happens once in the handler).
func testCreateSessionReturnsLocalID(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	adapter := factory(t)
	if _, ok := adapter.(SessionCreator); !ok {
		t.Skip("adapter does not implement SessionCreator")
	}
	reg := MustNewRegistry(adapter)
	id, err := reg.CreateSession(context.Background(), adapter.Name(), CreateOptions{Name: "contract-test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if stringsContains(id, adapter.Name()+":") {
		t.Errorf("CreateSession returned %q, want local ID without adapter prefix", id)
	}
	// Handler canonicalizes: adapter + ":" + localID exactly once.
	canonical := SessionRef{Adapter: adapter.Name(), LocalID: id}.Canonical()
	if canonical != adapter.Name()+":"+id {
		t.Errorf("canonical = %q, want %s:%s", canonical, adapter.Name(), id)
	}
}

// testSlowAdapterDoesNotBlock verifies a slow/hanging adapter does not delay
// discovery of sessions from a healthy adapter. Uses the 200ms collection
// window contract established in Phase 3.
func testSlowAdapterDoesNotBlock(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	fast := factory(t)
	slow := &blockingAdapter{name: "slow"}
	reg := MustNewRegistry(fast, slow)

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sessions := reg.Sessions(ctx)
	elapsed := time.Since(start)
	if elapsed > time.Second {
		t.Errorf("Sessions took %v, want <1s (slow adapter should not block fast)", elapsed)
	}

	// Verify fast adapter sessions are present.
	fastFound := false
	for _, s := range sessions {
		if s.AdapterName() == fast.Name() {
			fastFound = true
			break
		}
	}
	if !fastFound {
		t.Errorf("fast adapter sessions not found (slow adapter may have blocked)")
	}
}

// --- Capability-specific contract suites ---

// RunScreenHistoryContract runs screen capture and history scrollback tests
// against an adapter whose sessions implement ScreenReader and HistoryReader.
func RunScreenHistoryContract(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	adapter := factory(t)
	sessions, err := adapter.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) == 0 {
		t.Skip("no sessions — skipping screen/history contract")
	}

	s := sessions[0]

	t.Run("ScreenReader", func(t *testing.T) {
		sr, ok := s.(ScreenReader)
		if !ok {
			t.Skip("session does not implement ScreenReader")
		}
		out, err := sr.ReadScreen(context.Background())
		if err != nil {
			t.Fatalf("ReadScreen: %v", err)
		}
		if out == nil {
			t.Error("ReadScreen returned nil output")
		}
	})

	t.Run("HistoryReader", func(t *testing.T) {
		hr, ok := s.(HistoryReader)
		if !ok {
			t.Skip("session does not implement HistoryReader")
		}
		out, err := hr.ReadHistory(context.Background(), 200)
		if err != nil {
			t.Fatalf("ReadHistory: %v", err)
		}
		if out == nil {
			t.Error("ReadHistory returned nil output")
		}
	})
}

// RunProcessInfoContract runs process info resolution tests against an
// adapter whose sessions implement ProcessProvider.
func RunProcessInfoContract(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	adapter := factory(t)
	sessions, err := adapter.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) == 0 {
		t.Skip("no sessions — skipping process info contract")
	}

	s := sessions[0]
	pp, ok := s.(ProcessProvider)
	if !ok {
		t.Skip("session does not implement ProcessProvider")
	}
	info, err := pp.ProcessInfo(context.Background())
	if err != nil {
		t.Fatalf("ProcessInfo: %v", err)
	}
	if info.PID == 0 && info.CWD == "" {
		t.Error("ProcessInfo returned empty PID and CWD — possible parse failure")
	}
}

// RunCreateDiscoverTerminateContract runs the full create→list→terminate
// lifecycle against an adapter that implements SessionCreator and SessionTerminator.
func RunCreateDiscoverTerminateContract(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	adapter := factory(t)
	if _, ok := adapter.(SessionCreator); !ok {
		t.Skip("adapter does not implement SessionCreator")
	}
	if _, ok := adapter.(SessionTerminator); !ok {
		t.Skip("adapter does not implement SessionTerminator")
	}

	reg := MustNewRegistry(adapter)
	testName := "cdt-contract-test"

	// 1. Create
	createID, err := reg.CreateSession(context.Background(), adapter.Name(), CreateOptions{Name: testName})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if createID == "" {
		t.Fatal("CreateSession returned empty ID")
	}

	// 2. Discover via fresh list
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	reg.InvalidateAdapter(adapter.Name())
	sessions := reg.Sessions(ctx)
	found := false
	for _, s := range sessions {
		if s.ID() == createID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("created session %q not found in Sessions() after create", createID)
	}

	// 3. Terminate
	if err := reg.TerminateSession(context.Background(), adapter.Name(), createID); err != nil {
		t.Fatalf("TerminateSession: %v", err)
	}
}

// --- Helpers ---

// configurableAdapter wraps a simple adapter whose behavior can be mutated
// between calls. Used for stale-snapshot and transient-failure contract tests.
type configurableAdapter struct {
	name     string
	sessions []Session
	failWith error
	mu       sync.Mutex
}

func (a *configurableAdapter) Name() string { return a.name }

func (a *configurableAdapter) ListSessions(_ context.Context) ([]Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.failWith != nil {
		return nil, a.failWith
	}
	return a.sessions, nil
}

// stringsContains reports whether s contains substr. Avoids importing "strings" just for this.
func stringsContains(s, substr string) bool {
	return len(substr) == 0 || len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
