package term

import (
	"fmt"
	"sync"
	"testing"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/transcript"
)

// R9-B — production-path activation, explicit capacity states with no ignored
// results, and a deterministic accept-vs-replacement interleaving with a concrete
// unsafe negative control. Replaces the former direct-gate telemetry/exhaustion/
// reclaim tests.

// gateCurrentHandle reads the current endpoint handle for a session and the total
// endpoint count under the gate lock.
func gateCurrentHandle(g *RuntimeDeliveryGate, sid string) (string, int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.current[sid], len(g.endpoints)
}

// ── B1: real TelemetryService.processSession → adapter+launch correlation → Activate ──
//
// Direct gate.Activate calls are forbidden as evidence here. The gate is driven ONLY
// through s1cPoll → processSession, exactly as production wires it (telemetry_service
// line ~331). Repeated same-runtime polls preserve one handle; a real launch-generation
// change (production RegisterOrReplaceLaunch) makes exactly one replacement; the next
// same-generation poll makes none; correlation loss deactivates the endpoint.
func TestDeliveryGate_ProductionTelemetryPathActivation(t *testing.T) {
	dir := t.TempDir()
	logPath := dir + "/codex.jsonl"
	writeLines(t, logPath, []string{
		`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","cli_version":"0.144.1"}}`,
		`{"timestamp":"2026-07-06T13:29:37.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","approval_id":"appr-1"}}`,
	})
	sid := "controlled_pty:b1codex"
	svc, sess := s1cSvc(t, "codex", logPath, sid)
	gate := NewRuntimeDeliveryGate()
	svc.SetDeliveryGate(gate)
	defer transcript.RemoveLaunch(sid)
	spec := transcript.LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1"}
	transcript.RegisterFirstLaunch(spec)

	// Poll 1 establishes the endpoint through the production path.
	s1cPoll(svc, sess, sid, "codex")
	// Correlation must have been ManagedLaunch (the only branch that activates) —
	// confirmed by the produced waiting_approval status.
	if rec, _, ok := svc.statusStore.Current(sid); !ok || rec.Status != agent.StatusWaitingApproval {
		t.Fatalf("expected managed-launch waiting_approval status, got %+v ok=%v", rec, ok)
	}
	h1, count1 := gateCurrentHandle(gate, sid)
	if h1 == "" {
		t.Fatal("production processSession path did not activate a delivery endpoint")
	}

	// Repeated same-RuntimeRef polls: idempotent — same handle, same endpoint count.
	for i := 0; i < 5; i++ {
		s1cPoll(svc, sess, sid, "codex")
		h, count := gateCurrentHandle(gate, sid)
		if h != h1 || count != count1 {
			t.Fatalf("poll %d: handle %q→%q, endpoints %d→%d (repeated same-runtime poll must be idempotent)",
				i, h1, h, count1, count)
		}
	}

	// A REAL launch-generation change through the production boundary. This resets the
	// adapter ingestion epoch and deactivates the old delivery generation
	// (invalidateForLaunch), exactly as production relaunch does; the relaunch then
	// writes fresh log content that re-establishes the managed correlation.
	if _, replaced := svc.RegisterOrReplaceLaunch(spec); !replaced {
		t.Fatal("RegisterOrReplaceLaunch should report a replacement")
	}
	appendLines(t, logPath, []string{
		`{"timestamp":"2026-07-06T13:30:10.000Z","type":"session_meta","payload":{"session_id":"s1","cli_version":"0.144.1"}}`,
		`{"timestamp":"2026-07-06T13:30:11.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","approval_id":"appr-2"}}`,
	})
	s1cPoll(svc, sess, sid, "codex")
	h2, _ := gateCurrentHandle(gate, sid)
	if h2 == "" || h2 == h1 {
		t.Fatalf("launch-generation change must produce exactly one new handle: %q→%q", h1, h2)
	}
	// Exact ownership after replacement (blocker R11-2): the empty old generation is
	// reclaimed net-zero, so order/current/endpoints hold EXACTLY the new handle h2 —
	// a stale h1 left in order (equal length) must NOT pass.
	gate.mu.Lock()
	if len(gate.order) != 1 || gate.order[0] != h2 {
		t.Errorf("order after replacement = %v, want exactly [%s]", gate.order, h2)
	}
	if len(gate.current) != 1 || gate.current[sid] != h2 {
		t.Errorf("current after replacement = %v, want {%s:%s}", gate.current, sid, h2)
	}
	if len(gate.endpoints) != 1 || gate.endpoints[h2] == nil {
		t.Errorf("endpoints after replacement have %d entries, want exactly {%s}", len(gate.endpoints), h2)
	}
	if gate.endpoints[h1] != nil {
		t.Errorf("old generation %q not reclaimed after replacement", h1)
	}
	gate.mu.Unlock()

	// Next poll at the SAME generation (no new content) makes no further replacement and
	// leaves the exact state intact.
	s1cPoll(svc, sess, sid, "codex")
	h3, _ := gateCurrentHandle(gate, sid)
	if h3 != h2 {
		t.Fatalf("same-generation poll must not replace the handle: %q→%q", h2, h3)
	}
	gate.mu.Lock()
	if len(gate.order) != 1 || gate.order[0] != h2 || len(gate.endpoints) != 1 || gate.current[sid] != h2 {
		t.Errorf("same-generation poll perturbed state: order=%v current=%v endpoints=%d", gate.order, gate.current, len(gate.endpoints))
	}
	gate.mu.Unlock()

	// Correlation loss (launch binding removed) deactivates acceptance on the next poll.
	// Deactivate clears the current mapping and marks the endpoint inactive but RETAINS
	// it (empty → future safe victim): assert the exact intended state.
	transcript.RemoveLaunch(sid)
	s1cPoll(svc, sess, sid, "codex")
	gate.mu.Lock()
	if len(gate.current) != 0 {
		t.Errorf("current after correlation loss = %v, want empty", gate.current)
	}
	if len(gate.order) != 1 || gate.order[0] != h2 {
		t.Errorf("order after correlation loss = %v, want [%s] (endpoint retained)", gate.order, h2)
	}
	if len(gate.endpoints) != 1 {
		t.Errorf("endpoints after correlation loss = %d, want exactly 1 (retained inactive)", len(gate.endpoints))
	}
	if e := gate.endpoints[h2]; e == nil || e.active {
		t.Errorf("endpoint %q after correlation loss must be retained and inactive: %+v", h2, e)
	}
	gate.mu.Unlock()
	// Acceptance is genuinely disabled (not merely unmapped): a fully-canonical request
	// for this session is rejected. (This is a deactivation assertion, not production
	// activation evidence — B1's activation proof is the processSession path above.)
	if _, _, ok := gate.Accept(gateReq(sid, "a1", "k1", dig("d"), RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 3, StreamGen: 0}, []byte("x"))); ok {
		t.Error("Accept must fail after correlation-loss deactivation")
	}
}

