package term

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

// R9-A mandatory coverage: exact-limit / one-over bounds and canonical grammar for
// every retained identity field at BOTH authority boundaries (Activate, Accept),
// aggregate byte exhaustion that actually reaches the global bound, the combined
// payload+metadata charged-item boundary, and payload non-aliasing. Every rejection
// asserts that no accounting structure mutated.

func r9rt() RuntimeRef {
	return RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
}

// itemSnap is a defensive deep copy of one queued AcceptedDelivery. Payload is stored
// as a string so it is an immutable copy (a later in-place mutation of the live queue's
// payload bytes cannot alias it), and Binding/ClaimToken/ReceiptID are compared by
// content via reflect.DeepEqual.
type itemSnap struct {
	binding    ApprovalExecutionBinding
	claimToken string
	receiptID  string
	payload    string
}

// endpointSnap captures a single endpoint's full identity + accounting content AND the
// full content of every queued item, not just its existence/length, so a destructive
// delete+replace (equal count) or an in-place mutation of an existing queued item
// (equal length/bytes) is detected.
type endpointSnap struct {
	sessionID   string
	active      bool
	capacity    int
	queuedBytes int
	seq         int
	nonce       string
	adapter     string
	version     string
	launchGen   int64
	streamGen   int
	queue       []itemSnap
}

// gateSnap is a DEEP snapshot of the gate: the exact current mapping (sid→id), the
// order slice (handle sequence), every endpoint's content INCLUDING its queued items,
// and the global byte total.
type gateSnap struct {
	current    map[string]string
	order      []string
	endpoints  map[string]endpointSnap
	totalBytes int
}

func snapshotGate(g *RuntimeDeliveryGate) gateSnap {
	g.mu.Lock()
	defer g.mu.Unlock()
	s := gateSnap{
		current:    map[string]string{},
		order:      append([]string(nil), g.order...),
		endpoints:  map[string]endpointSnap{},
		totalBytes: g.totalBytes,
	}
	for sid, id := range g.current {
		s.current[sid] = id
	}
	for id, e := range g.endpoints {
		var q []itemSnap
		for i := range e.queue {
			it := e.queue[i]
			q = append(q, itemSnap{
				binding:    it.Binding,
				claimToken: it.ClaimToken,
				receiptID:  it.ReceiptID,
				payload:    string(it.Payload), // immutable defensive copy
			})
		}
		s.endpoints[id] = endpointSnap{
			sessionID: e.sessionID, active: e.active, capacity: e.capacity,
			queuedBytes: e.queuedBytes, seq: e.seq, nonce: e.nonce,
			adapter: e.runtime.Adapter, version: e.runtime.Version,
			launchGen: e.runtime.LaunchGen, streamGen: e.runtime.StreamGen,
			queue: q,
		}
	}
	return s
}

// assertUnchanged requires the ENTIRE gate state — current mapping, order sequence,
// per-endpoint identity/ownership/accounting, and the full content of every queued
// item (Binding, ClaimToken, ReceiptID, Payload) — to be byte-for-byte identical.
// Count-only equality is insufficient (blockers 4 and R11-1).
func (want gateSnap) assertUnchanged(t *testing.T, g *RuntimeDeliveryGate, ctx string) {
	t.Helper()
	got := snapshotGate(g)
	if !reflect.DeepEqual(want, got) {
		t.Errorf("%s: gate state changed on a rejected operation:\n before=%+v\n after =%+v", ctx, want, got)
	}
}

// ── Activate-boundary bounds: SessionID, Runtime.Adapter, Runtime.Version ──

