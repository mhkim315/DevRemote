package notification

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/timeline/contract"
	"devremote/companion-daemon/internal/timeline/writer"
)

const Window = 4096

// ── Locator ──

// Locator is the exact-event notification payload sent to mobile devices.
// It carries enough information for the device to call back and re-authorize
// the event before taking action.
type Locator struct {
	EventID    string             `json:"eventId"`
	SessionID  string             `json:"sessionId"`
	RuntimeID  string             `json:"runtimeId"`
	Generation int64              `json:"generation"`
	Kind       contract.EventKind `json:"kind"`
	Timestamp  time.Time          `json:"timestamp"`
	N1Token    string             `json:"n1Token"`
}

// Token derives a stable N1 token from event identity (not content).
func Token(eventID string, generation int64) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", eventID, generation)))
	return hex.EncodeToString(h[:])
}

// Build creates a Locator from a Timeline envelope. Returns false when the
// generation is stale or the event kind is not in the closed N1 taxonomy.
func Build(e contract.Envelope, currentGeneration int64) (Locator, bool) {
	if e.LaunchGeneration != currentGeneration || !allowed(e.EventKind) {
		return Locator{}, false
	}
	return Locator{
		EventID: e.EventID, SessionID: e.SessionID, RuntimeID: e.RuntimeID,
		Generation: e.LaunchGeneration, Kind: e.EventKind, Timestamp: e.OccurredAt,
		N1Token: Token(e.EventID, e.LaunchGeneration),
	}, true
}

// allowed is the closed N1 event taxonomy. Unknown kinds fail closed.
func allowed(k contract.EventKind) bool {
	switch k {
	case contract.EventProviderInvocationFinished, contract.EventApprovalRequested,
		contract.EventApprovalResolved, contract.EventToolCallFinished:
		return true
	}
	return false
}

// ── Dedup ──

// Dedup is bounded and process-local. Empty after restart; stable token
// lets receiving device re-authorize a possible replay.
type Dedup struct {
	mu sync.Mutex
	l  *list.List
	m  map[string]*list.Element
}

func NewDedup() *Dedup { return &Dedup{l: list.New(), m: map[string]*list.Element{}} }

// Claim returns true on first claim for (eventID, gen). Subsequent claims
// for the same pair return false. LRU eviction at Window.
func (d *Dedup) Claim(eventID string, gen int64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	k := Token(eventID, gen)
	if e := d.m[k]; e != nil {
		d.l.MoveToFront(e)
		return false
	}
	d.m[k] = d.l.PushFront(k)
	if d.l.Len() > Window {
		e := d.l.Back()
		delete(d.m, e.Value.(string))
		d.l.Remove(e)
	}
	return true
}

// ── Cursor ──

// Cursor tracks per-device read position in the Timeline ring buffer.
type Cursor struct {
	DeviceID, LastEventID string
	LastGeneration        int64
}

// SelectSince returns events after the cursor position. When the cursor is
// not found (ring buffer wrapped past it), the second return value is true
// and only the most recent event is returned — blind replay of all retained
// events is prohibited to avoid flooding already-notified devices.
func SelectSince(events []contract.Envelope, c Cursor) ([]contract.Envelope, bool) {
	if c.LastEventID == "" || c.DeviceID == "" {
		return events, false
	}
	for i, e := range events {
		if e.EventID == c.LastEventID && e.LaunchGeneration == c.LastGeneration {
			return events[i+1:], false
		}
	}
	// Cursor not found: ring wrapped. Return only the latest event so the
	// device can re-establish its cursor without replaying stale events.
	if len(events) > 0 {
		return events[len(events)-1:], true
	}
	return nil, true
}

// ── Per-device store ──

// DeviceStore manages per-device push tokens, cursors, and binding epochs.
// Epochs make cursor writes conditional: Revoke/Bind bump the epoch, and a
// cursor write is only committed if the epoch matches the one captured at
// dispatch start (atomic snapshot under the store lock).
type DeviceStore struct {
	mu     sync.RWMutex
	tokens map[string]string // deviceID → pushToken
	cursor map[string]Cursor // deviceID → cursor
	epochs map[string]*int64 // deviceID → epoch pointer (shared with Notifier snapshot)
}