// ── B2: explicit capacity states, no ignored ok/handle/receipt/drain ──

// B2.1 — all slots active: an extra activation fails with all state unchanged.
func TestDeliveryGate_CapacityAllActiveFailsUnchanged(t *testing.T) {
	rt := r9rt()
	g := NewRuntimeDeliveryGate()
	for i := 0; i < maxGateEndpoints; i++ {
		h, ok := g.Activate(fmt.Sprintf("codex:act%d", i), rt, 0)
		if !ok || h == "" {
			t.Fatalf("activate %d: ok=%v h=%q", i, ok, h)
		}
	}
	before := snapshotGate(g)
	if len(before.endpoints) != maxGateEndpoints {
		t.Fatalf("precondition: %d active endpoints, want %d", len(before.endpoints), maxGateEndpoints)
	}
	h, ok := g.Activate("codex:overflow", rt, 0)
	if ok || h != "" {
		t.Fatalf("all-active overflow must fail closed, got h=%q ok=%v", h, ok)
	}
	before.assertUnchanged(t, g, "all-active overflow")
}

// B2.2 — a retired NON-EMPTY endpoint at the bound is not a safe victim: an extra
// activation fails before drain, and the exact item/receipt/bytes remain.
func TestDeliveryGate_CapacityRetiredNonEmptyFailsBeforeDrain(t *testing.T) {
	rt := r9rt()
	g := NewRuntimeDeliveryGate()
	hKeep, ok := g.Activate("codex:keep", rt, 4)
	if !ok || hKeep == "" {
		t.Fatalf("activate keep: ok=%v h=%q", ok, hKeep)
	}
	rcpt, hAccept, ok := g.Accept(gateReq("codex:keep", "aKeep", "kk", dig("d"), rt, []byte("must-survive")))
	if !ok || hAccept != hKeep || rcpt.ReceiptID == "" {
		t.Fatalf("accept keep: ok=%v handle=%q receipt=%q", ok, hAccept, rcpt.ReceiptID)
	}
	g.Deactivate("codex:keep") // retired, still holds its item
	// Fill the remaining slots with active endpoints so the total reaches the bound.
	for i := 0; i < maxGateEndpoints-1; i++ {
		if h, ok := g.Activate(fmt.Sprintf("codex:f%d", i), rt, 0); !ok || h == "" {
			t.Fatalf("fill %d: ok=%v h=%q", i, ok, h)
		}
	}
	before := snapshotGate(g)
	if len(before.endpoints) != maxGateEndpoints {
		t.Fatalf("precondition endpoints=%d, want %d", len(before.endpoints), maxGateEndpoints)
	}
	if len(before.endpoints[hKeep].queue) != 1 || before.endpoints[hKeep].queuedBytes == 0 {
		t.Fatalf("precondition: keep must hold 1 item with bytes, got queue=%d bytes=%d",
			len(before.endpoints[hKeep].queue), before.endpoints[hKeep].queuedBytes)
	}
	// The only retired endpoint is non-empty → no safe victim → extra activation fails.
	if h, ok := g.Activate("codex:extra", rt, 0); ok || h != "" {
		t.Fatalf("activation must fail with only a non-empty retired endpoint, got h=%q ok=%v", h, ok)
	}
	before.assertUnchanged(t, g, "retired-nonempty overflow")
	// The retained item survives intact and is drainable by its captured handle.
	items := g.Drain(hKeep)
	if len(items) != 1 || string(items[0].Payload) != "must-survive" || items[0].ReceiptID != rcpt.ReceiptID {
		t.Fatalf("retained item lost/altered: %+v (want receipt %q)", items, rcpt.ReceiptID)
	}
}

