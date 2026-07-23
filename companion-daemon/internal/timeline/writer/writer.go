// Package writer provides the explicitly constructed, fail-open shadow sink
// for Canonical Timeline envelopes. It owns no authority and starts no
// goroutines. Readers poll through a bounded ring buffer.
package writer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"devremote/companion-daemon/internal/timeline/contract"
)

const (
	DefaultFileMode os.FileMode = 0o600
	recentEnvelopes             = 128
)

var ErrInvalidConfig = errors.New("timeline writer: invalid configuration")

// Config identifies an explicitly selected shadow file.
type Config struct{ Path string }

// Stats describes outcomes observed by this best-effort writer.
type Stats struct{ Appended, Dropped, Failures uint64 }

type appendFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

// Writer serializes append records and exposes a bounded ReadRecent ring buffer.
// It starts zero goroutines — callers poll ReadRecent on their own schedule.
type Writer struct {
	mu       sync.Mutex
	file     appendFile
	closed   bool
	appended uint64
	dropped  uint64
	failures uint64

	ring []contract.Envelope
	pos  int
	full bool
}

// Open constructs a writer for one explicit path.
func Open(config Config) (*Writer, error) {
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
	return newWriter(f), nil
}

func newWriter(file appendFile) *Writer {
	return &Writer{file: file, ring: make([]contract.Envelope, recentEnvelopes)}
}

// Append validates, frames, and writes one envelope. On success it pushes
// a copy into the ring buffer for ReadRecent consumers.
func (w *Writer) Append(envelope contract.Envelope) bool {
	if err := envelope.Validate(); err != nil {
		w.recordDrop(true)
		return false
	}
	record, err := json.Marshal(envelope)
	if err != nil {
		w.recordDrop(true)
		return false
	}
	record = append(record, '\n')

	w.mu.Lock()
	if w.closed || w.file == nil {
		w.dropped++
		w.mu.Unlock()
		return false
	}
	n, err := w.file.Write(record)
	if err != nil || n != len(record) {
		w.dropped++
		w.failures++
		w.mu.Unlock()
		return false
	}
	if err := w.file.Sync(); err != nil {
		w.dropped++
		w.failures++
		w.mu.Unlock()
		return false
	}
	w.appended++
	w.ring[w.pos] = envelope
	w.pos++
	if w.pos >= len(w.ring) {
		w.pos = 0
		w.full = true
	}
	w.mu.Unlock()
	return true
}

// ReadRecent returns the most recent N envelopes in insertion order.
// N is clamped to the ring buffer size; an empty or 0 request returns nil.
func (w *Writer) ReadRecent(n int) []contract.Envelope {
	if n <= 0 {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	capacity := len(w.ring)
	size := w.pos
	if !w.full {
		size = w.pos
	} else {
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
			idx := (start + size - n + i) % capacity
			out[i] = w.ring[idx]
		}
	} else {
		copy(out, w.ring[w.pos-n:w.pos])
	}
	return out
}

func (w *Writer) recordDrop(failure bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.dropped++
	if failure {
		w.failures++
	}
}

func (w *Writer) Stats() Stats {
	w.mu.Lock()
	defer w.mu.Unlock()
	return Stats{Appended: w.appended, Dropped: w.dropped, Failures: w.failures}
}

// Close is idempotent. No goroutines to drain — just closes the file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.file == nil {
		return nil
	}
	return w.file.Close()
}
