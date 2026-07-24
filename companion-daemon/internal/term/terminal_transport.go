package term

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
)

var errInputAuthorization = errors.New("terminal input authorization rejected")

// ErrInputNotOwner is returned when a non-owner device attempts to write
// input. The caller should surface the current owner identity to the client.
var ErrInputNotOwner = errors.New("terminal input not owner")

// InputOwnerInfo describes the current input owner for a terminal session.
// DeviceID is empty when there is no owner (unowned). IsSelf is always false
// in the server-side struct; callers set it per-connection.
type InputOwnerInfo struct {
	DeviceID  string `json:"deviceId,omitempty"`
	ConnID    string `json:"-"`
	IsSelf    bool   `json:"isSelf"`
	Since     int64  `json:"since,omitempty"` // unix nanos
}

// DefaultInputOwnerTimeout is the inactivity duration after which input
// ownership expires and the session returns to unowned state.
const DefaultInputOwnerTimeout = 30 * time.Second

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

	// R2: input ownership tracking. One writer at a time across all
	// connected clients (mobile WebView, local IPC subscriber).
	inputOwnerDeviceID   string
	inputOwnerConnID     string
	inputOwnerLastActive time.Time
	ownerTimeout         time.Duration

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
		sessionID:    sessionID,
		generation:   gen,
		writer:       writer,
		resizer:      resizer,
		recorder:     rec,
		authorizer:   authorizer,
		ownerTimeout: DefaultInputOwnerTimeout,
	}
}

// setOwnerTimeout overrides the default ownership expiry. Only for tests.
func (t *TerminalTransport) setOwnerTimeout(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ownerTimeout = d
}

// Retire marks the transport handle as retired: WriteInput becomes a no-op,
// Resize is a no-op, subscriber fan-out is denied, and input ownership is
// released. Called when the generation is superseded.
func (t *TerminalTransport) Retire() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.retired = true
	t.writer = nil
	t.resizer = nil
	t.recorder = nil
	t.inputOwnerDeviceID = ""
	t.inputOwnerConnID = ""
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
		t.inputOwnerDeviceID = ""
		t.inputOwnerConnID = ""
	}
}

// R2: ClaimInput attempts to claim input ownership for a device+connection.
// Returns the current owner info after the attempt. If the session is
// unowned, the caller becomes owner. If the caller is already owner, the
// last-active time is refreshed. If another device owns input, the claim
// is denied (explicit transfer requires the claim-input endpoint).
func (t *TerminalTransport) ClaimInput(deviceID, connID string) (*InputOwnerInfo, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.retired {
		return nil, false
	}

	now := time.Now()

	// Check expiry: if current owner has timed out, clear ownership.
	if t.inputOwnerDeviceID != "" && t.inputOwnerDeviceID != deviceID {
		if now.Sub(t.inputOwnerLastActive) > t.ownerTimeout {
			t.inputOwnerDeviceID = ""
			t.inputOwnerConnID = ""
		}
	}

	// Unowned → first writer claims ownership.
	if t.inputOwnerDeviceID == "" {
		t.inputOwnerDeviceID = deviceID
		t.inputOwnerConnID = connID
		t.inputOwnerLastActive = now
		return &InputOwnerInfo{DeviceID: deviceID, ConnID: connID, Since: now.UnixNano(), IsSelf: true}, true
	}

	// Already owned by this device → refresh.
	if t.inputOwnerDeviceID == deviceID {
		t.inputOwnerLastActive = now
		return &InputOwnerInfo{DeviceID: deviceID, ConnID: t.inputOwnerConnID, Since: t.inputOwnerLastActive.UnixNano(), IsSelf: true}, true
	}

	// Owned by another device → denied.
	return &InputOwnerInfo{DeviceID: t.inputOwnerDeviceID, ConnID: t.inputOwnerConnID, Since: t.inputOwnerLastActive.UnixNano(), IsSelf: false}, false
}

// ReleaseInput releases input ownership if the given connection ID matches
// the current owner. Called on WebSocket close / disconnect.
func (t *TerminalTransport) ReleaseInput(connID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inputOwnerConnID == connID {
		t.inputOwnerDeviceID = ""
		t.inputOwnerConnID = ""
	}
}

// CheckWriteAccess reports whether a device has write access to the PTY.
// Returns the current owner info. granted is true when the device is the
// owner or when the session is unowned (first-write-wins handled by
// ClaimInput inside WriteInput).
func (t *TerminalTransport) CheckWriteAccess(deviceID string) (granted bool, owner *InputOwnerInfo) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.retired {
		return false, nil
	}

	// Unowned — first write will claim.
	if t.inputOwnerDeviceID == "" {
		return true, nil
	}

	// Expired ownership.
	if t.inputOwnerDeviceID != deviceID && time.Since(t.inputOwnerLastActive) > t.ownerTimeout {
		return true, nil
	}

	owner = &InputOwnerInfo{
		DeviceID: t.inputOwnerDeviceID,
		ConnID:   t.inputOwnerConnID,
		Since:    t.inputOwnerLastActive.UnixNano(),
		IsSelf:   t.inputOwnerDeviceID == deviceID,
	}
	return t.inputOwnerDeviceID == deviceID, owner
}

