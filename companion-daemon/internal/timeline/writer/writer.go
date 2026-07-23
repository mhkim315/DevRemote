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
	"sync"
	"sync/atomic"
	"time"

	"devremote/companion-daemon/internal/timeline/contract"
)

const (
	DefaultFileMode  os.FileMode = 0o600
	recentEnvelopes              = 128
	submitBufCap                 = 256
	closeDrainTimeout            = 5 * time.Second
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

func newProducerToken() ProducerToken {
	b := make([]byte, 16)
	rand.Read(b)
	return ProducerToken{id: hex.EncodeToString(b)}
}

// ProducerHandle identifies one bound managed-runtime generation. Fields
// are unexported — only the composition root can bind.
type producerHandle struct {
	provider   string
	runtimeID  string
	sessionID  string
	generation int64
	token      ProducerToken
}

// Capability is the interface exposed to managed runtimes for submission.
type Capability struct {
	h     producerHandle
	token ProducerToken
	writer *Writer
}

// Token returns the opaque token that must match the stored handle.
func (c Capability) Token() ProducerToken { return c.token }

// SubmitAfterCommit is the fail-open, non-blocking submission path. Callers
// must call it AFTER their primary authority has committed. Returns false
// on drop (channel full, unregistered, invalid envelope).
func (c Capability) SubmitAfterCommit(envelope contract.Envelope) bool {
	if c.writer == nil {
		return false
	}
	return c.writer.submit(envelope, c.h, c.token)
}

// ProducerStore is the concrete auth implementation. Only the composition
// root creates capabilities; managed runtimes receive opaque Capability values.
type ProducerStore struct {
	mu     sync.RWMutex
	active map[string]producerHandle
}

// NewProducerStore returns an empty producer store.
func NewProducerStore() *ProducerStore {
	return &ProducerStore{active: make(map[string]producerHandle)}
}

// Bind creates a capability for a managed runtime generation. The returned
// Capability carries an opaque token that must match at submission time.
func (s *ProducerStore) Bind(provider, runtimeID, sessionID string, generation int64) Capability {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%s:%s:%d", provider, sessionID, generation)
	tok := newProducerToken()
	h := producerHandle{
		provider: provider, runtimeID: runtimeID, sessionID: sessionID,
		generation: generation, token: tok,
	}
	s.active[key] = h
	return Capability{h: h, token: tok}
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
	key := fmt.Sprintf("%s:%s:%d", h.provider, h.sessionID, h.generation)
	stored, ok := s.active[key]
	return ok && stored.token == tok
}

// ProducerAuth is the minimal interface for capability checks.
type ProducerAuth interface {
	IsBound(h producerHandle, tok ProducerToken) bool
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
	WorkerExited  bool
	InFlight      int
	PendingDropped uint64
}

// Writer serializes append records through a non-blocking submission channel
// and a single I/O worker.
type Writer struct {
	mu        sync.Mutex
	file      appendFile
	closed    bool
	closing   bool
	appended  uint64
	dropped   uint64
	failures  uint64
	ring      []contract.Envelope
	pos       int
	full      bool
	ringMu    sync.RWMutex
	submitCh  chan submitWork
	workerWg  sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
	auth      ProducerAuth
	health    Health
	config    Config
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
		file:     file,
		ring:     make([]contract.Envelope, recentEnvelopes),
		submitCh: make(chan submitWork, submitBufCap),
		auth:     auth,
		config:   config,
	}
	w.startWorker()
	return w
}

func (w *Writer) startWorker() {
	w.workerWg.Add(1)
	go func() {
		defer w.workerWg.Done()
		for work := range w.submitCh {
			w.processSubmit(work)
		}
	}()
}

