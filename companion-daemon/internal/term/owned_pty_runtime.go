package term

import (
	"context"
	"fmt"
	"io"
	"log"
	"syscall"
	"sync"
	"time"

	"devremote/companion-daemon/internal/sessionid"
	"devremote/companion-daemon/internal/transcript"
)

// PA2c: OwnedPTYRuntime is the single lifecycle owner for controlled-PTY
// runtimes. It owns launch identity, the generation-bound lifecycle store
// (the former controlled-PTY portion of SessionCatalog), process
// termination, and exactly-once terminal convergence. It is NOT a
// TerminalTransport: byte transport cannot terminate, delete, certify,
// replace, or restore a runtime.
//
// Temporary PA2c seam (removed in PA2d): creation and process-group
// termination call into the existing mux PTY spawn primitives through
// *mux.Registry. The Registry is used ONLY as the spawn/terminate seam for
// runtimes this owner launched — never for lifecycle authority, which lives
// exclusively in the generation-bound store below.

// CatalogEntry is the controlled-PTY lifecycle record (row shape preserved
// byte-for-byte from the pre-PA2c SessionCatalog so /api/sessions
// projections are unchanged).
type CatalogEntry struct {
	ID        string         `json:"id"` // canonical <adapter>:<local-id>
	Adapter   string         `json:"adapter"`
	ProfileID string         `json:"profileId,omitempty"`
	Name      string         `json:"name,omitempty"`
	State     LifecycleState `json:"state"`
	StartedAt time.Time      `json:"startedAt"`
	EndedAt   *time.Time     `json:"endedAt,omitempty"`
	ExitCode  *int           `json:"exitCode,omitempty"`

	// Generation is the daemon-allocated launch generation for this exact
	// runtime instance. Lifecycle transitions are accepted only for the
	// matching generation (stale-generation operations are rejected at the
	// store lock — the linearization point). Not serialized: lifecycle
	// generations are server-internal, never client-asserted.
	Generation int64 `json:"-"`

	// cleanup is the immutable generation-bound final-cleanup capability
	// (ownedCleanup) captured at creation; claimed at most once by the
	// winning finalizeRecord. Not serialized.
	cleanup ownedCleanup

	// recorder is the generation-bound Recorder for this exact runtime.
	// Captured at creation for instance-guarded cleanup on replacement.
	// Not serialized.
	recorder *Recorder

	// session is the exact adapter session identity captured at creation.
	// Used by replacement pre-install barrier for CompareAndTerminate.
	// Not serialized.
	ptyHandle PTYHandle

	// transport is the generation-bound TerminalTransport handle for this
	// exact runtime. Created at launch; retired when the record is
	// superseded. Exposed via Transport() for WriteInput/Resize/subscriber
	// fan-out. Not serialized.
	transport *TerminalTransport

	// cleanupDone closes when this generation's claimed cleanup capability
	// has finished. Convergers that lose the claim (natural exit vs Stop vs
	// Kill) wait on it — a channel wait, never a lock held across I/O — so
	// an action returns only after the terminal cleanup landed.
	cleanupDone chan struct{}

	// handle is the IMMUTABLE process handle captured at creation and bound
	// to exactly this generation. Lifecycle signalling uses ONLY this
	// captured handle — never a later mutable Registry lookup by canonical
	// id — so a replacement under the same id can never receive a signal
	// meant for a prior generation. Claimed under the store mutex at the
	// decisive transition; nil for test fakes without a runtime process.
	handle PTYHandle

	killRequested bool // set by Kill so the final state becomes killed not exited
}

// OwnedPTYRuntime owns controlled-PTY launch + lifecycle. All transitions
// are guarded by the store mutex (per-session decisive generation check) and
// per-session action locks; no lock is held across process signalling or
// waits.
type OwnedPTYRuntime struct {
	spawn      ManagedPTYLauncherV1  // PB.5b: narrow platform-neutral launcher (no mux.Adapter dependency)
	transcript *transcript.Service // T3: cleared on Delete
	status     StatusClearer       // S1: cleared on Delete
	graceful   time.Duration
	killGrace  time.Duration

	mu      sync.Mutex
	entries map[string]*CatalogEntry
	nextGen int64
	now     func() time.Time

	lockMu sync.Mutex
	locks  map[string]*sync.Mutex
}

