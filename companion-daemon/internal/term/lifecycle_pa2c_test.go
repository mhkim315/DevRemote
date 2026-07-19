package term

import (
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
)

// ── PA2c focused tests (docs/PA2_LIFECYCLE_TRANSPORT_CONTRACT.md §PA2c) ──

// fakeProviderOwner is a typed-outcome lifecycle owner performing the
// decisive generation comparison itself (modelling the frozen provider's
// lifecycle lock) and returning the CLOSED outcome vocabulary.
type fakeProviderOwner struct {
	mu           sync.Mutex
	currentEpoch int64
	calls        []struct {
		Action string
		ID     string
		Epoch  int64
	}
	// signalsToCurrent counts terminations that reached the CURRENT
	// (replacement) process — must stay zero for stale dispatches.
	signalsToCurrent int
}

func (f *fakeProviderOwner) decide(action, id string, epoch int64) LifecycleOutcome {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct {
		Action string
		ID     string
		Epoch  int64
	}{action, id, epoch})
	if epoch != f.currentEpoch {
		// Decisive comparison: stale generation rejected WITHOUT touching the
		// current process, expressed as the typed outcome — no error strings.
		return OutcomeStaleGeneration
	}
	f.signalsToCurrent++
	return OutcomeAccepted
}

func (f *fakeProviderOwner) Stop(id string, epoch int64) LifecycleOutcome {
	return f.decide("stop", id, epoch)
}
func (f *fakeProviderOwner) Kill(id string, epoch int64) LifecycleOutcome {
	return f.decide("kill", id, epoch)
}
func (f *fakeProviderOwner) Delete(id string, epoch int64) LifecycleOutcome {
	return f.decide("delete", id, epoch)
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
	ctlAdapter, _ := reg.Adapter("controlled_pty")
	owned := NewOwnedPTYRuntime(ctlAdapter, nil)
	svc := NewLifecycleService(owned, nil)
	codex := &fakeProviderOwner{currentEpoch: 7}
	claude := &fakeProviderOwner{currentEpoch: 3}
	cat := newFakeManagedCatalog()
	svc.WireManagedOwners(cat, codex, claude)
	return svc, codex, claude, cat
}

// Contract test 1: managed stop/kill/delete routes hit the provider owners
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

// Contract tests 3+6 (R1 fixed): a replacement between catalog lookup and
// dispatch is rejected by the owner's decisive comparison as the EXACT typed
// stale-generation outcome — asserted precisely, deterministically, and
// BEFORE the replacement publishes anywhere (the federated catalog still
// serves the stale epoch when the outcome is produced). The replacement
// process receives no signal.
func TestPA2c_StaleGeneration_RejectedWithoutSignalingReplacement(t *testing.T) {
	svc, codex, _, cat := pa2cDispatcher(t)
	// Catalog serves epoch 6 (pre-replacement snapshot)...
	cat.put(ManagedSessionRecord{SessionID: "codex_app_server:c1", Provider: "codex", Version: "0.144.1", Epoch: 6})
	// ...but the provider's current runtime is epoch 7 (replacement won and
	// has NOT been published to any read path).
	codex.currentEpoch = 7

	_, err := svc.Stop(context.Background(), "codex_app_server:c1")
	if err != ErrLifecycleStaleGeneration {
		t.Fatalf("stale stop err = %v, want exact ErrLifecycleStaleGeneration (pre-publication)", err)
	}
	if codex.signalsToCurrent != 0 {
		t.Fatalf("stale dispatch signalled the replacement process %d times", codex.signalsToCurrent)
	}

	// Once the replacement publishes, the same route dispatches cleanly to
	// the current generation.
	cat.put(ManagedSessionRecord{SessionID: "codex_app_server:c1", Provider: "codex", Version: "0.144.1", Epoch: 7})
	res2, err2 := svc.Stop(context.Background(), "codex_app_server:c1")
	if err2 != nil || res2.State != LifecycleExited {
		t.Fatalf("current-epoch stop after publication = %+v err=%v", res2, err2)
	}
	if codex.signalsToCurrent != 1 {
		t.Fatalf("signals to current process = %d, want exactly 1", codex.signalsToCurrent)
	}
}

