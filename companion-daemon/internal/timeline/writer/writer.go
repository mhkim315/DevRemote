// Package writer provides the explicitly constructed, fail-open shadow sink
// for Canonical Timeline envelopes. It owns no authority and starts exactly
// one I/O worker goroutine for non-blocking submission.
package writer

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"devremote/companion-daemon/internal/timeline/contract"
)

const (
	DefaultFileMode   os.FileMode = 0o600
	recentEnvelopes               = 128
	submitBufCap                  = 256
	closeDrainTimeout             = 5 * time.Second
)

var ErrInvalidConfig = errors.New("timeline writer: invalid configuration")

type Config struct {
	Path string
}

// ProducerToken is an opaque per-handle auth token. Only the composition
// root can create valid tokens; callers cannot forge handles.
type ProducerToken struct {
	id string
}

func newProducerToken() (ProducerToken, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ProducerToken{}, err
	}
	return ProducerToken{id: hex.EncodeToString(b)}, nil
}

// ProducerHandle identifies one bound managed-runtime generation. Fields
// are unexported — only the composition root can bind.
type producerHandle struct {
	provider   string
	runtimeID  string
	sessionID  string
	generation int64
	token      ProducerToken
	kinds      map[contract.EventKind]struct{}
}

// Capability is the interface exposed to managed runtimes for submission.
type Capability struct {
	h      producerHandle
	token  ProducerToken
	writer *Writer
}

// Token returns the opaque token that must match the stored handle.
func (c Capability) Token() ProducerToken { return c.token }

// SubmitAfterCommit is the fail-open, non-blocking submission path. Callers
// must call it AFTER their primary authority has committed. Returns false
// on drop (queue full, unregistered, invalid envelope).
func (c Capability) SubmitAfterCommit(envelope contract.Envelope) bool {
	if c.writer == nil {
		return false
	}
	return c.writer.submit(envelope, c.h, c.token)
}

// ProducerStore is the concrete auth implementation. Only the composition
// root creates capabilities; managed runtimes receive opaque Capability values.
// Implements RuntimeVerifier (self-verifying via stored handles).
type ProducerStore struct {
	mu     sync.RWMutex
	active map[string]producerHandle
	writer *Writer
}

// IsManagedSession checks whether the session identity has a bound handle.
func (s *ProducerStore) IsManagedSession(sessionID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, h := range s.active {
		if h.sessionID == sessionID {
			return true
		}
	}
	return false
}

// RuntimeOf returns the identity of a bound session.
func (s *ProducerStore) RuntimeOf(sessionID string) (provider, runtimeID string, generation int64, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, h := range s.active {
		if h.sessionID == sessionID {
			return h.provider, h.runtimeID, h.generation, true
		}
	}
	return "", "", 0, false
}

// NewProducerStore returns an empty producer store.
func NewProducerStore() *ProducerStore {
	return &ProducerStore{active: make(map[string]producerHandle)}
}

// Bind creates a capability for a managed runtime generation. The returned
// Capability carries an opaque token that must match at submission time.
func (s *ProducerStore) Bind(provider, runtimeID, sessionID string, generation int64, kinds ...contract.EventKind) (Capability, error) {
	if !validBinding(provider, sessionID) || runtimeID == "" || generation <= 0 || len(kinds) == 0 {
		return Capability{}, ErrInvalidConfig
	}
	for _, kind := range kinds {
		if !isBoundEventKind(kind) {
			return Capability{}, ErrInvalidConfig
		}
	}
	boundKinds := make(map[contract.EventKind]struct{}, len(kinds))
	for _, kind := range kinds {
		boundKinds[kind] = struct{}{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writer == nil {
		return Capability{}, ErrInvalidConfig
	}
	key := fmt.Sprintf("%s:%s:%d", provider, sessionID, generation)
	tok, err := newProducerToken()
	if err != nil {
		return Capability{}, err
	}
	h := producerHandle{
		provider: provider, runtimeID: runtimeID, sessionID: sessionID,
		generation: generation, token: tok, kinds: boundKinds,
	}
	s.active[key] = h
	return Capability{h: h, token: tok, writer: s.writer}, nil
}

func (s *ProducerStore) attach(w *Writer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writer = w
}

// Revoke removes a producer by identity. Future submissions with the
// revoked handle are dropped.
func (s *ProducerStore) Revoke(provider, sessionID string, generation int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%s:%s:%d", provider, sessionID, generation)
	delete(s.active, key)
}

// IsBound checks whether a handle exists AND its token matches.
func (s *ProducerStore) IsBound(h producerHandle, tok ProducerToken) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isBoundLocked(h, tok)
}

func (s *ProducerStore) isBoundLocked(h producerHandle, tok ProducerToken) bool {
	key := fmt.Sprintf("%s:%s:%d", h.provider, h.sessionID, h.generation)
	stored, ok := s.active[key]
	return ok && stored.provider == h.provider && stored.runtimeID == h.runtimeID &&
		stored.sessionID == h.sessionID && stored.generation == h.generation && stored.token == tok
}

// authorizeAnd holds the binding read lock through fn. Revoke takes the write
// lock, so once Revoke returns no submission authenticated by the old token can
// still enqueue.
func (s *ProducerStore) authorizeAnd(h producerHandle, tok ProducerToken, fn func()) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.isBoundLocked(h, tok) {
		return false
	}
	fn()
	return true
}