// NewOwnedPTYRuntime constructs the controlled-PTY lifecycle owner.
func NewOwnedPTYRuntime(spawn ManagedPTYLauncherV1, transcriptSvc *transcript.Service) *OwnedPTYRuntime {
	return &OwnedPTYRuntime{
		spawn:      spawn,
		transcript: transcriptSvc,
		graceful:   5 * time.Second,
		killGrace:  2 * time.Second,
		entries:    make(map[string]*CatalogEntry),
		now:        time.Now,
		locks:      make(map[string]*sync.Mutex),
	}
}

// SetStatusClearer wires the S1 agent-activity store so Delete clears it.
func (o *OwnedPTYRuntime) SetStatusClearer(c StatusClearer) { o.status = c }

// Transport returns the generation-bound TerminalTransport handle for the
// given session, or nil if not found / superseded. Callers use the handle
// for WriteInput, Resize, and subscriber fan-out without accessing the
// underlying PTY directly.
func (o *OwnedPTYRuntime) Transport(sessionID string) (*TerminalTransport, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	e, ok := o.entries[sessionID]
	if !ok || e.transport == nil {
		return nil, false
	}
	return e.transport, true
}

func (o *OwnedPTYRuntime) lockFor(id string) *sync.Mutex {
	o.lockMu.Lock()
	defer o.lockMu.Unlock()
	l, ok := o.locks[id]
	if !ok {
		l = &sync.Mutex{}
		o.locks[id] = l
	}
	return l
}

// ownedCleanup is the immutable, generation-bound final-cleanup capability
// captured at creation (PA2c-R2). It removes exactly THIS generation's
// spawn-seam registry entry (instance-guarded) and exactly THIS generation's
// Recorder. It is claimed AT MOST ONCE, under the store mutex, by the
// winning generation-guarded finalizeRecord — never selected by mutable
// canonical-id lookup at cleanup time.
type ownedCleanup func(ctx context.Context)

// Create launches a controlled-PTY runtime. PA3 Step 6a: prefers
// CreateSessionAndCapture for atomic session identity + pre-install barrier
// with GenerationCleanupCapability. Falls back to legacy ownSpawn if the
// adapter does not implement SessionCreatorWithIdentity.
func (o *OwnedPTYRuntime) Create(ctx context.Context, cfg SpawnConfig, profileID, name string) (string, error) {
	if o.spawn == nil {
		return "", fmt.Errorf("no spawn launcher")
	}
	return o.createWithCapture(ctx, cfg, profileID, name, o.spawn)
}

// createWithCapture uses CreateSessionAndCapture + GenerationCleanupCapability.
// Pre-install barrier: claims old capability, executes it, waits for
// Completion.Done(), then creates the new session.
func (o *OwnedPTYRuntime) createWithCapture(ctx context.Context, cfg SpawnConfig, profileID, name string, launcher ManagedPTYLauncherV1) (string, error) {
	// Pre-install barrier: check for existing entry under o.mu.
	canonicalID := sessionid.SessionRef{Adapter: o.spawn.Name(), LocalID: cfg.Name}.Canonical()
	o.mu.Lock()
	existing, isReplacement := o.entries[canonicalID]
	var oldCap *GenerationCleanupCapability
	if isReplacement {
		// PA3 Step 6a R2: capture exact Session + Recorder from existing entry.
		oldCap = &GenerationCleanupCapability{
			Generation:  existing.Generation,
			Session:     existing.ptyHandle,  // exact adapter session identity from CatalogEntry
			Recorder:    existing.recorder, // instance-guarded Recorder
			Transport:   existing.transport,
			CanonicalID: canonicalID,
			LocalID:     sessionid.ParseSessionID(canonicalID).LocalID,
			Completion:  NewGenerationCompletion(),
		}
	}
	o.mu.Unlock()

	// Execute old cleanup outside locks.
	if oldCap != nil {
		oldCap.Execute(ctx)
		<-oldCap.Completion.Done()
	}

	// Create adapter session — identity captured atomically.
	sess, err := launcher.Spawn(ctx, cfg)
	if err != nil {
		return "", err
	}
	canonicalID = sessionid.SessionRef{Adapter: o.spawn.Name(), LocalID: sess.ID()}.Canonical()

	// Construct GenerationCleanupCapability immediately after CreateSessionAndCapture.
	completion := NewGenerationCompletion()
	cap := &GenerationCleanupCapability{
		Generation:  0,
		Session:     sess,
		CanonicalID: canonicalID,
		LocalID:     sess.ID(),
		Completion:  completion,
	}
	defer func() {
		if cap.Generation == 0 {
			cap.Execute(ctx) // rollback: cleanup on any failure before publish
		}
	}()

	// Start Recorder unconditionally.
	stream, err := openPTYStream(sess)
	if err != nil {
		return "", fmt.Errorf("open stream: %w", err)
	}
	rec := StartRecorderUnconditional(canonicalID, stream)

	// Build transport and register.
	type writeResizer interface {
		io.Writer
		Resize(int, int) error
	}
	wr, _ := sess.(writeResizer)
	transport := newTerminalTransport(canonicalID, 0, wr, wr, rec)
	cap.Transport = transport
	cap.Recorder = rec

	if phFound == nil {
		DeleteRecorderIfSame(canonicalID, rec)
		return "", fmt.Errorf("session does not support managed process")
	}

	cleanup := o.newCleanup(canonicalID, sess, rec)
	// Test seam: deterministic failure before publication triggers defer rollback.
	if testCreateWithCaptureFailAfterCapture != nil {
		if err := testCreateWithCaptureFailAfterCapture(); err != nil {
			return "", err
		}
	}
	gen := o.register(canonicalID, profileID, name, sess, cleanup, transport, rec)
	cap.Generation = gen // success — prevent defer rollback
	o.watchExit(canonicalID, gen, rec)
	return canonicalID, nil
}

