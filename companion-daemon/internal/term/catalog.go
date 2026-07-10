package term

import (
	"sync"
	"time"
)

// CatalogEntry is a Session Catalog row: the daemon-authoritative lifecycle
// record for a managed session. It is separate from the Registry (live
// runtime) and the Recorder (capture) so an exited runtime can leave the
// Registry while its catalog row and history remain readable.
type CatalogEntry struct {
	ID        string         `json:"id"` // canonical <adapter>:<local-id>
	Adapter   string         `json:"adapter"`
	ProfileID string         `json:"profileId,omitempty"`
	Name      string         `json:"name,omitempty"`
	State     LifecycleState `json:"state"`
	StartedAt time.Time      `json:"startedAt"`
	EndedAt   *time.Time     `json:"endedAt,omitempty"`
	ExitCode  *int           `json:"exitCode,omitempty"`

	killRequested bool // set by Kill so the final state becomes killed not exited
}

// SessionCatalog is an in-memory store of managed-session lifecycle records.
// All transitions are guarded so lifecycle actions are idempotent and races
// (concurrent Stop, Stop/Kill, natural-exit/Stop) converge on one final state.
type SessionCatalog struct {
	mu      sync.Mutex
	entries map[string]*CatalogEntry
	now     func() time.Time
}

func NewSessionCatalog() *SessionCatalog {
	return &SessionCatalog{entries: make(map[string]*CatalogEntry), now: time.Now}
}

// Add registers a freshly created managed session as running.
func (c *SessionCatalog) Add(id, adapter, profileID, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[id] = &CatalogEntry{
		ID:        id,
		Adapter:   adapter,
		ProfileID: profileID,
		Name:      name,
		State:     LifecycleRunning,
		StartedAt: c.now(),
	}
}

// Get returns a copy of the entry.
func (c *SessionCatalog) Get(id string) (CatalogEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[id]
	if !ok {
		return CatalogEntry{}, false
	}
	return *e, true
}

// beginStop transitions running/starting → stopping. proceed is true only for
// the caller that performed the transition (so exactly one caller drives
// termination). found is false for an unknown session.
func (c *SessionCatalog) beginStop(id string) (proceed bool, current LifecycleState, found bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[id]
	if !ok {
		return false, "", false
	}
	if e.State == LifecycleRunning || e.State == LifecycleStarting {
		e.State = LifecycleStopping
		return true, e.State, true
	}
	return false, e.State, true
}

// requestKill records kill intent so the final state becomes killed. proceed is
// false only when the session is already terminal.
func (c *SessionCatalog) requestKill(id string) (proceed bool, current LifecycleState, found bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[id]
	if !ok {
		return false, "", false
	}
	if e.State.Terminal() {
		return false, e.State, true
	}
	e.killRequested = true
	return true, e.State, true
}

// finalize records the terminal state exactly once. killed if a Kill was
// requested, otherwise exited. Idempotent: a no-op once terminal, so natural
// exit, Stop, and Kill converge on one final row.
func (c *SessionCatalog) finalize(id string, exitCode *int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[id]
	if !ok || e.State.Terminal() {
		return
	}
	if e.killRequested {
		e.State = LifecycleKilled
	} else {
		e.State = LifecycleExited
	}
	t := c.now()
	e.EndedAt = &t
	e.ExitCode = exitCode
}

// remove deletes the catalog row (used by Delete, only from a terminal state).
func (c *SessionCatalog) remove(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, id)
}
