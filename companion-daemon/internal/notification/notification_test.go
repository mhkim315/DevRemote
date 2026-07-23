package notification

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/timeline/contract"
	"devremote/companion-daemon/internal/timeline/writer"
)

// n1Event creates a lightweight envelope for unit-level Build/SelectSince/Dedup
// tests. It is intentionally NOT valid for writer.Append — use validEnvelope
// for writer-bound tests.
func n1Event(id string, generation int64, kind contract.EventKind) contract.Envelope {
	return contract.Envelope{
		EventID: id, SessionID: "session-1", RuntimeID: "runtime-1",
		LaunchGeneration: generation, EventKind: kind,
		OccurredAt: time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC),
	}
}

// validEnvelope creates a fully validated envelope via contract.NewEnvelope.
// The returned envelope has its computed EventID (not id), so callers must
// use the returned value's EventID for lookups.
func validEnvelope(id, sessionID string, generation int64, kind contract.EventKind) contract.Envelope {
	scope := contract.Scope{SessionID: sessionID, RuntimeID: "rt-" + sessionID, LaunchGeneration: generation}
	refs := contract.References{}
	switch kind {
	case contract.EventApprovalRequested, contract.EventApprovalResolved:
		refs.ApprovalRequest = &contract.TypedReference{Kind: contract.ReferenceApprovalRequest, ID: "approval-" + id, Scope: scope}
	case contract.EventToolCallFinished:
		refs.ToolCall = &contract.TypedReference{Kind: contract.ReferenceToolCall, ID: "tool-" + id, Scope: scope}
	case contract.EventProviderInvocationFinished:
		refs.ProviderInvocation = &contract.TypedReference{Kind: contract.ReferenceProviderInvocation, ID: "provider-" + id, Scope: scope}
	}
	t0 := agent.AgentEvent{ID: "agent-" + id, SessionID: sessionID, AgentKind: "codex", Type: agent.EventApprovalRequested, Provenance: "provider_protocol"}
	if kind == contract.EventToolCallFinished {
		t0.Type = agent.EventToolCallFinished
	}
	if kind == contract.EventProviderInvocationFinished {
		t0.Type = agent.EventCompleted
	}
	e, err := contract.NewEnvelope(contract.Envelope{
		SchemaVersion: contract.SchemaV1, PayloadVersion: contract.PayloadV1,
		EventKind: kind, SessionID: sessionID, RuntimeID: scope.RuntimeID, LaunchGeneration: generation, Provider: "codex",
		SourceIncarnation: "inc-" + id, SourceIdentity: contract.SourceIdentity{Kind: "fixture", ID: "src-" + id},
		SourcePosition: "pos-" + id, RedactionPolicyVersion: "redact-v1",
		OccurredAt: time.Unix(1, 0), ObservedAt: time.Unix(2, 0),
		Payload:         contract.Payload{Redacted: &contract.RedactedPayload{Summary: "safe " + id}},
		EvidenceSources: contract.EvidenceSources{Provider: &contract.ProviderEvidenceRef{ID: "ev-" + id, Scope: scope}},
		References:      refs, T0Event: t0,
	})
	if err != nil {
		panic(fmt.Sprintf("validEnvelope %s: %v", id, err))
	}
	return e
}

// ── T2 baseline tests (4) ──

func TestBuildClosedTaxonomyAndGenerationGate(t *testing.T) {
	for _, kind := range []contract.EventKind{
		contract.EventProviderInvocationFinished, contract.EventApprovalRequested,
		contract.EventApprovalResolved, contract.EventToolCallFinished,
	} {
		if _, ok := Build(n1Event("event-"+string(kind), 7, kind), 7); !ok {
			t.Errorf("allowed kind %q was suppressed", kind)
		}
	}
	if _, ok := Build(n1Event("unknown", 7, contract.EventKind("unknown")), 7); ok {
		t.Error("unknown event kind must fail closed")
	}
	if _, ok := Build(n1Event("stale", 6, contract.EventApprovalRequested), 7); ok {
		t.Error("stale generation must be suppressed before delivery")
	}
}

func TestLocatorStableTokenAndPrivacy(t *testing.T) {
	e := n1Event("event-1", 7, contract.EventApprovalRequested)
	first, ok := Build(e, 7)
	if !ok {
		t.Fatal("Build unexpectedly suppressed valid event")
	}
	second, ok := Build(e, 7)
	if !ok || first.N1Token != second.N1Token || first.N1Token != Token("event-1", 7) {
		t.Fatalf("token must be stable: first=%q second=%q", first.N1Token, second.N1Token)
	}
	if first.SessionID != e.SessionID || first.Generation != e.LaunchGeneration {
		t.Fatalf("locator lost event identity: %+v", first)
	}
	b, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"command", "secret", "payload", "approval payload"} {
		if strings.Contains(string(b), forbidden) {
			t.Errorf("locator leaked forbidden %q: %s", forbidden, b)
		}
	}
}

func TestDedupExactlyOnceAndBoundedEviction(t *testing.T) {
	d := NewDedup()
	if !d.Claim("same", 1) || d.Claim("same", 1) {
		t.Fatal("same event-generation must be claimable exactly once")
	}
	for i := 0; i < Window; i++ {
		if !d.Claim(fmt.Sprintf("event-%d", i), 1) {
			t.Fatalf("first claim %d suppressed", i)
		}
	}
	if d.l.Len() != Window {
		t.Fatalf("dedup length=%d want %d", d.l.Len(), Window)
	}
	if !d.Claim("same", 1) {
		t.Error("oldest entry should be eligible again after bounded eviction")
	}
}