// R1 blocker 3: the PRODUCTION provider adapter (NewManagedProviderOwner over
// a frozen-service-shaped call) classifies a stale rejection from the
// provider-OWNED registry record — deterministically, before any replacement
// publication to the federated catalog, and without reading the provider's
// error string.
func TestPA2c_R1_ProviderWrapper_StaleBeforePublication(t *testing.T) {
	provReg := NewManagedSessionRegistry(8)
	const sid = "codex_app_server:c1"
	// The provider-owned registry holds the CURRENT (replacement) runtime at
	// epoch 7.
	if err := provReg.Register(ManagedSessionRecord{
		SessionID: sid, Provider: "codex", Version: "0.144.1", Epoch: 7, ProcessID: "p1",
	}); err != nil {
		t.Fatalf("seed provider registry: %v", err)
	}
	// The frozen service surface rejects with an ARBITRARY error the wrapper
	// must not parse.
	var currentSignals int
	stop := func(id string, epoch int64) error {
		if epoch != 7 {
			return fmt.Errorf("some opaque provider refusal text %d", epoch)
		}
		currentSignals++
		return nil
	}
	owner := NewManagedProviderOwner(provReg, stop, stop, stop)

	reg := mux.MustNewRegistry(newLCAdapter("controlled_pty", true))
	ctlAdapter, _ := reg.Adapter("controlled_pty")
	svc := NewLifecycleService(NewOwnedPTYRuntime(ctlAdapter, nil), nil)
	cat := newFakeManagedCatalog()
	// The FEDERATED catalog still serves the pre-replacement epoch 6 — the
	// replacement has published nowhere outside the provider's own registry.
	cat.put(ManagedSessionRecord{SessionID: sid, Provider: "codex", Version: "0.144.1", Epoch: 6})
	svc.WireManagedOwners(cat, owner, nil)

	_, err := svc.Stop(context.Background(), sid)
	if err != ErrLifecycleStaleGeneration {
		t.Fatalf("err = %v, want exact ErrLifecycleStaleGeneration from provider-owned registry classification", err)
	}
	if currentSignals != 0 {
		t.Fatalf("replacement runtime was signalled %d times by a stale dispatch", currentSignals)
	}
	// Typed pre-call classification also covers the remaining closed
	// outcomes without invoking the provider (R2: pre-call authoritative
	// state only).
	if oc := owner.Stop("codex_app_server:missing", 7); oc != OutcomeNotFound {
		t.Fatalf("Stop(unknown) = %s, want not_found", oc)
	}
}

// R1 blockers 1+2 (owned PTY): after beginStop claims the generation's
// immutable handle, a same-id replacement can proceed WHILE the stale action
// is blocked in signal delivery (no lifecycle lock across I/O), the
// replacement's process is never signalled, and the stale finalize cannot
// touch the replacement record or its registry session.
type blockingHandle struct {
	started  chan struct{} // closed when TerminateGroup begins blocking
	release  chan struct{} // closed by the test to let the signal complete
	signals  int32
	mu       sync.Mutex
	forceLog []bool
}

func (h *blockingHandle) TerminateGroup(force bool) error {
	h.mu.Lock()
	h.signals++
	h.forceLog = append(h.forceLog, force)
	h.mu.Unlock()
	close(h.started)
	<-h.release
	return nil
}

type countingHandle struct{ signals atomic.Int32 }

func (h *countingHandle) TerminateGroup(bool) error {
	h.signals.Add(1)
	return nil
}

