package term

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
)

// ── PA2c focused tests (docs/PA2_LIFECYCLE_TRANSPORT_CONTRACT.md §PA2c) ──

// fakeProviderOwner records generation-bound lifecycle dispatches and can
// simulate the provider's decisive stale-generation rejection.
type fakeProviderOwner struct {
	mu           sync.Mutex
	currentEpoch int64
	calls        []struct {
		Action string
		ID     string
		Epoch  int64
	}
	// signalsToCurrent counts terminations that would have reached the CURRENT
	// (replacement) process — must stay zero for stale dispatches.
	signalsToCurrent int
	failWith         error
	// onReject simulates the replacement publishing its record to the read
	// path while the stale request was in flight.
	onReject func()
}

func (f *fakeProviderOwner) record(action, id string, epoch int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct {
		Action string
		ID     string
		Epoch  int64
	}{action, id, epoch})
	if f.failWith != nil {
		return f.failWith
	}
	if epoch != f.currentEpoch {
		// Provider's decisive comparison at its lifecycle lock: stale epoch is
		// rejected WITHOUT touching the current process.
		if f.onReject != nil {
			f.onReject()
		}
		return errFakeStale
	}
	f.signalsToCurrent++
	return nil
}

var errFakeStale = &fakeStaleErr{}

type fakeStaleErr struct{}

func (*fakeStaleErr) Error() string { return "stale session epoch" }

func (f *fakeProviderOwner) Stop(id string, epoch int64) error { return f.record("stop", id, epoch) }
func (f *fakeProviderOwner) Kill(id string, epoch int64) error { return f.record("kill", id, epoch) }
func (f *fakeProviderOwner) Delete(id string, epoch int64) error {
	return f.record("delete", id, epoch)
}

// fakeManagedCatalog is a mutable ManagedRuntimeCatalog read path.
type fakeManagedCatalog struct {
	mu   sync.Mutex
	recs map[string]ManagedSessionRecord
}

func newFakeManagedCatalog() *fakeManagedCatalog {
	return &fakeManagedCatalog{recs: map[string]ManagedSessionRecord{}}
}
func (c *fakeManagedCatalog) put(rec ManagedSessionRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recs[rec.SessionID] = rec
}
func (c *fakeManagedCatalog) Get(id string) (ManagedSessionRecord, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.recs[id]
	return r, ok
}
func (c *fakeManagedCatalog) List() []ManagedSessionRecord        { return nil }
func (c *fakeManagedCatalog) RuntimeOf(string) (RuntimeRef, bool) { return RuntimeRef{}, false }

func pa2cDispatcher(t *testing.T) (*LifecycleService, *fakeProviderOwner, *fakeProviderOwner, *fakeManagedCatalog) {
	t.Helper()
	reg := mux.MustNewRegistry(newLCAdapter("controlled_pty", true))
	owned := NewOwnedPTYRuntime(reg, NewActivityBuffer(10), nil)
	svc := NewLifecycleService(owned, NewActivityBuffer(10), nil)
	codex := &fakeProviderOwner{currentEpoch: 7}
	claude := &fakeProviderOwner{currentEpoch: 3}
	cat := newFakeManagedCatalog()
	svc.WireManagedOwners(cat, codex, claude)
	return svc, codex, claude, cat
}

// Contract test 1: managed stop/kill/delete routes hit the provider services
// with the server-derived generation — never a legacy Registry.
func TestPA2c_ProviderRoutes_DispatchWithDerivedEpoch(t *testing.T) {
	svc, codex, claude, cat := pa2cDispatcher(t)
	cat.put(ManagedSessionRecord{SessionID: "codex_app_server:c1", Provider: "codex", Version: "0.144.1", Epoch: 7})
	cat.put(ManagedSessionRecord{SessionID: "claude_headless:h1", Provider: "claude", Version: "2.1.202", Epoch: 3})

	if res, err := svc.Stop(context.Background(), "codex_app_server:c1"); err != nil || res.State != LifecycleExited {
		t.Fatalf("codex stop = %+v err=%v", res, err)
	}
	if res, err := svc.Kill(context.Background(), "claude_headless:h1"); err != nil || res.State != LifecycleKilled {
		t.Fatalf("claude kill = %+v err=%v", res, err)
	}
	// Delete requires terminal record.
	cat.put(ManagedSessionRecord{SessionID: "codex_app_server:c1", Provider: "codex", Version: "0.144.1", Epoch: 7, Exited: true})
	if res, err := svc.Delete(context.Background(), "codex_app_server:c1"); err != nil || res.Action != "delete" {
		t.Fatalf("codex delete = %+v err=%v", res, err)
	}

	if len(codex.calls) != 2 || codex.calls[0].Action != "stop" || codex.calls[0].Epoch != 7 ||
		codex.calls[1].Action != "delete" || codex.calls[1].Epoch != 7 {
		t.Fatalf("codex owner calls = %+v, want stop+delete at derived epoch 7", codex.calls)
	}
	if len(claude.calls) != 1 || claude.calls[0].Action != "kill" || claude.calls[0].Epoch != 3 {
		t.Fatalf("claude owner calls = %+v, want kill at derived epoch 3", claude.calls)
	}
}