// B2.3 — draining+deactivating one endpoint yields a deterministic safe victim that a
// new activation reclaims, while another active endpoint and its item are untouched.
func TestDeliveryGate_CapacitySafeVictimReclaimedDeterministically(t *testing.T) {
	rt := r9rt()
	g := NewRuntimeDeliveryGate()
	// An unrelated endpoint that must remain untouched.
	hOther, ok := g.Activate("codex:other", rt, 4)
	if !ok || hOther == "" {
		t.Fatalf("activate other: ok=%v", ok)
	}
	if _, h, ok := g.Accept(gateReq("codex:other", "aO", "kO", dig("d"), rt, []byte("other-item"))); !ok || h != hOther {
		t.Fatalf("accept other: ok=%v h=%q", ok, h)
	}
	// The victim: accept then drain (empty) then deactivate → retired+empty = safe victim.
	hVictim, ok := g.Activate("codex:victim", rt, 4)
	if !ok || hVictim == "" {
		t.Fatalf("activate victim: ok=%v", ok)
	}
	if _, h, ok := g.Accept(gateReq("codex:victim", "aV", "kV", dig("d"), rt, []byte("victim-item"))); !ok || h != hVictim {
		t.Fatalf("accept victim: ok=%v h=%q", ok, h)
	}
	if drained := g.Drain(hVictim); len(drained) != 1 {
		t.Fatalf("drain victim returned %d items, want 1", len(drained))
	}
	g.Deactivate("codex:victim")
	// Fill to the bound: other(1) + victim(1) + fill = maxGateEndpoints.
	for i := 0; i < maxGateEndpoints-2; i++ {
		if h, ok := g.Activate(fmt.Sprintf("codex:g%d", i), rt, 0); !ok || h == "" {
			t.Fatalf("fill %d: ok=%v", i, ok)
		}
	}
	countBefore := 0
	g.mu.Lock()
	countBefore = len(g.endpoints)
	g.mu.Unlock()
	if countBefore != maxGateEndpoints {
		t.Fatalf("precondition endpoints=%d, want %d", countBefore, maxGateEndpoints)
	}
	// A new session at the bound reclaims exactly the safe (retired+empty) victim.
	hNew, ok := g.Activate("codex:new", rt, 0)
	if !ok || hNew == "" {
		t.Fatalf("activation with a safe victim available must succeed, got h=%q ok=%v", hNew, ok)
	}
	g.mu.Lock()
	victimGone := g.endpoints[hVictim] == nil
	countAfter := len(g.endpoints)
	g.mu.Unlock()
	if !victimGone {
		t.Error("safe victim was not reclaimed")
	}
	if countAfter != maxGateEndpoints {
		t.Errorf("reclaim should be net-zero: %d→%d", countBefore, countAfter)
	}
	// The unrelated endpoint and its item are untouched.
	items := g.Drain(hOther)
	if len(items) != 1 || string(items[0].Payload) != "other-item" {
		t.Errorf("unrelated endpoint's item disturbed by reclaim: %+v", items)
	}
}

