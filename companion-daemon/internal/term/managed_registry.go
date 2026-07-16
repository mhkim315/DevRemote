// Package term — SP0-P2: minimal owned-session registry for native managed
// sessions. This is the single semantic-status authority store for managed
// sessions. It has NO dependency on mux.Registry, discovery, adapter
// snapshots, screen readers, JSONL, or telemetry — native app-server events
// (applied by the owning runtime's event pump) are the only status writers.
package term

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// ManagedNativeStatus is the closed semantic-status vocabulary for managed
// sessions. Values come only from exact native protocol events; unknown
// events can never fabricate a known status.
type ManagedNativeStatus string

const (
	// ManagedStatusIdle — registered, handshake complete, no active turn yet.
	ManagedStatusIdle ManagedNativeStatus = "idle"
	// ManagedStatusWorking — exact native `turn/started` for the bound thread.
	ManagedStatusWorking ManagedNativeStatus = "working"
	// ManagedStatusCompleted — exact native `turn/completed` for the bound
	// thread (the semantic completed/idle state after a turn).
	ManagedStatusCompleted ManagedNativeStatus = "completed"
	// ManagedStatusExited — the daemon-owned child exited; set only via
	// MarkExited (process-lifecycle authority), never via a native event.
	ManagedStatusExited ManagedNativeStatus = "exited"
)

// ManagedSessionRecord is the provider-neutral owned-session record. Identity
// fields (SessionID, Provider, Version, Epoch, ProcessID, OS, Arch, CreatedAt,
// CertifiedDigest) are immutable after Register. ProcessID is an opaque
// launcher-derived token — never parsed and never exposed in any DTO.
type ManagedSessionRecord struct {
	SessionID       string
	Provider        string
	Version         string
	Epoch           int64
	ProcessID       string
	OS              string
	Arch            string
	CreatedAt       time.Time
	CertifiedDigest string // hex-encoded SHA-256 of attested binary (C1D)

	NativeStatus    ManagedNativeStatus
	StatusChangedAt time.Time
	Exited          bool
}

// ManagedSessionRegistry is a bounded, concurrency-safe owned-session store.
// One mutex is the single linearization point for Register, Get, List,
// UpdateNativeStatus, MarkExited, Remove, and Close. Reads return copies.
// No lock is ever held across external I/O.
type ManagedSessionRegistry struct {
	mu      sync.Mutex
	closed  bool
	max     int
	records map[string]ManagedSessionRecord
}

// NewManagedSessionRegistry creates a registry with a fixed capacity.
// Registration on a full registry fails closed.
func NewManagedSessionRegistry(max int) *ManagedSessionRegistry {
	return &ManagedSessionRegistry{max: max, records: make(map[string]ManagedSessionRecord)}
}

// Register inserts a new record with status idle. Fails closed on a closed
// registry, duplicate SessionID, or capacity exhaustion — existing records
// are never replaced or mutated by a failed Register.
func (g *ManagedSessionRegistry) Register(rec ManagedSessionRecord) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return fmt.Errorf("managed registry closed")
	}
	if _, dup := g.records[rec.SessionID]; dup {
		return fmt.Errorf("managed session %q already registered", rec.SessionID)
	}
	if len(g.records) >= g.max {
		return fmt.Errorf("managed session capacity exhausted (%d)", g.max)
	}
	rec.NativeStatus = ManagedStatusIdle
	rec.StatusChangedAt = rec.CreatedAt
	rec.Exited = false
	g.records[rec.SessionID] = rec
	return nil
}

// UpdateNativeStatus applies a native semantic-status transition. It is
// accepted only when the registry is open, the record exists, the record has
// not exited, the epoch matches the record's current epoch exactly, and the
// status is in the closed non-exited vocabulary. Anything else is rejected
// without mutating state — a closed/old-epoch event can never restore or
// fabricate a current status.
func (g *ManagedSessionRegistry) UpdateNativeStatus(sessionID string, epoch int64, status ManagedNativeStatus) bool {
	switch status {
	case ManagedStatusIdle, ManagedStatusWorking, ManagedStatusCompleted:
	default:
		return false // exited is reserved for MarkExited; unknown values rejected
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return false
	}
	rec, ok := g.records[sessionID]
	if !ok || rec.Exited || rec.Epoch != epoch {
		return false
	}
	rec.NativeStatus = status
	rec.StatusChangedAt = time.Now()
	g.records[sessionID] = rec
	return true
}

// MarkExited records child exit for the current epoch. The record becomes
// non-current: all later native updates for it are rejected.
func (g *ManagedSessionRegistry) MarkExited(sessionID string, epoch int64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return false
	}
	rec, ok := g.records[sessionID]
	if !ok || rec.Exited || rec.Epoch != epoch {
		return false
	}
	rec.Exited = true
	rec.NativeStatus = ManagedStatusExited
	rec.StatusChangedAt = time.Now()
	g.records[sessionID] = rec
	return true
}

// Get returns a copy of the record.
func (g *ManagedSessionRegistry) Get(sessionID string) (ManagedSessionRecord, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	rec, ok := g.records[sessionID]
	return rec, ok
}

// List returns copies of all records, ordered by CreatedAt then SessionID.
func (g *ManagedSessionRegistry) List() []ManagedSessionRecord {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]ManagedSessionRecord, 0, len(g.records))
	for _, rec := range g.records {
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].SessionID < out[j].SessionID
	})
	return out
}

// Remove deletes the record. Later updates for the removed ID are rejected.
func (g *ManagedSessionRegistry) Remove(sessionID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.records[sessionID]
	delete(g.records, sessionID)
	return ok
}

// Close permanently rejects all further registrations and status updates.
// Called at daemon shutdown before the owned children are stopped so a late
// pump event cannot update state.
func (g *ManagedSessionRegistry) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
}
