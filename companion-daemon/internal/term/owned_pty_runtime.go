package term

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"devremote/companion-daemon/internal/mux"
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
	handle mux.ManagedProcess

	killRequested bool // set by Kill so the final state becomes killed not exited
}

// OwnedPTYRuntime owns controlled-PTY launch + lifecycle. All transitions
// are guarded by the store mutex (per-session decisive generation check) and
// per-session action locks; no lock is held across process signalling or
// waits.
type OwnedPTYRuntime struct {
	reg        *mux.Registry       // temporary PA2c spawn/terminate seam (PA2d removes)
	activity   *ActivityBuffer     // recorder readiness + Delete cleanup
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
func NewOwnedPTYRuntime(reg *mux.Registry, activity *ActivityBuffer, transcriptSvc *transcript.Service) *OwnedPTYRuntime {
	return &OwnedPTYRuntime{
		reg:        reg,
		activity:   activity,
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

// Create launches a controlled-PTY runtime through the temporary mux spawn
// seam, proves its Recorder is ready, captures the MANDATORY immutable
// process handle and the generation-bound cleanup capability for this exact
// launch, registers the generation-bound lifecycle record, and starts the
// exactly-once exit watcher on the EXACT Recorder returned by the spawn
// (closing the fast-exit race). Failure — including a failed handle capture
// — rolls the spawn back and leaves no visible runtime: a running
// generation is NEVER published without its exact process binding.
func (o *OwnedPTYRuntime) Create(ctx context.Context, opts mux.CreateOptions, profileID, name string) (string, error) {
	canonicalID, rec, err := o.spawnControlled(ctx, opts)
	if err != nil {
		return "", err
	}
	sess, handle, herr := o.captureHandle(ctx, canonicalID)
	if herr != nil {
		// PA2c-R2: handle capture is MANDATORY. Roll back the spawn exactly
		// like a recorder-readiness failure — terminate the just-created
		// runtime and drop its recorder — and fail creation unpublished.
		ref := sessionid.ParseSessionID(canonicalID)
		_ = o.reg.TerminateSession(ctx, ref.Adapter, ref.LocalID)
		DeleteRecorder(canonicalID)
		return "", fmt.Errorf("owned pty create: process handle capture failed: %w", herr)
	}
	cleanup := o.newCleanup(canonicalID, sess, rec)
	gen := o.register(canonicalID, profileID, name, handle, cleanup)
	o.watchExit(canonicalID, gen, rec)
	return canonicalID, nil
}

// captureHandle resolves the freshly-spawned session and its process handle
// from the spawn seam exactly once, at creation. An error means the runtime
// exposes no process control and MUST NOT be published as running.
func (o *OwnedPTYRuntime) captureHandle(ctx context.Context, canonicalID string) (mux.Session, mux.ManagedProcess, error) {
	if o.reg == nil {
		return nil, nil, fmt.Errorf("no spawn seam registry")
	}
	sess, err := o.reg.FindSession(ctx, canonicalID)
	if err != nil {
		return nil, nil, fmt.Errorf("session not found after create: %w", err)
	}
	mp, ok := sess.(mux.ManagedProcess)
	if !ok {
		return nil, nil, fmt.Errorf("session exposes no process control")
	}
	return sess, mp, nil
}

// newCleanup builds the generation-bound final-cleanup capability. The
// registry removal is ATOMICALLY conditional (PA2c-R3): a single
// Registry.CompareAndTerminateSession call locates the current session
// identity under the snapshot lock, compares it against the immutable
// instance captured at creation, and only on exact match terminates the
// spawn-seam entry. A replacement (different instance, same canonical id)
// is returned as ErrStaleSessionIdentity without touching the adapter.
// Recorder removal is instance-guarded via DeleteRecorderIfSame.
func (o *OwnedPTYRuntime) newCleanup(canonicalID string, sess mux.Session, rec *Recorder) ownedCleanup {
	return func(ctx context.Context) {
		if o.reg != nil && sess != nil {
			_ = o.reg.CompareAndTerminateSession(ctx, canonicalID, sess)
			// Outcome is nil (terminated), ErrStaleSessionIdentity
			// (replacement — no deletion), or ErrSessionNotFound (already
			// cleaned up). In every case the capability did not touch a
			// replacement.
		}
		DeleteRecorderIfSame(canonicalID, rec)
	}
}

// register adds the running record under a fresh generation, bound to its
// immutable process handle and cleanup capability.
func (o *OwnedPTYRuntime) register(canonicalID, profileID, name string, handle mux.ManagedProcess, cleanup ownedCleanup) int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.nextGen++
	gen := o.nextGen
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

// spawnControlled is the temporary PA2c mux spawn seam: create the PTY via
// the existing adapter primitive and prove the Recorder is ready before the
// runtime may be exposed. PA2d replaces this with the owned transport.
func (o *OwnedPTYRuntime) spawnControlled(ctx context.Context, opts mux.CreateOptions) (string, *Recorder, error) {
	return createControlledSession(ctx, o.reg, o.activity, opts)
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
func (o *OwnedPTYRuntime) beginStop(id string, gen int64) (proceed bool, current LifecycleState, found, stale bool, handle mux.ManagedProcess) {
	o.mu.Lock()
	defer o.mu.Unlock()
	e, ok := o.entries[id]
	if !ok {
		return false, "", false, false, nil
	}
	if e.Generation != gen {
		return false, e.State, true, true, nil
	}
	if e.State == LifecycleRunning || e.State == LifecycleStarting {
		e.State = LifecycleStopping
		return true, e.State, true, false, e.handle
	}
	return false, e.State, true, false, nil
}

// requestKill records kill intent for the EXACT generation and claims the
// generation's immutable process handle.
func (o *OwnedPTYRuntime) requestKill(id string, gen int64) (proceed bool, current LifecycleState, found, stale bool, handle mux.ManagedProcess) {
	o.mu.Lock()
	defer o.mu.Unlock()
	e, ok := o.entries[id]
	if !ok {
		return false, "", false, false, nil
	}
	if e.Generation != gen {
		return false, e.State, true, true, nil
	}
	if e.State.Terminal() {
		return false, e.State, true, false, nil
	}
	e.killRequested = true
	return true, e.State, true, false, e.handle
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
	var handle mux.ManagedProcess
	if force {
		proceed, current, found, stale, handle = o.requestKill(id, gen)
	} else {
		proceed, current, found, stale, handle = o.beginStop(id, gen)
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
	if handle != nil {
		if terr := handle.TerminateGroup(force); terr != nil {
			log.Printf("owned pty %s %s: signal error: %v", action, id, terr)
		}
	}
	if !o.awaitExit(id, handle) {
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
func (o *OwnedPTYRuntime) awaitExit(id string, mp mux.ManagedProcess) bool {
	rec := GetRecorder(id)
	if rec == nil {
		// No runtime recorder to observe (test fake, or already finalized).
		return true
	}
	select {
	case <-rec.Done():
		return true
	case <-time.After(o.graceful):
	}
	if mp != nil {
		if terr := mp.TerminateGroup(true); terr != nil {
			log.Printf("owned pty %s: SIGKILL escalation error: %v", id, terr)
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
	if o.activity != nil {
		o.activity.Clear(id)
	}
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
func (o *OwnedPTYRuntime) RegisterForTestWithHandle(canonicalID, profileID, name string, handle mux.ManagedProcess, rec *Recorder) int64 {
	// Test records carry a recorder-only cleanup (no captured spawn-seam
	// session instance); it is still generation-claimed like production.
	cleanup := func(context.Context) { DeleteRecorderIfSame(canonicalID, rec) }
	gen := o.register(canonicalID, profileID, name, handle, cleanup)
	o.watchExit(canonicalID, gen, rec)
	return gen
}