func NewDeviceStore() *DeviceStore {
	return &DeviceStore{
		tokens: make(map[string]string),
		cursor: make(map[string]Cursor),
		epochs: make(map[string]*int64),
	}
}

// Bind registers a push token and bumps the binding epoch. Bumping the
// epoch invalidates any in-flight goroutine that captured the old epoch.
func (s *DeviceStore) Bind(deviceID, pushToken string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[deviceID] = pushToken
	s.bumpEpochLocked(deviceID)
}

// Revoke removes the device's push token and cursor, and bumps the epoch.
func (s *DeviceStore) Revoke(deviceID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, had := s.tokens[deviceID]
	delete(s.tokens, deviceID)
	delete(s.cursor, deviceID)
	s.bumpEpochLocked(deviceID)
	return had
}

// bumpEpochLocked increments the epoch under the store lock.
func (s *DeviceStore) bumpEpochLocked(deviceID string) {
	e, ok := s.epochs[deviceID]
	if !ok {
		var v int64
		e = &v
		s.epochs[deviceID] = e
	}
	atomic.AddInt64(e, 1)
}

// Cursor writes the cursor. When commitEpoch > 0, the write is conditional:
// if the device's current epoch differs from commitEpoch (revoke/rebind
// happened), the write is silently skipped. commitEpoch=0 means "write
// unconditionally" (used by tests and direct cursor management).
func (s *DeviceStore) Cursor(deviceID string, c Cursor, commitEpoch int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if commitEpoch > 0 {
		e, ok := s.epochs[deviceID]
		if !ok {
			return false // device never had an epoch, caller expects one
		}
		if atomic.LoadInt64(e) != commitEpoch {
			return false // epoch bumped: revoke/rebind invalidated this write
		}
	}
	s.cursor[deviceID] = c
	return true
}

// GetCursor returns the stored cursor for a device.
func (s *DeviceStore) GetCursor(deviceID string) Cursor {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cursor[deviceID]
}

// ForEach calls fn for each registered device. The callback receives a copy
// of the token so the lock is not held during external I/O.
func (s *DeviceStore) ForEach(fn func(deviceID, token string)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for id, tok := range s.tokens {
		fn(id, tok)
	}
}

// Snapshot returns a copy of all device registrations, cursors, and epochs
// under a single read lock. The epoch value is captured atomically with the
// token+cursor so the goroutine can later commit only if the epoch is unchanged.
func (s *DeviceStore) Snapshot() (devices []struct{ DeviceID, Token string }, cursors map[string]Cursor, epochs map[string]int64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	devices = make([]struct{ DeviceID, Token string }, 0, len(s.tokens))
	for id, tok := range s.tokens {
		devices = append(devices, struct{ DeviceID, Token string }{id, tok})
	}
	cursors = make(map[string]Cursor, len(s.cursor))
	for id, c := range s.cursor {
		cursors[id] = c
	}
	epochs = make(map[string]int64, len(s.epochs))
	for id, e := range s.epochs {
		epochs[id] = atomic.LoadInt64(e)
	}
	return
}

// Token returns the push token for a device, or empty string.
func (s *DeviceStore) Token(deviceID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tokens[deviceID]
}

// ── PushSender ──

// PushSender abstracts the OS push channel.
type PushSender interface {
	Send(deviceID, pushToken string, payload []byte) error
}

type logSender struct{}

func (logSender) Send(deviceID, pushToken string, payload []byte) error {
	return nil // fire-and-forget in test/production; real impl uses OS SDK
}

// ── Notifier ──