func TestPA2c_R1_ReplacementDuringBlockedSignal_NeverSignalsNewProcess(t *testing.T) {
	adapter := newLCAdapter("controlled_pty", true)
	reg := mux.MustNewRegistry(adapter)
	ctlAdapter, _ := reg.Adapter("controlled_pty")
	owned := NewOwnedPTYRuntime(ctlAdapter, nil)
	owned.graceful = 50 * time.Millisecond
	owned.killGrace = 50 * time.Millisecond

	const id = "controlled_pty:replace-1"
	h1 := &blockingHandle{started: make(chan struct{}), release: make(chan struct{})}
	h2 := &countingHandle{}
	gen1 := owned.RegisterForTestWithHandle(id, "", "old", h1, nil)

	// Stale Stop blocks inside h1.TerminateGroup — OUTSIDE every lock.
	stopDone := make(chan error, 1)
	go func() {
		_, err := owned.Stop(context.Background(), id)
		stopDone <- err
	}()
	<-h1.started

	// The replacement proceeds promptly while the stale signal is blocked —
	// this would deadlock if any lifecycle lock were held across the I/O.
	regDone := make(chan int64, 1)
	go func() {
		regDone <- owned.RegisterForTestWithHandle(id, "", "new", h2, nil)
	}()
	var gen2 int64
	select {
	case gen2 = <-regDone:
	case <-time.After(2 * time.Second):
		t.Fatal("replacement blocked behind a stale action's signal I/O — lifecycle lock held across TerminateGroup")
	}
	if gen2 <= gen1 {
		t.Fatalf("replacement generation %d not newer than %d", gen2, gen1)
	}

	// Let the stale action finish: no recorder exists, so awaitExit succeeds
	// and finalize targets gen1 — which is stale now, so it must be a no-op.
	close(h1.release)
	if err := <-stopDone; err != nil {
		t.Fatalf("stale stop returned %v (idempotent completion expected)", err)
	}

	// The replacement is untouched: still running at gen2, its process never
	// signalled, its registry entry never terminated by the stale finalize.
	e, ok := owned.Get(id)
	if !ok || e.State != LifecycleRunning || e.Generation != gen2 {
		t.Fatalf("replacement record = %+v ok=%v, want running at gen %d", e, ok, gen2)
	}
	if h2.signals.Load() != 0 {
		t.Fatalf("replacement process signalled %d times by the stale action", h2.signals.Load())
	}
	if h1.signals != 1 {
		t.Fatalf("captured old handle signalled %d times, want exactly 1", h1.signals)
	}
	adapter.mu.Lock()
	terminated := len(adapter.terminated)
	adapter.mu.Unlock()
	if terminated != 0 {
		t.Fatalf("stale finalize terminated %d registry sessions of the replacement", terminated)
	}
}

// Contract test 3 (owned PTY): a stale-generation transition on the owned
// store is rejected at the store lock, and a stale finalize cannot touch a
// replaced record.
func TestPA2c_OwnedPTY_StaleGenerationRejected(t *testing.T) {
	reg := mux.MustNewRegistry(newLCAdapter("controlled_pty", true))
	ctlAdapter, _ := reg.Adapter("controlled_pty")
	owned := NewOwnedPTYRuntime(ctlAdapter, nil)
	id := "controlled_pty:r1"
	gen1 := owned.RegisterForTest(id, "", "n", nil)
	// Same canonical id relaunched: a NEW generation replaces the record.
	gen2 := owned.RegisterForTest(id, "", "n", nil)
	if gen2 <= gen1 {
		t.Fatalf("generations not monotonic: %d then %d", gen1, gen2)
	}
	// The decisive store-lock comparison rejects the stale transition.
	if proceed, _, found, stale, _ := owned.beginStop(id, gen1); proceed || !found || !stale {
		t.Fatalf("beginStop(stale gen) = proceed=%v found=%v stale=%v, want rejected stale", proceed, found, stale)
	}
	// A stale finalize is a no-op: the replacement record stays running.
	owned.finalize(id, gen1)
	if e, ok := owned.Get(id); !ok || e.State != LifecycleRunning || e.Generation != gen2 {
		t.Fatalf("record after stale finalize = %+v ok=%v, want running at gen2", e, ok)
	}
	// The current generation still transitions normally.
	if proceed, _, _, stale, _ := owned.beginStop(id, gen2); !proceed || stale {
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
// the legacy IsManaged/Register bypasses are gone; lifecycle signalling
// never resolves a process from the Registry at action time.
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
	// PA2d: no Registry resolution at all in the owned lifecycle path.
	// FindSession count is zero (capture uses adapter.ListSessions; cleanup
	// calls CompareAndTerminate on the adapter directly, not through
	// Registry).
	src, err := os.ReadFile("owned_pty_runtime.go")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(src), "FindSession"); n != 0 {
		t.Errorf("owned_pty_runtime.go has %d FindSession calls, want 0 (PA2d: adapter.ListSessions for capture, adapter CompareAndTerminate for cleanup)", n)
	}
	if !strings.Contains(string(src), "CompareAndTerminate(") {
		t.Error("owned_pty_runtime.go does not call CompareAndTerminate (adapter-level) — cleanup must use the atomic API")
	}
}

// Contract test 7 (no provider rows): the owned-PTY store carries only
// controlled_pty rows; provider creates register nothing here.
func TestPA2c_OwnedStore_NoProviderRows(t *testing.T) {
	reg := mux.MustNewRegistry(newLCAdapter("controlled_pty", true))
	ctlAdapter, _ := reg.Adapter("controlled_pty")
	owned := NewOwnedPTYRuntime(ctlAdapter, nil)
	owned.RegisterForTest("controlled_pty:a", "shell", "a", nil)
	for _, e := range owned.List() {
		if e.Adapter != "controlled_pty" {
			t.Errorf("owned store row with adapter %q — provider rows must live only in ManagedRuntimeCatalog", e.Adapter)
		}
	}
}