// InputOwner returns the current input owner info, or nil if unowned/retired.
// The IsSelf field is always false; callers set it per-connection.
func (t *TerminalTransport) InputOwner() *InputOwnerInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.retired || t.inputOwnerDeviceID == "" {
		return nil
	}

	// Check expiry.
	if time.Since(t.inputOwnerLastActive) > t.ownerTimeout {
		return nil
	}

	return &InputOwnerInfo{
		DeviceID: t.inputOwnerDeviceID,
		ConnID:   t.inputOwnerConnID,
		Since:    t.inputOwnerLastActive.UnixNano(),
	}
}

// TransferInput transfers input ownership from the current owner to a new
// device+connection. Returns the new owner info, or an error if the
// requesting device is not authorized to transfer (must be current owner
// or there must be no owner).
func (t *TerminalTransport) TransferInput(fromDeviceID, toDeviceID, toConnID string) (*InputOwnerInfo, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.retired {
		return nil, ErrInputNotOwner
	}

	now := time.Now()

	// Check expiry of current owner.
	if t.inputOwnerDeviceID != "" && t.inputOwnerDeviceID != fromDeviceID {
		if now.Sub(t.inputOwnerLastActive) > t.ownerTimeout {
			t.inputOwnerDeviceID = ""
			t.inputOwnerConnID = ""
		}
	}

	// If there's an owner and it's not the fromDevice, reject.
	if t.inputOwnerDeviceID != "" && t.inputOwnerDeviceID != fromDeviceID {
		return &InputOwnerInfo{DeviceID: t.inputOwnerDeviceID, ConnID: t.inputOwnerConnID, Since: t.inputOwnerLastActive.UnixNano(), IsSelf: false}, ErrInputNotOwner
	}

	// Transfer or initial claim.
	t.inputOwnerDeviceID = toDeviceID
	t.inputOwnerConnID = toConnID
	t.inputOwnerLastActive = now
	return &InputOwnerInfo{DeviceID: toDeviceID, ConnID: toConnID, Since: now.UnixNano(), IsSelf: true}, nil
}

// WriteInput writes keystrokes/input to the owned PTY, gated by the
// generation guard. A retired handle silently discards input (fail-closed).
// Authorization and transport capture are the commit boundary. The PTY write
// occurs after releasing the transport lock so no internal lock spans external
// I/O; a retirement racing after capture is ordered after this commit.
//
// R2: WriteInput enforces the one-writer policy. The first write from any
// device claims ownership (first-write-wins). Subsequent writes from a
// non-owner device are rejected with ErrInputNotOwner.
func (t *TerminalTransport) WriteInput(data []byte, deviceID string, deviceEpoch uint64) (int, error) {
	// Check retired state first — retiree silently drops input (fail-closed),
	// same behavior as before R2 ownership tracking.
	t.mu.RLock()
	w := t.writer
	authorizer := t.authorizer
	retired := t.retired
	t.mu.RUnlock()

	if retired || w == nil {
		return 0, nil // retired — fail closed
	}

	// R2: Claim or verify input ownership before authorization.
	// First-write-wins: the first device to write after session creation
	// or ownership expiry becomes the input owner.
	owner, claimed := t.ClaimInput(deviceID, "")
	if !claimed {
		if owner != nil && owner.DeviceID != "" {
			return 0, fmt.Errorf("%w: input owned by %s", ErrInputNotOwner, owner.DeviceID)
		}
		return 0, fmt.Errorf("%w: transport retired or unavailable", ErrInputNotOwner)
	}

	if authorizer == nil {
		return 0, devicetrust.ErrNoAuthority
	}
	if err := authorizer.AuthorizeAndCommit(deviceID, deviceEpoch, devicetrust.IntentWSInput, func() error { return nil }); err != nil {
		return 0, fmt.Errorf("%w: %v", errInputAuthorization, err)
	}
	return w.Write(data)
}

// WriteInputAsOwner writes input to the PTY without claiming ownership.
// The caller must already be the verified input owner. Used by the
// IPC subscriber path where ownership was verified on connection.
func (t *TerminalTransport) WriteInputAsOwner(data []byte, deviceID string, deviceEpoch uint64) (int, error) {
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
	if err := t.authorizer.AuthorizeAndCommit(deviceID, deviceEpoch, devicetrust.IntentWSInput, func() error { return nil }); err != nil {
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
		if err := authorizer.AuthorizeAndCommit(deviceID, deviceEpoch, devicetrust.IntentPTYResize, func() error { return nil }); err != nil {
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