// createLegacy is the pre-PA3 creation path using ownSpawn.
// PB.5a: createLegacy removed — all creation goes through createWithCapture.
func (o *OwnedPTYRuntime) createLegacy(ctx context.Context, cfg SpawnConfig, profileID, name string) (string, error) {
	return "", fmt.Errorf("legacy create path removed in PB.5a")
}

// openPTYStream opens the terminal stream from a session.
func openPTYStream(h PTYHandle) (ptyStream, error) {
	return h, nil
}

// captureSession resolves the freshly-spawned session and its process
// handle from the adapter's session list exactly once, at creation. An
// error means the runtime exposes no process control and MUST NOT be
// published as running.
func (o *OwnedPTYRuntime) captureSession(ctx context.Context, localID string) (PTYHandle, error) {
	if o.spawn == nil {
		return nil, fmt.Errorf("no spawn launcher")
	}
	sessions, err := o.spawn.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	for _, s := range sessions {
		if s.ID() == localID {
			return s, nil
			if !ok {
				return nil, fmt.Errorf("session exposes no process control")
			}
			return s, nil
		}
	}
	return nil, fmt.Errorf("session not found after create")
}

// newCleanup builds the generation-bound final-cleanup capability. The
// adapter MUST implement SessionIdentityTerminator — PA2d requires
// fail-closed atomic generation-bound cleanup. Recorder removal is
// instance-guarded via DeleteRecorderIfSame.
func (o *OwnedPTYRuntime) newCleanup(canonicalID string, sess PTYHandle, rec *Recorder) ownedCleanup {
	return func(ctx context.Context) {
		
		if !ok {
			log.Printf("FATAL: owned PTY cleanup for %s: adapter does not support atomic conditional termination", canonicalID)
			DeleteRecorderIfSame(canonicalID, rec)
			return
		}
		DeleteRecorderIfSame(canonicalID, rec)
	}
}

// register adds the running record under a fresh generation, bound to its
// immutable process handle and cleanup capability.
func (o *OwnedPTYRuntime) register(canonicalID, profileID, name string, ph PTYHandle, cleanup ownedCleanup, transport *TerminalTransport, rec *Recorder) int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.nextGen++
	gen := o.nextGen
	if old, ok := o.entries[canonicalID]; ok && old.transport != nil {
		old.transport.Retire()
	}
	if transport != nil {
		transport.generation = gen
	}
	o.entries[canonicalID] = &CatalogEntry{
		ID:          canonicalID,
		Adapter:     "controlled_pty",
		ProfileID:   profileID,
		Name:        name,
		State:       LifecycleRunning,
		StartedAt:   o.now(),
		Generation:  gen,
		handle:      handle,
		cleanup:     cleanup,
		transport:   transport,
		recorder:    rec,
		ptyHandle:   sess,
		cleanupDone: make(chan struct{}),
	}
	return gen
}

