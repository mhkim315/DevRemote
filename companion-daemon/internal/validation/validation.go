// Package validation defines frozen clean-snapshot finding bindings. It is a
// pure contract: it does not dispatch validators, inspect dirty worktrees, or
// authorize acceptance, merge, or lifecycle transitions.
package validation

import (
	"errors"
	"sync"
	"time"

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

const (
	subscriberQueueCap = 64
	maxCallbacks       = 8
	subscriberTimeout  = 2 * time.Second
	closeDeadline      = 5 * time.Second
)

type subscriberEntry struct {
	id uint64
	fn Subscriber
}

type ValidationStore struct {
	mu             sync.RWMutex
	results        []ValidationResult
	subscribers    []subscriberEntry
	nextSubID      uint64
	subscriberCh   chan ValidationResult
	subscriberDone chan struct{}
	callbackSem    chan struct{}
	callbacks      sync.WaitGroup
	closed         bool
	closeOnce      sync.Once
}

func (s *ValidationStore) dispatchSubscribers() {
	for result := range s.subscriberCh {
		s.mu.RLock()
		subscribers := append([]subscriberEntry(nil), s.subscribers...)
		s.mu.RUnlock()
		for _, subscriber := range subscribers {
			s.callbackSem <- struct{}{}
			s.callbacks.Add(1)
			done := make(chan struct{})
			go s.invokeSubscriber(subscriber.fn, result, done)
			select {
			case <-done:
			case <-time.After(subscriberTimeout):
			}
		}
	}
	s.callbacks.Wait()
	close(s.subscriberDone)
}

func (s *ValidationStore) startSubscriberLoopLocked() {
	if s.subscriberCh != nil {
		return
	}
	s.subscriberCh = make(chan ValidationResult, subscriberQueueCap)
	s.subscriberDone = make(chan struct{})
	s.callbackSem = make(chan struct{}, maxCallbacks)
	go s.dispatchSubscribers()
}

func (s *ValidationStore) invokeSubscriber(fn Subscriber, result ValidationResult, done chan<- struct{}) {
	defer s.callbacks.Done()
	defer func() {
		<-s.callbackSem
		close(done)
		recover()
	}()
	fn(result)
}

// Submit validates and retains one result, then performs one non-blocking
// enqueue while holding store-owned state so Close cannot race channel closure.
func (s *ValidationStore) Submit(result ValidationResult) error {
	if err := result.Validate(); err != nil {
		return err
	}
	result = cloneResult(result)

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrInvalid
	}
	s.results = append(s.results, result)
	if len(s.subscribers) > 0 {
		s.startSubscriberLoopLocked()
	}
	if s.subscriberCh != nil {
		select {
		case s.subscriberCh <- cloneResult(result):
		default:
		}
	}
	s.mu.Unlock()
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

// NewValidationStore returns an empty store. Its bounded worker starts only
// when a submitted result has an observer, avoiding idle runtime goroutines.
func NewValidationStore() *ValidationStore {
	return &ValidationStore{}
}

// Close is idempotent. It drains queued results and waits at most five seconds
// for bounded callback work; validation remains non-authoritative either way.
func (s *ValidationStore) Close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		if s.subscriberCh != nil {
			close(s.subscriberCh)
		}
		s.mu.Unlock()
		if s.subscriberDone == nil {
			return
		}
		select {
		case <-s.subscriberDone:
		case <-time.After(closeDeadline):
		}
	})
}

// Subscribe adds an observational callback and returns its cleanup function.
func (s *ValidationStore) Subscribe(fn Subscriber) func() {
	if fn == nil {
		return func() {}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return func() {}
	}
	s.nextSubID++
	id := s.nextSubID
	s.subscribers = append(s.subscribers, subscriberEntry{id: id, fn: fn})
	return func() { s.unsubscribe(id) }
}

func (s *ValidationStore) unsubscribe(id uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, subscriber := range s.subscribers {
		if subscriber.id == id {
			s.subscribers = append(s.subscribers[:i], s.subscribers[i+1:]...)
			return
		}
	}
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