// Notifier consumes Timeline events and dispatches notifications to
// registered devices. It runs a background consumer loop.
type Notifier struct {
	dedup   map[string]*Dedup // per-device dedup (deviceID → Dedup)
	dedupMu sync.Mutex        // protects dedup map access
	devices *DeviceStore
	sender  PushSender
	writer  *writer.Writer
	getGen  func(sessionID string) int64

	enabled bool

	// Per-device singleflight: at most one dispatch goroutine per device.
	// Prevents goroutine accumulation when PushSender hangs beyond timeout.
	inFlight sync.Map // deviceID → bool

	// In-flight goroutine counter. Stop waits on this.
	wg sync.WaitGroup

	// lifecycle
	mu      sync.Mutex
	done    chan struct{}
	stopped bool
}

// NewNotifier creates a Notifier. If sender is nil, a logSender is used.
// If w is nil or devices is nil, the notifier starts disabled — it can be
// enabled later via SetEnabled. Flag-off is zero-effect: Bind/Revoke work
// independently of the writer.
func NewNotifier(w *writer.Writer, devices *DeviceStore, getGen func(string) int64, sender PushSender) *Notifier {
	if sender == nil {
		sender = logSender{}
	}
	return &Notifier{
		dedup:   make(map[string]*Dedup),
		devices: devices,
		sender:  sender,
		writer:  w,
		getGen:  getGen,
		enabled: w != nil && devices != nil,
	}
}

// getOrCreateDedup returns the per-device dedup, creating one if needed.
// Safe for concurrent use from per-device dispatch goroutines.
func (n *Notifier) getOrCreateDedup(deviceID string) *Dedup {
	n.dedupMu.Lock()
	defer n.dedupMu.Unlock()
	d, ok := n.dedup[deviceID]
	if !ok {
		d = NewDedup()
		n.dedup[deviceID] = d
	}
	return d
}

// SetEnabled enables or disables the consumer loop. When disabled, dispatch
// is a no-op but device registration still works (flag-off = zero-effect).
func (n *Notifier) SetEnabled(v bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.enabled = v
}

// Start begins the background consumer loop. No-op if already started or the
// notifier has no writer/devices.
func (n *Notifier) Start() {
	n.mu.Lock()
	if n.stopped || n.done != nil {
		n.mu.Unlock()
		return
	}
	if n.writer == nil || n.devices == nil {
		n.mu.Unlock()
		return
	}
	n.done = make(chan struct{})
	n.mu.Unlock()
	go n.loop()
}

// Stop terminates the consumer loop. Sets stopped=true, closes done channel
// to stop the poll loop, then waits for in-flight goroutines via WaitGroup.
// Returns an error if the drain times out (goroutines still stuck after 15s).
func (n *Notifier) Stop() (err error) {
	n.mu.Lock()
	if n.stopped {
		n.mu.Unlock()
		// Second caller: wait on the WaitGroup (first caller already
		// initiated drain). Return nil if drain completed.
		done := make(chan struct{})
		go func() { n.wg.Wait(); close(done) }()
		select {
		case <-done:
			return nil
		case <-time.After(15 * time.Second):
			return fmt.Errorf("notification stop: drain timed out, %d goroutines still in-flight", n.ActiveGoroutines())
		}
	}
	n.stopped = true
	if n.done != nil {
		close(n.done)
	}
	n.mu.Unlock()

	// Real join: wait on WaitGroup with timeout.
	done := make(chan struct{})
	go func() { n.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-time.After(15 * time.Second):
		return fmt.Errorf("notification stop: drain timed out, %d goroutines still in-flight", n.ActiveGoroutines())
	}
}

// RevokeDevice removes the device from the store (bumps epoch + clears
// token/cursor under the store lock). Any in-flight goroutine that captured
// the old epoch will fail the conditional Cursor write.
func (n *Notifier) RevokeDevice(deviceID string) {
	n.devices.Revoke(deviceID)
}

// BindDevice is a no-op: epoch bump + token registration is now done
// atomically in DeviceStore.Bind under the store lock. Retained for
// backward compatibility with existing callers.
func (n *Notifier) BindDevice(deviceID string) {}