func (w *Writer) processSubmit(work submitWork) {
	defer func() {
		if r := recover(); r != nil {
			atomic.AddUint64(&w.dropped, 1)
			atomic.AddUint64(&w.failures, 1)
			w.health.markDegraded(fmt.Sprintf("worker panic: %v", r))
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

	w.mu.Lock()
	if w.closed || w.file == nil {
		atomic.AddUint64(&w.dropped, 1)
		w.mu.Unlock()
		return
	}
	n, err := w.file.Write(record)
	if err != nil || n != len(record) {
		atomic.AddUint64(&w.dropped, 1)
		atomic.AddUint64(&w.failures, 1)
		w.health.markDegraded(fmt.Sprintf("write failure: %v", err))
		w.mu.Unlock()
		return
	}
	if err := w.file.Sync(); err != nil {
		atomic.AddUint64(&w.dropped, 1)
		atomic.AddUint64(&w.failures, 1)
		w.health.markDegraded(fmt.Sprintf("sync failure: %v", err))
		w.mu.Unlock()
		return
	}
	atomic.AddUint64(&w.appended, 1)
	w.mu.Unlock()

	w.ringMu.Lock()
	w.ring[w.pos] = work.envelope
	w.pos++
	if w.pos >= len(w.ring) {
		w.pos = 0
		w.full = true
	}
	w.ringMu.Unlock()
}

// submit is the internal entry point gated by auth.
func (w *Writer) submit(envelope contract.Envelope, h producerHandle, tok ProducerToken) bool {
	if w.auth != nil && !w.auth.IsBound(h, tok) {
		atomic.AddUint64(&w.dropped, 1)
		return false
	}
	if err := envelope.Validate(); err != nil {
		atomic.AddUint64(&w.dropped, 1)
		return false
	}
	// Validate envelope identity against handle.
	if envelope.Provider != h.provider || envelope.SessionID != h.sessionID || envelope.LaunchGeneration != h.generation {
		atomic.AddUint64(&w.dropped, 1)
		return false
	}
	w.mu.Lock()
	if w.closing || w.closed {
		atomic.AddUint64(&w.dropped, 1)
		w.mu.Unlock()
		return false
	}
	w.mu.Unlock()
	select {
	case w.submitCh <- submitWork{envelope: envelope, handle: h, token: tok}:
		return true
	default:
		atomic.AddUint64(&w.dropped, 1)
		w.health.markDegraded("submission channel full")
		return false
	}
}

// Append is retained for backward compatibility (ring-buffer tests, cockpit).
// In the activation path, prefer Capability.SubmitAfterCommit.
func (w *Writer) Append(envelope contract.Envelope) bool {
	if err := envelope.Validate(); err != nil {
		atomic.AddUint64(&w.dropped, 1)
		atomic.AddUint64(&w.failures, 1)
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
	if w.closed || w.file == nil {
		atomic.AddUint64(&w.dropped, 1)
		w.mu.Unlock()
		return false
	}
	n, err := w.file.Write(record)
	if err != nil || n != len(record) {
		atomic.AddUint64(&w.dropped, 1)
		atomic.AddUint64(&w.failures, 1)
		w.health.markDegraded("write failure")
		w.mu.Unlock()
		return false
	}
	if err := w.file.Sync(); err != nil {
		atomic.AddUint64(&w.dropped, 1)
		atomic.AddUint64(&w.failures, 1)
		w.health.markDegraded("sync failure")
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
	if n <= 0 { return nil }
	w.ringMu.RLock()
	defer w.ringMu.RUnlock()
	capacity := len(w.ring)
	size := w.pos
	if w.full { size = capacity }
	if n > capacity { n = capacity }
	if n > size { n = size }
	if n == 0 { return nil }
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

// Close is truthful: it serializes with submissions via mutex, atomically
// drains pending items, reports outcome, and closes the file. No send-on-
// closed panic.
func (w *Writer) Close() CloseResult {
	var result CloseResult
	w.closeOnce.Do(func() {
		w.mu.Lock()
		w.closing = true
		close(w.submitCh)
		w.mu.Unlock()
		done := make(chan struct{})
		go func() {
			w.workerWg.Wait()
			close(done)
		}()
		select {
		case <-done:
			result.WorkerExited = true
		case <-time.After(closeDrainTimeout):
			result.PendingDropped = uint64(len(w.submitCh))
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		w.closed = true
		if w.file != nil {
			w.closeErr = w.file.Close()
		}
	})
	return result
}
