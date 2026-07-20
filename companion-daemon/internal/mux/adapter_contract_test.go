package mux

import (
	"context"
	"errors"
	"io"
	"net/url"
	"sync"
	"testing"
	"time"
)

// ContractConfig declares which capabilities the adapter under test is expected
// to support. Capability suites marked true will FAIL (not skip) if the adapter
// does not expose the expected capability.
type ContractConfig struct {
	ExpectScreenReader      bool
	ExpectHistoryReader     bool
	ExpectStreamOpener      bool
	ExpectProcessProvider   bool
	ExpectSessionCreator    bool
	ExpectSessionTerminator bool
	ExpectProcessSnapshot   bool
}

// ContractAdapterFactory creates a fresh adapter for each contract sub-test.
type ContractAdapterFactory func(t *testing.T) Adapter

// RunAdapterContract runs the full required adapter contract suite.
func RunAdapterContract(t *testing.T, name string, factory ContractAdapterFactory) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		// Factory-adapter tests (exercise the actual adapter).
		t.Run("NameStable", func(t *testing.T) { testNameStable(t, factory) })
		t.Run("ListSessions_Basic", func(t *testing.T) { testListSessionsBasic(t, factory) })
		t.Run("ListSessions_ContextCancellation", func(t *testing.T) { testFactoryCtxCancel(t, factory) })
		t.Run("ListSessions_ContextTimeout", func(t *testing.T) { testFactoryCtxTimeout(t, factory) })
		t.Run("Snapshot_IdentityMatch", func(t *testing.T) { testSnapshotIdentityMatch(t, factory) })
		t.Run("ConcurrentListSafety", func(t *testing.T) { testConcurrentListSafety(t, factory) })
		t.Run("StaleSession_EndedRace", func(t *testing.T) { testStaleSessionEndedRace(t, factory) })
		t.Run("UnavailableVsNotFound", func(t *testing.T) { testUnavailableVsNotFound(t, factory) })
		t.Run("CreateSession_ReturnsLocalID", func(t *testing.T) { testCreateSessionReturnsLocalID(t, factory) })
		t.Run("SlowAdapter_DoesNotBlock", func(t *testing.T) { testSlowAdapterDoesNotBlock(t, factory) })

		// Registry / core tests (use synthetic adapters).
		t.Run("ListSessions_Empty", func(t *testing.T) { testListSessionsEmpty(t) })
		t.Run("StaleSnapshot_OnTransientFailure", func(t *testing.T) { testStaleSnapshotOnFailure(t, factory) })
		t.Run("ProbeAdapter_ErrorTaxonomy", func(t *testing.T) { testProbeAdapterTaxonomy(t) })
		t.Run("SentinelErrors_Distinct", func(t *testing.T) { testSentinelErrorsDistinct(t) })
		t.Run("CanonicalID_Stability", func(t *testing.T) { testCanonicalIDStability(t) })
	})
}

// --- Factory-adapter tests ---

func testNameStable(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	adapter := factory(t)
	n1 := adapter.Name()
	if n1 == "" {
		t.Fatal("Name() returned empty string")
	}
	for i := 0; i < 100; i++ {
		if n := adapter.Name(); n != n1 {
			t.Errorf("Name() = %q on call %d, want %q", n, i, n1)
		}
	}
}

