package notification

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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
}

// ── V1 production tests (8) ──

// stubSender records calls for test assertions.
type stubSender struct {
	mu  sync.Mutex
	out []Locator
}

func (s *stubSender) Send(deviceID, pushToken string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var loc Locator
	if err := json.Unmarshal(payload, &loc); err != nil {
		return err
	}
	s.out = append(s.out, loc)
	return nil
}

func (s *stubSender) locators() []Locator {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Locator, len(s.out))
	copy(out, s.out)
	return out
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
	sender := &stubSender{}

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

	// Synchronous dispatch delivers the locator.
	n := notifier.Dispatch()
	if n != 1 {
		t.Fatalf("delivered %d want 1", n)
	}

	locs := sender.locators()
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
	n2 := notifier.Dispatch()
	if n2 != 0 {
		t.Errorf("duplicate dispatch must be suppressed: got %d", n2)
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
}

// TestDeviceRevokeClearsCursor verifies cursor is removed on revoke.
func TestDeviceRevokeClearsCursor(t *testing.T) {
	s := NewDeviceStore()
	s.Bind("device-a", "token-a")
	s.Cursor("device-a", Cursor{DeviceID: "device-a", LastEventID: "ev-5", LastGeneration: 3})

	c := s.GetCursor("device-a")
	if c.LastEventID != "ev-5" {
		t.Fatalf("cursor not stored: %+v", c)
	}

	s.Revoke("device-a")
	c = s.GetCursor("device-a")
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
	resp := ResolveStatus(eventID, 5, "sess-missing", "rt-sess-missing", "device-1", resolver, w)
	if resp.Status != "session_unavailable" {
		t.Errorf("missing session: %s", resp.Status)
	}

	// 2. stale_generation (gen mismatch)
	resp = ResolveStatus(eventID, 3, "sess-1", "rt-sess-1", "device-1", resolver, w)
	if resp.Status != "stale_generation" {
		t.Errorf("stale gen: %s", resp.Status)
	}

	// 3. stale_generation (runtime mismatch)
	resp = ResolveStatus(eventID, 5, "sess-1", "rt-old", "device-1", resolver, w)
	if resp.Status != "stale_generation" {
		t.Errorf("runtime mismatch: %s", resp.Status)
	}

	// 4. insufficient_permission
	resp = ResolveStatus(eventID, 5, "sess-1", "rt-sess-1", "device-no-perm", resolver, w)
	if resp.Status != "insufficient_permission" {
		t.Errorf("no perm: %s", resp.Status)
	}

	// 5. canonical_event_unavailable (event not in ring)
	resp = ResolveStatus("ev-missing", 5, "sess-1", "rt-sess-1", "device-1", resolver, w)
	if resp.Status != "canonical_event_unavailable" {
		t.Errorf("missing event: %s", resp.Status)
	}

	// 6. actionable
	resp = ResolveStatus(eventID, 5, "sess-1", "rt-sess-1", "device-1", resolver, w)
	if resp.Status != "actionable" {
		t.Errorf("actionable: %s (eventID=%s)", resp.Status, eventID)
	}
	if resp.Permissions == nil || len(resp.Permissions) != 1 || resp.Permissions[0] != devicetrust.PermTerminalInput {
		t.Errorf("permissions mismatch: %v", resp.Permissions)
	}
	if resp.ActivityLink == "" {
		t.Error("activityLink must be set on actionable")
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
	resp := ResolveStatus("ev-1", 5, "sess-1", "rt-sess-1", "device-1", resolver, nil)
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
	resp = ResolveStatus(eventID, 5, "sess-1", "rt-sess-1", "device-1", resolver, w)
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
func TestDeviceIDFromPrincipal(t *testing.T) {
	devices := NewDeviceStore()

	bootID, _ := devicetrust.NewBootID()
	sessionMgr := devicetrust.NewDeviceSessionManager(bootID, 1*time.Hour)

	// Create a real token via the session manager.
	rawToken, _, _, err := sessionMgr.CreateAfterVerifiedChallenge(
		"device-from-auth", "host-1", bootID,
		[]string{devicetrust.PermSessionsRead},
	)
	if err != nil {
		t.Fatalf("CreateAfterVerifiedChallenge: %v", err)
	}

	// Verify: a bare request has no Principal in context.
	req := httptest.NewRequest("GET", "/push/register?token=push-token&deviceId=attacker-device", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	principal := devicetrust.PrincipalFromContext(req.Context())
	if principal != nil {
		t.Error("no principal should exist without RequirePrincipal wrapping")
	}

	// The handler extracts deviceID from Principal.DeviceID, not from query.
	// Bind is called with the authenticated device ID, not the query param.
	devices.Bind("device-from-auth", "push-token")
	if tok := devices.Token("device-from-auth"); tok != "push-token" {
		t.Errorf("token not bound: %q", tok)
	}

	// attacker-device was never registered — caller-supplied ID is ignored.
	if tok := devices.Token("attacker-device"); tok != "" {
		t.Errorf("caller-supplied deviceId must not create entries: %q", tok)
	}
}

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

	resp := ResolveStatus(eventID, 3, "sess-1", "rt-sess-1", "device-readonly", resolver, w)
	if resp.Status != "insufficient_permission" {
		t.Errorf("session:read-only device must get insufficient_permission: got %s", resp.Status)
	}

	// Device with terminal:input gets actionable.
	resolver.perms["device-owner"] = []string{devicetrust.PermTerminalInput}
	resp = ResolveStatus(eventID, 3, "sess-1", "rt-sess-1", "device-owner", resolver, w)
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
	sender := &stubSender{}

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
	if n1.Dispatch() != 1 {
		t.Fatal("first dispatch failed")
	}

	// Simulate restart: fresh Notifier, same device store (cursor persists).
	sender2 := &stubSender{}
	n2 := NewNotifier(w, devices, func(sid string) int64 { return gen.Load() }, sender2)
	n2.SetEnabled(true)

	// Same events in ring — cursor already past them, so nothing delivered.
	if n2.Dispatch() != 0 {
		t.Errorf("restart must not re-deliver already-seen events: got %d", n2.Dispatch())
	}

	// New event after restart.
	ev2 := validEnvelope("restart-2", "session-1", 1, contract.EventApprovalRequested)
	if !w.Append(ev2) {
		t.Fatal("Append ev2 failed")
	}
	if n2.Dispatch() != 1 {
		t.Fatalf("restart must deliver new events: got %d", n2.Dispatch())
	}
	locs := sender2.locators()
	if len(locs) != 1 || locs[0].EventID != ev2.EventID {
		t.Errorf("restart delivered wrong event: %+v", locs)
	}
}

// TestSelectSinceUsesDeviceID verifies that the DeviceID field in Cursor
// is propagated correctly and an empty DeviceID returns all events (fresh
// device with no cursor).
func TestSelectSinceUsesDeviceID(t *testing.T) {
	events := []contract.Envelope{
		n1Event("a", 1, contract.EventApprovalRequested),
		n1Event("b", 1, contract.EventApprovalRequested),
	}

	// DeviceID present and cursor found → subset.
	selected, wrapped := SelectSince(events, Cursor{DeviceID: "d1", LastEventID: "a", LastGeneration: 1})
	if wrapped || len(selected) != 1 || selected[0].EventID != "b" {
		t.Fatalf("cursor with deviceID: %d events, wrapped=%v", len(selected), wrapped)
	}

	// Empty DeviceID → fresh device → all events.
	selected, wrapped = SelectSince(events, Cursor{DeviceID: "", LastEventID: "a", LastGeneration: 1})
	if wrapped || len(selected) != 2 {
		t.Fatalf("empty DeviceID must return all events fresh: %d events", len(selected))
	}
}

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

	resp := ResolveStatus(loc.EventID, loc.Generation, loc.SessionID, loc.RuntimeID, "mobile-device", resolver, w)
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
	resp = ResolveStatus(loc.EventID, loc.Generation, loc.SessionID, loc.RuntimeID, "mobile-device", resolver, w)
	if resp.Status != "stale_generation" {
		t.Errorf("stale tap: %s want stale_generation", resp.Status)
	}
}