func TestDeliveryGate_ActivateBoundsExactAndOneOver(t *testing.T) {
	rt := r9rt()
	// SessionID: "codex:" (6) + local. Exact = 512 total → accept; 513 → reject.
	sidExact := "codex:" + makeLargeStr(maxSessionIDLen-6)
	sidOver := "codex:" + makeLargeStr(maxSessionIDLen-6+1)
	if len(sidExact) != maxSessionIDLen || len(sidOver) != maxSessionIDLen+1 {
		t.Fatalf("fixture length: exact=%d over=%d", len(sidExact), len(sidOver))
	}
	if h, ok := g4Activate(t, sidExact, rt); !ok || h == "" {
		t.Errorf("exact-limit SessionID must activate")
	}
	g := NewRuntimeDeliveryGate()
	before := snapshotGate(g)
	if h, ok := g.Activate(sidOver, rt, 4); ok || h != "" {
		t.Errorf("one-over SessionID must fail closed, got %q", h)
	}
	before.assertUnchanged(t, g, "sessionID one-over")

	// Runtime.Adapter: grammar [a-z][a-z0-9_-]*, bound maxVersionLen (64).
	adExact := makeLargeStr(maxVersionLen)    // 64 × 'x' — valid grammar
	adOver := makeLargeStr(maxVersionLen + 1) // 65 × 'x' — over bound
	if h, ok := g4Activate(t, "codex:s1", RuntimeRef{Adapter: adExact, Version: "1.0"}); !ok || h == "" {
		t.Errorf("exact-limit adapter must activate")
	}
	g2 := NewRuntimeDeliveryGate()
	b2 := snapshotGate(g2)
	if h, ok := g2.Activate("codex:s1", RuntimeRef{Adapter: adOver, Version: "1.0"}, 4); ok || h != "" {
		t.Errorf("one-over adapter must fail closed, got %q", h)
	}
	b2.assertUnchanged(t, g2, "adapter one-over")

	// Runtime.Version: grammar [A-Za-z0-9][A-Za-z0-9._-]{0,63}, bound 64.
	vExact := makeLargeStr(maxVersionLen)    // 64 × 'x'
	vOver := makeLargeStr(maxVersionLen + 1) // 65 × 'x'
	if h, ok := g4Activate(t, "codex:s1", RuntimeRef{Adapter: "codex", Version: vExact}); !ok || h == "" {
		t.Errorf("exact-limit version must activate")
	}
	g3 := NewRuntimeDeliveryGate()
	b3 := snapshotGate(g3)
	if h, ok := g3.Activate("codex:s1", RuntimeRef{Adapter: "codex", Version: vOver}, 4); ok || h != "" {
		t.Errorf("one-over version must fail closed, got %q", h)
	}
	b3.assertUnchanged(t, g3, "version one-over")
}

func g4Activate(t *testing.T, sid string, rt RuntimeRef) (string, bool) {
	t.Helper()
	g := NewRuntimeDeliveryGate()
	return g.Activate(sid, rt, 4)
}

// ── Accept-boundary bounds: ApprovalID exact/one-over + re-validated identity ──

func TestDeliveryGate_AcceptApprovalIDBounds(t *testing.T) {
	rt := r9rt()
	// exact 256 → accepted.
	g := NewRuntimeDeliveryGate()
	g.Activate("codex:s1", rt, 4)
	idExact := makeLargeStr(authMaxApprovalIDLen)
	if _, _, ok := g.Accept(gateReq("codex:s1", idExact, "k1", dig("d"), rt, []byte("x"))); !ok {
		t.Errorf("exact-limit ApprovalID must be accepted")
	}
	// one-over 257 → rejected, state unchanged.
	g2 := NewRuntimeDeliveryGate()
	g2.Activate("codex:s1", rt, 4)
	before := snapshotGate(g2)
	idOver := makeLargeStr(authMaxApprovalIDLen + 1)
	if _, _, ok := g2.Accept(gateReq("codex:s1", idOver, "k1", dig("d"), rt, []byte("x"))); ok {
		t.Errorf("one-over ApprovalID must be rejected")
	}
	before.assertUnchanged(t, g2, "approvalID one-over")
}