func testListSessionsBasic(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	adapter := factory(t)
	sessions, err := adapter.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if sessions == nil {
		t.Error("ListSessions returned nil slice")
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

func testFactoryCtxCancel(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	adapter := factory(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sessions, err := adapter.ListSessions(ctx)
	if err == nil {
		t.Fatal("ListSessions with cancelled ctx: got nil error, want context.Canceled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("ListSessions error does not wrap Canceled: %v", err)
	}
	if sessions != nil {
		t.Errorf("ListSessions with cancelled ctx returned %d sessions, want nil", len(sessions))
	}
}

func testFactoryCtxTimeout(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	adapter := factory(t)
	// Use already-expired context so it fires before any internal
	// timeout wrapping (e.g. legacy's 3s ListSessions deadline).
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	_, err := adapter.ListSessions(ctx)
	if err == nil {
		t.Error("factory adapter must propagate context timeout; mock runner must check ctx.Err()")
	}
}

func testSnapshotIdentityMatch(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	adapter := factory(t)
	reg := MustNewRegistry(adapter)
	discovered := reg.Sessions(context.Background())
	snap, ok := reg.Snapshot(adapter.Name())
	if !ok {
		t.Fatal("Snapshot not found after Sessions()")
	}
	discIDs := make(map[string]bool)
	for _, s := range discovered {
		discIDs[s.ID()] = true
	}
	for _, s := range snap.Sessions {
		if !discIDs[s.ID()] {
			t.Errorf("session %q in snapshot but not in Sessions()", s.ID())
		}
	}
}

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
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent ListSessions timed out (possible deadlock)")
	}
	close(errs)
	for err := range errs {
		t.Errorf("concurrent ListSessions error: %v", err)
	}
}

func testStaleSessionEndedRace(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	base := factory(t)
	cfg := &configurableAdapter{name: base.Name(), sessions: []Session{&testSession{id: "s1", adapter: base.Name()}}}
	reg := MustNewRegistry(cfg)
	_ = reg.Sessions(context.Background())
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

func testUnavailableVsNotFound(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	base := factory(t)
	t.Run("unregistered", func(t *testing.T) {
		reg := MustNewRegistry()
		_, err := reg.FindSession(context.Background(), "nonexistent:s1")
		if !errors.Is(err, ErrAdapterUnavailable) {
			t.Errorf("error = %v, want ErrAdapterUnavailable", err)
		}
	})
	t.Run("adapter_down_not_not_found", func(t *testing.T) {
		cfg := &configurableAdapter{name: base.Name(), sessions: []Session{&testSession{id: "s1", adapter: base.Name()}}}
		reg := MustNewRegistry(cfg)
		_ = reg.Sessions(context.Background())
		cfg.mu.Lock()
		cfg.failWith = errors.New("adapter down")
		cfg.mu.Unlock()
		_, err := reg.FindSession(context.Background(), base.Name()+":s1")
		if !errors.Is(err, ErrAdapterUnavailable) {
			t.Errorf("error = %v, want ErrAdapterUnavailable", err)
		}
		if errors.Is(err, ErrSessionNotFound) {
			t.Error("error wraps ErrSessionNotFound, want ErrAdapterUnavailable only")
		}
	})
}

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
}

func testSlowAdapterDoesNotBlock(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	fast := factory(t)
	slow := &blockingAdapter{name: "slow"}
	reg := MustNewRegistry(fast, slow)
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sessions := reg.Sessions(ctx)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Sessions took %v, want <1s", elapsed)
	}
	found := false
	for _, s := range sessions {
		if s.AdapterName() == fast.Name() {
			found = true
			break
		}
	}
	if !found {
		t.Error("fast adapter sessions not found")
	}
}

// --- Registry / core tests ---

func testListSessionsEmpty(t *testing.T) {
	t.Helper()
	sessions, err := (&testAdapter{name: "empty"}).ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions on empty adapter: %v", err)
	}
	if sessions != nil && len(sessions) != 0 {
		t.Errorf("empty adapter returned %d sessions, want 0 or nil", len(sessions))
	}
}

func testStaleSnapshotOnFailure(t *testing.T, factory ContractAdapterFactory) {
	t.Helper()
	base := factory(t)
	cfg := &configurableAdapter{name: base.Name(), sessions: []Session{&testSession{id: "s1", adapter: base.Name()}}}
	reg := MustNewRegistry(cfg)
	_ = reg.Sessions(context.Background())
	cfg.mu.Lock()
	cfg.failWith = errors.New("transient connection refused")
	cfg.mu.Unlock()
	reg.InvalidateAdapter(base.Name())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = reg.Sessions(ctx)
	snap, _ := reg.Snapshot(base.Name())
	if len(snap.Sessions) != 1 {
		t.Errorf("stale snapshot has %d sessions after failure, want 1", len(snap.Sessions))
	}
	if snap.LastError == nil {
		t.Error("snapshot LastError is nil after transient failure")
	}
}

func testProbeAdapterTaxonomy(t *testing.T) {
	t.Helper()
	t.Run("timeout", func(t *testing.T) {
		blocker := &blockingAdapter{name: "test"}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		err := ProbeAdapter(ctx, blocker, 50*time.Millisecond)
		if err == nil {
			t.Fatal("ProbeAdapter: got nil, want error")
		}
		if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrAdapterUnavailable) {
			t.Errorf("error taxonomy incomplete: %v", err)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		blocker := &blockingAdapter{name: "test"}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := ProbeAdapter(ctx, blocker, 4*time.Second)
		if err == nil {
			t.Fatal("ProbeAdapter: got nil, want error")
		}
		if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrAdapterUnavailable) {
			t.Errorf("error taxonomy incomplete: %v", err)
		}
	})
}

