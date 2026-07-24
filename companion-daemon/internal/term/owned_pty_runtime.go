package term

import (
	"context"
	"fmt"
	"io"
	"sync"
	"syscall"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/transcript"
)

// CatalogEntry is the generation-bound lifecycle record for one V1 launch.
type CatalogEntry struct {
	ID            string         `json:"id"`
	Adapter       string         `json:"adapter"`
	ProfileID     string         `json:"profileId,omitempty"`
	Name          string         `json:"name,omitempty"`
	State         LifecycleState `json:"state"`
	StartedAt     time.Time      `json:"startedAt"`
	EndedAt       *time.Time     `json:"endedAt,omitempty"`
	ExitCode      *int           `json:"exitCode,omitempty"`
	Generation    int64          `json:"-"`
	Identity      LaunchIdentity `json:"-"`
	cleanup       ownedCleanup
	recorder      *Recorder
	transport     *TerminalTransport
	cleanupDone   chan struct{}
	handle        PTYHandle
	killRequested bool
}

type OwnedPTYRuntime struct {
	v1Spawn             ManagedPTYLauncherV1
	transcript          *transcript.Service
	status              StatusClearer
	graceful, killGrace time.Duration
	mu                  sync.Mutex
	entries             map[string]*CatalogEntry
	nextGen             int64
	now                 func() time.Time
	lockMu              sync.Mutex
	locks               map[string]*sync.Mutex
	authorizer          devicetrust.MutationAuthorizer
}

func NewOwnedPTYRuntime(authorizer devicetrust.MutationAuthorizer, v1 ManagedPTYLauncherV1, transcriptSvc *transcript.Service) (*OwnedPTYRuntime, error) {
	if authorizer == nil {
		return nil, fmt.Errorf("owned PTY mutation authorizer is required")
	}
	return &OwnedPTYRuntime{v1Spawn: v1, transcript: transcriptSvc, graceful: 5 * time.Second, killGrace: 2 * time.Second, entries: make(map[string]*CatalogEntry), now: time.Now, locks: make(map[string]*sync.Mutex), authorizer: authorizer}, nil
}
func (o *OwnedPTYRuntime) SetStatusClearer(c StatusClearer) { o.status = c }
func (o *OwnedPTYRuntime) Transport(id string) (*TerminalTransport, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	e, ok := o.entries[id]
	if !ok || e.transport == nil {
		return nil, false
	}
	return e.transport, true
}
func (o *OwnedPTYRuntime) lockFor(id string) *sync.Mutex {
	o.lockMu.Lock()
	defer o.lockMu.Unlock()
	if o.locks[id] == nil {
		o.locks[id] = &sync.Mutex{}
	}
	return o.locks[id]
}

type ownedCleanup func(context.Context) CleanupOutcome

// Create has one launch path: V1 Spawn. A missing V1 launcher is a hard failure.
func (o *OwnedPTYRuntime) Create(ctx context.Context, cfg SpawnConfig, profileID, name string, deviceID string, deviceEpoch uint64) (string, error) {
	if o.v1Spawn == nil {
		return "", fmt.Errorf("no V1 launcher")
	}
	if err := o.authorizer.AuthorizeCommit(deviceID, deviceEpoch, devicetrust.IntentSessionCreate); err != nil {
		return "", err
	}
	canonicalID := "controlled_pty:" + cfg.Name
	// Recheck immediately before any replacement cleanup or PTY launch. This
	// is the create commit boundary; a denied create must not terminate an
	// existing generation.
	if err := o.authorizer.AuthorizeCommit(deviceID, deviceEpoch, devicetrust.IntentSessionCreate); err != nil {
		return "", err
	}
	// Retire the exact prior generation before launching a replacement. The
	// generation check inside finalize prevents a stale cleanup from touching a
	// later launch with the same canonical id.
	if previous, ok := o.currentGeneration(canonicalID); ok {
		o.finalize(canonicalID, previous)
	}
	result, err := o.v1Spawn.Spawn(ctx, cfg)
	if err != nil {
		return "", err
	}
	if result.Handle == nil || result.ProcessCleanup == nil || result.Identity.InstanceID == "" {
		if result.ProcessCleanup != nil {
			result.ProcessCleanup.Execute(ctx)
		}
		return "", fmt.Errorf("V1 launcher returned incomplete launch result")
	}
	stream, ok := result.Handle.(interface{ Read([]byte) (int, error) })
	if !ok {
		result.ProcessCleanup.Execute(ctx)
		return "", fmt.Errorf("V1 handle does not expose a PTY stream")
	}
	ps := &handleStream{PTYHandle: result.Handle, reader: stream}
	rec := StartRecorderUnconditional(canonicalID, ps)
	transport := newTerminalTransport(canonicalID, 0, handleWriter{result.Handle}, result.Handle, rec, o.authorizer)
	cleanup := func(c context.Context) CleanupOutcome { return result.ProcessCleanup.Execute(c) }
	gen := o.register(canonicalID, profileID, name, result.Handle, result.Identity, cleanup, transport, rec)
	o.watchExit(canonicalID, gen, rec)
	return canonicalID, nil
}

