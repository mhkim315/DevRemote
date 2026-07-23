// Package validation defines frozen clean-snapshot finding bindings. It is a
// pure contract: it does not dispatch validators, inspect dirty worktrees, or
// authorize acceptance, merge, or lifecycle transitions.
package validation

import (
	"errors"
	"sync"

	"devremote/companion-daemon/internal/workspace"
)

var (
	ErrInvalid = errors.New("validation: invalid binding")
	ErrStale   = errors.New("validation: finding is stale and cannot authorize")
)

const recentResults = 64

// SnapshotBinding freezes every identity that a validation result may rely on.
type SnapshotBinding struct {
	RepositoryID                                                              string
	Manifest                                                                  workspace.SnapshotManifest
	SnapshotID                                                                string
	LeaseEpoch                                                                uint64
	ValidatorProvider, ValidatorModel, ValidatorRuntimeID, ValidatorSessionID string
	ValidatorGeneration, ConfigEpoch                                          uint64
	EvidenceDigest, ArtifactDigest                                            string
	IsolationProfile                                                          workspace.IsolationProfile
}

func (b SnapshotBinding) Validate() error {
	if b.RepositoryID == "" || b.SnapshotID == "" || b.ValidatorProvider == "" || b.ValidatorModel == "" || b.ValidatorRuntimeID == "" || b.ValidatorSessionID == "" || b.EvidenceDigest == "" || b.ArtifactDigest == "" || b.Manifest.Validate() != nil || b.IsolationProfile != workspace.IsolationRepoOnly {
		return ErrInvalid
	}
	return nil
}

type Finding struct {
	ID, Summary string
	Binding     SnapshotBinding
	Stale       bool
}
type ValidationResult struct {
	ID       string
	Binding  SnapshotBinding
	Findings []Finding
}

func (r ValidationResult) Validate() error {
	if r.ID == "" || r.Binding.Validate() != nil {
		return ErrInvalid
	}
	for _, f := range r.Findings {
		if f.ID == "" || f.Summary == "" || f.Binding.Validate() != nil || !sameBinding(f.Binding, r.Binding) {
			return ErrInvalid
		}
	}
	return nil
}

// ValidationStore is a restart-volatile, in-memory record with a bounded ring
// buffer for polling consumers. Zero goroutines — callers read on demand.
type ValidationStore struct {
	mu      sync.RWMutex
	results []ValidationResult // full history for ReadAll
	ring    []ValidationResult // ring buffer for ReadRecent
	pos     int
	full    bool
}

// NewValidationStore returns an empty store.
func NewValidationStore() *ValidationStore {
	return &ValidationStore{ring: make([]ValidationResult, recentResults)}
}

// Submit validates and retains one result. It pushes into both the full
// history and the ring buffer for polling readers.
func (s *ValidationStore) Submit(result ValidationResult) error {
	if err := result.Validate(); err != nil {
		return err
	}
	result = cloneResult(result)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.results = append(s.results, result)
	s.ring[s.pos] = cloneResult(result)
	s.pos++
	if s.pos >= len(s.ring) {
		s.pos = 0
		s.full = true
	}
	return nil
}

// ReadAll returns a defensive snapshot of full history.
func (s *ValidationStore) ReadAll() []ValidationResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	results := make([]ValidationResult, len(s.results))
	for i, result := range s.results {
		results[i] = cloneResult(result)
	}
	return results
}

// ReadRecent returns the most recent N results in insertion order.
// N is clamped to the ring buffer size.
func (s *ValidationStore) ReadRecent(n int) []ValidationResult {
	if n <= 0 {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	capacity := len(s.ring)
	size := s.pos
	if s.full {
		size = capacity
	}
	if n > capacity {
		n = capacity
	}
	if n > size {
		n = size
	}
	if n == 0 {
		return nil
	}
	out := make([]ValidationResult, n)
	if s.full {
		start := (s.pos - size + capacity) % capacity
		for i := 0; i < n; i++ {
			idx := (start + size - n + i) % capacity
			out[i] = cloneResult(s.ring[idx])
		}
	} else {
		for i := 0; i < n; i++ {
			out[i] = cloneResult(s.ring[s.pos-n+i])
		}
	}
	return out
}

// Close clears the ring buffer. No goroutines to drain.
func (s *ValidationStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ring = nil
}

func cloneResult(result ValidationResult) ValidationResult {
	result.Findings = append([]Finding(nil), result.Findings...)
	return result
}

type StalenessCheck struct{ Current SnapshotBinding }

func (s StalenessCheck) Stale(binding SnapshotBinding) bool {
	return binding.Validate() != nil || s.Current.Validate() != nil || !sameBinding(binding, s.Current)
}
func (s StalenessCheck) Apply(f Finding) Finding { f.Stale = s.Stale(f.Binding); return f }

func (f Finding) CanAuthorize(current SnapshotBinding) error {
	if (StalenessCheck{Current: current}).Stale(f.Binding) {
		return ErrStale
	}
	return nil
}

func sameBinding(a, b SnapshotBinding) bool { return a == b }