func testSentinelErrorsDistinct(t *testing.T) {
	t.Helper()
	errs := []error{ErrSessionNotFound, ErrAdapterUnavailable, ErrUnsupported, ErrTimeout, ErrInvalidSessionID, ErrDuplicateAdapter}
	for i, e1 := range errs {
		for j, e2 := range errs {
			if i != j && errors.Is(e1, e2) {
				t.Errorf("%v and %v should be distinct", e1, e2)
			}
		}
	}
}

func testCanonicalIDStability(t *testing.T) {
	t.Helper()
	t.Run("LocalID_with_colon", func(t *testing.T) {
		for _, tt := range []struct{ in, ad, lid string }{
			{"legacy:aider", "legacy", "aider"},
			{"legacy:session:with:colons", "legacy", "session:with:colons"},
			{"legacy:surface:42", "legacy", "surface:42"},
		} {
			ref := ParseSessionID(tt.in)
			if ref.Adapter != tt.ad || ref.LocalID != tt.lid {
				t.Errorf("ParseSessionID(%q) = (%q, %q)", tt.in, ref.Adapter, ref.LocalID)
			}
		}
	})
	t.Run("Unicode", func(t *testing.T) {
		for _, id := range []string{"legacy:한글", "legacy:セッション", "legacy:中文"} {
			if ParseSessionID(id).LocalID == "" {
				t.Errorf("ParseSessionID(%q) empty LocalID", id)
			}
		}
	})
	t.Run("URL_round_trip", func(t *testing.T) {
		lid := "session:with:colons_한글"
		canon := SessionRef{Adapter: "legacy", LocalID: lid}.Canonical()
		enc := url.QueryEscape(canon)
		dec, _ := url.QueryUnescape(enc)
		ref := ParseSessionID(dec)
		if ref.Adapter != "legacy" || ref.LocalID != lid {
			t.Errorf("round-trip failed: %q %q", ref.Adapter, ref.LocalID)
		}
	})
	t.Run("edge_cases", func(t *testing.T) {
		for _, id := range []string{"", ":", "only-adapter:", ":only-local", "a:b:c:d:e"} {
			_ = ParseSessionID(id)
			_ = MigrateLegacyID(id)
		}
	})
}

// --- Capability helpers ---

func requireOrSkip(t *testing.T, cfg *ContractConfig, expected bool, name string, ok bool) {
	t.Helper()
	if cfg != nil && expected && !ok {
		t.Fatalf("expected adapter to support %s but it does not", name)
	}
	if !ok {
		t.Skipf("session does not implement %s", name)
	}
}

// --- Capability-specific contract suites ---

func RunScreenHistoryContract(t *testing.T, cfg *ContractConfig, factory ContractAdapterFactory) {
	t.Helper()
	t.Run("ScreenHistory", func(t *testing.T) {
		adapter := factory(t)
		sessions, err := adapter.ListSessions(context.Background())
		if err != nil {
			t.Fatalf("ListSessions: %v", err)
		}
		if len(sessions) == 0 {
			t.Skip("no sessions")
		}
		s := sessions[0]

		t.Run("ScreenReader", func(t *testing.T) {
			_, ok := s.(ScreenReader)
			requireOrSkip(t, cfg, cfg != nil && cfg.ExpectScreenReader, "ScreenReader", ok)
			out, err := s.(ScreenReader).ReadScreen(context.Background())
			if err != nil {
				t.Fatalf("ReadScreen: %v", err)
			}
			if out == nil {
				t.Error("ReadScreen returned nil")
			}
		})
		t.Run("HistoryReader", func(t *testing.T) {
			_, ok := s.(HistoryReader)
			requireOrSkip(t, cfg, cfg != nil && cfg.ExpectHistoryReader, "HistoryReader", ok)
			out, err := s.(HistoryReader).ReadHistory(context.Background(), 200)
			if err != nil {
				t.Fatalf("ReadHistory: %v", err)
			}
			if out == nil {
				t.Error("ReadHistory returned nil")
			}
		})
	})
}

