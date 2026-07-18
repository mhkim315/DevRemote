package term

import (
	"context"
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

// Create launches a controlled-PTY runtime through the temporary mux spawn
// seam, proves its Recorder is ready, registers the generation-bound
// lifecycle record, and starts the exactly-once exit watcher on the EXACT
// Recorder returned by the spawn (closing the fast-exit race). Failure
// leaves no visible runtime.
func (o *OwnedPTYRuntime) Create(ctx context.Context, opts mux.CreateOptions, profileID, name string) (string, error) {
	canonicalID, rec, err := o.spawnControlled(ctx, opts)
	if err != nil {
		return "", err
	}
	gen := o.register(canonicalID, profileID, name)
	o.watchExit(canonicalID, gen, rec)
	return canonicalID, nil
}

// register adds the running record under a fresh generation.
func (o *OwnedPTYRuntime) register(canonicalID, profileID, name string) int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.nextGen++
	gen := o.nextGen
	o.entries[canonicalID] = &CatalogEntry{
		ID:         canonicalID,
		Adapter:    "controlled_pty",
		ProfileID:  profileID,
		Name:       name,
		State:      LifecycleRunning,
		StartedAt:  o.now(),
		Generation: gen,
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
// generation. The store mutex is the decisive linearization point: a
// replacement between derivation and this check rejects the stale request.
func (o *OwnedPTYRuntime) beginStop(id string, gen int64) (proceed bool, current LifecycleState, found, stale bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	e, ok := o.entries[id]
	if !ok {
		return false, "", false, false
	}
	if e.Generation != gen {
		return false, e.State, true, true
	}
	if e.State == LifecycleRunning || e.State == LifecycleStarting {
		e.State = LifecycleStopping
		return true, e.State, true, false
	}
	return false, e.State, true, false
}

// requestKill records kill intent for the EXACT generation.
func (o *OwnedPTYRuntime) requestKill(id string, gen int64) (proceed bool, current LifecycleState, found, stale bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	e, ok := o.entries[id]
	if !ok {
		return false, "", false, false
	}
	if e.Generation != gen {
		return false, e.State, true, true
	}
	if e.State.Terminal() {
		return false, e.State, true, false
	}
	e.killRequested = true
	return true, e.State, true, false
}

// finalizeRecord records the terminal state exactly once for the EXACT
// generation. Idempotent once terminal; a stale-generation finalize (the id
// was replaced) is a no-op, so an old watcher can never terminate a newer
// runtime record.
func (o *OwnedPTYRuntime) finalizeRecord(id string, gen int64) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	e, ok := o.entries[id]
	if !ok || e.Generation != gen || e.State.Terminal() {
		return false
	}
	if e.killRequested {
		e.State = LifecycleKilled
	} else {
		e.State = LifecycleExited
	}
	t := o.now()
	e.EndedAt = &t
	return true
}

// managedProcess resolves the live spawn-seam process handle. Nil when the
// runtime already left the Registry (already exited).
func (o *OwnedPTYRuntime) managedProcess(ctx context.Context, id string) mux.ManagedProcess {
	if o.reg == nil {
		return nil
	}
	sess, err := o.reg.FindSession(ctx, id)
	if err != nil {
		return nil
	}
	mp, ok := sess.(mux.ManagedProcess)
	if !ok {
		return nil
	}
	return mp
}

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

	lock := o.lockFor(id)
	lock.Lock()
	var proceed bool
	var current LifecycleState
	var found, stale bool
	if force {
		proceed, current, found, stale = o.requestKill(id, gen)
	} else {
		proceed, current, found, stale = o.beginStop(id, gen)
	}
	if !found {
		lock.Unlock()
		return LifecycleResult{}, ErrLifecycleNotFound
	}
	if stale {
		lock.Unlock()
		return LifecycleResult{}, ErrLifecycleStaleGeneration
	}
	if !proceed {
		lock.Unlock()
		return LifecycleResult{SessionID: id, Action: action, State: current}, nil
	}
	mp := o.managedProcess(ctx, id)
	if mp != nil {
		if terr := mp.TerminateGroup(force); terr != nil {
			log.Printf("owned pty %s %s: signal error: %v", action, id, terr)
		}
	}
	lock.Unlock()

	// No lock across the exit wait.
	if !o.awaitExit(id, mp) {
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
func (o *OwnedPTYRuntime) finalize(id string, gen int64) {
	lock := o.lockFor(id)
	lock.Lock()
	defer lock.Unlock()
	if !o.finalizeRecord(id, gen) {
		return
	}
	// Process is already dead (Recorder EOF) before the seam cleanup.
	if o.reg != nil {
		ref := sessionid.ParseSessionID(id)
		_ = o.reg.TerminateSession(context.Background(), ref.Adapter, ref.LocalID)
	}
	DeleteRecorder(id)
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
// runtime recorder). Returns the allocated generation.
func (o *OwnedPTYRuntime) RegisterForTest(canonicalID, profileID, name string, rec *Recorder) int64 {
	gen := o.register(canonicalID, profileID, name)
	o.watchExit(canonicalID, gen, rec)
	return gen
}