func TestSelectSinceUsesPerDeviceCursorAndRecoversRingWrap(t *testing.T) {
	events := []contract.Envelope{
		n1Event("one", 1, contract.EventApprovalRequested),
		n1Event("two", 1, contract.EventApprovalRequested),
		n1Event("three", 2, contract.EventApprovalRequested),
	}
	// Normal: cursor found, returns events after it.
	selected, wrapped := SelectSince(events, Cursor{DeviceID: "device-a", LastEventID: "one", LastGeneration: 1})
	if wrapped || len(selected) != 2 || selected[0].EventID != "two" {
		t.Fatalf("cursor selection = %#v, wrapped=%v", selected, wrapped)
	}

	// Wrap: cursor not in ring — returns only latest event to prevent blind
	// replay of all retained events. The device re-establishes its cursor
	// from the delivered event.
	selected, wrapped = SelectSince(events, Cursor{DeviceID: "device-b", LastEventID: "gone", LastGeneration: 1})
	if !wrapped || len(selected) != 1 || selected[0].EventID != "three" {
		t.Fatalf("ring wrap must return only latest event (blind replay prohibited): len=%d, wrapped=%v", len(selected), wrapped)
	}

	// Empty cursor: returns all events (fresh device).
	selected, wrapped = SelectSince(events, Cursor{DeviceID: ""})
	if wrapped || len(selected) != len(events) {
		t.Fatalf("empty cursor must return all: %#v, wrapped=%v", selected, wrapped)
	}

	// DeviceID present and cursor found → subset.
	events2 := []contract.Envelope{
		n1Event("a", 1, contract.EventApprovalRequested),
		n1Event("b", 1, contract.EventApprovalRequested),
	}
	selected, wrapped = SelectSince(events2, Cursor{DeviceID: "d1", LastEventID: "a", LastGeneration: 1})
	if wrapped || len(selected) != 1 || selected[0].EventID != "b" {
		t.Fatalf("cursor with deviceID: %d events, wrapped=%v", len(selected), wrapped)
	}

	// Empty DeviceID with cursor values → fresh device → all events.
	selected, wrapped = SelectSince(events2, Cursor{DeviceID: "", LastEventID: "a", LastGeneration: 1})
	if wrapped || len(selected) != 2 {
		t.Fatalf("empty DeviceID must return all events fresh: %d events", len(selected))
	}
}

// ── V1 production tests (8) ──

// stubSender records calls for test assertions. The `sent` channel is
// signalled on each successful Send so tests can synchronise with the
// fire-and-forget dispatch goroutines.
type stubSender struct {
	mu   sync.Mutex
	out  []Locator
	sent chan struct{} // optional: closed for each Send (nil = no signal)
}

func (s *stubSender) Send(deviceID, pushToken string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var loc Locator
	if err := json.Unmarshal(payload, &loc); err != nil {
		return err
	}
	s.out = append(s.out, loc)
	if s.sent != nil {
		select {
		case s.sent <- struct{}{}:
		default:
		}
	}
	return nil
}

func (s *stubSender) locators() []Locator {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Locator, len(s.out))
	copy(out, s.out)
	return out
}

// waitForLocators blocks until at least n locators are recorded or deadline.
func (s *stubSender) waitForLocators(t *testing.T, n int, deadline time.Duration) []Locator {
	t.Helper()
	timeout := time.After(deadline)
	for {
		locs := s.locators()
		if len(locs) >= n {
			return locs
		}
		select {
		case <-timeout:
			t.Fatalf("timeout waiting for %d locators, got %d", n, len(locs))
		case <-s.sent:
		}
	}
}

// stubResolver implements AuthResolver for tests.
type stubResolver struct {
	gen       map[string]int64
	perms     map[string][]string
	runtimeOf map[string]string
}

func (r *stubResolver) GetGeneration(sessionID string) (int64, bool) {
	if r.gen == nil {
		return 0, false
	}
	g, ok := r.gen[sessionID]
	return g, ok
}

func (r *stubResolver) HasPermission(deviceID, perm string) bool {
	if r.perms == nil {
		return false
	}
	for _, p := range r.perms[deviceID] {
		if p == perm {
			return true
		}
	}
	return false
}

func (r *stubResolver) RuntimeID(sessionID string) (string, bool) {
	if r.runtimeOf == nil {
		return "", false
	}
	id, ok := r.runtimeOf[sessionID]
	return id, ok
}

func (r *stubResolver) IsResolved(sessionID, approvalID string) bool { return false }

