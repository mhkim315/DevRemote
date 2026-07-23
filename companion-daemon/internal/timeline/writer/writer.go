// Package writer provides the explicitly constructed, fail-open shadow sink
// for Canonical Timeline envelopes. It owns no authority and starts no
// goroutines until a Writer is explicitly constructed.
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

const (
	DefaultFileMode    os.FileMode = 0o600
	subscriberQueueCap             = 64
	maxCallbacks                   = 8
	subscriberTimeout              = 2 * time.Second
	closeDeadline                  = 5 * time.Second
)

var ErrInvalidConfig = errors.New("timeline writer: invalid configuration")

type Config struct{ Path string }
type Stats struct{ Appended, Dropped, Failures uint64 }

type appendFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

// Subscriber observes a successful shadow append. It is not an authority
// callback and must not rely on delivery; queue pressure intentionally drops it.
type Subscriber func(contract.Envelope)

type subscriberEntry struct {
	id uint64
	fn Subscriber
}

// Writer serializes append records and owns one bounded observer dispatcher.
// Observer work is isolated from Append and cannot affect primary authority.
type Writer struct {
	mu          sync.Mutex
	file        appendFile
	closed      bool
	appended    uint64
	dropped     uint64
	failures    uint64
	subscribers []subscriberEntry
	nextSubID   uint64

	subscriberCh   chan contract.Envelope
	subscriberDone chan struct{}
	callbackSem    chan struct{}
	callbacks      sync.WaitGroup
	closeOnce      sync.Once
	closeErr       error
}

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
	w := &Writer{
		file: file, subscriberCh: make(chan contract.Envelope, subscriberQueueCap),
		subscriberDone: make(chan struct{}), callbackSem: make(chan struct{}, maxCallbacks),
	}
	go w.dispatchSubscribers()
	return w
}

// Subscribe adds an observer and returns a cleanup function. Subscription
// changes are serialized with queue closure, so an observer is never queued
// after its writer has closed.
func (w *Writer) Subscribe(fn Subscriber) func() {
	if fn == nil {
		return func() {}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return func() {}
	}
	w.nextSubID++
	id := w.nextSubID
	w.subscribers = append(w.subscribers, subscriberEntry{id: id, fn: fn})
	return func() { w.unsubscribe(id) }
}

func (w *Writer) unsubscribe(id uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i, subscriber := range w.subscribers {
		if subscriber.id == id {
			w.subscribers = append(w.subscribers[:i], w.subscribers[i+1:]...)
			return
		}
	}
}

func (w *Writer) dispatchSubscribers() {
	for envelope := range w.subscriberCh {
		w.mu.Lock()
		subscribers := append([]subscriberEntry(nil), w.subscribers...)
		w.mu.Unlock()
		for _, subscriber := range subscribers {
			w.callbackSem <- struct{}{}
			w.callbacks.Add(1)
			done := make(chan struct{})
			go w.invokeSubscriber(subscriber.fn, envelope, done)
			select {
			case <-done:
			case <-time.After(subscriberTimeout):
			}
		}
	}
	w.callbacks.Wait()
	close(w.subscriberDone)
}

func (w *Writer) invokeSubscriber(fn Subscriber, envelope contract.Envelope, done chan<- struct{}) {
	defer w.callbacks.Done()
	defer func() {
		<-w.callbackSem
		close(done)
		recover()
	}()
	fn(envelope)
}

// Append is best-effort and never returns an error to its caller. It validates
// and frames an envelope before taking the writer lock. Observer enqueue is
// one non-blocking send while holding only writer-owned state.
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
	select {
	case w.subscriberCh <- envelope:
	default: // observer pressure is fail-open and does not change append success
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

func (w *Writer) Stats() Stats {
	w.mu.Lock()
	defer w.mu.Unlock()
	return Stats{Appended: w.appended, Dropped: w.dropped, Failures: w.failures}
}

// Close rejects new appends, drains the bounded observer queue, and waits at
// most five seconds for callbacks. A non-cooperative observer is bounded by
// the semaphore and cannot delay daemon shutdown indefinitely.
func (w *Writer) Close() error {
	w.closeOnce.Do(func() {
		w.mu.Lock()
		w.closed = true
		close(w.subscriberCh)
		w.mu.Unlock()
		select {
		case <-w.subscriberDone:
		case <-time.After(closeDeadline):
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.file != nil {
			w.closeErr = w.file.Close()
		}
	})
	return w.closeErr
}