// ── PA2c-R2 remediation tests ──

// handlelessSession supports streaming (recorder readiness passes) but is NOT
// a mux.ManagedProcess — modelling a spawn whose process control could not be
// bound.
type handlelessSession struct {
	id     string
	stream *fakeStream
}

func (s *handlelessSession) ID() string          { return s.id }
func (s *handlelessSession) AdapterName() string { return "controlled_pty" }
func (s *handlelessSession) Title() string       { return s.id }
func (s *handlelessSession) OpenStream(context.Context) (mux.TerminalStream, error) {
	return s.stream, nil
}

type handlelessAdapter struct {
	mu         sync.Mutex
	sessions   map[string]*handlelessSession
	terminated []string
}

func (a *handlelessAdapter) Name() string { return "controlled_pty" }
func (a *handlelessAdapter) ListSessions(context.Context) ([]mux.Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]mux.Session, 0, len(a.sessions))
	for _, s := range a.sessions {
		out = append(out, s)
	}
	return out, nil
}
func (a *handlelessAdapter) CreateSession(_ context.Context, opts mux.CreateOptions) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sessions[opts.Name] = &handlelessSession{id: opts.Name, stream: &fakeStream{closed: make(chan struct{})}}
	return opts.Name, nil
}
func (a *handlelessAdapter) TerminateSession(_ context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.terminated = append(a.terminated, id)
	delete(a.sessions, id)
	return nil
}
func (a *handlelessAdapter) TranscriptCaptureMode() mux.TranscriptCaptureMode {
	return mux.CaptureModeByteStream
}
func (a *handlelessAdapter) ManagedLifecycle() bool { return true }

// R2 finding 1: creation MUST NOT publish a running generation without its
// exact process handle — a failed capture rolls the spawn back unpublished.
func TestPA2c_R2_CreateWithoutHandle_FailsWithoutPublishing(t *testing.T) {
	adapter := &handlelessAdapter{sessions: map[string]*handlelessSession{}}
	reg := mux.MustNewRegistry(adapter)
	ctlAdapter, _ := reg.Adapter("controlled_pty")
	owned := NewOwnedPTYRuntime(ctlAdapter, nil)

	local := genLocalID("nohandle")
	_, err := owned.Create(context.Background(), mux.CreateOptions{Name: local}, "shell", "n")
	if err == nil {
		t.Fatal("Create published a running generation without a process handle")
	}
	if rows := owned.List(); len(rows) != 0 {
		t.Fatalf("owned store has %d rows after failed capture, want 0 (unpublished)", len(rows))
	}
	// PA2d-R2: the SessionIdentityTerminator check now fails BEFORE any
	// spawn occurs (fail-closed at the adapter capability boundary), so
	// the adapter was never asked to terminate a runtime. The store is
	// empty and no recorder was ever started.
	adapter.mu.Lock()
	terminated := len(adapter.terminated)
	adapter.mu.Unlock()
	if terminated != 0 {
		t.Fatalf("spawned runtime terminated %d times on pre-spawn rejection, want 0", terminated)
	}
	// No recorder was created (spawn never happened).
	if GetRecorder("controlled_pty:"+local) != nil {
		t.Fatal("recorder present despite pre-spawn rejection")
	}
}

