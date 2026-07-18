package term

import (
	"context"
	"errors"
	"log"
	"slices"
	"sync"
	"time"

	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/sessionid"
	"devremote/companion-daemon/internal/transcript"
)

// Lifecycle errors map to structured, non-500 handler responses.
var (
	ErrLifecycleNotFound    = errors.New("session not found")
	ErrLifecycleUnsupported = errors.New("session does not support managed lifecycle")
	ErrLifecycleNotTerminal = errors.New("session must be stopped before it can be deleted")
	// ErrLifecycleTerminateFailed: the process could not be confirmed dead even
	// after SIGKILL. The runtime is left intact (not marked terminal) → 500.
	ErrLifecycleTerminateFailed = errors.New("could not confirm session termination")
)

// LifecycleResult is the structured result of a lifecycle action.
type LifecycleResult struct {
	SessionID string         `json:"sessionId"`
	Action    string         `json:"action"`
	State     LifecycleState `json:"state"`
}

// StatusClearer drops S1 agent-activity status for a session. The production
// Delete path calls it so a deleted session immediately loses its activity record
// (and a recreated id cannot inherit it). TelemetryService satisfies this.
type StatusClearer interface{ Clear(sessionID string) }

// LifecycleService is the single owner of Stop/Kill/Delete for managed
// sessions. Every action converges here: capability check, catalog state,
// process-group termination, and one-time runtime cleanup. Per-session locks
// serialize concurrent actions; finalize is guarded by the catalog terminal
// state so registry removal and recorder stop happen exactly once, and natural
// exit, Stop and Kill all converge on one final state.
type LifecycleService struct {
	reg        *mux.Registry
	activity   *ActivityBuffer
	transcript *transcript.Service // T3: cleared on session delete
	status     StatusClearer       // S1: agent-activity store, cleared on session delete
	catalog    *SessionCatalog
	graceful   time.Duration
	killGrace  time.Duration

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewLifecycleService(reg *mux.Registry, activity *ActivityBuffer, transcriptSvc *transcript.Service) *LifecycleService {
	return &LifecycleService{
		reg:        reg,
		transcript: transcriptSvc,
		activity:   activity,
		catalog:    NewSessionCatalog(),
		graceful:   5 * time.Second,
		killGrace:  2 * time.Second,
		locks:      make(map[string]*sync.Mutex),
	}
}

// Catalog exposes the read-only catalog (state lookups in handlers/tests).
func (s *LifecycleService) Catalog() *SessionCatalog { return s.catalog }

// SetStatusClearer wires the S1 agent-activity store so Delete clears it. Wired
// in the composition root after the TelemetryService is constructed.
func (s *LifecycleService) SetStatusClearer(c StatusClearer) { s.status = c }

func (s *LifecycleService) lockFor(id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.locks[id]
	if !ok {
		l = &sync.Mutex{}
		s.locks[id] = l
	}
	return l
}

// Register records a freshly created managed session and starts the exit
// watcher on the EXACT Recorder returned by create. Passing the recorder (vs a
// fresh GetRecorder lookup) closes the fast-exit race: even if the process has
// already exited and the recorder unregistered, its Done channel is closed and
// the watcher finalizes immediately, so the catalog cannot stay stuck running.
// rec is nil only for test fakes with no runtime recorder.
func (s *LifecycleService) Register(canonicalID, adapter, profileID, name string, rec *Recorder) {
	s.catalog.Add(canonicalID, adapter, profileID, name)
	if rec == nil {
		return
	}
	go func() {
		<-rec.Done()
		s.finalize(canonicalID)
	}()
}

// IsManaged reports whether a session is a Pokit-managed runtime — either a
// live session whose adapter declares managedLifecycle, or an ended session
// that still has a catalog row (only managed sessions are cataloged). Used to
// route the legacy query DELETE through the M2 contract.
func (s *LifecycleService) IsManaged(ctx context.Context, id string) bool {
	if sess, err := s.reg.FindSession(ctx, id); err == nil {
		adapter, ok := s.reg.Adapter(sess.AdapterName())
		return ok && slices.Contains(mux.AdapterCapabilities(adapter), mux.CapManagedLifecycle)
	}
	_, ok := s.catalog.Get(id)
	return ok
}

// managedLive resolves a live session and enforces the capability boundary:
// its adapter must declare managedLifecycle. Capability-driven, not by name.
func (s *LifecycleService) managedLive(ctx context.Context, id string) (mux.Session, error) {
	sess, err := s.reg.FindSession(ctx, id)
	if err != nil {
		return nil, ErrLifecycleNotFound
	}
	adapter, ok := s.reg.Adapter(sess.AdapterName())
	if !ok || !slices.Contains(mux.AdapterCapabilities(adapter), mux.CapManagedLifecycle) {
		return nil, ErrLifecycleUnsupported
	}
	return sess, nil
}

// Stop gracefully terminates a managed session's process group (SIGTERM), waits
// the bounded graceful timeout, and — per the approved M2 termination policy —
// escalates to SIGKILL if the group is still alive. Idempotent: a concurrent or
// repeated Stop returns the current state without re-terminating.
func (s *LifecycleService) Stop(ctx context.Context, id string) (LifecycleResult, error) {
	sess, err := s.managedLive(ctx, id)
	if err != nil {
		// A session that already ended is removed from the Registry; a repeated
		// Stop is then idempotent success reflecting the terminal state.
		if entry, ok := s.catalog.Get(id); ok && entry.State.Terminal() {
			return LifecycleResult{SessionID: id, Action: "stop", State: entry.State}, nil
		}
		return LifecycleResult{}, err
	}
	mp, ok := sess.(mux.ManagedProcess)
	if !ok {
		return LifecycleResult{}, ErrLifecycleUnsupported
	}

	lock := s.lockFor(id)
	lock.Lock()
	proceed, current, found := s.catalog.beginStop(id)
	if !found {
		lock.Unlock()
		return LifecycleResult{}, ErrLifecycleNotFound
	}
	if !proceed {
		lock.Unlock()
		return LifecycleResult{SessionID: id, Action: "stop", State: current}, nil
	}
	if terr := mp.TerminateGroup(false); terr != nil {
		log.Printf("lifecycle stop %s: SIGTERM error: %v", id, terr)
	}
	lock.Unlock()

	if !s.awaitExit(id, mp) {
		// Could not confirm the process died even after SIGKILL — do NOT
		// finalize: leave the runtime + recorder intact rather than falsely
		// reporting a live process as exited.
		return LifecycleResult{SessionID: id, Action: "stop", State: LifecycleStopping}, ErrLifecycleTerminateFailed
	}
	s.finalize(id)

	state := LifecycleExited
	if entry, ok := s.catalog.Get(id); ok {
		state = entry.State
	}
	return LifecycleResult{SessionID: id, Action: "stop", State: state}, nil
}

// Kill force-terminates the process group (SIGKILL) immediately. Works while a
// Stop is in progress and converges on the same cleanup. Idempotent.
func (s *LifecycleService) Kill(ctx context.Context, id string) (LifecycleResult, error) {
	sess, err := s.managedLive(ctx, id)
	if err != nil {
		if entry, ok := s.catalog.Get(id); ok && entry.State.Terminal() {
			return LifecycleResult{SessionID: id, Action: "kill", State: entry.State}, nil
		}
		return LifecycleResult{}, err
	}
	mp, ok := sess.(mux.ManagedProcess)
	if !ok {
		return LifecycleResult{}, ErrLifecycleUnsupported
	}

	lock := s.lockFor(id)
	lock.Lock()
	proceed, current, found := s.catalog.requestKill(id)
	if !found {
		lock.Unlock()
		return LifecycleResult{}, ErrLifecycleNotFound
	}
	if !proceed {
		lock.Unlock()
		return LifecycleResult{SessionID: id, Action: "kill", State: current}, nil
	}
	if terr := mp.TerminateGroup(true); terr != nil {
		log.Printf("lifecycle kill %s: SIGKILL error: %v", id, terr)
	}
	lock.Unlock()

	if !s.awaitExit(id, mp) {
		return LifecycleResult{SessionID: id, Action: "kill", State: LifecycleStopping}, ErrLifecycleTerminateFailed
	}
	s.finalize(id)

	state := LifecycleKilled
	if entry, ok := s.catalog.Get(id); ok {
		state = entry.State
	}
	return LifecycleResult{SessionID: id, Action: "kill", State: state}, nil
}

// Delete removes an ENDED managed session's catalog row, activity/transcript
// history and recorder references. A running/stopping session is a conflict —
// it must be stopped first. Non-managed sessions are rejected; unknown → 404.
func (s *LifecycleService) Delete(ctx context.Context, id string) (LifecycleResult, error) {
	// Same per-session synchronization as Stop/Kill/finalize so a Delete cannot
	// race a finalize into inconsistent state.
	lock := s.lockFor(id)
	lock.Lock()
	defer lock.Unlock()

	// If still live, enforce capability and refuse to silently delete a runtime.
	if sess, err := s.reg.FindSession(ctx, id); err == nil {
		adapter, ok := s.reg.Adapter(sess.AdapterName())
		if !ok || !slices.Contains(mux.AdapterCapabilities(adapter), mux.CapManagedLifecycle) {
			return LifecycleResult{}, ErrLifecycleUnsupported
		}
		if entry, ok := s.catalog.Get(id); !ok || !entry.State.Terminal() {
			return LifecycleResult{}, ErrLifecycleNotTerminal
		}
	}
	entry, ok := s.catalog.Get(id)
	if !ok {
		return LifecycleResult{}, ErrLifecycleNotFound
	}
	if !entry.State.Terminal() {
		return LifecycleResult{}, ErrLifecycleNotTerminal
	}
	// Exactly this canonical session's records are removed.
	s.catalog.remove(id)
	if s.activity != nil {
		s.activity.Clear(id)
	}
	if s.transcript != nil {
		s.transcript.ClearTranscript(id)
	}
	// S1: a deleted session immediately loses its agent-activity record so a
	// recreated id cannot inherit it.
	if s.status != nil {
		s.status.Clear(id)
	}
	DeleteRecorder(id)
	// Keep the per-session lock in the map: removing it while another operation
	// holds a reference would let two callers use different mutexes for one id.
	return LifecycleResult{SessionID: id, Action: "delete", State: LifecycleExited}, nil
}

// awaitExit waits for confirmed process death (Recorder EOF) up to the graceful
// timeout; on timeout it escalates to SIGKILL and waits briefly more. Returns
// true only when death is confirmed (Recorder Done). A false result means the
// caller must NOT finalize the session as terminal.
func (s *LifecycleService) awaitExit(id string, mp mux.ManagedProcess) bool {
	rec := GetRecorder(id)
	if rec == nil {
		// No runtime recorder to observe (test fake, or already finalized). There
		// is no live process to leave running, so treat as exited.
		return true
	}
	select {
	case <-rec.Done():
		return true
	case <-time.After(s.graceful):
	}
	if terr := mp.TerminateGroup(true); terr != nil {
		log.Printf("lifecycle %s: SIGKILL escalation error: %v", id, terr)
	}
	select {
	case <-rec.Done():
		return true
	case <-time.After(s.killGrace):
		return false
	}
}

// finalize runs the one-time runtime cleanup and records the terminal state.
// Serialized by the per-session lock and guarded by the catalog terminal state,
// so registry removal and recorder stop happen exactly once and the catalog is
// terminal when finalize returns — natural exit and Stop/Kill converge here.
// (Exit code is not captured in M2: reaping the process here would race the
// adapter's own exit watcher over the shared exec.Cmd; the lifecycle STATE is
// authoritative, not the numeric code.)
func (s *LifecycleService) finalize(id string) {
	lock := s.lockFor(id)
	lock.Lock()
	defer lock.Unlock()
	if e, ok := s.catalog.Get(id); !ok || e.State.Terminal() {
		return
	}
	// Process is already dead (Recorder EOF) before we remove the registry entry.
	ref := sessionid.ParseSessionID(id)
	_ = s.reg.TerminateSession(context.Background(), ref.Adapter, ref.LocalID)
	DeleteRecorder(id)
	s.catalog.finalize(id, nil)
}
