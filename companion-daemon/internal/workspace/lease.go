package workspace

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrLeaseHeld        = errors.New("workspace: cooperative lease held")
	ErrStaleOwner       = errors.New("workspace: stale lease owner")
	ErrLeaseExpired     = errors.New("workspace: lease expired")
	ErrExternalDrift    = errors.New("workspace: external repository drift")
	ErrSnapshotMismatch = errors.New("workspace: snapshot mismatch")
)

// Lease is a cooperative POKIT writer claim. It does not lock the filesystem
// and cannot prevent external editors, shells, or Git clients from changing a
// repository.
type Lease struct {
	OwnerRuntimeID        string    `json:"ownerRuntimeId"`
	OwnerSessionID        string    `json:"ownerSessionId"`
	OwnerLaunchGeneration int64     `json:"ownerLaunchGeneration"`
	RepositoryID          string    `json:"repositoryId"`
	SnapshotID            string    `json:"snapshotId"`
	Epoch                 uint64    `json:"epoch"`
	ExpiresAt             time.Time `json:"expiresAt"`
	HeartbeatAt           time.Time `json:"heartbeatAt"`
	TreeHash              string    `json:"treeHash"`
}

type AcquireRequest struct {
	Identity              Identity
	OwnerRuntimeID        string
	OwnerSessionID        string
	OwnerLaunchGeneration int64
	TTL                   time.Duration
}

// Manager is an in-memory cooperative lease ledger. Expiry provides
// daemon-crash recovery; a new daemon can acquire after the old owner's lease
// expires. It has no goroutines, filesystem state, or authority callback.
type Manager struct {
	mu     sync.Mutex
	now    func() time.Time
	leases map[string]Lease
	epochs map[string]uint64
}

func NewManager(now func() time.Time) *Manager {
	if now == nil {
		now = time.Now
	}
	return &Manager{now: now, leases: make(map[string]Lease), epochs: make(map[string]uint64)}
}

func (m *Manager) Acquire(request AcquireRequest) (Lease, error) {
	if err := request.Identity.Validate(); err != nil || request.OwnerRuntimeID == "" || request.OwnerSessionID == "" || request.OwnerLaunchGeneration < 0 {
		return Lease{}, ErrInvalidIdentity
	}
	if err := validateTTL(request.TTL); err != nil {
		return Lease{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	if existing, ok := m.leases[request.Identity.RepositoryID]; ok && now.Before(existing.ExpiresAt) {
		return Lease{}, ErrLeaseHeld
	}
	next := m.epochs[request.Identity.RepositoryID] + 1
	m.epochs[request.Identity.RepositoryID] = next
	lease := Lease{OwnerRuntimeID: request.OwnerRuntimeID, OwnerSessionID: request.OwnerSessionID, OwnerLaunchGeneration: request.OwnerLaunchGeneration, RepositoryID: request.Identity.RepositoryID, SnapshotID: request.Identity.SnapshotID, TreeHash: request.Identity.TreeHash, Epoch: next, HeartbeatAt: now, ExpiresAt: now.Add(request.TTL)}
	m.leases[lease.RepositoryID] = lease
	return lease, nil
}

// Release performs compare-and-release: only the exact current owner/epoch can
// release a lease. An old process cannot release a replacement owner's claim.
func (m *Manager) Release(lease Lease) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, err := m.currentLocked(lease)
	if err != nil {
		return err
	}
	delete(m.leases, current.RepositoryID)
	return nil
}

func (m *Manager) Heartbeat(lease Lease, ttl time.Duration) (Lease, error) {
	if err := validateTTL(ttl); err != nil {
		return Lease{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, err := m.currentLocked(lease)
	if err != nil {
		return Lease{}, err
	}
	now := m.now()
	if !now.Before(current.ExpiresAt) {
		delete(m.leases, current.RepositoryID)
		return Lease{}, ErrLeaseExpired
	}
	current.HeartbeatAt, current.ExpiresAt = now, now.Add(ttl)
	m.leases[current.RepositoryID] = current
	return current, nil
}

func (m *Manager) Check(lease Lease) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, err := m.currentLocked(lease)
	if err != nil {
		return err
	}
	if !m.now().Before(current.ExpiresAt) {
		delete(m.leases, current.RepositoryID)
		return ErrLeaseExpired
	}
	return nil
}

// DetectDrift compares an observed workspace identity supplied by an external
// controller. A changed snapshot/tree invalidates the current lease's
// validation run but never attempts to undo an external edit.
func (m *Manager) DetectDrift(observed Identity) error {
	if err := observed.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	lease, ok := m.leases[observed.RepositoryID]
	if !ok {
		return nil
	}
	if lease.SnapshotID != observed.SnapshotID || lease.TreeHash != observed.TreeHash {
		delete(m.leases, observed.RepositoryID)
		return ErrExternalDrift
	}
	return nil
}

func (m *Manager) currentLocked(candidate Lease) (Lease, error) {
	current, ok := m.leases[candidate.RepositoryID]
	if !ok || current.Epoch != candidate.Epoch || current.OwnerRuntimeID != candidate.OwnerRuntimeID || current.OwnerSessionID != candidate.OwnerSessionID || current.OwnerLaunchGeneration != candidate.OwnerLaunchGeneration || current.SnapshotID != candidate.SnapshotID {
		return Lease{}, ErrStaleOwner
	}
	return current, nil
}
