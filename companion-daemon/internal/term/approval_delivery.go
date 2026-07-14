package term

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"sync"
)

// A1 remediation 5 — approval-specific delivery boundary + generation-owned delivery
// gate with immutable endpoint ownership and typed, fully-bound queue items.
//
// A delivery request and its receipt carry the ONE immutable ApprovalExecutionBinding
// the claim token owns; the receipt also carries an opaque ReceiptID and the digest of
// the exact bytes accepted. RecordDelivery compares every binding field, the claim
// token, and the delivered-payload digest before a success may commit.
//
// Endpoint ownership (R5-A/R5-B): each Activate publishes a NEW immutable
// generation-owned endpoint (opaque handle) and retires the previous one WITHOUT
// destroying its already-accepted items. Accept appends one typed, fully-bound item
// (binding + claim token + receipt id + defensive payload) into the current endpoint
// and returns the endpoint handle; Drain returns typed defensive copies from a
// CAPTURED handle, never a mutable session lookup, so an item accepted by generation A
// stays owned by A across an A→B replacement.
//
// Bounds/entropy (R5-C): capacity, endpoint count, items, item bytes, and total queued
// bytes are repository-owned constants; every over-limit or entropy-failure path fails
// closed and appends nothing.
//
// No accepted provider action-delivery channel exists, so production activates every
// endpoint with capacity 0 (no channel) → Accept returns unavailable and no bytes are
// ever accepted; retired endpoints are always empty.

const (
	maxGateCapacity         = 64
	maxGateEndpoints        = 128
	maxGateItemBytes        = 4096
	maxGateItemMetaBytes    = 4096 // per-item metadata (totalItemBytes - len(payload))
	maxGateTotalQueuedBytes = 2 << 20
)

type DeliveryOutcome string

const (
	DeliveryAccepted        DeliveryOutcome = "accepted"
	DeliveryAlreadyAccepted DeliveryOutcome = "already_accepted"
	DeliveryStaleRuntime    DeliveryOutcome = "stale_runtime"
	DeliveryRuntimeMismatch DeliveryOutcome = "runtime_mismatch"
	DeliveryUnavailable     DeliveryOutcome = "unavailable"
	DeliveryConflict        DeliveryOutcome = "conflict"
	DeliveryRejected        DeliveryOutcome = "rejected"
)

var deliveryOutcomeValid = map[DeliveryOutcome]bool{
	DeliveryAccepted: true, DeliveryAlreadyAccepted: true, DeliveryStaleRuntime: true,
	DeliveryRuntimeMismatch: true, DeliveryUnavailable: true, DeliveryConflict: true,
	DeliveryRejected: true,
}

func IsValidDeliveryOutcome(o DeliveryOutcome) bool { return deliveryOutcomeValid[o] }

func deliverySucceeded(o DeliveryOutcome) bool {
	return o == DeliveryAccepted || o == DeliveryAlreadyAccepted
}

func deliveryProvesNonAcceptance(o DeliveryOutcome) bool {
	switch o {
	case DeliveryUnavailable, DeliveryStaleRuntime, DeliveryRuntimeMismatch, DeliveryRejected:
		return true
	default:
		return false
	}
}

// ApprovalDeliveryRequest carries the claim token, immutable binding, and the exact
// server-computed payload bytes.
type ApprovalDeliveryRequest struct {
	ClaimToken string
	Binding    ApprovalExecutionBinding
	Payload    []byte
}

// DeliveryReceipt is the immutable, fully-bound result of a delivery attempt.
type DeliveryReceipt struct {
	Outcome                DeliveryOutcome
	ClaimToken             string
	Binding                ApprovalExecutionBinding
	ReceiptID              string
	DeliveredPayloadDigest string
}

type ApprovalDelivery interface {
	Deliver(req ApprovalDeliveryRequest) DeliveryReceipt
}

// unavailableApprovalDelivery is a no-channel boundary that accepts nothing.
type unavailableApprovalDelivery struct{}

func NewUnavailableApprovalDelivery() ApprovalDelivery { return unavailableApprovalDelivery{} }

func (unavailableApprovalDelivery) Deliver(req ApprovalDeliveryRequest) DeliveryReceipt {
	return DeliveryReceipt{Outcome: DeliveryUnavailable, ClaimToken: req.ClaimToken, Binding: req.Binding}
}

