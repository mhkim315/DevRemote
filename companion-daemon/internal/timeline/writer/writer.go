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
	"time"

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

// Subscriber observes successfully appended envelopes. It is observational
// only; callbacks run asynchronously and cannot block or alter Append.
type Subscriber func(contract.Envelope)

// Writer serializes append records. It is deliberately not a store, queue,
// authority callback, or recovery state machine.
type Writer struct {
	mu           sync.Mutex
	file         appendFile
	closed       bool
	appended     uint64
	dropped      uint64
	failures     uint64
	subscribers  []Subscriber
	subscriberCh chan subscriberWork
	subDone      chan struct{} // closed when worker goroutine exits
	wg           sync.WaitGroup
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

func newWriter(file appendFile) *Writer {
	w := &Writer{file: file}
	w.startSubscriberLoop()
	return w
}

// Subscribe adds an observational callback. Nil callbacks are ignored.
// Subscribers must not block; slow subscribers are dropped silently.
func (w *Writer) Subscribe(fn Subscriber) {
	if fn == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.subscribers = append(w.subscribers, fn)
}

// subscriberWork is a bounded item delivered to the single dispatch goroutine.
type subscriberWork struct {
	fn       Subscriber
	envelope contract.Envelope
}

const subscriberTimeout = 2 * time.Second

// startSubscriberLoop runs a single dispatch worker. Each subscriber callback
// runs in its own goroutine with a 2s timeout; panics are recovered.
func (w *Writer) startSubscriberLoop() {
	ch := make(chan subscriberWork, 64)
	w.subscriberCh = ch
	w.subDone = make(chan struct{})
	go func() {
		for {
			select {
			case work, ok := <-ch:
				if !ok {
					close(w.subDone)
					return
				}
				w.runSubscriber(work)
			case <-w.subDone:
				return
			}
		}
	}()
}

func (w *Writer) runSubscriber(work subscriberWork) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		defer func() { recover() }()
		done := make(chan struct{}, 1)
		go func() {
			work.fn(work.envelope)
			done <- struct{}{}
		}()
		select {
		case <-done:
		case <-time.After(subscriberTimeout):
		}
	}()
}

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
	subscribers := append([]Subscriber(nil), w.subscribers...)
	// Send under lock so Close cannot close subscriberCh concurrently.
	// Non-blocking send drops on overflow. After Close, stopped=true and
	// sends are skipped (channel may be drained).
	for _, subscriber := range subscribers {
		if w.closed {
			break
		}
		select {
		case w.subscriberCh <- subscriberWork{fn: subscriber, envelope: envelope}:
		default:
			w.dropped++
		}
	}
	w.mu.Unlock()
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

// Close is idempotent. Signals the worker to stop accepting new work,
// waits for in-flight subscribers to complete, then closes the file.
func (w *Writer) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	if w.subscriberCh != nil {
		close(w.subscriberCh)
	}
	w.mu.Unlock()
	// Wait for dispatch worker and all in-flight subscribers.
	if w.subDone != nil {
		<-w.subDone
	}
	w.wg.Wait()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	return w.file.Close()
}