func validBinding(provider, sessionID string) bool {
	switch provider {
	case "codex":
		return strings.HasPrefix(sessionID, "codex_app_server:")
	case "claude":
		return strings.HasPrefix(sessionID, "claude_headless:")
	default:
		return false
	}
}

func (h producerHandle) allows(kind contract.EventKind) bool {
	_, ok := h.kinds[kind]
	return ok
}

func isBoundEventKind(kind contract.EventKind) bool {
	switch kind {
	case contract.EventProviderInvocationStarted, contract.EventProviderInvocationFinished,
		contract.EventApprovalRequested, contract.EventApprovalResolved,
		contract.EventToolCallStarted, contract.EventToolCallFinished,
		contract.EventStreamObserved:
		return true
	default:
		return false
	}
}

// ProducerAuth is the minimal interface for capability checks.
type ProducerAuth interface {
	IsBound(h producerHandle, tok ProducerToken) bool
	authorizeAnd(h producerHandle, tok ProducerToken, fn func()) bool
}

var _ ProducerAuth = (*ProducerStore)(nil)

type appendFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

type Stats struct {
	Appended uint64
	Dropped  uint64
	Failures uint64
}

type Health struct {
	mu       sync.RWMutex
	degraded bool
	reason   string
}

func (h *Health) markDegraded(reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.degraded = true
	h.reason = reason
}

func (h *Health) snapshot() (bool, string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.degraded, h.reason
}

type submitWork struct {
	envelope contract.Envelope
	handle   producerHandle
	token    ProducerToken
}

// CloseResult reports the outcome of a truthful Close.
type CloseResult struct {
	WorkerExited   bool
	InFlight       int
	PendingDropped uint64
}

// Writer serializes append records through a non-blocking bounded queue
// and a single I/O worker.
type Writer struct {
	mu            sync.Mutex
	file          appendFile
	closed        bool
	closing       bool
	inFlight      int
	appended      uint64
	dropped       uint64
	failures      uint64
	ring          []contract.Envelope
	pos           int
	full          bool
	ringMu        sync.RWMutex
	submitQueue   []submitWork
	submitCond    *sync.Cond
	workerWg      sync.WaitGroup
	workerDone    chan struct{}
	closeWait     time.Duration
	closeOnce     sync.Once
	closeErr      error
	closeResult   CloseResult
	auth          ProducerAuth
	health        Health
	config        Config
	beforeWrite   func() // deterministic test seam; nil in production
	beforeEnqueue func() // deterministic test seam; nil in production
}