// watchExit finalizes the exact generation when the runtime's Recorder
// observes EOF (natural exit). rec is nil only for test fakes.
func (o *OwnedPTYRuntime) watchExit(canonicalID string, gen int64, rec *Recorder) {
	if rec == nil {
		return
	}
	go func() {
		<-rec.Done()
		o.finalize(canonicalID, gen)
	}()
}

// testCreateWithCaptureFailAfterCapture is a test seam. When non-nil,
// createWithCapture calls it after recorder/transport setup but before
// register(), triggering the deferred cap.Execute() rollback.
var testCreateWithCaptureFailAfterCapture func() error

// spawnControlled is the PA2d owned transport: create the PTY directly
// through the owned adapter and prove the Recorder is ready.
// PB.5a: ownSpawn removed — creation goes through launcher.
func (o *OwnedPTYRuntime) ownSpawn(ctx context.Context, cfg SpawnConfig) (string, *Recorder, error) {
	return "", nil, fmt.Errorf("legacy ownSpawn removed in PB.5a")
}

// startRecorder proves a freshly-created session's Recorder is ready via
// the adapter's session list (no Registry lookup).
func (o *OwnedPTYRuntime) startRecorder(ctx context.Context, canonicalID string) (*Recorder, error) {
	sessions, err := o.spawn.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	ref := sessionid.ParseSessionID(canonicalID)
	var phFound PTYHandle
	for _, s := range sessions {
		if s.ID() == ref.LocalID {
			phFound = s
			break
		}
	}
	if phFound == nil {
		return nil, fmt.Errorf("session not found after create")
	}
	_ = s
	if !ok {
		return nil, fmt.Errorf("session does not support live streaming")
	}
	rec, subCh := EnsureRecorder(canonicalID, func() (ptyStream, error) { return phFound, nil })
	if rec == nil {
		return nil, fmt.Errorf("recorder failed to start (stream unavailable)")
	}
	rec.Unsubscribe(subCh)
	return rec, nil
}

// terminateAdapterSession removes a spawn entry by local id via adapter.
func (o *OwnedPTYRuntime) terminateAdapterSession(ctx context.Context, localID string) error {
	return o.spawn.Terminate(ctx, localID, true)
}

// Get returns a copy of the lifecycle record.
func (o *OwnedPTYRuntime) Get(id string) (CatalogEntry, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	e, ok := o.entries[id]
	if !ok {
		return CatalogEntry{}, false
	}
	return *e, true
}

// List returns copies of every record, including retained terminal rows.
func (o *OwnedPTYRuntime) List() []CatalogEntry {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]CatalogEntry, 0, len(o.entries))
	for _, e := range o.entries {
		out = append(out, *e)
	}
	return out
}

// currentGeneration derives the server-side generation for a lifecycle
// action. Clients never assert generations.
func (o *OwnedPTYRuntime) currentGeneration(id string) (int64, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	e, ok := o.entries[id]
	if !ok {
		return 0, false
	}
	return e.Generation, true
}

// beginStop transitions running/starting → stopping for the EXACT
// generation and CLAIMS the generation's immutable process handle. The store
// mutex is the decisive linearization point: a replacement between
// derivation and this check rejects the stale request, and the claimed
// handle can never be a later generation's process.
func (o *OwnedPTYRuntime) beginStop(id string, gen int64) (proceed bool, current LifecycleState, found, stale bool, ph PTYHandle, rec *Recorder) {
	o.mu.Lock()
	defer o.mu.Unlock()
	e, ok := o.entries[id]
	if !ok {
		return false, "", false, false, nil, nil
	}
	if e.Generation != gen {
		return false, e.State, true, true, nil, nil
	}
	if e.State == LifecycleRunning || e.State == LifecycleStarting {
		e.State = LifecycleStopping
		return true, e.State, true, false, e.ptyHandle, e.recorder
	}
	return false, e.State, true, false, nil, nil
}

// requestKill records kill intent for the EXACT generation and claims the
// generation's immutable process handle.
func (o *OwnedPTYRuntime) requestKill(id string, gen int64) (proceed bool, current LifecycleState, found, stale bool, ph PTYHandle, rec *Recorder) {
	o.mu.Lock()
	defer o.mu.Unlock()
	e, ok := o.entries[id]
	if !ok {
		return false, "", false, false, nil, nil
	}
	if e.Generation != gen {
		return false, e.State, true, true, nil, nil
	}
	if e.State.Terminal() {
		return false, e.State, true, false, nil, nil
	}
	e.killRequested = true
	return true, e.State, true, false, e.ptyHandle, e.recorder
}