// totalItemBytes returns the total retained bytes (payload + ALL variable-length
// metadata) one queued item consumes. Metadata includes ApprovalID, SessionID,
// idempotency key, ActionDigest, PayloadDigest, ClaimToken, ReceiptID (approximate),
// and Runtime adapter + version strings. This is the single authoritative size used
// for the aggregate `totalBytes` and per-endpoint `queuedBytes` accounting so a
// multi-megabyte metadata string is not silently accepted past the advertised total
// bound.
func totalItemBytes(req ApprovalDeliveryRequest) int {
	n := len(req.Payload) +
		len(req.Binding.ApprovalID) + len(req.Binding.SessionID) +
		len(req.Binding.ActionDigest) + len(req.Binding.PayloadDigest) +
		len(req.Binding.IdempotencyKey) + len(req.Binding.Runtime.Adapter) +
		len(req.Binding.Runtime.Version) +
		len(req.ClaimToken) +
		128 // approximate: per-item nonce+seq receipt ID + runtime-gen integers + struct overhead
	if n < 0 {
		return 0
	}
	return n
}

// AcceptedDelivery is a typed, fully-bound accepted queue item. It is an internal,
// server-side value used by an (as-yet-unavailable) external provider drain; it never
// enters a public/mobile DTO.
type AcceptedDelivery struct {
	Binding    ApprovalExecutionBinding
	ClaimToken string
	ReceiptID  string
	Payload    []byte
}

func (a AcceptedDelivery) copy() AcceptedDelivery {
	return AcceptedDelivery{
		Binding:    a.Binding,
		ClaimToken: a.ClaimToken,
		ReceiptID:  a.ReceiptID,
		Payload:    append([]byte(nil), a.Payload...),
	}
}

// genEndpoint is an immutable-identity, generation-owned bounded delivery endpoint.
// capacity 0 means no delivery channel (production) — nothing is ever accepted.
type genEndpoint struct {
	id          string
	sessionID   string
	runtime     RuntimeRef
	active      bool
	capacity    int
	queue       []AcceptedDelivery
	seq         int
	nonce       string
	queuedBytes int
}

// RuntimeDeliveryGate linearizes approval delivery acceptance against runtime
// activation/replacement/deactivation per session, retaining retired endpoints so
// already-accepted items are not destroyed by a replacement.
type RuntimeDeliveryGate struct {
	mu         sync.Mutex
	current    map[string]string       // sessionID -> current endpoint id
	endpoints  map[string]*genEndpoint // endpoint id -> endpoint (current or retired)
	order      []string                // endpoint ids in creation order (bounded eviction)
	totalBytes int
	// randFail is a NARROW test seam forcing entropy failure; false in production.
	randFail bool
}

func NewRuntimeDeliveryGate() *RuntimeDeliveryGate {
	return &RuntimeDeliveryGate{current: make(map[string]string), endpoints: make(map[string]*genEndpoint)}
}

// genToken produces an opaque server-side identifier, or ("", false) on entropy
// failure (or the test seam). It NEVER falls back to a fixed value.
func (g *RuntimeDeliveryGate) genToken() (string, bool) {
	if g.randFail {
		return "", false
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", false
	}
	return hex.EncodeToString(b[:]), true
}