type handleWriter struct{ PTYHandle }

func (w handleWriter) Write(p []byte) (int, error) { return w.PTYHandle.Write(p) }

type handleStream struct {
	PTYHandle
	reader interface{ Read([]byte) (int, error) }
}

func (s *handleStream) Read(p []byte) (int, error) { return s.reader.Read(p) }
func (s *handleStream) Close() error               { return s.CloseTransport() }
func (s *handleStream) GetSize() (int, int, error) {
	if sized, ok := s.reader.(interface{ GetSize() (int, int, error) }); ok {
		return sized.GetSize()
	}
	return 0, 0, fmt.Errorf("PTY size unavailable")
}

func (o *OwnedPTYRuntime) register(id, profileID, name string, handle PTYHandle, identity LaunchIdentity, cleanup ownedCleanup, transport *TerminalTransport, rec *Recorder) int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.nextGen++
	g := o.nextGen
	if old := o.entries[id]; old != nil && old.transport != nil {
		old.transport.Retire()
	}
	transport.generation = g
	o.entries[id] = &CatalogEntry{ID: id, Adapter: "controlled_pty", ProfileID: profileID, Name: name, State: LifecycleRunning, StartedAt: identity.StartedAt, Generation: g, Identity: identity, handle: handle, cleanup: cleanup, transport: transport, recorder: rec, cleanupDone: make(chan struct{})}
	return g
}
func (o *OwnedPTYRuntime) watchExit(id string, g int64, rec *Recorder) {
	go func() { <-rec.Done(); o.finalize(id, g) }()
}
func (o *OwnedPTYRuntime) Get(id string) (CatalogEntry, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	e, ok := o.entries[id]
	if !ok {
		return CatalogEntry{}, false
	}
	return *e, true
}
func (o *OwnedPTYRuntime) List() []CatalogEntry {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]CatalogEntry, 0, len(o.entries))
	for _, e := range o.entries {
		out = append(out, *e)
	}
	return out
}
func (o *OwnedPTYRuntime) currentGeneration(id string) (int64, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	e, ok := o.entries[id]
	if !ok {
		return 0, false
	}
	return e.Generation, true
}
func sameIdentity(a, b LaunchIdentity) bool {
	return a.InstanceID == b.InstanceID && a.StartedAt.Equal(b.StartedAt)
}
func (o *OwnedPTYRuntime) beginStop(id string, g int64) (bool, LifecycleState, bool, bool, PTYHandle, LaunchIdentity) {
	o.mu.Lock()
	defer o.mu.Unlock()
	e, ok := o.entries[id]
	if !ok {
		return false, "", false, false, nil, LaunchIdentity{}
	}
	if e.Generation != g {
		return false, e.State, true, true, nil, LaunchIdentity{}
	}
	if e.State == LifecycleRunning || e.State == LifecycleStarting {
		e.State = LifecycleStopping
		return true, e.State, true, false, e.handle, e.Identity
	}
	return false, e.State, true, false, nil, LaunchIdentity{}
}
func (o *OwnedPTYRuntime) requestKill(id string, g int64) (bool, LifecycleState, bool, bool, PTYHandle, LaunchIdentity) {
	o.mu.Lock()
	defer o.mu.Unlock()
	e, ok := o.entries[id]
	if !ok {
		return false, "", false, false, nil, LaunchIdentity{}
	}
	if e.Generation != g {
		return false, e.State, true, true, nil, LaunchIdentity{}
	}
	if e.State.Terminal() {
		return false, e.State, true, false, nil, LaunchIdentity{}
	}
	e.killRequested = true
	return true, e.State, true, false, e.handle, e.Identity
}
func (o *OwnedPTYRuntime) Stop(ctx context.Context, id string, deviceID string, deviceEpoch uint64) (LifecycleResult, error) {
	return o.terminate(ctx, id, "stop", false, deviceID, deviceEpoch)
}
func (o *OwnedPTYRuntime) Kill(ctx context.Context, id string, deviceID string, deviceEpoch uint64) (LifecycleResult, error) {
	return o.terminate(ctx, id, "kill", true, deviceID, deviceEpoch)
}
func (o *OwnedPTYRuntime) terminate(ctx context.Context, id, action string, force bool, deviceID string, deviceEpoch uint64) (LifecycleResult, error) {
	g, ok := o.currentGeneration(id)
	if !ok {
		return LifecycleResult{}, ErrLifecycleNotFound
	}
	l := o.lockFor(id)
	l.Lock()
	intent := devicetrust.IntentSessionStop
	if force {
		intent = devicetrust.IntentSessionKill
	}
	if err := o.authorizer.AuthorizeCommit(deviceID, deviceEpoch, intent); err != nil {
		l.Unlock()
		return LifecycleResult{}, err
	}
	var proceed, found, stale bool
	var state LifecycleState
	var h PTYHandle
	var identity LaunchIdentity
	if force {
		proceed, state, found, stale, h, identity = o.requestKill(id, g)
	} else {
		proceed, state, found, stale, h, identity = o.beginStop(id, g)
	}
	l.Unlock()
	if !found {
		return LifecycleResult{}, ErrLifecycleNotFound
	}
	if stale {
		return LifecycleResult{}, ErrLifecycleStaleGeneration
	}
	if !proceed {
		return LifecycleResult{SessionID: id, Action: action, State: state}, nil
	}
	if !o.currentIdentity(id, g, identity) {
		return LifecycleResult{}, ErrLifecycleStaleGeneration
	}
	if h == nil {
		// RegisterForTest rows have no process. They are already terminal from
		// the V1 process boundary's perspective and must not dereference a
		// missing handle while exercising HTTP lifecycle responses.
		o.finalize(id, g)
		e, _ := o.Get(id)
		return LifecycleResult{SessionID: id, Action: action, State: e.State}, nil
	}
	if force {
		_ = h.Kill()
	} else {
		_ = h.Signal(syscall.SIGTERM)
	}
	out := o.awaitExit(ctx, h)
	if !out.Exited {
		return LifecycleResult{SessionID: id, Action: action, State: LifecycleStopping}, ErrLifecycleTerminateFailed
	}
	o.finalize(id, g)
	e, _ := o.Get(id)
	return LifecycleResult{SessionID: id, Action: action, State: e.State}, nil
}
func (o *OwnedPTYRuntime) currentIdentity(id string, g int64, identity LaunchIdentity) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	e := o.entries[id]
	return e != nil && e.Generation == g && sameIdentity(e.Identity, identity)
}
func (o *OwnedPTYRuntime) awaitExit(ctx context.Context, h PTYHandle) LifecycleOutcome {
	waitCtx, cancel := context.WithTimeout(ctx, o.graceful)
	defer cancel()
	out := h.Wait(waitCtx)
	if out.Exited {
		return out
	}
	_ = h.Kill()
	waitCtx, cancel = context.WithTimeout(ctx, o.killGrace)
	defer cancel()
	return h.Wait(waitCtx)
}
func (o *OwnedPTYRuntime) finalizeRecord(id string, g int64) (ownedCleanup, bool, chan struct{}) {
	o.mu.Lock()
	defer o.mu.Unlock()
	e := o.entries[id]
	if e == nil || e.Generation != g {
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
func (o *OwnedPTYRuntime) finalize(id string, g int64) {
	l := o.lockFor(id)
	l.Lock()
	cleanup, won, done := o.finalizeRecord(id, g)
	l.Unlock()
	if won {
		if cleanup != nil {
			cleanup(context.Background())
		}
		close(done)
		return
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
}
func (o *OwnedPTYRuntime) Delete(ctx context.Context, id string, deviceID string, deviceEpoch uint64) (LifecycleResult, error) {
	l := o.lockFor(id)
	l.Lock()
	if err := o.authorizer.AuthorizeCommit(deviceID, deviceEpoch, devicetrust.IntentSessionDelete); err != nil {
		l.Unlock()
		return LifecycleResult{}, err
	}
	e, ok := o.Get(id)
	if !ok {
		l.Unlock()
		return LifecycleResult{}, ErrLifecycleNotFound
	}
	if !e.State.Terminal() {
		l.Unlock()
		return LifecycleResult{}, ErrLifecycleNotTerminal
	}
	o.mu.Lock()
	delete(o.entries, id)
	o.mu.Unlock()
	l.Unlock()
	if o.transcript != nil {
		o.transcript.ClearTranscript(id)
	}
	if o.status != nil {
		o.status.Clear(id)
	}
	if e.recorder != nil {
		e.recorder.Stop()
	}
	return LifecycleResult{SessionID: id, Action: "delete", State: LifecycleExited}, nil
}
func (o *OwnedPTYRuntime) RegisterForTest(id, profileID, name string, rec *Recorder) int64 {
	return o.register(id, profileID, name, nil, LaunchIdentity{InstanceID: "test", StartedAt: o.now()}, func(context.Context) CleanupOutcome {
		if rec != nil {
			rec.Stop()
		}
		return CleanupOutcome{Completed: true}
	}, newTerminalTransport(id, 0, io.Discard, nil, rec, o.authorizer), rec)
}