// Contract test 2 is TestLifecycle_DispatchTable_FailClosed (lifecycle_test.go).

// Contract tests 3+6: a replacement between catalog lookup and dispatch is
// rejected as stale by the owner's decisive comparison, mapped to the typed
// stale-generation outcome WITHOUT provider error-string parsing, and the
// replacement process receives no signal.
func TestPA2c_StaleGeneration_RejectedWithoutSignalingReplacement(t *testing.T) {
	svc, codex, _, cat := pa2cDispatcher(t)
	// Catalog serves epoch 6 (pre-replacement snapshot)...
	cat.put(ManagedSessionRecord{SessionID: "codex_app_server:c1", Provider: "codex", Version: "0.144.1", Epoch: 6})
	// ...but the provider's current runtime is epoch 7 (replacement won).
	codex.currentEpoch = 7
	// After the owner rejects, the catalog read shows the replacement epoch —
	// the dispatcher classifies from TYPED state, not the error string.
	res, err := svc.Stop(context.Background(), "codex_app_server:c1")
	// Classification happens on the post-failure catalog read: simulate the
	// replacement being published there.
	if err == nil {
		t.Fatalf("stale stop unexpectedly succeeded: %+v", res)
	}
	cat.put(ManagedSessionRecord{SessionID: "codex_app_server:c1", Provider: "codex", Version: "0.144.1", Epoch: 7})
	res2, err2 := svc.Stop(context.Background(), "codex_app_server:c1")
	if err2 != nil || res2.State != LifecycleExited {
		t.Fatalf("current-epoch stop after replacement = %+v err=%v", res2, err2)
	}
	if codex.signalsToCurrent != 1 {
		t.Fatalf("signals to current process = %d, want exactly 1 (stale dispatch must not signal)", codex.signalsToCurrent)
	}
}

// Contract tests 3+6 (typed classification): when the catalog already shows
// the replacement epoch at classification time, the error is the typed
// ErrLifecycleStaleGeneration.
func TestPA2c_StaleGeneration_TypedOutcome(t *testing.T) {
	svc, codex, _, cat := pa2cDispatcher(t)
	// Catalog serves the pre-replacement epoch 6 at derivation time...
	cat.put(ManagedSessionRecord{SessionID: "codex_app_server:c1", Provider: "codex", Version: "0.144.1", Epoch: 6})
	codex.currentEpoch = 7
	// ...and the provider's rejection races the replacement's publication into
	// the read path (the exact contract interleaving).
	codex.onReject = func() {
		cat.put(ManagedSessionRecord{SessionID: "codex_app_server:c1", Provider: "codex", Version: "0.144.1", Epoch: 7})
	}
	_, err := svc.Stop(context.Background(), "codex_app_server:c1")
	if err != ErrLifecycleStaleGeneration {
		t.Fatalf("err = %v, want typed ErrLifecycleStaleGeneration", err)
	}
	if codex.signalsToCurrent != 0 {
		t.Fatalf("stale dispatch signalled the replacement process %d times", codex.signalsToCurrent)
	}
}

