package term

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"devremote/companion-daemon/internal/devicetrust"
)

var errInputAuthorization = errors.New("terminal input authorization rejected")

// PA2d: TerminalTransport is the owned-PTY transport handle. It owns
// generation-bound WriteInput, Resize, bounded live replay, and
// subscriber fan-out (WebSocket + IPC). It exposes no raw Read or
// OpenStream — the Recorder remains the sole PTY reader.

// ptyStream is the local interface for a PTY byte stream (replaces
// the recorder's byte stream). It combines ReadWriteCloser with Resize.
type ptyStream interface {
	io.ReadWriteCloser
	Resize(rows, cols int) error
}

// TerminalTransport is a generation-bound, per-session transport handle.
// Each owned-PTY launch creates one; a replacement retires the old handle.
type TerminalTransport struct {
	sessionID  string
	generation int64

	mu      sync.RWMutex
	writer  io.Writer // the underlying PTY (for WriteInput)
	resizer interface {
		Resize(rows, cols int) error
	}
	recorder   *Recorder // direct reference; nil if never set
	authorizer devicetrust.MutationAuthorizer
	retired    bool // set by Retire/RetireIfGeneration; checked by IsRetired

	// subscriberFanOutHook is an optional test hook called inside the
	// RLock critical section after recorder capture and before unlock.
	// nil means no-op. Set directly from same-package tests only.
	subscriberFanOutHook func()
}

// newTerminalTransport builds the transport handle for a freshly-launched
// session. The session must satisfy io.Writer (PTY Write) and
// Resize(int,int) error. The recorder is an optional direct reference;
// when nil, SubscriberFanOut returns false (no subscriber capability).
func newTerminalTransport(sessionID string, gen int64, writer io.Writer, resizer interface{ Resize(int, int) error }, rec *Recorder, authorizer devicetrust.MutationAuthorizer) *TerminalTransport {
	return &TerminalTransport{
		sessionID:  sessionID,
		generation: gen,
		writer:     writer,
		resizer:    resizer,
		recorder:   rec,
		authorizer: authorizer,
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
// Authorization and transport capture are the commit boundary. The PTY write
// occurs after releasing the transport lock so no internal lock spans external
// I/O; a retirement racing after capture is ordered after this commit.
func (t *TerminalTransport) WriteInput(data []byte, deviceID string, deviceEpoch uint64) (int, error) {
	t.mu.RLock()
	w := t.writer
	if w == nil {
		t.mu.RUnlock()
		return 0, nil // retired — fail closed
	}
	if t.authorizer == nil {
		t.mu.RUnlock()
		return 0, devicetrust.ErrNoAuthority
	}
	if err := t.authorizer.AuthorizeCommit(deviceID, deviceEpoch, devicetrust.IntentWSInput); err != nil {
		t.mu.RUnlock()
		return 0, fmt.Errorf("%w: %v", errInputAuthorization, err)
	}
	t.mu.RUnlock()
	return w.Write(data)
}

// Resize changes the PTY geometry, gated by the generation guard.
func (t *TerminalTransport) Resize(rows, cols int, deviceID string, deviceEpoch uint64) error {
	t.mu.RLock()
	r := t.resizer
	authorizer := t.authorizer
	if r != nil && authorizer == nil {
		t.mu.RUnlock()
		return devicetrust.ErrNoAuthority
	}
	if r != nil {
		if err := authorizer.AuthorizeCommit(deviceID, deviceEpoch, devicetrust.IntentPTYResize); err != nil {
			t.mu.RUnlock()
			return err
		}
	}
	t.mu.RUnlock()
	if r == nil {
		return nil // retired — fail closed
	}
	return r.Resize(rows, cols)
}

// SubscriberFanOut returns a bootstrap snapshot, live subscriber channel, and
// the direct Recorder reference in a single atomic read. The returned channel
// is the EXACT channel registered with the Recorder — it must be passed to
// rec.Unsubscribe(ch) to clean up the subscription.
// PA4-Final-R17: single lock linearization point — retirement cannot interleave.
func (t *TerminalTransport) SubscriberFanOut(sessionID string) (bootstrap []byte, ch chan []byte, rec *Recorder, ok bool) {
	// Verify sessionID matches this transport's owner (no lock needed — immutable).
	if sessionID != t.sessionID {
		return nil, nil, nil, false
	}
	// Single critical section: check retired, capture recorder, subscribe.
	// Lock ordering: TerminalTransport.mu → Recorder.mu (no cycle exists).
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.retired {
		return nil, nil, nil, false
	}
	r := t.recorder
	if r == nil {
		return nil, nil, nil, false
	}
	bootstrap, ch = r.SubscribeWithBootstrap()
	if t.subscriberFanOutHook != nil {
		t.subscriberFanOutHook()
	}
	return bootstrap, ch, r, true
}

// Geom returns the PTY geometry from the transport's Recorder, gated by
// generation. Returns 0, 0, false if retired or no recorder.
func (t *TerminalTransport) Geom() (rows, cols int, ok bool) {
	if t.IsRetired() {
		return 0, 0, false
	}
	t.mu.RLock()
	rec := t.recorder
	t.mu.RUnlock()
	if rec == nil {
		return 0, 0, false
	}
	return rec.GetSize()
}

// IsRetired reports whether the transport handle has been superseded.
func (t *TerminalTransport) IsRetired() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.retired
}