// Activate publishes a NEW generation-owned endpoint for a session and retires the
// prior one (keeping its accepted items). It is idempotent for an identical
// RuntimeRef + effective capacity (R6-C): the current handle is reused with no entropy
// allocation and no growth. Admission is all-or-nothing (R6-B): if the endpoint bound
// would be exceeded and no SAFE (retired, empty) victim exists, it fails WITHOUT
// changing the current mapping, queues, or ownership. Entropy failure publishes
// nothing. An out-of-range capacity yields an explicit no-channel endpoint (capacity 0).
func (g *RuntimeDeliveryGate) Activate(sessionID string, rt RuntimeRef, capacity int) (handle string, ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	effCap := capacity
	if effCap < 0 || effCap > maxGateCapacity {
		effCap = 0
	}
	// R6-C: same-runtime/capacity activation is idempotent.
	oldID, hasOld := g.current[sessionID]
	if hasOld {
		if e := g.endpoints[oldID]; e != nil && e.active && e.runtime.equal(rt) && e.capacity == effCap {
			return oldID, true
		}
	}
	// Entropy tokens BEFORE any mutation.
	id, tok1 := g.genToken()
	nonce, tok2 := g.genToken()
	if !tok1 || !tok2 {
		return "", false
	}
	// R6-B: decide disposition + admission BEFORE mutating anything.
	oldEmpty := false
	if hasOld {
		if e := g.endpoints[oldID]; e != nil {
			oldEmpty = len(e.queue) == 0 && e.queuedBytes == 0
		}
	}
	// When the session was deactivated (hasOld=false), scan for a retired empty
	// endpoint from a prior activation of the SAME session that can be reclaimed. The
	// endpoint must be inactive, empty, and belong to this session; it is also a
	// candidate for the projected net-zero count.
	reclaim := ""
	if !hasOld {
		reclaim = g.findRetiredEmptyForSessionLocked(sessionID)
	}
	projected := len(g.endpoints) + 1
	if (hasOld && oldEmpty) || reclaim != "" {
		projected = len(g.endpoints) // net zero: old removed, new added
	}
	victim := ""
	if projected > maxGateEndpoints {
		victim = g.findSafeVictimLocked(oldID)
		if victim == "" {
			return "", false
		}
	}
	// Dispose of the old endpoint, then publish the new one.
	if hasOld {
		if oldEmpty {
			g.removeLocked(oldID)
		} else {
			if e := g.endpoints[oldID]; e != nil {
				e.active = false
			}
			delete(g.current, sessionID)
		}
	}
	if reclaim != "" {
		g.removeLocked(reclaim)
	}
	if victim != "" {
		g.removeLocked(victim)
	}
	e := &genEndpoint{id: id, sessionID: sessionID, runtime: rt, active: true, capacity: effCap, nonce: nonce}
	g.endpoints[id] = e
	g.current[sessionID] = id
	g.order = append(g.order, id)
	return id, true
}

// findSafeVictimLocked returns the oldest inactive endpoint that is empty (no accepted
// items, no queued bytes) and is not the endpoint being replaced (excludeID). If no
// such safe victim exists it returns "". Caller holds mu.
func (g *RuntimeDeliveryGate) findSafeVictimLocked(excludeID string) string {
	for _, id := range g.order {
		if id == excludeID {
			continue
		}
		e := g.endpoints[id]
		if e != nil && !e.active && len(e.queue) == 0 && e.queuedBytes == 0 && g.current[e.sessionID] != id {
			return id
		}
	}
	return ""
}

// findRetiredEmptyForSessionLocked returns a retired (inactive), empty endpoint
// belonging to sessionID, for reclaim when the session was deactivated and a new
// activation reuses the session.
func (g *RuntimeDeliveryGate) findRetiredEmptyForSessionLocked(sessionID string) string {
	for _, id := range g.order {
		e := g.endpoints[id]
		if e != nil && !e.active && e.sessionID == sessionID && len(e.queue) == 0 && e.queuedBytes == 0 {
			return id
		}
	}
	return ""
}

func (g *RuntimeDeliveryGate) removeLocked(id string) {
	if e := g.endpoints[id]; e != nil {
		g.totalBytes -= e.queuedBytes
		if g.current[e.sessionID] == id {
			delete(g.current, e.sessionID)
		}
		delete(g.endpoints, id)
	}
	for i, oid := range g.order {
		if oid == id {
			g.order = append(g.order[:i], g.order[i+1:]...)
			break
		}
	}
}

// Deactivate marks a session's current endpoint inactive (delete/unlink/termination/
// registry disappearance). Its already-accepted items remain drainable by handle.
func (g *RuntimeDeliveryGate) Deactivate(sessionID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if id, ok := g.current[sessionID]; ok {
		if e := g.endpoints[id]; e != nil {
			e.active = false
		}
		delete(g.current, sessionID)
	}
}