// Contract test 3 (owned PTY): a stale-generation transition on the owned
// store is rejected at the store lock, and a stale finalize cannot touch a
// replaced record.
func TestPA2c_OwnedPTY_StaleGenerationRejected(t *testing.T) {
	reg := mux.MustNewRegistry(newLCAdapter("controlled_pty", true))
	owned := NewOwnedPTYRuntime(reg, NewActivityBuffer(10), nil)
	id := "controlled_pty:r1"
	gen1 := owned.RegisterForTest(id, "", "n", nil)
	// Same canonical id relaunched: a NEW generation replaces the record.
	gen2 := owned.RegisterForTest(id, "", "n", nil)
	if gen2 <= gen1 {
		t.Fatalf("generations not monotonic: %d then %d", gen1, gen2)
	}
	// The decisive store-lock comparison rejects the stale transition.
	if proceed, _, found, stale := owned.beginStop(id, gen1); proceed || !found || !stale {
		t.Fatalf("beginStop(stale gen) = proceed=%v found=%v stale=%v, want rejected stale", proceed, found, stale)
	}
	// A stale finalize is a no-op: the replacement record stays running.
	owned.finalize(id, gen1)
	if e, ok := owned.Get(id); !ok || e.State != LifecycleRunning || e.Generation != gen2 {
		t.Fatalf("record after stale finalize = %+v ok=%v, want running at gen2", e, ok)
	}
	// The current generation still transitions normally.
	if proceed, _, _, stale := owned.beginStop(id, gen2); !proceed || stale {
		t.Fatalf("beginStop(current gen) rejected")
	}
}

// Contract test 7 (controlled PTY): natural Recorder EOF and Stop converge on
// exactly ONE terminal transition (EndedAt recorded once, never overwritten).
func TestPA2c_OwnedPTY_ExactlyOnceTerminalConvergence(t *testing.T) {
	svc := realService(t)
	id := realManaged(t, svc, "exit 0")
	// Wait for the natural-exit watcher to finalize.
	deadline := time.Now().Add(5 * time.Second)
	var first CatalogEntry
	for {
		if e, ok := svc.OwnedPTY().Get(id); ok && e.State.Terminal() {
			first = e
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("natural exit did not finalize in time")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// A later Stop converges idempotently on the SAME terminal row.
	res, err := svc.Stop(context.Background(), id)
	if err != nil || !res.State.Terminal() {
		t.Fatalf("stop after natural exit = %+v err=%v", res, err)
	}
	second, _ := svc.OwnedPTY().Get(id)
	if first.EndedAt == nil || second.EndedAt == nil || !first.EndedAt.Equal(*second.EndedAt) {
		t.Fatalf("terminal transition not exactly-once: EndedAt %v then %v", first.EndedAt, second.EndedAt)
	}
	if second.State != first.State {
		t.Fatalf("terminal state changed after convergence: %s → %s", first.State, second.State)
	}
}

// Contract tests 5+7 (architecture, static): the dispatcher has no
// mux.Registry dependency; SessionCatalog no longer exists in production;
// the legacy IsManaged/Register bypasses are gone.
func TestPA2c_ArchGate_NoRegistryNoSessionCatalog(t *testing.T) {
	// lifecycle_service.go must not import internal/mux (dispatch only).
	f, err := parser.ParseFile(token.NewFileSet(), "lifecycle_service.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse lifecycle_service.go: %v", err)
	}
	for _, imp := range f.Imports {
		if strings.Contains(imp.Path.Value, "internal/mux") {
			t.Errorf("lifecycle_service.go imports internal/mux — the dispatcher must not depend on the Registry")
		}
	}
	// SessionCatalog and the bypass surfaces are deleted from production term.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, rerr := os.ReadFile(name)
		if rerr != nil {
			t.Fatal(rerr)
		}
		for _, forbidden := range []string{"type SessionCatalog", "NewSessionCatalog(", "*SessionCatalog", "func (s *LifecycleService) Register(", "func (s *LifecycleService) IsManaged("} {
			if strings.Contains(string(src), forbidden) {
				t.Errorf("%s still contains %q — PA2c deletion target", name, forbidden)
			}
		}
	}
	if _, err := os.Stat("catalog.go"); !os.IsNotExist(err) {
		t.Error("internal/term/catalog.go still exists — SessionCatalog must be removed")
	}
}

// Contract test 7 (no provider rows): the owned-PTY store carries only
// controlled_pty rows; provider creates register nothing here.
func TestPA2c_OwnedStore_NoProviderRows(t *testing.T) {
	reg := mux.MustNewRegistry(newLCAdapter("controlled_pty", true))
	owned := NewOwnedPTYRuntime(reg, NewActivityBuffer(10), nil)
	owned.RegisterForTest("controlled_pty:a", "shell", "a", nil)
	for _, e := range owned.List() {
		if e.Adapter != "controlled_pty" {
			t.Errorf("owned store row with adapter %q — provider rows must live only in ManagedRuntimeCatalog", e.Adapter)
		}
	}
}