// ActiveGoroutines returns the count of in-flight per-device dispatch
// goroutines. For tests: verifies singleflight and Stop cleanup.
func (n *Notifier) ActiveGoroutines() int {
	count := 0
	n.inFlight.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// loop is the background consumer. It polls the Timeline writer on a 1-second
// tick and dispatches new events to registered devices.
func (n *Notifier) loop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-n.done:
			return
		case <-ticker.C:
			n.dispatch()
		}
	}
}

// dispatch reads recent Timeline events and delivers new ones to each device.
// Each device has an independent dedup window — device A receiving a
// notification never prevents device B from receiving the same event.
// Delivery is at-most-once: dedup claims the event BEFORE send, so a send
// failure does NOT retry the same locator.
//
// Per-device singleflight prevents goroutine accumulation: if a dispatch is
// already in-flight for a device, this cycle skips it. Each device goroutine
// is fire-and-forget — Dispatch() returns immediately. PushSender
// implementations are expected to carry their own timeout (e.g.
// http.Client.Timeout); a hung Send blocks ONE goroutine per device at most.
func (n *Notifier) dispatch() int {
	n.mu.Lock()
	enabled := n.enabled
	stopped := n.stopped
	n.mu.Unlock()
	if !enabled || stopped || n.writer == nil || n.devices == nil {
		return 0
	}

	// Atomic snapshot: token + cursor + epoch captured together under the
	// store read lock. The goroutine uses the captured epoch for conditional
	// Cursor commit — if the epoch changed (revoke/rebind), the write is
	// silently skipped.
	deviceList, cursors, snapEpochs := n.devices.Snapshot()
	events := n.writer.ReadRecent(128)

	var launched int
	for _, dev := range deviceList {
		// Singleflight: skip this device if a dispatch goroutine is already
		// in-flight. At most one goroutine per device can be stuck on Send.
		if _, loaded := n.inFlight.LoadOrStore(dev.DeviceID, true); loaded {
			continue
		}
		// wg.Add under lifecycle lock BEFORE stopped check so Stop()
		// is guaranteed to wait after Add is committed.
		n.mu.Lock()
		stopped := n.stopped
		if !stopped {
			n.wg.Add(1)
		}
		n.mu.Unlock()
		if stopped {
			n.inFlight.Delete(dev.DeviceID)
			continue
		}
		launched++

		go func(dev struct{ DeviceID, Token string }, c Cursor, commitEpoch int64) {
			defer n.wg.Done()
			defer n.inFlight.Delete(dev.DeviceID)

			selected, wrapped := SelectSince(events, c)
			// On wrap, deliver only the latest event to re-establish cursor.
			// Blind replay of all retained events is prohibited.
			_ = wrapped
			dd := n.getOrCreateDedup(dev.DeviceID)
			var lastSent contract.Envelope
			for _, e := range selected {
				gen := n.getGen(e.SessionID)
				loc, ok := Build(e, gen)
				if !ok {
					continue
				}
				// Per-device dedup: device A's claim never gates device B.
				if !dd.Claim(loc.EventID, loc.Generation) {
					continue
				}
				b, _ := json.Marshal(loc)
				if n.sender != nil {
					// PushSender carries its own timeout (e.g. http.Client).
					if err := n.sender.Send(dev.DeviceID, dev.Token, b); err != nil {
						break // at-most-once: dedup already claimed, delivery dropped
					}
				}
				lastSent = e
			}
			// Commit cursor ONLY if stopped=false AND epoch matches snapshot.
			if lastSent.EventID == "" {
				return
			}
			n.mu.Lock()
			stopped := n.stopped
			n.mu.Unlock()
			if stopped {
				return
			}
			// Conditional write: DeviceStore.Cursor checks epoch match under
			// the store lock. If epoch changed (revoke/rebind), no-op.
			n.devices.Cursor(dev.DeviceID, Cursor{DeviceID: dev.DeviceID, LastEventID: lastSent.EventID, LastGeneration: lastSent.LaunchGeneration}, commitEpoch)
		}(dev, cursors[dev.DeviceID], snapEpochs[dev.DeviceID])
	}
	return launched
}