// ── B3: deterministic accept-vs-replacement interleaving + unsafe negative control ──
//
// An accept bound to generation A ENTERS before an A→B replacement. Using the narrow
// acceptEntryHook, the test pauses the accept at a named contested point, drives the
// replacement to completion, inspects the contested state (current mapping, endpoint
// ownership, queue, item count, sequence, byte totals), then releases the accept. The
// single-lock re-read of current at commit rejects the stale accept. A staleGate
// negative control reproduces the destructive admission the real gate prevents.
func TestDeliveryGate_DeterministicAcceptVsReplacementInterleaving(t *testing.T) {
	rtA := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	rtB := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 6, StreamGen: 0}

	g := NewRuntimeDeliveryGate()
	hA, ok := g.Activate("codex:s1", rtA, 4)
	if !ok || hA == "" {
		t.Fatalf("activate A: ok=%v", ok)
	}
	reqA := gateReq("codex:s1", "a1", "k1", dig("adA"), rtA, []byte("A-bytes"))

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	g.acceptEntryHook = func() {
		once.Do(func() {
			close(entered)
			<-release
		})
	}

	acceptOK := make(chan bool, 1)
	go func() { _, _, ok := g.Accept(reqA); acceptOK <- ok }()

	<-entered // G1 is paused at accept entry, BEFORE the lock.

	// Contested point: drive the replacement to completion.
	hB, ok := g.Activate("codex:s1", rtB, 4)
	if !ok || hB == "" || hB == hA {
		t.Fatalf("replacement activate: ok=%v hB=%q hA=%q", ok, hB, hA)
	}
	// Inspect the contested intermediate state while the accept is still paused. The
	// old A endpoint was empty (production capacity 0, no committed item), so the
	// replacement reclaims it net-zero: A is REMOVED and B is the sole current endpoint.
	g.mu.Lock()
	if g.current["codex:s1"] != hB {
		t.Errorf("current mapping not B: %q", g.current["codex:s1"])
	}
	if g.endpoints[hA] != nil {
		t.Errorf("empty retired A must be reclaimed (removed) at the contested point: %+v", g.endpoints[hA])
	}
	if e := g.endpoints[hB]; e == nil || !e.active || len(e.queue) != 0 || e.queuedBytes != 0 || e.seq != 0 {
		t.Errorf("B must be the current, active, empty, seq-0 endpoint: %+v", e)
	}
	// Endpoint count and order sequence at the contested point: exactly one endpoint
	// (B), and order holds only hB — A left no residue.
	if len(g.endpoints) != 1 {
		t.Errorf("endpoint count at contested point = %d, want 1 (A reclaimed)", len(g.endpoints))
	}
	if len(g.order) != 1 || g.order[0] != hB {
		t.Errorf("order at contested point = %v, want [%s]", g.order, hB)
	}
	if g.totalBytes != 0 {
		t.Errorf("no bytes must be accounted at the contested point, got %d", g.totalBytes)
	}
	g.mu.Unlock()

	close(release) // let the stale accept proceed to the lock.
	if <-acceptOK {
		t.Fatal("stale accept bound to retired generation A was admitted after replacement")
	}
	// Post-release: nothing landed in either generation; accounting is clean.
	if items := g.Drain(hA); len(items) != 0 {
		t.Errorf("retired A gained an item: %+v", items)
	}
	if items := g.Drain(hB); len(items) != 0 {
		t.Errorf("B gained the stale item: %+v", items)
	}
	g.mu.Lock()
	tb := g.totalBytes
	g.mu.Unlock()
	if tb != 0 {
		t.Errorf("byte accounting corrupted by the interleaving: %d", tb)
	}

	// Negative control: a check-then-write gate that captures the target endpoint at
	// accept entry and appends after the replacement ADMITS the stale item into the
	// retired generation — the exact destructive admission the atomic gate forbids.
	sg := newStaleGate()
	eA := sg.activate("codex:s1", rtA)
	captured := sg.resolve("codex:s1") // captured == eA at entry
	if captured != eA {
		t.Fatal("negative control setup: resolve did not capture the active endpoint")
	}
	sg.activate("codex:s1", rtB)           // replacement in the contested window
	sg.commit(captured, []byte("A-bytes")) // appends to the now-retired eA
	if len(eA.queue) != 1 || eA.active {
		t.Fatalf("negative control must reproduce a stale admission into the retired gen: queue=%d active=%v", len(eA.queue), eA.active)
	}
}

// staleGate models a delivery gate that resolves the target endpoint at accept ENTRY
// and writes to that captured endpoint later without re-checking whether it is still
// current — the vulnerable check-then-write pattern. Used only as B3's negative control.
type staleEndpoint struct {
	rt     RuntimeRef
	active bool
	queue  [][]byte
}

type staleGate struct {
	mu      sync.Mutex
	current map[string]*staleEndpoint
}

func newStaleGate() *staleGate { return &staleGate{current: map[string]*staleEndpoint{}} }

func (g *staleGate) activate(sid string, rt RuntimeRef) *staleEndpoint {
	g.mu.Lock()
	defer g.mu.Unlock()
	if e := g.current[sid]; e != nil {
		e.active = false
	}
	e := &staleEndpoint{rt: rt, active: true}
	g.current[sid] = e
	return e
}

func (g *staleGate) resolve(sid string) *staleEndpoint {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.current[sid]
}

func (g *staleGate) commit(e *staleEndpoint, payload []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	e.queue = append(e.queue, payload)
}