// Accept independently re-validates identity grammar (a valid endpoint exists, but the
// request binding carries a malformed identity → rejected before any mutation).
func TestDeliveryGate_AcceptRevalidatesIdentityGrammar(t *testing.T) {
	rt := r9rt()
	cases := []struct {
		desc string
		mut  func(r *ApprovalDeliveryRequest)
	}{
		{"session-control", func(r *ApprovalDeliveryRequest) { r.Binding.SessionID = "codex:a\x01b" }},
		{"session-adapter-space", func(r *ApprovalDeliveryRequest) { r.Binding.SessionID = "cod ex:s1" }},
		{"session-uppercase-adapter", func(r *ApprovalDeliveryRequest) { r.Binding.SessionID = "Codex:s1" }},
		{"adapter-uppercase", func(r *ApprovalDeliveryRequest) { r.Binding.Runtime.Adapter = "Codex" }},
		{"version-slash", func(r *ApprovalDeliveryRequest) { r.Binding.Runtime.Version = "1/0" }},
		{"version-backslash", func(r *ApprovalDeliveryRequest) { r.Binding.Runtime.Version = "1\\0" }},
		{"version-traversal", func(r *ApprovalDeliveryRequest) { r.Binding.Runtime.Version = "../x" }},
		{"version-leading-dot", func(r *ApprovalDeliveryRequest) { r.Binding.Runtime.Version = ".1" }},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			g := NewRuntimeDeliveryGate()
			g.Activate("codex:s1", rt, 4)
			before := snapshotGate(g)
			r := gateReq("codex:s1", "a1", "k1", dig("d"), rt, []byte("x"))
			c.mut(&r)
			if _, _, ok := g.Accept(r); ok {
				t.Fatalf("%s: malformed identity accepted", c.desc)
			}
			before.assertUnchanged(t, g, c.desc)
		})
	}
}

// ── Combined payload+metadata charged-item boundary (exact 4096 / one-over 4097) ──

func TestDeliveryGate_ChargedItemBoundaryExactAndOneOver(t *testing.T) {
	rt := r9rt()
	sid, approval, key, ad := "codex:s1", "a1", "k1", dig("d")
	// charged(payload=nil) is the fixed base; grow payload to hit maxGateItemBytes exactly.
	base := chargedItemBytes(gateReq(sid, approval, key, ad, rt, nil))
	pLen := maxGateItemBytes - base
	if pLen <= 0 {
		t.Fatalf("base %d ≥ item limit %d", base, maxGateItemBytes)
	}
	exact := gateReq(sid, approval, key, ad, rt, make([]byte, pLen))
	if got := chargedItemBytes(exact); got != maxGateItemBytes {
		t.Fatalf("charged=%d, want exactly %d", got, maxGateItemBytes)
	}
	g := NewRuntimeDeliveryGate()
	g.Activate(sid, rt, 4)
	if _, _, ok := g.Accept(exact); !ok {
		t.Errorf("exact charged-item boundary (%d) must be accepted", maxGateItemBytes)
	}
	// one-over: +1 payload byte → charged 4097 → rejected, state unchanged.
	g2 := NewRuntimeDeliveryGate()
	g2.Activate(sid, rt, 4)
	before := snapshotGate(g2)
	over := gateReq(sid, approval, key, ad, rt, make([]byte, pLen+1))
	if got := chargedItemBytes(over); got != maxGateItemBytes+1 {
		t.Fatalf("one-over charged=%d, want %d", got, maxGateItemBytes+1)
	}
	if _, _, ok := g2.Accept(over); ok {
		t.Errorf("one-over charged-item (%d) must be rejected", maxGateItemBytes+1)
	}
	before.assertUnchanged(t, g2, "charged one-over")
}