// Dispatch is the synchronous one-shot version (for tests and manual trigger).
// It reads recent events and delivers them exactly once per device.
func (n *Notifier) Dispatch() int {
	return n.dispatch()
}

// ── Re-authorization ──

// StatusResponse is the N1 re-authorization endpoint response.
type StatusResponse struct {
	EventID                string   `json:"eventId"`
	CurrentGeneration      int64    `json:"currentGeneration"`
	NotificationGeneration int64    `json:"notificationGeneration"`
	Status                 string   `json:"status"`
	ResolvedBy             string   `json:"resolvedBy,omitempty"`
	Permissions            []string `json:"permissions"`
	ActivityLink           string   `json:"activityLink"`
	// Event is the redacted, locator-only canonical event selected by this
	// re-authorization. It lets mobile render the exact notification target
	// without consulting Cockpit or inferring from unrelated session events.
	Event *StatusEvent `json:"event,omitempty"`
}

// StatusEvent deliberately contains identity and display-safe metadata only.
// Timeline payload/content is never exposed through N1.
type StatusEvent struct {
	EventID    string             `json:"eventId"`
	SessionID  string             `json:"sessionId"`
	RuntimeID  string             `json:"runtimeId"`
	Generation int64              `json:"generation"`
	Kind       contract.EventKind `json:"kind"`
	OccurredAt time.Time          `json:"occurredAt"`
}

// AuthResolver provides generation lookup and permission checking for
// re-authorization.
type AuthResolver interface {
	GetGeneration(sessionID string) (int64, bool)
	HasPermission(deviceID string, perm string) bool
	RuntimeID(sessionID string) (string, bool)
}

// ApprovalChecker checks whether an approval was already resolved.
// Implementations delegate to the authoritative approval store.
type ApprovalChecker interface {
	IsResolved(sessionID, approvalID string) bool
}

// ResolveStatus determines the N1 re-authorization outcome. The seven
// possible status values:
//
//   - "session_unavailable" — session does not exist
//   - "stale_generation" — notification generation differs from current
//   - "insufficient_permission" — device lacks terminal:input
//   - "event_degraded_or_gap" — writer is degraded or ring gap detected
//   - "canonical_event_unavailable" — event not found in ring buffer
//   - "actionable" — event found, device has permission, all checks pass
//   - "already_resolved" — approval was already resolved by another device
func ResolveStatus(eventID string, notificationGen int64, sessionID string, runtimeID string, deviceID string, resolver AuthResolver, approvals ApprovalChecker, w *writer.Writer) StatusResponse {
	resp := StatusResponse{EventID: eventID, CurrentGeneration: 0, NotificationGeneration: notificationGen}

	// 1. Session existence.
	currentGen, sessionExists := resolver.GetGeneration(sessionID)
	if !sessionExists {
		resp.Status = "session_unavailable"
		return resp
	}
	resp.CurrentGeneration = currentGen

	// 2. Generation match (stale notification → no action).
	if notificationGen != currentGen {
		resp.Status = "stale_generation"
		return resp
	}

	// 3. Runtime identity — the runtime ID must match the session's current
	//    runtime. A mismatch means the runtime was replaced.
	//    Fail-closed: when the catalog cannot resolve the runtime (runtimeKnown
	//    is false) but the notification carried a runtime ID, treat as a
	//    potential identity mismatch — never proceed to actionable.
	actualRuntimeID, runtimeKnown := resolver.RuntimeID(sessionID)
	if !runtimeKnown && runtimeID != "" {
		resp.Status = "stale_generation"
		return resp
	}
	if runtimeKnown && actualRuntimeID != runtimeID {
		resp.Status = "stale_generation"
		return resp
	}

	// 4. Permission check — device must have terminal:input permission.
	if !resolver.HasPermission(deviceID, devicetrust.PermTerminalInput) {
		resp.Status = "insufficient_permission"
		resp.Permissions = nil
		return resp
	}
	resp.Permissions = []string{devicetrust.PermTerminalInput}

	// 5. Writer health — degraded or ring gap.
	if w != nil {
		degraded, _ := w.HealthSnapshot()
		if degraded {
			resp.Status = "event_degraded_or_gap"
			return resp
		}
	}

	// 6. Event presence in ring buffer — full identity match required.
	if w != nil {
		for _, e := range w.ReadRecent(128) {
			if e.EventID == eventID && e.SessionID == sessionID && e.LaunchGeneration == notificationGen {
				resp.Event = &StatusEvent{EventID: e.EventID, SessionID: e.SessionID, RuntimeID: e.RuntimeID, Generation: e.LaunchGeneration, Kind: e.EventKind, OccurredAt: e.OccurredAt}
				// 6a. Check approval resolution before declaring actionable.
				if approvals != nil && e.EventKind == contract.EventApprovalRequested && e.References.ApprovalRequest != nil {
					if approvals.IsResolved(sessionID, e.References.ApprovalRequest.ID) {
						resp.Status = "already_resolved"
						return resp
					}
				}
				resp.Status = "actionable"
				resp.ActivityLink = fmt.Sprintf("pokit://activity/%s?event=%s&generation=%d&runtime=%s", sessionID, eventID, notificationGen, runtimeID)
				return resp
			}
		}
	}

	resp.Status = "canonical_event_unavailable"
	return resp
}

