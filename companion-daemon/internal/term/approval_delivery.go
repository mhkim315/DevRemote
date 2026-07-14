package term

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"sync"

	"devremote/companion-daemon/internal/mux"
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

// ── R8-A: canonical metadata validators ──

// isHex64 reports whether s is exactly 64 lowercase hex characters (SHA-256).
func isHex64(s string) bool { return len(s) == 64 && allHex(s) }

func allHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// validClaimToken reports whether t uses the canonical opaque-token encoding
// (32 hex chars, the store's newClaimToken).
func validClaimToken(t string) bool { return len(t) == 32 && allHex(t) }

// validSessionID reports whether s is a CANONICAL compound session ID. R9-A: length
// alone is insufficient — control characters, whitespace, path-shaped strings and
// invalid adapter grammar must be rejected. The single canonical boundary in
// internal/mux is reused: parse once, require a non-empty adapter and local part, an
// exact canonical round trip, SessionRef.Validate (rejects control chars) and
// ValidateAdapterName ([a-z][a-z0-9_-]*) on the adapter.
func validSessionID(s string) bool {
	if len(s) == 0 || len(s) > maxSessionIDLen {
		return false
	}
	ref := mux.ParseSessionID(s)
	if ref.Adapter == "" || ref.LocalID == "" {
		return false
	}
	if ref.Canonical() != s {
		return false
	}
	if ref.Validate() != nil {
		return false
	}
	return mux.ValidateAdapterName(ref.Adapter) == nil
}

// validApprovalID is the store's bound: non-empty, max 256 chars.
func validApprovalID(s string) bool { return len(s) > 0 && len(s) <= authMaxApprovalIDLen }

// validAdapterID validates a runtime adapter/provider identity with the existing
// accepted adapter grammar (R9-A2), not length alone.
func validAdapterID(s string) bool {
	return len(s) > 0 && len(s) <= maxVersionLen && mux.ValidateAdapterName(s) == nil
}

// validVersion enforces one bounded ASCII version grammar (R9-A: `[A-Za-z0-9]
// [A-Za-z0-9._-]{0,63}`). The first byte must be alphanumeric; the remainder may add
// dot/underscore/dash. Slash, backslash, control characters, whitespace and
// traversal-shaped values (which require a leading dot or a separator) are rejected.
func validVersion(v string) bool {
	if len(v) == 0 || len(v) > maxVersionLen {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			continue
		}
		if i > 0 && (c == '.' || c == '_' || c == '-') {
			continue
		}
		return false
	}
	return true
}

// validGateBindingMeta validates EVERY variable-length canonical field before
// the gate retains it. A non-canonical digest/token/key or an over-length session/
// approval/version/adapter rejects the request before any mutation.
func validGateBindingMeta(req ApprovalDeliveryRequest) bool {
	b := req.Binding
	return b.ApprovalID != "" && b.SessionID != "" && b.ActionDigest != "" && b.PayloadDigest != "" &&
		validCanonicalKey(b.IdempotencyKey) && req.ClaimToken != "" &&
		validSessionID(b.SessionID) && validApprovalID(b.ApprovalID) &&
		validAdapterID(b.Runtime.Adapter) && validVersion(b.Runtime.Version) &&
		isHex64(b.ActionDigest) && isHex64(b.PayloadDigest) &&
		validClaimToken(req.ClaimToken)
}

// gateItemFixedCharge is a CONSERVATIVE, repository-owned fixed per-item accounting
// charge. It is NOT an exact byte count: it covers the derived ReceiptID (nonce hex
// 32 + '-' + up to ~10 seq digits) plus struct/bookkeeping headroom. It is named and
// accounted separately from the exact retained variable bytes so no estimate is ever
// called "exact".
const gateItemFixedCharge = 64

// exactRetainedVariableBytes returns the EXACT number of retained variable-length
// bytes: the payload plus every stored metadata string (approval, session, both
// digests, idempotency key, runtime adapter/version, claim token). It excludes the
// conservative fixed charge.
func exactRetainedVariableBytes(req ApprovalDeliveryRequest) int {
	return len(req.Payload) +
		len(req.Binding.ApprovalID) + len(req.Binding.SessionID) +
		len(req.Binding.ActionDigest) + len(req.Binding.PayloadDigest) +
		len(req.Binding.IdempotencyKey) + len(req.Binding.Runtime.Adapter) +
		len(req.Binding.Runtime.Version) +
		len(req.ClaimToken)
}

// chargedItemBytes is the value admission is metered against:
//
//	exact retained variable bytes + conservative fixed charge.
//
// Overflow safety (no checked-add needed): payload ≤ maxGateItemBytes (4096) and the
// eight identity/digest/token fields are each individually bounded by their validators
// (512+256+64+64+128+64+64+32 = 1184), so exactRetainedVariableBytes < 5280 and
// chargedItemBytes < 5344 for any request that reaches accounting. The running
// g.totalBytes is bounded by maxGateTotalQueuedBytes (2 MiB); g.totalBytes +
// chargedItemBytes < 2 MiB + 6 KiB, far below math.MaxInt32.
func chargedItemBytes(req ApprovalDeliveryRequest) int {
	return exactRetainedVariableBytes(req) + gateItemFixedCharge
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
	// acceptEntryHook is a NARROW test seam invoked at the very start of Accept,
	// BEFORE the gate lock is taken, to force a deterministic accept-vs-replacement
	// interleaving (R9-B3). It is nil in production and is set once by a test before
	// any concurrent use, so it is never mutated concurrently.
	acceptEntryHook func()
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

	// R8-A: invalid capacity fails closed. No silent clamp — a negative or oversized
	// capacity is an explicit error; the caller must provide a valid value.
	if capacity < 0 || capacity > maxGateCapacity {
		return "", false
	}
	effCap := capacity
	// R6-C: same-runtime/capacity activation is idempotent.
	oldID, hasOld := g.current[sessionID]
	if hasOld {
		if e := g.endpoints[oldID]; e != nil && e.active && e.runtime.equal(rt) && e.capacity == effCap {
			return oldID, true
		}
	}
	// R8-A: validate session and runtime identity before publishing.
	if !validSessionID(sessionID) || !validAdapterID(rt.Adapter) || !validVersion(rt.Version) {
		return "", false
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
	// R9-B3 test seam: a deterministic pause BEFORE the lock, so a test can drive a
	// replacement into the accept-vs-activate window. nil in production.
	if g.acceptEntryHook != nil {
		g.acceptEntryHook()
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	// R8-A: canonical metadata validation — every variable-length field, digest
	// format, token encoding, and ID syntax is enforced before the gate retains
	// anything.
	if !validGateBindingMeta(req) {
		return DeliveryReceipt{}, "", false
	}
	b := req.Binding
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
	totalItem := chargedItemBytes(req) // exact retained variable bytes + fixed charge
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
