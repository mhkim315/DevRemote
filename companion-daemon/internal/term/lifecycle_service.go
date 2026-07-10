package term

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"devremote/companion-daemon/internal/mux"
)

// Lifecycle errors map to structured, non-500 handler responses.
var (
	ErrLifecycleNotFound    = errors.New("session not found")
	ErrLifecycleUnsupported = errors.New("session does not support managed lifecycle")
	ErrLifecycleNotTerminal = errors.New("session must be stopped before it can be deleted")
)

// LifecycleResult is the structured result of a lifecycle action.
type LifecycleResult struct {
	SessionID string         `json:"sessionId"`
	Action    string         `json:"action"`
	State     LifecycleState `json:"state"`
}

// LifecycleService is the single owner of Stop/Kill/Delete for managed
// sessions. Every action converges here: capability check, catalog state,
// process-group termination, and one-time runtime cleanup. Per-session locks
// serialize concurrent actions; finalize is guarded by the catalog terminal
// state so registry removal and recorder stop happen exactly once, and natural
// exit, Stop and Kill all converge on one final state.
type LifecycleService struct {
	reg      *mux.Registry
	activity *ActivityBuffer
	catalog  *SessionCatalog
	graceful time.Duration

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewLifecycleService(reg *mux.Registry, activity *ActivityBuffer) *LifecycleService {
	return &LifecycleService{
		reg:      reg,
		activity: activity,
		catalog:  NewSessionCatalog(),
		graceful: 5 * time.Second,
		locks:    make(map[string]*sync.Mutex),
	}
}

// Catalog exposes the read-only catalog (state lookups in handlers/tests).
func (s *LifecycleService) Catalog() *SessionCatalog { return s.catalog }

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
// watcher that finalizes the catalog + runtime when the process ends. Called
// right after the Recorder is confirmed ready.
func (s *LifecycleService) Register(canonicalID, adapter, profileID, name string) {
	s.catalog.Add(canonicalID, adapter, profileID, name)
	rec := GetRecorder(canonicalID)
	if rec == nil {
		return
	}
	// Single convergence point: process death (natural OR from Stop/Kill) closes
	// the PTY → Recorder EOF → Done; finalize runs the one-time cleanup.
	go func() {
		<-rec.Done()
		s.finalize(canonicalID)
	}()
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
	_ = mp.TerminateGroup(false) // SIGTERM to the whole group
	lock.Unlock()

	s.awaitExit(id, mp)
	s.finalize(id)

	entry, _ := s.catalog.Get(id)
	return LifecycleResult{SessionID: id, Action: "stop", State: entry.State}, nil
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
	_ = mp.TerminateGroup(true) // SIGKILL to the whole group
	lock.Unlock()

	s.awaitExit(id, mp)
	s.finalize(id)

	entry, _ := s.catalog.Get(id)
	return LifecycleResult{SessionID: id, Action: "kill", State: entry.State}, nil
}

// Delete removes an ENDED managed session's catalog row, activity/transcript
// history and recorder references. A running/stopping session is a conflict —
// it must be stopped first. Non-managed sessions are rejected; unknown → 404.
func (s *LifecycleService) Delete(ctx context.Context, id string) (LifecycleResult, error) {
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
	DeleteRecorder(id)
	s.mu.Lock()
	delete(s.locks, id)
	s.mu.Unlock()
	return LifecycleResult{SessionID: id, Action: "delete", State: LifecycleExited}, nil
}

// awaitExit waits for the process to die (Recorder EOF) up to the graceful
// timeout; on timeout it escalates to SIGKILL and waits briefly more.
func (s *LifecycleService) awaitExit(id string, mp mux.ManagedProcess) {
	rec := GetRecorder(id)
	if rec == nil {
		return
	}
	select {
	case <-rec.Done():
		return
	case <-time.After(s.graceful):
	}
	_ = mp.TerminateGroup(true) // escalate: SIGKILL the group
	select {
	case <-rec.Done():
	case <-time.After(2 * time.Second):
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
	ref := mux.ParseSessionID(id)
	_ = s.reg.TerminateSession(context.Background(), ref.Adapter, ref.LocalID)
	DeleteRecorder(id)
	s.catalog.finalize(id, nil)
}