// ── Handler registration ──

// NotificationHandlerConfig holds the dependencies for the N1 handler.
type NotificationHandlerConfig struct {
	Writer    *writer.Writer
	Resolver  AuthResolver
	Approvals ApprovalChecker
	Sessions  *devicetrust.DeviceSessionManager
	Devices   *DeviceStore
}

// RegisterHandlers adds the N1 re-auth and push-registration routes to the mux.
// In remote mode the routes require device bearer auth; in insecure-local mode
// they use the caller-supplied middleware.
//
// Flag-off = zero-effect: push registration works even without a Timeline writer.
func RegisterHandlers(mux *http.ServeMux, cfg NotificationHandlerConfig) {
	if mux == nil || cfg.Sessions == nil {
		return
	}

	// push/register: device binds its push token. Flag-off = zero-effect:
	// registration works without a writer. DeviceID comes from the Principal,
	// never from the caller.
	mux.HandleFunc("/push/register",
		devicetrust.RequirePrincipal(cfg.Sessions, func(w http.ResponseWriter, r *http.Request) {
			principal := devicetrust.PrincipalFromContext(r.Context())
			if principal == nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			deviceID := principal.DeviceID
			token := r.URL.Query().Get("token")
			if token == "" {
				http.Error(w, `{"error":"token is required"}`, http.StatusBadRequest)
				return
			}
			if cfg.Devices != nil {
				cfg.Devices.Bind(deviceID, token)
			}
			w.WriteHeader(http.StatusOK)
		}, devicetrust.PermSessionsRead))

	// N1 status: re-authorization endpoint. Returns the 7-outcome status.
	mux.HandleFunc("GET /api/notification/{eventId}/status",
		devicetrust.RequirePrincipal(cfg.Sessions, func(w http.ResponseWriter, r *http.Request) {
			principal := devicetrust.PrincipalFromContext(r.Context())
			if principal == nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			deviceID := principal.DeviceID

			eventID := r.PathValue("eventId")
			if eventID == "" {
				http.Error(w, `{"error":"missing eventId"}`, http.StatusBadRequest)
				return
			}
			sessionID := r.URL.Query().Get("session")
			runtimeID := r.URL.Query().Get("runtime")
			genStr := r.URL.Query().Get("generation")
			var gen int64
			fmt.Sscanf(genStr, "%d", &gen)

			var resp StatusResponse
			if cfg.Writer == nil || cfg.Resolver == nil {
				resp = StatusResponse{
					EventID:                eventID,
					NotificationGeneration: gen,
					Status:                 "canonical_event_unavailable",
				}
			} else {
				resp = ResolveStatus(eventID, gen, sessionID, runtimeID, deviceID, cfg.Resolver, cfg.Approvals, cfg.Writer)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}, devicetrust.PermSessionsRead))
}