// ── Aggregate exhaustion that actually REACHES the global bound ──
//
// A single endpoint is capacity-limited to maxGateCapacity (64) items, so no single
// endpoint can reach maxGateTotalQueuedBytes. Deactivated endpoints retain their bytes,
// so the GLOBAL total is accumulated across many retired endpoints until the next
// otherwise-valid item is rejected ONLY by the aggregate g.totalBytes check — proven
// by (a) the item being individually valid, (b) the target endpoint having free
// capacity, and (c) the identical item succeeding on a fresh gate.
func TestDeliveryGate_AggregateExhaustionReachesGlobalBound(t *testing.T) {
	rt := r9rt()
	g := NewRuntimeDeliveryGate()
	// Large, individually-valid items so charged is near the per-item cap; fixed-width
	// session names keep the charged size constant across items.
	payload := make([]byte, 3400)
	c := chargedItemBytes(gateReq("codex:agg000", "aa", "kk", dig("d"), rt, payload))
	if c > maxGateItemBytes {
		t.Fatalf("probe item not individually valid: charged=%d", c)
	}
	n := 0
	for g.totalBytes+c <= maxGateTotalQueuedBytes {
		sid := fmt.Sprintf("codex:agg%03d", n)
		n++
		if _, ok := g.Activate(sid, rt, maxGateCapacity); !ok {
			t.Fatalf("activate %s", sid)
		}
		for i := 0; i < maxGateCapacity && g.totalBytes+c <= maxGateTotalQueuedBytes; i++ {
			if _, _, ok := g.Accept(gateReq(sid, "aa", "kk", dig("d"), rt, payload)); !ok {
				t.Fatalf("fill accept failed at endpoint %s item %d (totalBytes=%d)", sid, i, g.totalBytes)
			}
		}
		g.Deactivate(sid) // retire; bytes retained in the global total
	}
	// We are within one item of the global bound.
	if g.totalBytes <= maxGateTotalQueuedBytes-c {
		t.Fatalf("did not approach global bound: totalBytes=%d (limit %d, item %d)", g.totalBytes, maxGateTotalQueuedBytes, c)
	}
	if g.totalBytes < maxGateTotalQueuedBytes/2 {
		t.Fatalf("aggregate too small to prove global bound: %d", g.totalBytes)
	}
	// Fresh endpoint with free capacity; the SAME item (individually valid) must now be
	// rejected purely by the aggregate check, leaving the total unchanged.
	final := fmt.Sprintf("codex:agg%03d", n)
	if _, ok := g.Activate(final, rt, maxGateCapacity); !ok {
		t.Fatalf("activate final %s", final)
	}
	before := snapshotGate(g)
	if _, _, ok := g.Accept(gateReq(final, "aa", "kk", dig("d"), rt, payload)); ok {
		t.Fatalf("aggregate-exhausting item accepted (totalBytes=%d + %d > %d)", g.totalBytes, c, maxGateTotalQueuedBytes)
	}
	before.assertUnchanged(t, g, "aggregate exhaustion reject")
	// Non-vacuous: the identical item is accepted on a fresh gate (so the rejection was
	// the aggregate bound, not an intrinsic per-item defect).
	fresh := NewRuntimeDeliveryGate()
	fresh.Activate(final, rt, maxGateCapacity)
	if _, _, ok := fresh.Accept(gateReq(final, "aa", "kk", dig("d"), rt, payload)); !ok {
		t.Errorf("item is individually valid but rejected on a fresh gate — aggregate proof is vacuous")
	}
}

// Payload retained by Accept is a defensive copy: mutating the caller's slice after
// acceptance never changes the drained item, and the drained copies are independent.
func TestDeliveryGate_AcceptPayloadNonAliasing(t *testing.T) {
	rt := r9rt()
	g := NewRuntimeDeliveryGate()
	h, _ := g.Activate("codex:s1", rt, 4)
	payload := []byte("original")
	if _, _, ok := g.Accept(gateReq("codex:s1", "a1", "k1", dig("d"), rt, payload)); !ok {
		t.Fatal("accept")
	}
	payload[0] = 'X' // caller mutates its slice after acceptance
	items := g.Drain(h)
	if len(items) != 1 || string(items[0].Payload) != "original" {
		t.Fatalf("payload aliased caller slice: %q", items[0].Payload)
	}
	items[0].Payload[0] = 'Z' // mutate the drained copy
	// A second drain from a fresh accept must be unaffected by the earlier mutation.
	g.Accept(gateReq("codex:s1", "a2", "k2", dig("d"), rt, []byte("second")))
	again := g.Drain(h)
	if len(again) != 1 || string(again[0].Payload) != "second" {
		t.Errorf("drained copy mutation leaked across items: %q", again[0].Payload)
	}
}

