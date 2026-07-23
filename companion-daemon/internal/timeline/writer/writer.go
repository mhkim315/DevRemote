// Package writer provides the explicitly constructed, fail-open shadow sink
// for Canonical Timeline envelopes. It owns no authority and starts exactly
// one I/O worker goroutine for non-blocking submission.
package writer

import (
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
	DefaultFileMode   os.FileMode = 0o600
	recentEnvelopes               = 128
	submitBufCap                  = 256
	closeDrainTimeout             = 5 * time.Second
)

var ErrInvalidConfig = errors.New("timeline writer: invalid configuration")

type Config struct {
	Path string
}

// ProducerHandle identifies one bound managed-runtime generation.
type ProducerHandle struct {
	Provider     string
	RuntimeID    string
	SessionID    string
	Generation   int64
	Capabilities []string // permitted event kinds
}

type ProducerAuth interface {
	IsBound(h ProducerHandle) bool
}

type ProducerStore struct {
	mu     sync.RWMutex
	active map[string]ProducerHandle
}

func NewProducerStore() *ProducerStore {
	return &ProducerStore{active: make(map[string]ProducerHandle)}
}

func (s *ProducerStore) Bind(h ProducerHandle) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%s:%s:%d", h.Provider, h.SessionID, h.Generation)
	s.active[key] = h
}

func (s *ProducerStore) Revoke(provider, sessionID string, generation int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%s:%s:%d", provider, sessionID, generation)
	delete(s.active, key)
}

func (s *ProducerStore) IsBound(h ProducerHandle) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := fmt.Sprintf("%s:%s:%d", h.Provider, h.SessionID, h.Generation)
	_, ok := s.active[key]
	return ok
}

type appendFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

// Stats exposes non-blocking submission outcomes. All counters are monotonic.
type Stats struct {
	Appended uint64
	Dropped  uint64
	Failures uint64
}

// Health reports degradation state without self-persisting.
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
	handle   ProducerHandle
}

// Writer serializes append records through a non-blocking submission channel
// and a single I/O worker. Zero authority callbacks; no goroutine leaks.
type Writer struct {
	mu       sync.Mutex
	file     appendFile
	closed   bool
	appended uint64
	dropped  uint64
	failures uint64

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
	defer func() { recover() }()
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

	// Push to ring buffer for cockpit polling.
	w.ringMu.Lock()
	w.ring[w.pos] = work.envelope
	w.pos++
	if w.pos >= len(w.ring) {
		w.pos = 0
		w.full = true
	}
	w.ringMu.Unlock()
}

// SubmitAfterCommit is the fail-open, non-blocking submission path. Callers
// must call it AFTER their primary authority has committed. A false return
// means the envelope was dropped (channel full or closed). The caller never
// blocks on I/O.
func (w *Writer) SubmitAfterCommit(envelope contract.Envelope, handle ProducerHandle) bool {
	if w.auth != nil && !w.auth.IsBound(handle) {
		atomic.AddUint64(&w.dropped, 1)
		return false
	}
	if err := envelope.Validate(); err != nil {
		atomic.AddUint64(&w.dropped, 1)
		return false
	}
	select {
	case w.submitCh <- submitWork{envelope: envelope, handle: handle}:
		return true
	default:
		atomic.AddUint64(&w.dropped, 1)
		w.health.markDegraded("submission channel full")
		return false
	}
}

// Append is retained for backward compatibility (ring-buffer tests, cockpit).
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

func (w *Writer) HealthSnapshot() (degraded bool, reason string) {
	return w.health.snapshot()
}

func (w *Writer) ConfigSnapshot() Config { return w.config }

// Close signals the worker, drains pending items (best-effort), then closes the file.
func (w *Writer) Close() error {
	w.closeOnce.Do(func() {
		close(w.submitCh)
		done := make(chan struct{})
		go func() {
			w.workerWg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(closeDrainTimeout):
			atomic.AddUint64(&w.dropped, uint64(len(w.submitCh)))
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		w.closed = true
		if w.file != nil {
			w.closeErr = w.file.Close()
		}
	})
	return w.closeErr
}