func RunProcessInfoContract(t *testing.T, cfg *ContractConfig, factory ContractAdapterFactory) {
	t.Helper()
	t.Run("ProcessInfo", func(t *testing.T) {
		adapter := factory(t)
		sessions, err := adapter.ListSessions(context.Background())
		if err != nil {
			t.Fatalf("ListSessions: %v", err)
		}
		if len(sessions) == 0 {
			t.Skip("no sessions")
		}
		s := sessions[0]
		_, ok := s.(ProcessProvider)
		requireOrSkip(t, cfg, cfg != nil && cfg.ExpectProcessProvider, "ProcessProvider", ok)
		info, err := s.(ProcessProvider).ProcessInfo(context.Background())
		if err != nil {
			t.Fatalf("ProcessInfo: %v", err)
		}
		if info.PID == 0 && info.CWD == "" {
			t.Error("ProcessInfo returned empty PID and CWD")
		}
	})
}

func RunProcessSnapshotContract(t *testing.T, cfg *ContractConfig, factory ContractAdapterFactory) {
	t.Helper()
	t.Run("ProcessSnapshot", func(t *testing.T) {
		adapter := factory(t)
		psp, ok := adapter.(ProcessSnapshotProvider)
		requireOrSkip(t, cfg, cfg != nil && cfg.ExpectProcessSnapshot, "ProcessSnapshotProvider", ok)
		snap, err := psp.ProcessSnapshot(context.Background())
		if err != nil {
			t.Fatalf("ProcessSnapshot: %v", err)
		}
		if snap == nil {
			t.Error("ProcessSnapshot returned nil map")
			return
		}
		if len(snap) == 0 {
			t.Error("ProcessSnapshot returned empty map")
			return
		}
		// Cross-check snapshot keys against discovered sessions.
		sessions, listErr := adapter.ListSessions(context.Background())
		if listErr != nil {
			t.Fatalf("ListSessions: %v", listErr)
		}
		sessionIDs := make(map[string]bool)
		for _, s := range sessions {
			sessionIDs[s.ID()] = true
		}
		for key := range snap {
			if key == "" {
				t.Error("ProcessSnapshot map contains empty key")
			}
			if !sessionIDs[key] {
				t.Errorf("ProcessSnapshot key %q not in discovered sessions", key)
			}
		}
		// ProcessSnapshot keys are a subset of discovered sessions.
		// Not every session has an agent process; non-agent sessions
		// (e.g. bare shells without tags) are not required to appear.
		// The forward check above ensures snapshot keys are valid.
	})
}