// backingPtr returns the address of a string's backing array so a test can prove the
// gate does NOT pin a caller's large backing storage via a short substring (blocker 1).
func backingPtr(s string) uintptr {
	if len(s) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(unsafe.StringData(s)))
}

// R10 blocker 1: a caller can pass a short but valid identity/token that is a substring
// of a multi-megabyte backing string. Go strings share backing storage, so a naive gate
// would pin the whole array through the queued item and the endpoint. Accept and
// Activate must strings.Clone every retained string into a bounded allocation, so the
// retained strings point at NEW backing arrays, independent of the caller's giant one.
func TestDeliveryGate_RetainedStringsAreDeepCloned(t *testing.T) {
	// Each field below is a SHORT but individually-valid substring of a distinct
	// multi-megabyte backing array; retaining any of them naively would pin ~1 MiB.
	const big = 1 << 20
	sidBack := strings.Repeat("codex:s1", big/8) // 1 MiB, starts with a valid session id
	sid := sidBack[:8]                           // "codex:s1"
	ad := strings.Repeat("a", big)[:64]          // 64 lowercase hex
	tok := strings.Repeat("c", big)[:32]         // 32 lowercase hex
	appr := strings.Repeat("z", big)[:4]         // valid approval id
	ver := strings.Repeat("1", big)[:5]          // "11111", valid version grammar

	rt := RuntimeRef{Adapter: "codex", Version: ver, LaunchGen: 5, StreamGen: 2}
	g := NewRuntimeDeliveryGate()
	payload := []byte("p")
	req := ApprovalDeliveryRequest{
		ClaimToken: tok,
		Binding: ApprovalExecutionBinding{
			ApprovalID: appr, SessionID: sid, Runtime: rt,
			ActionDigest: ad, PayloadDigest: payloadDigest(payload), IdempotencyKey: "k1",
		},
		Payload: payload,
	}
	h, ok := g.Activate(sid, rt, 4)
	if !ok || h == "" {
		t.Fatalf("activate: ok=%v", ok)
	}
	if _, _, ok := g.Accept(req); !ok {
		t.Fatalf("accept of valid substring-backed request failed")
	}

	// The endpoint's retained sessionID/version and the queued item's strings must NOT
	// share the caller's giant backing arrays.
	g.mu.Lock()
	e := g.endpoints[h]
	if e == nil {
		g.mu.Unlock()
		t.Fatal("endpoint missing")
	}
	if backingPtr(e.sessionID) == backingPtr(sid) {
		t.Error("endpoint sessionID still shares the caller's backing array")
	}
	if backingPtr(e.runtime.Version) == backingPtr(ver) {
		t.Error("endpoint runtime.Version still shares the caller's backing array")
	}
	item := e.queue[0]
	g.mu.Unlock()
	if backingPtr(item.Binding.SessionID) == backingPtr(sid) {
		t.Error("queued SessionID still shares the caller's backing array")
	}
	if backingPtr(item.Binding.ApprovalID) == backingPtr(appr) {
		t.Error("queued ApprovalID still shares the caller's backing array")
	}
	if backingPtr(item.Binding.ActionDigest) == backingPtr(ad) {
		t.Error("queued ActionDigest still shares the caller's backing array")
	}
	if backingPtr(item.ClaimToken) == backingPtr(tok) {
		t.Error("queued ClaimToken still shares the caller's backing array")
	}
	// Content is preserved exactly (clone copies, never corrupts).
	if item.Binding.SessionID != sid || item.ClaimToken != tok || item.Binding.Runtime.Version != ver {
		t.Errorf("clone altered content: sid=%q tok(len)=%d ver=%q", item.Binding.SessionID, len(item.ClaimToken), item.Binding.Runtime.Version)
	}
}
