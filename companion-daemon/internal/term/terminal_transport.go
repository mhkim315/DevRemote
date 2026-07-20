package term

import (
	"io"
	"log"
	"sync"
)

// PA2d: TerminalTransport is the owned-PTY transport handle. It owns
// generation-bound WriteInput, Resize, bounded live replay, and
// subscriber fan-out (WebSocket + IPC). It exposes no raw Read or
// OpenStream — the Recorder remains the sole PTY reader.

// ptyStream is the local interface for a PTY byte stream (replaces
// mux.TerminalStream in the Recorder). It combines ReadWriteCloser with
// Resize.
type ptyStream interface {
	io.ReadWriteCloser
	Resize(rows, cols int) error
}

// TerminalTransport is a generation-bound, per-session transport handle.
// Each owned-PTY launch creates one; a replacement retires the old handle.
type TerminalTransport struct {
	sessionID  string
	generation int64

	mu       sync.RWMutex
	writer   io.Writer   // the underlying PTY (for WriteInput)
	resizer  interface { // Resize(int, int) error
		Resize(rows, cols int) error
	}
	recorder *Recorder // direct reference; nil if never set
	retired  bool      // set by Retire/RetireIfGeneration; checked by IsRetired
}

// newTerminalTransport builds the transport handle for a freshly-launched
// session. The session must satisfy io.Writer (PTY Write) and
// Resize(int,int) error. The recorder is an optional direct reference;
// when nil, SubscriberFanOut returns false (no subscriber capability).
func newTerminalTransport(sessionID string, gen int64, writer io.Writer, resizer interface{ Resize(int, int) error }, rec *Recorder) *TerminalTransport {
	return &TerminalTransport{
		sessionID:  sessionID,
		generation: gen,
		writer:     writer,
		resizer:    resizer,
		recorder:   rec,
	}
}

// Retire marks the transport handle as retired: WriteInput becomes a no-op,
// Resize is a no-op, subscriber fan-out is denied. Called when the
// generation is superseded.
func (t *TerminalTransport) Retire() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.retired = true
	t.writer = nil
	t.resizer = nil
	t.recorder = nil
}

// RetireIfGeneration retires the transport only if its generation matches.
// Instance-guarded: stale calls are no-ops.
func (t *TerminalTransport) RetireIfGeneration(gen int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.generation == gen {
		t.retired = true
		t.writer = nil
		t.resizer = nil
		t.recorder = nil
	}
}

// WriteInput writes keystrokes/input to the owned PTY, gated by the
// generation guard. A retired handle silently discards input (fail-closed).
func (t *TerminalTransport) WriteInput(data []byte) (int, error) {
	t.mu.RLock()
	w := t.writer
	t.mu.RUnlock()
	if w == nil {
		return 0, nil // retired — fail closed
	}
	return w.Write(data)
}

// Resize changes the PTY geometry, gated by the generation guard.
func (t *TerminalTransport) Resize(rows, cols int) error {
	t.mu.RLock()
	r := t.resizer
	t.mu.RUnlock()
	if r == nil {
		return nil // retired — fail closed
	}
	return r.Resize(rows, cols)
}

// SubscriberFanOut returns a bootstrap snapshot + live subscriber channel
// from the Recorder. The returned channel is the EXACT channel registered
// with the Recorder — it must be passed to rec.Unsubscribe(ch) to clean
// up the subscription. Returns nil, nil, false if no recorder is active.
// PA4-Final-R14: uses direct recorder reference (no global registry lookup).
func (t *TerminalTransport) SubscriberFanOut(sessionID string) (bootstrap []byte, ch chan []byte, ok bool) {
	// PA4.3: generation gate — retired transports cannot fan out.
	if t.IsRetired() {
		return nil, nil, false
	}
	// Verify sessionID matches this transport's owner.
	if sessionID != t.sessionID {
		return nil, nil, false
	}
	// PA4-Final-R14: direct recorder reference, not global registry lookup.
	t.mu.RLock()
	rec := t.recorder
	t.mu.RUnlock()
	if rec == nil {
		return nil, nil, false
	}
	bootstrap, ch = rec.SubscribeWithBootstrap()
	return bootstrap, ch, true
}

// IsRetired reports whether the transport handle has been superseded.
func (t *TerminalTransport) IsRetired() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.retired
}

// EnsureRecorderTransport starts a Recorder for the given session via an
// opener, replacing the mux.StreamOpener dependency. The opener is any
// value that can open a ptyStream.
func EnsureRecorderTransport(sessionID string, openStream func() (ptyStream, error)) (*Recorder, chan []byte) {
	stream, err := openStream()
	if err != nil {
		log.Printf("TerminalTransport: openStream failed for %s: %v", sessionID, err)
		return nil, nil
	}
	return StartRecorder(sessionID, stream)
}