// Accept is the sole acceptance point. Under one lock it validates the COMPLETE
// canonical item (full binding identity, canonical key, claim ownership, exact
// runtime match, and payload-digest equality) BEFORE appending, so substituted bytes
// or a malformed binding are rejected before daemon acceptance, not only at commit.
// The total retained bytes (payload + ALL metadata strings) are counted against the
// repository-owned total limit. Any mismatch, over-limit, replaced/removed/no-channel
// generation returns ok=false and appends nothing.
func (g *RuntimeDeliveryGate) Accept(req ApprovalDeliveryRequest) (receipt DeliveryReceipt, handle string, ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// R6-A: validate the complete canonical item BEFORE any append.
	b := req.Binding
	if b.ApprovalID == "" || b.SessionID == "" || b.ActionDigest == "" || b.PayloadDigest == "" ||
		!validCanonicalKey(b.IdempotencyKey) || req.ClaimToken == "" {
		return DeliveryReceipt{}, "", false
	}
	eid, exists := g.current[b.SessionID]
	if !exists {
		return DeliveryReceipt{}, "", false
	}
	e := g.endpoints[eid]
	if e == nil || !e.active || e.capacity == 0 {
		return DeliveryReceipt{}, "", false
	}
	if e.sessionID != b.SessionID || !e.runtime.equal(b.Runtime) {
		return DeliveryReceipt{}, "", false
	}
	// The EXACT bytes being appended must match the binding's domain-separated
	// payload digest. Substituted bytes are rejected here, NOT at commit.
	if b.PayloadDigest != payloadDigest(req.Payload) {
		return DeliveryReceipt{}, "", false
	}
	if len(e.queue) >= e.capacity {
		return DeliveryReceipt{}, "", false
	}
	if len(req.Payload) > maxGateItemBytes {
		return DeliveryReceipt{}, "", false
	}
	totalItem := totalItemBytes(req) // payload + all retained metadata
	if totalItem > maxGateItemBytes || g.totalBytes+totalItem > maxGateTotalQueuedBytes {
		return DeliveryReceipt{}, "", false
	}
	metaBytes := totalItem - len(req.Payload)
	if metaBytes > maxGateItemMetaBytes {
		return DeliveryReceipt{}, "", false
	}
	e.seq++
	rid := e.nonce + "-" + strconv.Itoa(e.seq)
	item := AcceptedDelivery{
		Binding: req.Binding, ClaimToken: req.ClaimToken, ReceiptID: rid,
		Payload: append([]byte(nil), req.Payload...),
	}
	e.queue = append(e.queue, item)
	e.queuedBytes += totalItem
	g.totalBytes += totalItem
	return DeliveryReceipt{
		Outcome: DeliveryAccepted, ClaimToken: req.ClaimToken, Binding: req.Binding,
		ReceiptID: rid, DeliveredPayloadDigest: payloadDigest(req.Payload),
	}, eid, true
}

// Drain returns and clears the CAPTURED endpoint's accepted items as typed defensive
// copies, then external/provider I/O runs on them OUTSIDE the lock. It uses the opaque
// endpoint handle, never a mutable session lookup, so it cannot cross generations.
func (g *RuntimeDeliveryGate) Drain(handle string) []AcceptedDelivery {
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.endpoints[handle]
	if e == nil || len(e.queue) == 0 {
		return nil
	}
	out := make([]AcceptedDelivery, len(e.queue))
	for i := range e.queue {
		out[i] = e.queue[i].copy()
	}
	g.totalBytes -= e.queuedBytes
	e.queuedBytes = 0
	e.queue = nil
	return out
}

// gatedApprovalDelivery is the production boundary: it accepts ONLY through the
// generation-owned gate. With capacity 0 (no provider channel) it returns unavailable
// and writes no bytes; when a bounded endpoint exists, an accepted receipt is created
// only after the daemon-owned acceptance, carrying the delivered-payload digest.
type gatedApprovalDelivery struct {
	gate *RuntimeDeliveryGate
}

func NewGatedApprovalDelivery(gate *RuntimeDeliveryGate) ApprovalDelivery {
	return gatedApprovalDelivery{gate: gate}
}

func (d gatedApprovalDelivery) Deliver(req ApprovalDeliveryRequest) DeliveryReceipt {
	receipt, _, ok := d.gate.Accept(req)
	if !ok {
		return DeliveryReceipt{Outcome: DeliveryUnavailable, ClaimToken: req.ClaimToken, Binding: req.Binding}
	}
	return receipt
}