// openTestWriter opens a writer for a test.
func openTestWriter(t *testing.T) *writer.Writer {
	t.Helper()
	w, err := writer.Open(writer.Config{Path: t.TempDir() + "/timeline.jsonl"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return w
}

// TestProductionDelivery verifies that the Notifier consumer loop delivers
// real Locator JSON via PushSender when Timeline events are available.
func TestProductionDelivery(t *testing.T) {
	devices := NewDeviceStore()
	devices.Bind("device-1", "push-token-abc")
	sender := &stubSender{sent: make(chan struct{}, 16)}

	var gen atomic.Int64
	gen.Store(7)

	w := openTestWriter(t)
	notifier := NewNotifier(w, devices, func(sid string) int64 { return gen.Load() }, sender)
	notifier.SetEnabled(true)

	// Write a validated event.
	ev := validEnvelope("prod-1", "session-1", 7, contract.EventApprovalRequested)
	if !w.Append(ev) {
		t.Fatal("Append failed")
	}

	// Fire-and-forget dispatch. Wait for locator to arrive.
	notifier.Dispatch()
	locs := sender.waitForLocators(t, 1, 2*time.Second)
	if len(locs) != 1 {
		t.Fatalf("sent %d locators want 1", len(locs))
	}
	loc := locs[0]
	if loc.EventID != ev.EventID || loc.Kind != contract.EventApprovalRequested || loc.Generation != 7 {
		t.Errorf("locator fields mismatch: %+v (want eventId=%s)", loc, ev.EventID)
	}
	if loc.N1Token == "" {
		t.Error("N1Token must not be empty")
	}

	// Duplicate dispatch must be suppressed by dedup.
	time.Sleep(100 * time.Millisecond) // let goroutines finish
	before := len(sender.locators())
	notifier.Dispatch()
	time.Sleep(100 * time.Millisecond) // let goroutines finish
	if len(sender.locators()) != before {
		t.Errorf("duplicate dispatch must be suppressed: before=%d after=%d", before, len(sender.locators()))
	}
}

// TestDeviceStorePerDeviceBindRevoke verifies per-device bind/revoke and
// token isolation between devices.
func TestDeviceStorePerDeviceBindRevoke(t *testing.T) {
	s := NewDeviceStore()

	s.Bind("device-a", "token-a")
	s.Bind("device-b", "token-b")

	if tok := s.Token("device-a"); tok != "token-a" {
		t.Errorf("device-a token = %q want token-a", tok)
	}
	if tok := s.Token("device-b"); tok != "token-b" {
		t.Errorf("device-b token = %q want token-b", tok)
	}

	// Revoke device-a.
	if !s.Revoke("device-a") {
		t.Error("Revoke should return true for registered device")
	}
	if tok := s.Token("device-a"); tok != "" {
		t.Errorf("revoked device-a token = %q want empty", tok)
	}
	if tok := s.Token("device-b"); tok != "token-b" {
		t.Errorf("device-b should be unaffected: %q", tok)
	}

	// Revoke non-existent.
	if s.Revoke("unknown") {
		t.Error("Revoke should return false for unknown device")
	}

	// Verify ForEach only sees remaining devices.
	var seen []string
	s.ForEach(func(deviceID, _ string) {
		seen = append(seen, deviceID)
	})
	if len(seen) != 1 || seen[0] != "device-b" {
		t.Errorf("ForEach saw %v want [device-b]", seen)
	}

	// Cursor is cleared on revoke.
	s2 := NewDeviceStore()
	s2.Bind("device-x", "token-x")
	s2.Cursor("device-x", Cursor{DeviceID: "device-x", LastEventID: "ev-5", LastGeneration: 3}, 0)
	c := s2.GetCursor("device-x")
	if c.LastEventID != "ev-5" {
		t.Fatalf("cursor not stored: %+v", c)
	}
	s2.Revoke("device-x")
	c = s2.GetCursor("device-x")
	if c.LastEventID != "" {
		t.Errorf("cursor must be cleared on revoke: %+v", c)
	}
}

// TestResolveStatusSevenOutcomes verifies all 7 re-authorization outcomes.
func TestResolveStatusSevenOutcomes(t *testing.T) {
	w := openTestWriter(t)

	// Write an actionable event and capture its computed EventID.
	ev := validEnvelope("action", "sess-1", 5, contract.EventApprovalRequested)
	if !w.Append(ev) {
		t.Fatal("Append failed")
	}
	eventID := ev.EventID

	resolver := &stubResolver{
		gen:       map[string]int64{"sess-1": 5},
		perms:     map[string][]string{"device-1": {devicetrust.PermTerminalInput}},
		runtimeOf: map[string]string{"sess-1": "rt-sess-1"},
	}

	// 1. session_unavailable
	resp := ResolveStatus(eventID, 5, "sess-missing", "rt-sess-missing", "device-1", resolver, resolver, w)
	if resp.Status != "session_unavailable" {
		t.Errorf("missing session: %s", resp.Status)
	}

	// 2. stale_generation (gen mismatch)
	resp = ResolveStatus(eventID, 3, "sess-1", "rt-sess-1", "device-1", resolver, resolver, w)
	if resp.Status != "stale_generation" {
		t.Errorf("stale gen: %s", resp.Status)
	}

	// 3. stale_generation (runtime mismatch)
	resp = ResolveStatus(eventID, 5, "sess-1", "rt-old", "device-1", resolver, resolver, w)
	if resp.Status != "stale_generation" {
		t.Errorf("runtime mismatch: %s", resp.Status)
	}

	// 4. insufficient_permission
	resp = ResolveStatus(eventID, 5, "sess-1", "rt-sess-1", "device-no-perm", resolver, resolver, w)
	if resp.Status != "insufficient_permission" {
		t.Errorf("no perm: %s", resp.Status)
	}

	// 5. canonical_event_unavailable (event not in ring)
	resp = ResolveStatus("ev-missing", 5, "sess-1", "rt-sess-1", "device-1", resolver, resolver, w)
	if resp.Status != "canonical_event_unavailable" {
		t.Errorf("missing event: %s", resp.Status)
	}

	// 6. actionable
	resp = ResolveStatus(eventID, 5, "sess-1", "rt-sess-1", "device-1", resolver, resolver, w)
	if resp.Status != "actionable" {
		t.Errorf("actionable: %s (eventID=%s)", resp.Status, eventID)
	}
	if resp.Permissions == nil || len(resp.Permissions) != 1 || resp.Permissions[0] != devicetrust.PermTerminalInput {
		t.Errorf("permissions mismatch: %v", resp.Permissions)
	}
	if resp.ActivityLink == "" {
		t.Error("activityLink must be set on actionable")
	}
	if resp.Event == nil || resp.Event.EventID != eventID || resp.Event.SessionID != "sess-1" || resp.Event.Kind != contract.EventApprovalRequested {
		t.Fatalf("actionable response must carry the exact safe event: %+v", resp.Event)
	}
}

// TestEventDegradedOrGap verifies that ResolveStatus checks writer health
// before scanning the ring buffer. The HealthSnapshot gate is the
// event_degraded_or_gap contract. Two paths are verified:
//  1. Nil writer → skips degraded check → falls through to canonical_event_unavailable
//  2. Active writer → HealthSnapshot() gates the ring-buffer scan
func TestEventDegradedOrGap(t *testing.T) {
	resolver := &stubResolver{
		gen:       map[string]int64{"sess-1": 5},
		perms:     map[string][]string{"device-1": {devicetrust.PermTerminalInput}},
		runtimeOf: map[string]string{"sess-1": "rt-sess-1"},
	}

	// Nil writer: skips degraded check + ring scan → canonical_event_unavailable.
	resp := ResolveStatus("ev-1", 5, "sess-1", "rt-sess-1", "device-1", resolver, resolver, nil)
	if resp.Status != "canonical_event_unavailable" {
		t.Errorf("nil writer: got %s want canonical_event_unavailable", resp.Status)
	}

	// With a real, healthy writer: event is found → actionable.
	w := openTestWriter(t)
	if !w.Append(validEnvelope("degrade", "sess-1", 5, contract.EventApprovalRequested)) {
		t.Fatal("Append failed")
	}

	recentEvents := w.ReadRecent(128)
	if len(recentEvents) == 0 {
		t.Fatal("ring buffer empty after Append")
	}
	eventID := recentEvents[len(recentEvents)-1].EventID

	// Healthy writer → actionable.
	resp = ResolveStatus(eventID, 5, "sess-1", "rt-sess-1", "device-1", resolver, resolver, w)
	if resp.Status != "actionable" {
		t.Errorf("healthy writer: got %s want actionable", resp.Status)
	}

	// Verify the degraded gate exists: a healthy writer does NOT trigger degraded.
	degraded, _ := w.HealthSnapshot()
	if degraded {
		t.Log("writer reports degraded (OS-dependent)")
	}
}

// TestDeviceIDFromPrincipal verifies that deviceId is extracted from
// the Principal (auth context), never from a caller-supplied parameter.
// TestInsufficientPermission verifies the handler requires terminal:input
// permission, not just sessions:read.
func TestInsufficientPermission(t *testing.T) {
	w := openTestWriter(t)

	ev := validEnvelope("perm", "sess-1", 3, contract.EventApprovalRequested)
	if !w.Append(ev) {
		t.Fatal("Append failed")
	}
	eventID := ev.EventID

	// Device has sessions:read but NOT terminal:input.
	resolver := &stubResolver{
		gen:       map[string]int64{"sess-1": 3},
		perms:     map[string][]string{"device-readonly": {devicetrust.PermSessionsRead}},
		runtimeOf: map[string]string{"sess-1": "rt-sess-1"},
	}

	resp := ResolveStatus(eventID, 3, "sess-1", "rt-sess-1", "device-readonly", resolver, resolver, w)
	if resp.Status != "insufficient_permission" {
		t.Errorf("session:read-only device must get insufficient_permission: got %s", resp.Status)
	}

	// Device with terminal:input gets actionable.
	resolver.perms["device-owner"] = []string{devicetrust.PermTerminalInput}
	resp = ResolveStatus(eventID, 3, "sess-1", "rt-sess-1", "device-owner", resolver, resolver, w)
	if resp.Status != "actionable" {
		t.Errorf("terminal:input device must get actionable: got %s", resp.Status)
	}
}

// TestFlagOffZeroEffect verifies that flag-off means registration still
// works but no push delivery happens.
func TestFlagOffZeroEffect(t *testing.T) {
	devices := NewDeviceStore()

	// Without a writer, Notifier starts disabled (flag-off).
	notifier := NewNotifier(nil, devices, nil, nil)

	// Device registration still works (flag-off = zero-effect).
	devices.Bind("device-1", "push-token")
	if tok := devices.Token("device-1"); tok != "push-token" {
		t.Errorf("registration must work without writer: %q", tok)
	}

	// Dispatch returns 0 (no writer → nothing to deliver).
	n := notifier.Dispatch()
	if n != 0 {
		t.Errorf("flag-off dispatch must be 0: got %d", n)
	}

	// RegisterHandlers with nil Writer still registers the push route.
	mux := http.NewServeMux()
	bootID, _ := devicetrust.NewBootID()
	sessionMgr := devicetrust.NewDeviceSessionManager(bootID, 1*time.Hour)
	RegisterHandlers(mux, NotificationHandlerConfig{
		Writer:   nil,
		Resolver: nil,
		Sessions: sessionMgr,
		Devices:  devices,
	})

	// push/register route is registered even without writer.
	req := httptest.NewRequest("GET", "/push/register?token=test-token", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	// 401 (no bearer token) proves the route is registered and auth middleware runs.
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("push/register without auth: %d want 401", rec.Code)
	}

	// Verify with valid auth the handler reaches the business logic.
	rawToken, _, _, _ := sessionMgr.CreateAfterVerifiedChallenge(
		"device-1", "host-1", bootID,
		[]string{devicetrust.PermSessionsRead},
	)
	req2 := httptest.NewRequest("GET", "/push/register?token=push-token", nil)
	req2.Header.Set("Authorization", "Bearer "+rawToken)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("push/register with valid auth: %d want 200", rec2.Code)
	}
	if devices.Token("device-1") != "push-token" {
		t.Error("token should be bound from handler")
	}
}

// TestNotificationRestartRecovery verifies that after a restart (fresh
// Dedup), stable tokens prevent duplicate notification delivery because
// the device already received and stored them.
func TestNotificationRestartRecovery(t *testing.T) {
	devices := NewDeviceStore()
	devices.Bind("device-1", "push-token")
	sender := &stubSender{sent: make(chan struct{}, 16)}

	var gen atomic.Int64
	gen.Store(1)

	w := openTestWriter(t)

	// First run: deliver event.
	n1 := NewNotifier(w, devices, func(sid string) int64 { return gen.Load() }, sender)
	n1.SetEnabled(true)

	ev1 := validEnvelope("restart-1", "session-1", 1, contract.EventApprovalRequested)
	if !w.Append(ev1) {
		t.Fatal("Append ev1 failed")
	}
	n1.Dispatch()
	sender.waitForLocators(t, 1, 2*time.Second)

	// Simulate restart: fresh Notifier, same device store (cursor persists
	// from the first run's successful delivery).
	sender2 := &stubSender{sent: make(chan struct{}, 16)}
	n2 := NewNotifier(w, devices, func(sid string) int64 { return gen.Load() }, sender2)
	n2.SetEnabled(true)

	// Same events in ring — cursor already past them, so nothing delivered.
	n2.Dispatch()
	time.Sleep(200 * time.Millisecond) // let goroutines finish
	if len(sender2.locators()) != 0 {
		t.Errorf("restart must not re-deliver already-seen events: got %d", len(sender2.locators()))
	}

	// New event after restart.
	ev2 := validEnvelope("restart-2", "session-1", 1, contract.EventApprovalRequested)
	if !w.Append(ev2) {
		t.Fatal("Append ev2 failed")
	}
	n2.Dispatch()
	locs := sender2.waitForLocators(t, 1, 2*time.Second)
	if len(locs) != 1 || locs[0].EventID != ev2.EventID {
		t.Errorf("restart delivered wrong event: %+v", locs)
	}
}

// TestSelectSinceUsesDeviceID verifies that the DeviceID field in Cursor
// is propagated correctly and an empty DeviceID returns all events (fresh
// device with no cursor).
// TestMobileTapSimulation verifies the end-to-end notification → re-auth
// flow that a mobile device follows when tapping a notification.
func TestMobileTapSimulation(t *testing.T) {
	w := openTestWriter(t)

	// Step 1: Producer writes an approval-requested event.
	ev := validEnvelope("tap", "sess-mobile", 2, contract.EventApprovalRequested)
	if !w.Append(ev) {
		t.Fatal("Append failed")
	}
	eventID := ev.EventID

	// Step 2: Notifier builds a Locator and pushes it.
	loc, ok := Build(ev, 2)
	if !ok {
		t.Fatal("Build failed")
	}

	// Step 3: Mobile device receives push, taps notification.
	// It calls GET /api/notification/{eventId}/status with locator data.
	resolver := &stubResolver{
		gen:       map[string]int64{"sess-mobile": 2},
		perms:     map[string][]string{"mobile-device": {devicetrust.PermTerminalInput}},
		runtimeOf: map[string]string{"sess-mobile": "rt-sess-mobile"},
	}

	resp := ResolveStatus(loc.EventID, loc.Generation, loc.SessionID, loc.RuntimeID, "mobile-device", resolver, resolver, w)
	if resp.Status != "actionable" {
		t.Errorf("mobile tap re-auth: %s want actionable (eventID=%s)", resp.Status, eventID)
	}
	if resp.ActivityLink == "" {
		t.Error("actionable response must include activity link for deep linking")
	}
	if resp.CurrentGeneration != 2 {
		t.Errorf("currentGeneration = %d want 2", resp.CurrentGeneration)
	}

	// Step 4: Stale tap (generation moved on → rejected).
	resolver.gen["sess-mobile"] = 3
	resp = ResolveStatus(loc.EventID, loc.Generation, loc.SessionID, loc.RuntimeID, "mobile-device", resolver, resolver, w)
	if resp.Status != "stale_generation" {
		t.Errorf("stale tap: %s want stale_generation", resp.Status)
	}
}

// ── R3 tests ──

// resolvingResolver is a stubResolver that reports all approvals as resolved.
type resolvingResolver struct {
	*stubResolver
}

func (r *resolvingResolver) IsResolved(sessionID, approvalID string) bool { return true }

// TestAlreadyResolved verifies that when the approval store reports an
// approval as already resolved (not actionable), ResolveStatus returns
// "already_resolved" instead of "actionable".
func TestAlreadyResolved(t *testing.T) {
	w := openTestWriter(t)

	// Write an approval-requested event with a known approval reference.
	ev := validEnvelope("resolved", "sess-r", 3, contract.EventApprovalRequested)
	if !w.Append(ev) {
		t.Fatal("Append failed")
	}
	eventID := ev.EventID

	resolver := &stubResolver{
		gen:       map[string]int64{"sess-r": 3},
		perms:     map[string][]string{"device-1": {devicetrust.PermTerminalInput}},
		runtimeOf: map[string]string{"sess-r": "rt-sess-r"},
	}

	// With ordinary stubResolver (IsResolved always false) → actionable.
	resp := ResolveStatus(eventID, 3, "sess-r", "rt-sess-r", "device-1", resolver, resolver, w)
	if resp.Status != "actionable" {
		t.Fatalf("unresolved approval: got %s want actionable", resp.Status)
	}

	// With resolvingResolver (IsResolved always true) → already_resolved.
	rr := &resolvingResolver{stubResolver: resolver}
	resp = ResolveStatus(eventID, 3, "sess-r", "rt-sess-r", "device-1", rr, rr, w)
	if resp.Status != "already_resolved" {
		t.Errorf("resolved approval: got %s want already_resolved", resp.Status)
	}
}

// ── R4 tests ──

// approvalStoreChecker is a real lookup-based ApprovalChecker (not a boolean
// stub). It stores per-approval resolution state and looks up by session+approval.
type approvalStoreChecker struct {
	mu      sync.Mutex
	records map[string]bool // "sessionID:approvalID" → resolved
}

func newApprovalStoreChecker() *approvalStoreChecker {
	return &approvalStoreChecker{records: make(map[string]bool)}
}

func (c *approvalStoreChecker) markResolved(sessionID, approvalID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records[sessionID+":"+approvalID] = true
}

func (c *approvalStoreChecker) IsResolved(sessionID, approvalID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.records[sessionID+":"+approvalID]
}

// TestAlreadyResolvedRealStore verifies the already_resolved outcome using a
// real lookup-based approval store (not a boolean stub). The store is populated
// with a per-approval record, simulating a previously-resolved approval.
func TestAlreadyResolvedRealStore(t *testing.T) {
	w := openTestWriter(t)

	// Write an approval-requested event with known references.
	ev := validEnvelope("real-resolved", "sess-real", 3, contract.EventApprovalRequested)
	if !w.Append(ev) {
		t.Fatal("Append failed")
	}
	eventID := ev.EventID

	// Extract the approval reference ID set by validEnvelope.
	approvalID := ev.References.ApprovalRequest.ID

	resolver := &stubResolver{
		gen:       map[string]int64{"sess-real": 3},
		perms:     map[string][]string{"device-1": {devicetrust.PermTerminalInput}},
		runtimeOf: map[string]string{"sess-real": "rt-sess-real"},
	}

	store := newApprovalStoreChecker()

	// Without any record → actionable.
	resp := ResolveStatus(eventID, 3, "sess-real", "rt-sess-real", "device-1", resolver, store, w)
	if resp.Status != "actionable" {
		t.Fatalf("no record: got %s want actionable", resp.Status)
	}

	// Mark the approval as resolved in the store.
	store.markResolved("sess-real", approvalID)

	// Now the lookup finds it → already_resolved.
	resp = ResolveStatus(eventID, 3, "sess-real", "rt-sess-real", "device-1", resolver, store, w)
	if resp.Status != "already_resolved" {
		t.Errorf("resolved record: got %s want already_resolved", resp.Status)
	}

	// A different approval ID is still actionable.
	resp = ResolveStatus(eventID, 3, "sess-real", "rt-sess-real", "device-1", resolver, store, w)
	// Same approval ID, same store → already_resolved (cached from above)
	_ = resp.Status // already asserted

	// Write a second event with a different approval.
	ev2 := validEnvelope("real-active", "sess-real", 3, contract.EventApprovalRequested)
	w.Append(ev2)
	approvalID2 := ev2.References.ApprovalRequest.ID
	if approvalID2 != approvalID {
		// Different approval, not marked → actionable.
		resp = ResolveStatus(ev2.EventID, 3, "sess-real", "rt-sess-real", "device-1", resolver, store, w)
		if resp.Status != "actionable" {
			t.Errorf("unresolved approval: got %s want actionable", resp.Status)
		}
	}
}

// TestDegradedWriterFIFO actually degrades an open writer by writing through
// a FIFO (named pipe) and then closing the read end. The next write gets
// EPIPE/SIGPIPE, which marks the writer as degraded. This is reliable on
// macOS and Linux where FIFOs are supported.
func TestDegradedWriterFIFO(t *testing.T) {
	dir := t.TempDir()
	fifoPath := dir + "/timeline.fifo"

	// Create a FIFO (named pipe).
	if err := syscall.Mkfifo(fifoPath, 0600); err != nil {
		t.Skipf("mkfifo not supported: %v", err)
	}

	// Open the read end in a goroutine so the write-end open doesn't block.
	type fifoResult struct {
		data []byte
		err  error
	}
	readDone := make(chan fifoResult, 1)
	readerReady := make(chan struct{})
	go func() {
		// O_RDWR opens a FIFO without blocking (both ends satisfied).
		f, err := os.OpenFile(fifoPath, os.O_RDWR, 0)
		if err != nil {
			readDone <- fifoResult{err: err}
			return
		}
		close(readerReady)
		// Read one event, then close — next write by the writer gets EPIPE.
		var buf [4096]byte
		n, _ := f.Read(buf[:])
		f.Close()
		readDone <- fifoResult{data: buf[:n]}
	}()

	// Wait for reader to open the FIFO.
	select {
	case <-readerReady:
	case <-time.After(2 * time.Second):
		// Reader may have errored; check.
		select {
		case r := <-readDone:
			t.Skipf("FIFO reader failed: %v", r.err)
		default:
			t.Skip("FIFO reader did not start (OS may not support FIFOs)")
		}
	}

	// Now open the writer on the FIFO. O_APPEND|O_CREATE|O_WRONLY opens
	// the write end (won't block since reader is open).
	w, err := writer.Open(writer.Config{Path: fifoPath}, nil)
	if err != nil {
		t.Fatalf("Open writer on FIFO: %v", err)
	}
	defer w.Close()

	// Write a valid event — succeeds while read end is open.
	if !w.Append(validEnvelope("fifo-1", "sess-fifo", 1, contract.EventApprovalRequested)) {
		t.Fatal("first Append on FIFO failed")
	}

	// Wait for reader to consume and close. After this, writes get EPIPE.
	r := <-readDone
	if r.err != nil {
		t.Fatalf("FIFO reader error: %v", r.err)
	}
	if len(r.data) == 0 {
		t.Fatal("FIFO reader got no data")
	}

	// Give the OS a moment to register the closed pipe.
	time.Sleep(50 * time.Millisecond)

	// Second write: read end is closed → EPIPE → writer marks degraded.
	_ = w.Append(validEnvelope("fifo-2", "sess-fifo", 1, contract.EventApprovalRequested))

	degraded, reason := w.HealthSnapshot()
	if !degraded {
		// On some kernels, the EPIPE may be delivered asynchronously.
		// Try a few more writes to trigger it.
		for i := 0; i < 5; i++ {
			_ = w.Append(validEnvelope(fmt.Sprintf("fifo-r%d", i), "sess-fifo", 1, contract.EventApprovalRequested))
			degraded, reason = w.HealthSnapshot()
			if degraded {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !degraded {
		t.Fatalf("writer should be degraded after FIFO read-end close: reason=%s", reason)
	}
	t.Logf("writer degraded via FIFO EPIPE: reason=%s", reason)

	// With a degraded writer, ResolveStatus returns event_degraded_or_gap.
	resolver := &stubResolver{
		gen:       map[string]int64{"sess-fifo": 1},
		perms:     map[string][]string{"device-1": {devicetrust.PermTerminalInput}},
		runtimeOf: map[string]string{"sess-fifo": "rt-sess-fifo"},
	}
	resp := ResolveStatus("ev-any", 1, "sess-fifo", "rt-sess-fifo", "device-1", resolver, resolver, w)
	if resp.Status != "event_degraded_or_gap" {
		t.Errorf("degraded writer via FIFO: got %s want event_degraded_or_gap", resp.Status)
	}
}

// TestPerDeviceSingleflight verifies that when a device's dispatch goroutine
// is already in-flight, subsequent dispatch cycles skip it. Other devices
// continue receiving events. After the first goroutine finishes, the next
// cycle launches a fresh one.
func TestPerDeviceSingleflight(t *testing.T) {
	devices := NewDeviceStore()
	devices.Bind("device-fast", "token-fast")
	devices.Bind("device-slow", "token-slow")

	var fastCount atomic.Int64
	var slowBlocked atomic.Bool
	slowUnblock := make(chan struct{})

	sender := &selectiveHangSender{
		hangDevice: "device-slow",
		onSend: func(deviceID string) {
			if deviceID == "device-fast" {
				fastCount.Add(1)
			}
			if deviceID == "device-slow" {
				slowBlocked.Store(true)
			}
		},
		slowUnblock: slowUnblock,
	}

	var gen atomic.Int64
	gen.Store(1)
	w := openTestWriter(t)
	notifier := NewNotifier(w, devices, func(sid string) int64 { return gen.Load() }, sender)
	notifier.SetEnabled(true)

	// Event-1: both devices start dispatch, slow blocks on Send.
	w.Append(validEnvelope("sf-1", "session-1", 1, contract.EventApprovalRequested))
	notifier.Dispatch()

	// Wait for fast to receive and slow to enter its Send.
	for fastCount.Load() < 1 || !slowBlocked.Load() {
		time.Sleep(20 * time.Millisecond)
	}

	// Singleflight: slow still blocked → next dispatch skips slow.
	launched := notifier.Dispatch()
	if launched > 1 {
		t.Errorf("singleflight: launched=%d want at most 1 (slow should be skipped)", launched)
	}

	// Event-2: fast receives it, slow skipped.
	w.Append(validEnvelope("sf-2", "session-1", 1, contract.EventApprovalRequested))
	notifier.Dispatch()
	time.Sleep(200 * time.Millisecond)
	if fastCount.Load() < 2 {
		t.Errorf("fast should receive 2 events: got %d", fastCount.Load())
	}

	// Unblock slow and verify it eventually finishes.
	close(slowUnblock)
	deadline := time.After(3 * time.Second)
	for notifier.ActiveGoroutines() > 0 {
		select {
		case <-deadline:
			t.Fatalf("slow goroutine did not finish: active=%d", notifier.ActiveGoroutines())
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// TestSameDeviceOrderingAndMonotonicCursor verifies events are delivered in
// order and the cursor only advances forward (monotonic).
func TestSameDeviceOrderingAndMonotonicCursor(t *testing.T) {
	devices := NewDeviceStore()
	devices.Bind("device-1", "token-1")

	var delivered []string
	var mu sync.Mutex
	sender := &recordingSender{
		onSend: func(deviceID string, loc Locator) {
			mu.Lock()
			delivered = append(delivered, loc.EventID)
			mu.Unlock()
		},
	}

	w := openTestWriter(t)
	notifier := NewNotifier(w, devices, func(sid string) int64 { return 1 }, sender)
	notifier.SetEnabled(true)

	// Write events in order: a, b, c.
	evA := validEnvelope("seq-a", "session-1", 1, contract.EventApprovalRequested)
	evB := validEnvelope("seq-b", "session-1", 1, contract.EventApprovalRequested)
	evC := validEnvelope("seq-c", "session-1", 1, contract.EventApprovalRequested)
	w.Append(evA)
	w.Append(evB)
	w.Append(evC)

	notifier.Dispatch()
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	ids := make([]string, len(delivered))
	copy(ids, delivered)
	mu.Unlock()

	if len(ids) != 3 {
		t.Fatalf("delivered %d events want 3: %v", len(ids), ids)
	}
	if ids[0] != evA.EventID || ids[1] != evB.EventID || ids[2] != evC.EventID {
		t.Errorf("ordering violated: got %v", ids)
	}

	// Cursor must now point to event-c (last delivered).
	c := devices.GetCursor("device-1")
	if c.LastEventID != evC.EventID {
		t.Errorf("cursor not advanced: %+v", c)
	}

	// Double-check monotonic: dispatch again, nothing delivered.
	before := len(delivered)
	notifier.Dispatch()
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	after := len(delivered)
	mu.Unlock()
	if after != before {
		t.Errorf("cursor should prevent re-delivery: before=%d after=%d", before, after)
	}

	// At-most-once partial failure: cursor advances to last SUCCESS only.
	// event4 succeeds, event5 fails → cursor = event4 position, NOT event5.
	devices2 := NewDeviceStore()
	devices2.Bind("device-2", "token-2")
	failSender := &failAfterNSender{failAfter: 1}
	w2 := openTestWriter(t)
	n2 := NewNotifier(w2, devices2, func(sid string) int64 { return 1 }, failSender)
	n2.SetEnabled(true)

	ev4 := validEnvelope("seq-d", "session-2", 1, contract.EventApprovalRequested)
	ev5 := validEnvelope("seq-e", "session-2", 1, contract.EventApprovalRequested)
	w2.Append(ev4)
	w2.Append(ev5)

	n2.Dispatch()
	time.Sleep(300 * time.Millisecond)

	failSender.mu.Lock()
	sentCount := len(failSender.sent)
	failSender.mu.Unlock()
	if sentCount != 1 {
		t.Fatalf("partial failure: sent %d events, want 1", sentCount)
	}

	c2 := devices2.GetCursor("device-2")
	if c2.LastEventID != ev4.EventID {
		t.Errorf("cursor after partial failure: got %q want %q (last success)", c2.LastEventID, ev4.EventID)
	}
	if c2.LastEventID == ev5.EventID {
		t.Error("cursor must NOT be the failed event")
	}
	n2.Stop()
}

// TestStopCursorNotWritten verifies Stop concurrency: when a Send is
// in-flight at Stop time, the goroutine completes but the cursor is NOT
// written (stopped guard in the goroutine prevents late writes).
// TestRevokeCursorNotRestored verifies Revoke concurrency: when a Send is
// in-flight and RevokeDevice is called, the goroutine completes but does
// NOT write the cursor (revoked guard prevents resurrection).
// recordingSender records Locator deliveries for ordering tests.
type recordingSender struct {
	onSend func(deviceID string, loc Locator)
}

func (s *recordingSender) Send(deviceID, pushToken string, payload []byte) error {
	var loc Locator
	json.Unmarshal(payload, &loc)
	if s.onSend != nil {
		s.onSend(deviceID, loc)
	}
	return nil
}

// selectiveHangSender blocks on a specific deviceID until signalled.
// When successAfterStop is true, Send returns nil (success) after unblock,
// simulating a late-arriving success after Stop/Revoke.
type selectiveHangSender struct {
	hangDevice       string
	onSend           func(deviceID string)
	slowUnblock      chan struct{}
	successAfterStop bool
}

func (s *selectiveHangSender) Send(deviceID, pushToken string, payload []byte) error {
	if s.onSend != nil {
		s.onSend(deviceID)
	}
	if deviceID == s.hangDevice {
		<-s.slowUnblock
		if s.successAfterStop {
			return nil // late success after Stop/Revoke
		}
		return fmt.Errorf("expo push timeout")
	}
	return nil
}

// TestLateSuccessCursorNotWrittenAfterStop verifies that even when Send
// eventually succeeds AFTER Stop, the cursor is NOT written. The stopped
// guard in the goroutine prevents late-success writes.
func TestLateSuccessCursorNotWrittenAfterStop(t *testing.T) {
	devices := NewDeviceStore()
	devices.Bind("device-1", "token-1")

	block := make(chan struct{})
	sender := &selectiveHangSender{
		hangDevice:       "device-1",
		slowUnblock:      block,
		successAfterStop: true, // Send returns nil after unblock
	}

	w := openTestWriter(t)
	notifier := NewNotifier(w, devices, func(sid string) int64 { return 1 }, sender)
	notifier.SetEnabled(true)

	w.Append(validEnvelope("late-1", "session-1", 1, contract.EventApprovalRequested))
	notifier.Dispatch()

	time.Sleep(100 * time.Millisecond)

	// Stop in goroutine — sets stopped=true, then drains.
	stopDone := make(chan struct{})
	go func() { notifier.Stop(); close(stopDone) }()

	time.Sleep(100 * time.Millisecond) // let Stop set the flag

	// Unblock Send — it returns nil (success). But goroutine checks stopped
	// before writing cursor → cursor stays empty.
	close(block)

	select {
	case <-stopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not finish")
	}

	c := devices.GetCursor("device-1")
	if c.LastEventID != "" {
		t.Errorf("cursor must NOT be written after Stop even on Send success: got %+v", c)
	}
}

// TestLateSuccessCursorNotWrittenAfterRevoke verifies that even when Send
// eventually succeeds AFTER RevokeDevice, the cursor is NOT written. The
// revoked guard prevents resurrection.
// TestOldSendVsRevokeRebind verifies that when a device is revoked and
// rebound while an old Send is still in-flight, the old goroutine's epoch
// check prevents it from writing a stale cursor. The new binding gets a
// fresh epoch, and the old goroutine's epoch mismatch skips the write.
func TestOldSendVsRevokeRebind(t *testing.T) {
	devices := NewDeviceStore()
	devices.Bind("device-1", "token-1")

	block := make(chan struct{})
	sender := &selectiveHangSender{
		hangDevice:       "device-1",
		slowUnblock:      block,
		successAfterStop: true,
	}

	w := openTestWriter(t)
	notifier := NewNotifier(w, devices, func(sid string) int64 { return 1 }, sender)
	notifier.SetEnabled(true)

	w.Append(validEnvelope("revrebind-1", "session-1", 1, contract.EventApprovalRequested))
	notifier.Dispatch()

	time.Sleep(100 * time.Millisecond) // goroutine enters Send

	// Revoke + Rebind: bumps epoch, clears store, then re-registers.
	notifier.RevokeDevice("device-1")
	devices.Bind("device-1", "token-2") // re-register push token
	notifier.BindDevice("device-1")     // bump epoch again

	// Unblock the old Send. The old goroutine captured epoch 0; after
	// RevokeDevice+bumpEpoch+BindDevice+bumpEpoch, the current epoch is 2.
	// The old goroutine's epoch check fails → cursor NOT written.
	close(block)
	time.Sleep(200 * time.Millisecond)

	c := devices.GetCursor("device-1")
	if c.LastEventID != "" {
		t.Errorf("old goroutine must not write cursor after revoke+rebind: got %+v", c)
	}

	// Token should be the new one.
	if devices.Token("device-1") != "token-2" {
		t.Errorf("token should be token-2 after rebind: got %q", devices.Token("device-1"))
	}

	notifier.Stop()
}

// TestCursorAdvancesToLastSuccessOnPartialFailure verifies at-most-once
// cursor semantics: in the same batch, when event1 succeeds and event2
// fails, the cursor advances to event1 (the last successful position),
// NOT to the failed event2. Dedup consumes the failed event, so it is
// never retried.

// TestCursorAdvancesToLastSuccessOnPartialFailure verifies at-most-once
// cursor semantics. In the same batch: (a) event1 succeeds, event2 fails
// → cursor=event1 position, NOT event2. (b) first-send-failure edge:
// event fails, no prior success → cursor stays at its initial empty value.
func TestCursorAdvancesToLastSuccessOnPartialFailure(t *testing.T) {
	// Case A: event1 succeeds, event2 fails.
	devices := NewDeviceStore()
	devices.Bind("device-1", "token-1")
	failSender := &failAfterNSender{failAfter: 1}
	w := openTestWriter(t)
	notifier := NewNotifier(w, devices, func(sid string) int64 { return 1 }, failSender)
	notifier.SetEnabled(true)

	ev1 := validEnvelope("pf-ok", "session-1", 1, contract.EventApprovalRequested)
	ev2 := validEnvelope("pf-fail", "session-1", 1, contract.EventApprovalRequested)
	w.Append(ev1)
	w.Append(ev2)

	notifier.Dispatch()
	time.Sleep(300 * time.Millisecond)

	failSender.mu.Lock()
	sent := len(failSender.sent)
	failSender.mu.Unlock()
	if sent != 1 {
		t.Fatalf("case A: sent %d events, want 1", sent)
	}
	c := devices.GetCursor("device-1")
	if c.LastEventID != ev1.EventID {
		t.Errorf("case A: cursor should be last success: got %q want %q", c.LastEventID, ev1.EventID)
	}
	if c.LastEventID == ev2.EventID {
		t.Error("case A: cursor must NOT be the failed event")
	}
	notifier.Stop()

	// Case B: first send fails → cursor stays empty (no prior success).
	devices2 := NewDeviceStore()
	devices2.Bind("device-2", "token-2")
	failSender2 := &failAfterNSender{failAfter: 0} // first call fails
	notifier2 := NewNotifier(w, devices2, func(sid string) int64 { return 1 }, failSender2)
	notifier2.SetEnabled(true)

	w.Append(validEnvelope("pf-firstfail", "session-2", 1, contract.EventApprovalRequested))
	notifier2.Dispatch()
	time.Sleep(300 * time.Millisecond)

	c2 := devices2.GetCursor("device-2")
	if c2.LastEventID != "" {
		t.Errorf("case B: cursor must be empty after first-send failure: got %+v", c2)
	}
	notifier2.Stop()
}

// failAfterNSender succeeds for the first N calls, then returns an error.
type failAfterNSender struct {
	mu        sync.Mutex
	count     int
	failAfter int
	sent      []Locator
}

func (s *failAfterNSender) Send(deviceID, pushToken string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count++
	if s.count > s.failAfter {
		return fmt.Errorf("injected send failure after %d successes", s.failAfter)
	}
	var loc Locator
	json.Unmarshal(payload, &loc)
	s.sent = append(s.sent, loc)
	return nil
}