// R2 finding 2: the generation-bound cleanup capability, even when CLAIMED
// while its generation was current and then raced by a same-id replacement
// BETWEEN the claim (check) and the invocation (cleanup), can never
// terminate the replacement's registry session or recorder: the capability
// is instance-guarded, not id-addressed.
func TestPA2c_R2_ReplacementBetweenClaimAndCleanup_NotTerminated(t *testing.T) {
	adapter := &lcAdapter{name: "controlled_pty", managed: true, sessions: map[string]*lcSession{}}
	reg := mux.MustNewRegistry(adapter)
	ctlAdapter, _ := reg.Adapter("controlled_pty")
	owned := NewOwnedPTYRuntime(ctlAdapter, nil)

	const local = "claimrace"
	const id = "controlled_pty:" + local

	// Generation 1: capture its exact session instance into the capability.
	adapter.add(local)
	sessA, _, err := owned.captureSession(context.Background(), local)
	if err != nil {
		t.Fatalf("capture gen1 session: %v", err)
	}
	cleanupA := owned.newCleanup(id, sessA, nil)
	gen1 := owned.register(id, "", "old", nil, cleanupA, nil, nil, nil)

	// CLAIM the capability while gen1 is still current (the "check").
	claimed, finalized, done := owned.finalizeRecord(id, gen1)
	if !finalized || claimed == nil || done == nil {
		t.Fatalf("finalizeRecord(current gen) = claimed=%v finalized=%v, want claim", claimed != nil, finalized)
	}

	// Replacement lands BETWEEN claim and cleanup: same canonical id, new
	// session instance, new generation.
	adapter.mu.Lock()
	adapter.sessions[local] = &lcSession{id: local, adapter: "controlled_pty"}
	adapter.mu.Unlock()
	reg.InvalidateAdapter("controlled_pty")
	gen2 := owned.register(id, "", "new", nil, func(context.Context) {}, nil, nil, nil)
	if gen2 <= gen1 {
		t.Fatalf("replacement generation not newer: %d then %d", gen1, gen2)
	}

	// Invoke the stale claimed capability: the instance guard must refuse to
	// terminate the replacement's session.
	claimed(context.Background())
	adapter.mu.Lock()
	terminated := len(adapter.terminated)
	_, present := adapter.sessions[local]
	adapter.mu.Unlock()
	if terminated != 0 {
		t.Fatalf("stale claimed cleanup terminated %d registry sessions of the replacement", terminated)
	}
	if !present {
		t.Fatal("replacement registry session missing after stale cleanup")
	}
	if e, ok := owned.Get(id); !ok || e.Generation != gen2 || e.State != LifecycleRunning {
		t.Fatalf("replacement record = %+v ok=%v, want running at gen %d", e, ok, gen2)
	}
	// And the capability can never be claimed twice; a stale generation gets
	// no wait channel either (it must never wait on a replacement).
	if cl, again, staleDone := owned.finalizeRecord(id, gen1); again || cl != nil || staleDone != nil {
		t.Fatal("stale generation finalized/claimed/waited a second time")
	}
}

// R2 finding 3: the real Codex-shaped timeout path — the frozen service
// marks the record exited at the SAME generation and then returns an error —
// must classify as termination_failed (HTTP 500), never as already_terminal
// success. already_terminal is produced ONLY from authoritative PRE-call
// state.
func TestPA2c_R2_CodexTimeout_MarkExitedThenError_IsTerminationFailed(t *testing.T) {
	provReg := NewManagedSessionRegistry(8)
	const sid = "codex_app_server:t1"
	if err := provReg.Register(ManagedSessionRecord{
		SessionID: sid, Provider: "codex", Version: "0.144.1", Epoch: 7, ProcessID: "p1",
	}); err != nil {
		t.Fatalf("seed provider registry: %v", err)
	}
	calls := 0
	stop := func(id string, epoch int64) error {
		calls++
		// Frozen ManagedCodexService.Stop timeout path: mark non-current so a
		// stopped session can never look live, then report the failure.
		provReg.MarkExited(id, epoch)
		return errors.New("managed session stop: child did not exit within the bound")
	}
	owner := NewManagedProviderOwner(provReg, stop, stop, stop)

	if oc := owner.Stop(sid, 7); oc != OutcomeTerminationFailed {
		t.Fatalf("MarkExited-then-error Stop outcome = %s, want termination_failed", oc)
	}
	if calls != 1 {
		t.Fatalf("provider called %d times, want 1", calls)
	}

	// Dispatcher/HTTP mapping stays contract-compliant: 500-class sentinel.
	if err := mapOutcome(OutcomeTerminationFailed); err != ErrLifecycleTerminateFailed {
		t.Fatalf("mapOutcome(termination_failed) = %v, want ErrLifecycleTerminateFailed", err)
	}

	// PRE-call authoritative state: the record is now genuinely exited, so a
	// repeated Stop is already_terminal WITHOUT invoking the provider.
	if oc := owner.Stop(sid, 7); oc != OutcomeAlreadyTerminal {
		t.Fatalf("pre-call exited Stop outcome = %s, want already_terminal", oc)
	}
	if calls != 1 {
		t.Fatalf("pre-call already_terminal invoked the provider (calls=%d), must not", calls)
	}
	if err := mapOutcome(OutcomeAlreadyTerminal); err != nil {
		t.Fatalf("mapOutcome(already_terminal) = %v, want nil (idempotent success)", err)
	}
}