// finalizeRecord records the terminal state exactly once for the EXACT
// generation and CLAIMS the generation's cleanup capability atomically with
// the currency check (PA2c-R2: no unlocked check-then-act — the capability
// is removed from the record under the store mutex, so it can be claimed at
// most once and only while its generation was the current record). A
// stale-generation finalize (the id was replaced) claims nothing.
// The third return is the generation's cleanupDone channel: non-nil whenever
// the EXACT generation exists in a terminal state (the losing converger
// waits on it); nil for a stale/unknown generation (a stale finalizer must
// never wait on — or touch — a replacement).
func (o *OwnedPTYRuntime) finalizeRecord(id string, gen int64) (ownedCleanup, bool, chan struct{}) {
	o.mu.Lock()
	defer o.mu.Unlock()
	e, ok := o.entries[id]
	if !ok || e.Generation != gen {
		return nil, false, nil
	}
	if e.State.Terminal() {
		return nil, false, e.cleanupDone
	}
	if e.killRequested {
		e.State = LifecycleKilled
	} else {
		e.State = LifecycleExited
	}
	t := o.now()
	e.EndedAt = &t
	cl := e.cleanup
	e.cleanup = nil
	return cl, true, e.cleanupDone
}

// managedProcess intentionally does not exist: lifecycle actions never
// resolve a process from the mutable Registry by canonical id. The only
// process a lifecycle action may signal is the immutable handle captured at
// creation and claimed at the decisive store transition.

// Stop gracefully terminates the runtime (TERM, bounded wait, KILL
// escalation) for the current server-derived generation. Idempotent on
// terminal records.
func (o *OwnedPTYRuntime) Stop(ctx context.Context, id string) (LifecycleResult, error) {
	return o.terminate(ctx, id, "stop", false)
}

// Kill force-terminates the runtime immediately. Converges with Stop and
// natural exit on exactly one terminal state.
func (o *OwnedPTYRuntime) Kill(ctx context.Context, id string) (LifecycleResult, error) {
	return o.terminate(ctx, id, "kill", true)
}

func (o *OwnedPTYRuntime) terminate(ctx context.Context, id, action string, force bool) (LifecycleResult, error) {
	gen, ok := o.currentGeneration(id)
	if !ok {
		return LifecycleResult{}, ErrLifecycleNotFound
	}

	// Decisive transition + immutable handle claim, under the action lock and
	// store mutex ONLY. Every lifecycle lock is released before any external
	// I/O (signal delivery, exit wait), so a same-id replacement can always
	// proceed while a stale action is signalling its captured process.
	lock := o.lockFor(id)
	lock.Lock()
	var proceed bool
	var current LifecycleState
	var found, stale bool
	var capturedRec *Recorder
	if force {
		proceed, current, found, stale, handle, capturedRec = o.requestKill(id, gen)
	} else {
		proceed, current, found, stale, handle, capturedRec = o.beginStop(id, gen)
	}
	lock.Unlock()

	if !found {
		return LifecycleResult{}, ErrLifecycleNotFound
	}
	if stale {
		return LifecycleResult{}, ErrLifecycleStaleGeneration
	}
	if !proceed {
		return LifecycleResult{SessionID: id, Action: action, State: current}, nil
	}
	// External I/O phase — no lifecycle lock held. The signal goes to the
	// handle captured at THIS generation's creation, never to a Registry
	// lookup that a replacement could have redirected.
	if ph != nil {
		var sig syscall.Signal = syscall.SIGTERM
		if force { sig = syscall.SIGKILL }
		if err := ph.Signal(sig); err != nil {
			log.Printf("owned pty %s %s: signal error: %v", action, id, err)
		}
	}
	// PA4-Final-R17: capturedRec from beginStop/requestKill at the
	// decisive generation check (under lock). Never re-read entries[id]
	// after the per-session action lock is released.
	if !o.awaitExit(capturedRec, handle) {
		return LifecycleResult{SessionID: id, Action: action, State: LifecycleStopping}, ErrLifecycleTerminateFailed
	}
	o.finalize(id, gen)

	state := LifecycleExited
	if force {
		state = LifecycleKilled
	}
	if e, ok := o.Get(id); ok {
		state = e.State
	}
	return LifecycleResult{SessionID: id, Action: action, State: state}, nil
}

