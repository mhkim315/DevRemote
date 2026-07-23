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

// SnapshotBinding freezes every identity that a validation result may rely on.
// A clean SnapshotManifest is required; dirty-worktree validation is deferred.
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

// Finding remains retained history even after it becomes stale.
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

// Subscriber observes a successfully submitted validation result. Subscribers
// are observational only: they do not participate in validation or any
// acceptance authority.
type Subscriber func(ValidationResult)

// ValidationStore is a restart-volatile, in-memory record of submitted
// validation results. It deliberately owns no validator dispatch, repository
// state, or acceptance authority.
type ValidationStore struct {
	mu          sync.RWMutex
	results     []ValidationResult
	subscribers []Subscriber
}

// Submit validates and retains one result, then notifies observers
// asynchronously. An observer cannot delay or change submission.
func (s *ValidationStore) Submit(result ValidationResult) error {
	if err := result.Validate(); err != nil {
		return err
	}
	result = cloneResult(result)

	s.mu.Lock()
	s.results = append(s.results, result)
	subscribers := append([]Subscriber(nil), s.subscribers...)
	s.mu.Unlock()

	for _, subscriber := range subscribers {
		go subscriber(cloneResult(result))
	}
	return nil
}

// ReadAll returns a defensive snapshot, so cockpit/read-model callers cannot
// mutate the stored validation history.
func (s *ValidationStore) ReadAll() []ValidationResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	results := make([]ValidationResult, len(s.results))
	for i, result := range s.results {
		results[i] = cloneResult(result)
	}
	return results
}

// Subscribe adds an observational callback. Nil callbacks are ignored.
func (s *ValidationStore) Subscribe(fn Subscriber) {
	if fn == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subscribers = append(s.subscribers, fn)
}

func cloneResult(result ValidationResult) ValidationResult {
	result.Findings = append([]Finding(nil), result.Findings...)
	return result
}

// StalenessCheck compares a finding/result binding against current explicit
// identities. Any change is stale; there is no fallback or automatic replay.
type StalenessCheck struct{ Current SnapshotBinding }

func (s StalenessCheck) Stale(binding SnapshotBinding) bool {
	return binding.Validate() != nil || s.Current.Validate() != nil || !sameBinding(binding, s.Current)
}
func (s StalenessCheck) Apply(f Finding) Finding { f.Stale = s.Stale(f.Binding); return f }

// CanAuthorize is intentionally false for stale findings. Callers still need
// their own authority; a fresh validation result alone is never authorization.
func (f Finding) CanAuthorize(current SnapshotBinding) error {
	if (StalenessCheck{Current: current}).Stale(f.Binding) {
		return ErrStale
	}
	return nil
}

func sameBinding(a, b SnapshotBinding) bool { return a == b }
