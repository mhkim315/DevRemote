// Package writer provides the explicitly constructed, fail-open shadow sink
// for Canonical Timeline envelopes. It owns no authority and starts no
// goroutines; callers must invoke it only after their primary authority has
// committed.
package writer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"devremote/companion-daemon/internal/timeline/contract"
)

const DefaultFileMode os.FileMode = 0o600

var ErrInvalidConfig = errors.New("timeline writer: invalid configuration")

// Config identifies an explicitly selected shadow file. There is no package
// default path, no global singleton, and no package initialization side effect.
type Config struct {
	Path string
}

// Stats describes outcomes observed by this best-effort writer. A drop or
// failure makes an equivalence run invalid; it never becomes a daemon error.
type Stats struct {
	Appended uint64
	Dropped  uint64
	Failures uint64
}

type appendFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

// Writer serializes append records. It is deliberately not a store, queue,
// authority callback, or recovery state machine.
type Writer struct {
	mu       sync.Mutex
	file     appendFile
	closed   bool
	appended uint64
	dropped  uint64
	failures uint64
}

// Open constructs a writer for one explicit path. Construction errors are
// returned so the composition root can log and disable this optional sink.
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

func newWriter(file appendFile) *Writer { return &Writer{file: file} }

// Append is best-effort and never returns an error to its caller. It validates
// and frames an envelope before taking the writer lock, then performs one
// append and sync while holding only writer-owned state. It must never be
// called while an authority lock is held.
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
	defer w.mu.Unlock()
	if w.closed || w.file == nil {
		w.dropped++
		return false
	}
	n, err := w.file.Write(record)
	if err != nil || n != len(record) {
		w.dropped++
		w.failures++
		return false
	}
	if err := w.file.Sync(); err != nil {
		w.dropped++
		w.failures++
		return false
	}
	w.appended++
	return true
}

func (w *Writer) recordDrop(failure bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.dropped++
	if failure {
		w.failures++
	}
}

// Stats returns an atomic snapshot under the writer-owned lock.
func (w *Writer) Stats() Stats {
	w.mu.Lock()
	defer w.mu.Unlock()
	return Stats{Appended: w.appended, Dropped: w.dropped, Failures: w.failures}
}

// Close is idempotent. A close failure is observable by the composition root,
// but cannot change any primary authority outcome.
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