func RunCreateDiscoverTerminateContract(t *testing.T, cfg *ContractConfig, factory ContractAdapterFactory) {
	t.Helper()
	t.Run("CreateDiscoverTerminate", func(t *testing.T) {
		adapter := factory(t)
		_, hc := adapter.(SessionCreator)
		_, ht := adapter.(SessionTerminator)
		requireOrSkip(t, cfg, cfg != nil && cfg.ExpectSessionCreator, "SessionCreator", hc)
		requireOrSkip(t, cfg, cfg != nil && cfg.ExpectSessionTerminator, "SessionTerminator", ht)

		reg := MustNewRegistry(adapter)
		testName := "cdt-contract-test"

		// Create
		cid, err := reg.CreateSession(context.Background(), adapter.Name(), CreateOptions{Name: testName})
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		if cid == "" {
			t.Fatal("CreateSession returned empty ID")
		}

		// Discover
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		reg.InvalidateAdapter(adapter.Name())
		sessions := reg.Sessions(ctx)
		found := false
		for _, s := range sessions {
			if s.ID() == cid {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("created session %q not found after create", cid)
		}

		// Terminate
		if err := reg.TerminateSession(context.Background(), adapter.Name(), cid); err != nil {
			t.Fatalf("TerminateSession: %v", err)
		}

		// Verify gone
		reg.InvalidateAdapter(adapter.Name())
		for _, s := range reg.Sessions(context.Background()) {
			if s.ID() == cid {
				t.Errorf("session %q still present after TerminateSession", cid)
				return
			}
		}
	})
}

func RunLiveStreamContract(t *testing.T, cfg *ContractConfig, factory ContractAdapterFactory) {
	t.Helper()
	t.Run("LiveStream", func(t *testing.T) {
		adapter := factory(t)
		sessions, err := adapter.ListSessions(context.Background())
		if err != nil {
			t.Fatalf("ListSessions: %v", err)
		}
		if len(sessions) == 0 {
			t.Skip("no sessions")
		}
		s := sessions[0]
		opener, ok := s.(StreamOpener)
		requireOrSkip(t, cfg, cfg != nil && cfg.ExpectStreamOpener, "StreamOpener", ok)

		t.Run("OpenStream", func(t *testing.T) {
			stream, err := opener.OpenStream(context.Background())
			if err != nil {
				if stringsContains(err.Error(), "exec") || stringsContains(err.Error(), "SpawnPTY") || stringsContains(err.Error(), "not found") {
					t.Skipf("OpenStream requires real backend: %v", err)
				}
				t.Fatalf("OpenStream: %v", err)
			}
			if stream == nil {
				t.Fatal("OpenStream returned nil")
			}
			stream.Close()
		})
		t.Run("Read", func(t *testing.T) {
			stream, err := opener.OpenStream(context.Background())
			if err != nil {
				// Allow skip for adapters that fail at PTY attach (e.g. legacy mock).
				if stringsContains(err.Error(), "exec") || stringsContains(err.Error(), "SpawnPTY") || stringsContains(err.Error(), "not found") {
					t.Skipf("OpenStream requires real backend: %v", err)
				}
				t.Fatalf("OpenStream: %v", err)
			}
			defer stream.Close()
			buf := make([]byte, 4096)
			done := make(chan struct{})
			var n int
			var readErr error
			go func() { n, readErr = stream.Read(buf); close(done) }()
			select {
			case <-done:
				if readErr != nil && readErr != io.EOF {
					t.Errorf("Read error: %v", readErr)
				}
				if n == 0 && readErr == nil {
					t.Error("Read returned 0 bytes with no error")
				}
				if n == 0 && readErr == io.EOF {
					t.Error("Read returned EOF with 0 bytes — no content received")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Read timed out")
			}
		})
		t.Run("Close", func(t *testing.T) {
			stream, err := opener.OpenStream(context.Background())
			if err != nil {
				if stringsContains(err.Error(), "exec") || stringsContains(err.Error(), "SpawnPTY") || stringsContains(err.Error(), "not found") {
					t.Skipf("OpenStream requires real backend: %v", err)
				}
				t.Fatalf("OpenStream: %v", err)
			}
			stream.Close()
			stream.Close() // idempotent
			done := make(chan struct{})
			go func() { _, _ = stream.Read(make([]byte, 64)); close(done) }()
			select {
			case <-done:
			case <-time.After(500 * time.Millisecond):
				t.Fatal("Read after Close timed out")
			}
		})
		t.Run("Resize", func(t *testing.T) {
			stream, err := opener.OpenStream(context.Background())
			if err != nil {
				if stringsContains(err.Error(), "exec") || stringsContains(err.Error(), "SpawnPTY") {
					t.Skipf("OpenStream requires real backend: %v", err)
				}
				t.Fatalf("OpenStream: %v", err)
			}
			defer stream.Close()
			if err := stream.Resize(80, 24); err != nil {
				t.Errorf("Resize failed: %v", err)
			}
		})
		t.Run("Write", func(t *testing.T) {
			stream, err := opener.OpenStream(context.Background())
			if err != nil {
				if stringsContains(err.Error(), "exec") || stringsContains(err.Error(), "SpawnPTY") || stringsContains(err.Error(), "not found") {
					t.Skipf("OpenStream requires real backend: %v", err)
				}
				t.Fatalf("OpenStream: %v", err)
			}
			defer stream.Close()
			n, err := stream.Write([]byte("ls\n"))
			if err != nil {
				// Some adapters (legacy) use InputWriter instead of stream.Write.
				// This is a valid design choice — not an error.
				if stringsContains(err.Error(), "not implemented") || stringsContains(err.Error(), "InputWriter") {
					t.Skipf("Write not implemented; adapter uses InputWriter: %v", err)
				}
				t.Fatalf("Write failed: %v", err)
			}
			if n == 0 {
				t.Error("Write returned 0 bytes")
			}
		})
		t.Run("WriteInput", func(t *testing.T) {
			iw, ok := s.(InputWriter)
			if !ok {
				t.Skip("no InputWriter")
			}
			if err := iw.WriteInput(context.Background(), []byte("echo test\n")); err != nil {
				t.Fatalf("WriteInput: %v", err)
			}
		})
	})
}

// --- Helpers ---

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