func Open(config Config, auth ProducerAuth) (*Writer, error) {
	if config.Path == "" || !filepath.IsAbs(config.Path) {
		return nil, ErrInvalidConfig
	}
	if err := os.MkdirAll(filepath.Dir(config.Path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(config.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, DefaultFileMode)
	if err != nil {
		return nil, err
	}
	return newWriter(f, config, auth), nil
}

func newWriter(file appendFile, config Config, auth ProducerAuth) *Writer {
	w := &Writer{
		file:        file,
		ring:        make([]contract.Envelope, recentEnvelopes),
		submitQueue: make([]submitWork, 0, submitBufCap),
		auth:        auth,
		config:      config,
		workerDone:  make(chan struct{}),
		closeWait:   closeDrainTimeout,
	}
	w.submitCond = sync.NewCond(&w.mu)
	if store, ok := auth.(*ProducerStore); ok {
		store.attach(w)
	}
	w.startWorker()
	return w
}

func (w *Writer) startWorker() {
	w.workerWg.Add(1)
	go func() {
		defer w.workerWg.Done()
		defer close(w.workerDone)
		defer func() {
			w.mu.Lock()
			file := w.file
			w.file = nil
			w.closed = true
			w.mu.Unlock()
			if file != nil {
				_ = file.Close()
			}
		}()
		for {
			w.mu.Lock()
			for len(w.submitQueue) == 0 && !w.closing {
				w.submitCond.Wait()
			}
			if w.closing {
				w.mu.Unlock()
				return
			}
			work := w.submitQueue[0]
			w.submitQueue[0] = submitWork{}
			w.submitQueue = w.submitQueue[1:]
			w.mu.Unlock()
			w.processSubmit(work)
			w.mu.Lock()
			closing := w.closing
			w.mu.Unlock()
			if closing {
				return
			}
		}
	}()
}

func (w *Writer) processSubmit(work submitWork) {
	claimed := false
	defer func() {
		if claimed {
			w.mu.Lock()
			w.inFlight--
			w.mu.Unlock()
		}
		if r := recover(); r != nil {
			atomic.AddUint64(&w.dropped, 1)
			atomic.AddUint64(&w.failures, 1)
			w.health.markDegraded("worker failure")
		}
	}()
	record, err := json.Marshal(work.envelope)
	if err != nil {
		atomic.AddUint64(&w.dropped, 1)
		atomic.AddUint64(&w.failures, 1)
		w.health.markDegraded("marshal failure")
		return
	}
	record = append(record, '\n')

	if w.beforeWrite != nil {
		w.beforeWrite()
	}
	w.mu.Lock()
	if w.closed || w.closing || w.file == nil {
		atomic.AddUint64(&w.dropped, 1)
		w.mu.Unlock()
		return
	}
	w.inFlight++
	claimed = true
	file := w.file
	w.mu.Unlock()
	n, err := file.Write(record)
	if err != nil || n != len(record) {
		atomic.AddUint64(&w.dropped, 1)
		atomic.AddUint64(&w.failures, 1)
		w.health.markDegraded("write failure")
		return
	}
	w.mu.Lock()
	closing := w.closing || w.closed
	w.mu.Unlock()
	if closing {
		atomic.AddUint64(&w.dropped, 1)
		return
	}
	if err := file.Sync(); err != nil {
		atomic.AddUint64(&w.dropped, 1)
		atomic.AddUint64(&w.failures, 1)
		w.health.markDegraded("sync failure")
		return
	}

	w.mu.Lock()
	if w.closing || w.closed {
		atomic.AddUint64(&w.dropped, 1)
		w.mu.Unlock()
		return
	}
	atomic.AddUint64(&w.appended, 1)
	w.ringMu.Lock()
	w.ring[w.pos] = work.envelope
	w.pos++
	if w.pos >= len(w.ring) {
		w.pos = 0
		w.full = true
	}
	w.ringMu.Unlock()
	w.inFlight--
	claimed = false
	w.mu.Unlock()
}

// submit is the internal entry point gated by auth.
func (w *Writer) submit(envelope contract.Envelope, h producerHandle, tok ProducerToken) bool {
	if err := envelope.Validate(); err != nil {
		atomic.AddUint64(&w.dropped, 1)
		return false
	}
	// Validate envelope identity against handle.
	if envelope.Provider != h.provider || envelope.SessionID != h.sessionID || envelope.RuntimeID != h.runtimeID || envelope.LaunchGeneration != h.generation || !h.allows(envelope.EventKind) {
		atomic.AddUint64(&w.dropped, 1)
		return false
	}
	if w.auth == nil {
		atomic.AddUint64(&w.dropped, 1)
		return false
	}
	enqueued := false
	authorized := w.auth.authorizeAnd(h, tok, func() {
		if w.beforeEnqueue != nil {
			w.beforeEnqueue()
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.closing || w.closed {
			atomic.AddUint64(&w.dropped, 1)
			return
		}
		if len(w.submitQueue) == submitBufCap {
			atomic.AddUint64(&w.dropped, 1)
			w.health.markDegraded("submission queue full")
			return
		}
		w.submitQueue = append(w.submitQueue, submitWork{envelope: envelope, handle: h, token: tok})
		w.submitCond.Signal()
		enqueued = true
	})
	if !authorized {
		atomic.AddUint64(&w.dropped, 1)
	}
	return enqueued
}

// Append is legacy-only. It is rejected when producer authorization is
// configured, so an activated Timeline path cannot bypass Capability binding.
func (w *Writer) Append(envelope contract.Envelope) bool {
	if w.auth != nil || envelope.Validate() != nil {
		atomic.AddUint64(&w.dropped, 1)
		return false
	}
	record, err := json.Marshal(envelope)
	if err != nil {
		atomic.AddUint64(&w.dropped, 1)
		atomic.AddUint64(&w.failures, 1)
		return false
	}
	record = append(record, '\n')
	w.mu.Lock()
	if w.closed || w.closing || w.file == nil {
		atomic.AddUint64(&w.dropped, 1)
		w.mu.Unlock()
		return false
	}
	n, err := w.file.Write(record)
	if err != nil || n != len(record) {
		atomic.AddUint64(&w.dropped, 1)
		atomic.AddUint64(&w.failures, 1)
		w.health.markDegraded("legacy write failure")
		w.mu.Unlock()
		return false
	}
	if err := w.file.Sync(); err != nil {
		atomic.AddUint64(&w.dropped, 1)
		atomic.AddUint64(&w.failures, 1)
		w.health.markDegraded("legacy sync failure")
		w.mu.Unlock()
		return false
	}
	atomic.AddUint64(&w.appended, 1)
	w.mu.Unlock()
	w.ringMu.Lock()
	w.ring[w.pos] = envelope
	w.pos++
	if w.pos >= len(w.ring) {
		w.pos = 0
		w.full = true
	}
	w.ringMu.Unlock()
	return true
}

func (w *Writer) ReadRecent(n int) []contract.Envelope {
	if n <= 0 {
		return nil
	}
	w.ringMu.RLock()
	defer w.ringMu.RUnlock()
	capacity := len(w.ring)
	size := w.pos
	if w.full {
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
	out := make([]contract.Envelope, n)
	if w.full {
		start := (w.pos - size + capacity) % capacity
		for i := 0; i < n; i++ {
			out[i] = w.ring[(start+size-n+i)%capacity]
		}
	} else {
		copy(out, w.ring[w.pos-n:w.pos])
	}
	return out
}

func (w *Writer) Stats() Stats {
	return Stats{
		Appended: atomic.LoadUint64(&w.appended),
		Dropped:  atomic.LoadUint64(&w.dropped),
		Failures: atomic.LoadUint64(&w.failures),
	}
}

func (w *Writer) HealthSnapshot() (bool, string) {
	return w.health.snapshot()
}

func (w *Writer) ConfigSnapshot() Config { return w.config }

// Close is truthful: it serializes with submissions, atomically detaches and
// accounts for pending work, and prevents any worker that has not entered file
// I/O from doing so after the closing transition. The worker owns the one
// eventual file close.
func (w *Writer) Close() CloseResult {
	w.closeOnce.Do(func() {
		w.mu.Lock()
		w.closing = true
		w.closeErr = nil
		w.closeResult.PendingDropped = uint64(len(w.submitQueue))
		atomic.AddUint64(&w.dropped, w.closeResult.PendingDropped)
		clear(w.submitQueue)
		w.submitQueue = nil
		w.submitCond.Broadcast()
		w.mu.Unlock()
		select {
		case <-w.workerDone:
			w.closeResult.WorkerExited = true
		case <-time.After(w.closeWait):
			w.mu.Lock()
			w.closeResult.InFlight = w.inFlight
			w.mu.Unlock()
		}
	})
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closeResult
}