// awaitExit waits for confirmed process death (Recorder EOF) up to the
// graceful timeout; on timeout it escalates to SIGKILL and waits briefly
// more. A false result means the caller must NOT finalize.
// PA4-Final-R16: receives the captured Recorder directly from the catalog
// entry. Never resolves by session ID via global GetRecorder.
func (o *OwnedPTYRuntime) awaitExit(rec *Recorder, ph PTYHandle) bool {
	if rec == nil {
		// No runtime recorder to observe (test fake, or already finalized).
		return true
	}
	select {
	case <-rec.Done():
		return true
	case <-time.After(o.graceful):
	}
	if ph != nil {
		if err := ph.Signal(syscall.SIGKILL); err != nil {
			log.Printf("owned pty: SIGKILL escalation error: %v", err)
		}
	}
	select {
	case <-rec.Done():
		return true
	case <-time.After(o.killGrace):
		return false
	}
}

// finalize runs the one-time runtime cleanup for the EXACT generation and
// records the terminal state. Natural exit (watchExit), Stop, and Kill all
// converge here; the generation-guarded store transition makes the terminal
// transition exactly-once, and a stale watcher cannot touch a replaced id.
// The action lock covers ONLY the store transition. Final cleanup is the
// generation-bound capability CLAIMED atomically inside finalizeRecord
// (PA2c-R2) and invoked outside every lifecycle lock; it is instance-guarded
// internally, so even a claimed capability racing a same-id replacement can
// never terminate the replacement's registry entry or recorder. No mutable
// id-based cleanup remains here.
func (o *OwnedPTYRuntime) finalize(id string, gen int64) {
	lock := o.lockFor(id)
	lock.Lock()
	cleanup, finalized, done := o.finalizeRecord(id, gen)
	lock.Unlock()
	if finalized {
		if cleanup != nil {
			cleanup(context.Background())
		}
		close(done)
		return
	}
	if done != nil {
		// The exact generation is terminal and the OTHER converger claimed
		// the cleanup: wait (bounded, lock-free) for it to land so callers
		// observe completed cleanup after any converging action returns.
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
}

// Delete removes an ENDED runtime's record, history, and recorder
// references. Running/stopping records are a conflict; unknown → not found.
func (o *OwnedPTYRuntime) Delete(ctx context.Context, id string) (LifecycleResult, error) {
	lock := o.lockFor(id)
	lock.Lock()
	defer lock.Unlock()

	e, ok := o.Get(id)
	if !ok {
		return LifecycleResult{}, ErrLifecycleNotFound
	}
	if !e.State.Terminal() {
		return LifecycleResult{}, ErrLifecycleNotTerminal
	}
	o.mu.Lock()
	delete(o.entries, id)
	o.mu.Unlock()

	if o.transcript != nil {
		o.transcript.ClearTranscript(id)
	}
	if o.status != nil {
		o.status.Clear(id)
	}
	DeleteRecorder(id)
	// Keep the per-session lock in the map: removing it while another operation
	// holds a reference would let two callers use different mutexes for one id.
	return LifecycleResult{SessionID: id, Action: "delete", State: LifecycleExited}, nil
}

// RegisterForTest seeds a record without spawning (test fakes with no
// runtime process handle). Returns the allocated generation.
func (o *OwnedPTYRuntime) RegisterForTest(canonicalID, profileID, name string, rec *Recorder) int64 {
	return o.RegisterForTestWithHandle(canonicalID, profileID, name, nil, rec)
}

// RegisterForTestWithHandle seeds a record bound to an explicit immutable
// process handle (deterministic replacement/signal tests). Returns the
// allocated generation.
func (o *OwnedPTYRuntime) RegisterForTestWithHandle(canonicalID, profileID, name string, ph PTYHandle, rec *Recorder) int64 {
	// Test records carry a recorder-only cleanup (no captured spawn-seam
	// session instance); it is still generation-claimed like production.
	cleanup := func(context.Context) { DeleteRecorderIfSame(canonicalID, rec) }
	gen := o.register(canonicalID, profileID, name, handle, cleanup, nil, rec, nil)
	o.watchExit(canonicalID, gen, rec)
	return gen
}
