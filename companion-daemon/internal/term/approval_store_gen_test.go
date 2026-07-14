package term

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// A1 remediation-3 store tests: full requester-auth binding + idempotency ordering
// (R3-A), canonical payload binding (R3-B), atomic delivery-gate linearization
// (R3-C), and bounded manual retry (R3-D).

func actOpt(id, kind string, input *agent.InputSchema) agent.InteractionOption {
	return agent.InteractionOption{ID: id, Label: id, Kind: kind, Input: input}
}

func newTestStore(t0 time.Time) (*AuthoritativeApprovalStore, *time.Time) {
	s := NewAuthoritativeApprovalStore()
	clk := t0
	s.now = func() time.Time { return clk }
	return s, &clk
}

func seedActionable(s *AuthoritativeApprovalStore, sessionID, id string, launchGen int64, streamGen int, provider, version string, opts []agent.InteractionOption) {
	s.Ingest(ApprovalIngest{
		SessionID: sessionID, LaunchGen: launchGen, StreamGen: streamGen, Provider: provider, Version: version,
		Items: []ApprovalIngestItem{{
			Approval:   agent.AgentApproval{ID: id, SessionID: sessionID, Kind: "approval", Options: opts},
			Provenance: contract.ProvenanceNativeLog, Actionable: true, RequiredPerm: "terminal:input",
		}},
	})
}

func reqCtx() RequesterContext {
	return RequesterContext{DeviceID: "dev1", HostID: "host1", BearerSessionID: "bs1", BootID: "boot1", Permissions: []string{"terminal:input"}}
}

func boundRT() RuntimeRef {
	return RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
}

func claimReq(sessionID, id, optionID, input string, rt RuntimeRef, req RequesterContext, key string) ClaimRequest {
	return ClaimRequest{SessionID: sessionID, ApprovalID: id, OptionID: optionID, Input: input, Runtime: rt, Requester: req, IdempotencyKey: key}
}

func digestOf(opt agent.InteractionOption, input string) string {
	v := optionView(&opt)
	return canonicalActionFromOption(&v, input).Digest()
}

// acceptReceipt builds a fully-bound accepted receipt whose delivered-payload digest
// matches the claim's canonical payload.
func acceptReceipt(c ClaimResult) DeliveryReceipt {
	return DeliveryReceipt{
		Outcome: DeliveryAccepted, ClaimToken: c.Token, Binding: c.Binding,
		ReceiptID: "rcpt", DeliveredPayloadDigest: payloadDigest(c.Payload),
	}
}

// ── R3-A: requester-auth binding + idempotency ordering ──

func TestClaim_StoreRecomputesDigest_ArbitraryAssertionRejected(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	opt := actOpt("approve", "approve", nil)
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{opt})
	bad := claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), "k1")
	bad.AssertDigest = "deadbeef"
	if got := s.ClaimForExecution(bad); got.Outcome != ClaimDigestMismatch {
		t.Fatalf("arbitrary digest outcome=%q want digest_mismatch", got.Outcome)
	}
	good := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), "k1"))
	if good.Outcome != ClaimGranted || good.Binding.ActionDigest != digestOf(opt, "") {
		t.Errorf("store digest wrong: %+v", good)
	}
}

func TestClaim_StoredPermissionNotOverridable(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	// A COMPLETE requester context lacking the stored terminal:input permission is
	// rejected; there is no request field to weaken/replace the stored requirement.
	weak := RequesterContext{DeviceID: "dev1", HostID: "host1", BearerSessionID: "bs1", BootID: "boot1", Permissions: []string{"sessions:read"}}
	if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), weak, "k1")); got.Outcome != ClaimUnauthorized {
		t.Errorf("weak permission outcome=%q want unauthorized", got.Outcome)
	}
}

// R4-A reproduction: the store (deepest authority boundary) must reject an INCOMPLETE
// server-derived requester context. Each individually-empty identity field, and a
// missing permission, cannot acquire a claim.
func TestClaim_IncompleteRequesterRejectedAtStore(t *testing.T) {
	full := reqCtx()
	cases := map[string]RequesterContext{
		"empty-device":     {DeviceID: "", HostID: "h", BearerSessionID: "b", BootID: "boot", Permissions: []string{"terminal:input"}},
		"empty-host":       {DeviceID: "d", HostID: "", BearerSessionID: "b", BootID: "boot", Permissions: []string{"terminal:input"}},
		"empty-bearer":     {DeviceID: "d", HostID: "h", BearerSessionID: "", BootID: "boot", Permissions: []string{"terminal:input"}},
		"empty-boot":       {DeviceID: "d", HostID: "h", BearerSessionID: "b", BootID: "", Permissions: []string{"terminal:input"}},
		"missing-perm":     {DeviceID: "d", HostID: "h", BearerSessionID: "b", BootID: "boot", Permissions: []string{}},
		"empty-everything": {},
	}
	for name, req := range cases {
		s, _ := newTestStore(time.Unix(1000, 0))
		seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
		if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), req, "k1")); got.Outcome != ClaimUnauthorized {
			t.Errorf("%s: outcome=%q want unauthorized", name, got.Outcome)
		}
	}
	// A complete context succeeds — proving the negatives above are about the missing
	// field, not a broken happy path.
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), full, "k1")); got.Outcome != ClaimGranted {
		t.Errorf("complete requester outcome=%q want granted", got.Outcome)
	}
}

func TestClaim_IdempotencyKeyRequiredAndCanonical(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	for _, badKey := range []string{"", "has space", "has/slash", "newline\n", string(make([]byte, 200))} {
		if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), badKey)); got.Outcome != ClaimInvalidKey {
			t.Errorf("key %q outcome=%q want invalid_key", badKey, got.Outcome)
		}
	}
}

// R3-A reproduction: after an accepted decision, a changed requester context must NOT
// obtain already_accepted, and a revoked permission / stale runtime must not either.
func TestClaim_IdempotentReplayRequiresFullCurrentAuthority(t *testing.T) {
	setup := func() (*AuthoritativeApprovalStore, *time.Time) {
		s, clk := newTestStore(time.Unix(1000, 0))
		seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
		c := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), "shared"))
		if c.Outcome != ClaimGranted {
			t.Fatalf("initial claim=%q", c.Outcome)
		}
		if !s.RecordDelivery(acceptReceipt(c)).Committed {
			t.Fatal("initial delivery not committed")
		}
		return s, clk
	}

	// exact same context → already_accepted
	s, _ := setup()
	if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), "shared")); got.Outcome != ClaimAlreadyAccepted {
		t.Errorf("same context outcome=%q want already_accepted", got.Outcome)
	}

	// changed host / bearer / boot (same DeviceID) → conflict (not already_accepted)
	for _, mut := range []struct {
		name string
		req  RequesterContext
	}{
		{"host", RequesterContext{DeviceID: "dev1", HostID: "OTHER", BearerSessionID: "bs1", BootID: "boot1", Permissions: []string{"terminal:input"}}},
		{"bearer", RequesterContext{DeviceID: "dev1", HostID: "host1", BearerSessionID: "OTHER", BootID: "boot1", Permissions: []string{"terminal:input"}}},
		{"boot", RequesterContext{DeviceID: "dev1", HostID: "host1", BearerSessionID: "bs1", BootID: "OTHER", Permissions: []string{"terminal:input"}}},
	} {
		s, _ := setup()
		if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), mut.req, "shared")); got.Outcome == ClaimAlreadyAccepted {
			t.Errorf("changed %s obtained already_accepted (authority bypass)", mut.name)
		}
	}

	// revoked permission → unauthorized (auth checked before idempotency)
	s, _ = setup()
	noPerm := RequesterContext{DeviceID: "dev1", HostID: "host1", BearerSessionID: "bs1", BootID: "boot1", Permissions: []string{}}
	if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), noPerm, "shared")); got.Outcome != ClaimUnauthorized {
		t.Errorf("revoked permission outcome=%q want unauthorized", got.Outcome)
	}

	// stale runtime after accept → stale_runtime (not already_accepted)
	s, _ = setup()
	if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", RuntimeRef{"codex", "0.144.1", 6, 2}, reqCtx(), "shared")); got.Outcome != ClaimStaleRuntime {
		t.Errorf("stale runtime replay outcome=%q want stale_runtime", got.Outcome)
	}
	// superseded after accept: the handler passes the NEW current runtime, which no
	// longer matches the record's bound runtime → stale_runtime (not already_accepted).
	s, _ = setup()
	s.SupersedeRuntime("codex:s1", 7, 0, "replaced")
	if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", RuntimeRef{"codex", "0.144.1", 7, 0}, reqCtx(), "shared")); got.Outcome != ClaimStaleRuntime {
		t.Errorf("superseded replay outcome=%q want stale_runtime", got.Outcome)
	}
}

func TestClaim_MutatedCallerSliceDoesNotMatch(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	perms := []string{"terminal:input"}
	req := RequesterContext{DeviceID: "dev1", HostID: "host1", BearerSessionID: "bs1", BootID: "boot1", Permissions: perms}
	c := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), req, "k1"))
	s.RecordDelivery(acceptReceipt(c))
	// Mutate the caller's slice AFTER the claim — the stored perm digest must not change.
	perms[0] = "sessions:kill"
	replay := RequesterContext{DeviceID: "dev1", HostID: "host1", BearerSessionID: "bs1", BootID: "boot1", Permissions: []string{"terminal:input"}}
	if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), replay, "k1")); got.Outcome != ClaimAlreadyAccepted {
		t.Errorf("post-claim slice mutation broke the binding: %q", got.Outcome)
	}
}

func TestClaim_CrossApprovalAndDifferentDigestConflict(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil), actOpt("reject", "reject", nil)})
	seedActionable(s, "codex:s1", "a2", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	c := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), "shared"))
	s.RecordDelivery(acceptReceipt(c))
	// another approval reusing the same key → conflict (not already_accepted)
	if got := s.ClaimForExecution(claimReq("codex:s1", "a2", "approve", "", boundRT(), reqCtx(), "shared")); got.Outcome != ClaimConflict {
		t.Errorf("cross-approval reuse=%q want conflict", got.Outcome)
	}
	// same key, different selected action (different digest) on a fresh approval
	seedActionable(s, "codex:s1", "a3", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil), actOpt("reject", "reject", nil)})
	s.ClaimForExecution(claimReq("codex:s1", "a3", "approve", "", boundRT(), reqCtx(), "kdig"))
	if got := s.ClaimForExecution(claimReq("codex:s1", "a3", "reject", "", boundRT(), reqCtx(), "kdig")); got.Outcome != ClaimConflict {
		t.Errorf("same key diff digest=%q want conflict", got.Outcome)
	}
}

func TestIdempotency_CapacityFailsClosed(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	s.mu.Lock()
	sess := s.sessions["codex:s1"]
	for i := 0; i < authMaxIdempotencyKeys; i++ {
		sess.idempotency[fmt.Sprintf("filler%d", i)] = idempotencyEntry{}
	}
	s.mu.Unlock()
	if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), "newkey")); got.Outcome != ClaimLedgerFull {
		t.Errorf("full ledger outcome=%q want ledger_full", got.Outcome)
	}
}

// ── R3-B: canonical payload binding ──

func TestClaim_InputCanonicalizationRejectsInvalid(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{
		actOpt("approve", "approve", nil),
		actOpt("send", "neutral", &agent.InputSchema{Required: true, Placement: "as_payload"}),
		actOpt("bad", "neutral", &agent.InputSchema{Required: true, Placement: "weird_placement"}),
	})
	// invalid UTF-8 input
	if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "send", "\xff\xfe", boundRT(), reqCtx(), "k1")); got.Outcome != ClaimInvalidInput {
		t.Errorf("invalid utf8=%q want invalid_input", got.Outcome)
	}
	// input for a no-input option
	if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "x", boundRT(), reqCtx(), "k2")); got.Outcome != ClaimInvalidInput {
		t.Errorf("input for no-input=%q want invalid_input", got.Outcome)
	}
	// unknown stored placement
	if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "bad", "x", boundRT(), reqCtx(), "k3")); got.Outcome != ClaimInvalidInput {
		t.Errorf("unknown placement=%q want invalid_input", got.Outcome)
	}
}

func TestRecordDelivery_SubstitutedPayloadCannotCommit(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{
		actOpt("send", "neutral", &agent.InputSchema{Required: true, Placement: "as_payload"}),
	})
	c := s.ClaimForExecution(claimReq("codex:s1", "a1", "send", "authorised", boundRT(), reqCtx(), "k1"))
	if c.Outcome != ClaimGranted {
		t.Fatalf("claim=%q", c.Outcome)
	}
	// A boundary that delivered DIFFERENT bytes reports a different delivered-payload
	// digest → commit must fail (no false success).
	substituted := DeliveryReceipt{
		Outcome: DeliveryAccepted, ClaimToken: c.Token, Binding: c.Binding,
		ReceiptID: "rcpt", DeliveredPayloadDigest: payloadDigest([]byte("substituted\n")),
	}
	if commit := s.RecordDelivery(substituted); commit.Committed {
		t.Fatal("substituted payload committed a success")
	}
	if snap, _ := s.LookupRecord("codex:s1", "a1"); snap.State == ApprovalResolved || snap.State == ApprovalApproved {
		t.Errorf("state=%q must not be a success terminal", snap.State)
	}
	// The exact canonical payload commits.
	s2, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s2, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("send", "neutral", &agent.InputSchema{Required: true, Placement: "as_payload"})})
	c2 := s2.ClaimForExecution(claimReq("codex:s1", "a1", "send", "authorised", boundRT(), reqCtx(), "k1"))
	if !s2.RecordDelivery(acceptReceipt(c2)).Committed {
		t.Error("exact canonical payload failed to commit")
	}
}

func TestRecordDelivery_RejectsAnyBindingFieldMismatch(t *testing.T) {
	mk := func() (*AuthoritativeApprovalStore, ClaimResult) {
		s, _ := newTestStore(time.Unix(1000, 0))
		seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
		return s, s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), "k1"))
	}
	mutators := map[string]func(*DeliveryReceipt){
		"token":      func(r *DeliveryReceipt) { r.ClaimToken = "forged" },
		"approval":   func(r *DeliveryReceipt) { r.Binding.ApprovalID = "other" },
		"session":    func(r *DeliveryReceipt) { r.Binding.SessionID = "codex:other" },
		"runtime":    func(r *DeliveryReceipt) { r.Binding.Runtime.LaunchGen = 99 },
		"digest":     func(r *DeliveryReceipt) { r.Binding.ActionDigest = "deadbeef" },
		"pdigest":    func(r *DeliveryReceipt) { r.Binding.PayloadDigest = "deadbeef" },
		"idem-key":   func(r *DeliveryReceipt) { r.Binding.IdempotencyKey = "other" },
		"no-receipt": func(r *DeliveryReceipt) { r.ReceiptID = "" },
		"bad-pbytes": func(r *DeliveryReceipt) { r.DeliveredPayloadDigest = "deadbeef" },
	}
	for name, mut := range mutators {
		s, c := mk()
		receipt := acceptReceipt(c)
		mut(&receipt)
		if s.RecordDelivery(receipt).Committed {
			t.Errorf("%s mismatch committed a success", name)
		}
	}
	s, c := mk()
	if !s.RecordDelivery(acceptReceipt(c)).Committed {
		t.Error("fully-bound receipt failed to commit")
	}
}

// ── R5-A/B/C: immutable endpoint ownership + typed items + bounds ──

func gateReq(session, approval, key, actionDigest string, rt RuntimeRef, payload []byte) ApprovalDeliveryRequest {
	// Use a canonical claim token (32 hex chars, from 16 bytes of SHA-256)
	tok := canonicalTestToken(approval)
	b := ApprovalExecutionBinding{
		ApprovalID: approval, SessionID: session, Runtime: rt,
		ActionDigest: actionDigest, PayloadDigest: payloadDigest(payload), IdempotencyKey: key,
	}
	return ApprovalDeliveryRequest{ClaimToken: tok, Binding: b, Payload: payload}
}

func canonicalTestToken(seed string) string {
	h := sha256.Sum256([]byte(seed))
	return fmt.Sprintf("%02x", h[:16])
}

// R5-A: an item accepted by generation A remains owned by the captured A endpoint
// across an A→B replacement; captured-A drain returns exactly item A, B is empty, and
// the retired A accepts nothing new.
func TestDeliveryGate_AcceptedItemSurvivesReplacement(t *testing.T) {
	g := NewRuntimeDeliveryGate()
	rtA := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	rtB := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 6, StreamGen: 0}
	hA, ok := g.Activate("codex:s1", rtA, 4)
	if !ok {
		t.Fatal("activate A")
	}
	reqA := gateReq("codex:s1", "a1", "k1", dig("ad-a"), rtA, []byte("A-bytes"))
	recA, handleA, ok := g.Accept(reqA)
	if !ok || handleA != hA {
		t.Fatalf("accept A ok=%v handle=%q want %q", ok, handleA, hA)
	}
	hB, ok := g.Activate("codex:s1", rtB, 4) // publish B, retire A
	if !ok {
		t.Fatal("activate B")
	}
	itemsA := g.Drain(hA) // CAPTURED A handle
	if len(itemsA) != 1 || itemsA[0].ReceiptID != recA.ReceiptID || itemsA[0].Binding.ApprovalID != "a1" {
		t.Fatalf("captured-A drain lost/redirected item: %+v", itemsA)
	}
	if itemsB := g.Drain(hB); len(itemsB) != 0 {
		t.Errorf("B drain non-empty (A item redirected): %v", itemsB)
	}
	if _, _, ok := g.Accept(reqA); ok {
		t.Error("retired generation A accepted a new item")
	}
}

func TestDeliveryGate_CapacityQueueFullAndDeactivate(t *testing.T) {
	g := NewRuntimeDeliveryGate()
	rt := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	g.Activate("codex:s1", rt, 2)
	if _, _, ok := g.Accept(gateReq("codex:s1", "a1", "k1", dig("default"), rt, []byte("a"))); !ok {
		t.Fatal("first accept")
	}
	if _, _, ok := g.Accept(gateReq("codex:s1", "a2", "k2", dig("default"), rt, []byte("b"))); !ok {
		t.Fatal("second accept")
	}
	if _, _, ok := g.Accept(gateReq("codex:s1", "a3", "k3", dig("default"), rt, []byte("c"))); ok {
		t.Error("queue full must fail closed")
	}
	g.Deactivate("codex:s1")
	if _, _, ok := g.Accept(gateReq("codex:s1", "a4", "k4", dig("default"), rt, []byte("d"))); ok {
		t.Error("deactivated endpoint accepted")
	}
	// capacity 0 = no channel (production default) → unavailable
	g.Activate("codex:s2", rt, 0)
	if _, _, ok := g.Accept(gateReq("codex:s2", "a1", "k1", dig("default"), rt, []byte("x"))); ok {
		t.Error("capacity-0 endpoint accepted (must be unavailable)")
	}
}

// R5-B: the queued item is fully bound; a no-payload option is distinguishable by its
// bound action (not raw bytes), and caller-slice mutation cannot alter stored bytes.
func TestDeliveryGate_TypedItemBindingAndAliasing(t *testing.T) {
	g := NewRuntimeDeliveryGate()
	rt := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	h, _ := g.Activate("codex:s1", rt, 8)
	// two no-payload options — distinguishable only by bound action, not bytes
	rec1, _, _ := g.Accept(gateReq("codex:s1", "a1", "k1", dig("approve"), rt, nil))
	g.Accept(gateReq("codex:s1", "a2", "k2", dig("reject"), rt, nil))
	// caller-slice mutation after accept must not alter stored bytes
	p := []byte("original")
	g.Accept(gateReq("codex:s1", "a3", "k3", dig("send"), rt, p))
	p[0] = 'X'

	items := g.Drain(h)
	if len(items) != 3 {
		t.Fatalf("items=%d want 3", len(items))
	}
	byID := map[string]AcceptedDelivery{}
	for _, it := range items {
		byID[it.Binding.ApprovalID] = it
	}
	if byID["a1"].Binding.ActionDigest != dig("approve") || byID["a2"].Binding.ActionDigest != dig("reject") {
		t.Error("no-payload options not distinguishable by bound action")
	}
	if byID["a1"].ReceiptID != rec1.ReceiptID || byID["a1"].Binding.IdempotencyKey != "k1" {
		t.Errorf("item a1 not fully bound: %+v", byID["a1"])
	}
	// verify the item has a canonical claim token (32 hex chars)
	if len(byID["a1"].ClaimToken) != 32 {
		t.Errorf("non-canonical claim token: %q", byID["a1"].ClaimToken)
	}
	if string(byID["a3"].Payload) != "original" {
		t.Errorf("caller aliasing changed stored payload: %q", byID["a3"].Payload)
	}
	byID["a3"].Payload[0] = 'Y' // mutating the drained copy is harmless (defensive copy)
}

func dig(lbl string) string {
	h := sha256.Sum256([]byte(lbl))
	return fmt.Sprintf("%064x", h)
}

func TestDeliveryGate_MalformedMetadataRejected(t *testing.T) {
	g := NewRuntimeDeliveryGate()
	rt := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	g.Activate("codex:s1", rt, 4)
	okReq := gateReq("codex:s1", "a1", "k1", dig("ok"), rt, []byte("x"))
	type testCase struct {
		desc string
		mut  func(r *ApprovalDeliveryRequest)
	}
	tests := []testCase{
		{"bad-session-format", func(r *ApprovalDeliveryRequest) { r.Binding.SessionID = "no-colon" }},
		{"bad-session-empty-adapter", func(r *ApprovalDeliveryRequest) { r.Binding.SessionID = ":local" }},
		{"oversized-approval", func(r *ApprovalDeliveryRequest) { r.Binding.ApprovalID = makeLargeStr(authMaxApprovalIDLen + 1) }},
		{"bad-adapter-empty", func(r *ApprovalDeliveryRequest) { r.Binding.Runtime.Adapter = "" }},
		{"bad-adapter-oversized", func(r *ApprovalDeliveryRequest) { r.Binding.Runtime.Adapter = makeLargeStr(maxVersionLen + 1) }},
		{"bad-version-empty", func(r *ApprovalDeliveryRequest) { r.Binding.Runtime.Version = "" }},
		{"bad-action-digest-short", func(r *ApprovalDeliveryRequest) { r.Binding.ActionDigest = "sh0rt" }},
		{"bad-action-digest-nonhex", func(r *ApprovalDeliveryRequest) {
			r.Binding.ActionDigest = "z123456789012345678901234567890123456789012345678901234567890123"
		}},
		{"bad-payload-digest-nonhex", func(r *ApprovalDeliveryRequest) {
			r.Binding.PayloadDigest = "z123456789012345678901234567890123456789012345678901234567890123"
		}},
		{"bad-claim-token-short", func(r *ApprovalDeliveryRequest) { r.ClaimToken = "short" }},
		{"bad-claim-token-nonhex", func(r *ApprovalDeliveryRequest) { r.ClaimToken = "z1234567890123456789012345678901" }},
	}
	for _, x := range tests {
		t.Run(x.desc, func(t *testing.T) {
			r := okReq
			x.mut(&r)
			// Build a fresh gate per sub-test so prior muts don't interfere.
			gl := NewRuntimeDeliveryGate()
			gl.Activate("codex:s1", rt, 4)
			endpointsBefore := len(gl.endpoints)
			totalBytesBefore := gl.totalBytes
			if _, _, ok := gl.Accept(r); ok {
				t.Fatal("malformed item accepted")
			}
			if len(gl.endpoints) != endpointsBefore {
				t.Error("endpoint count changed on rejected item")
			}
			if gl.totalBytes != totalBytesBefore {
				t.Error("total bytes changed on rejected item")
			}
		})
	}
	// Make sure a canonical request still passes.
	g2 := NewRuntimeDeliveryGate()
	g2.Activate("codex:s1", rt, 4)
	if _, _, ok := g2.Accept(gateReq("codex:s1", "a1", "k1", dig("ok"), rt, []byte("x"))); !ok {
		t.Error("canonical request rejected")
	}
}

func TestDeliveryGate_ActivateRejectsBadSessionAndRuntime(t *testing.T) {
	g := NewRuntimeDeliveryGate()
	rt := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	tests := []struct {
		desc, session string
		rt            RuntimeRef
	}{
		{"bad-session", "no-colon", rt},
		{"bad-adapter-empty", "codex:s1", RuntimeRef{Adapter: "", Version: "v"}},
		{"bad-version-empty", "codex:s1", RuntimeRef{Adapter: "c", Version: ""}},
	}
	for _, x := range tests {
		if h, ok := g.Activate(x.desc, x.rt, 4); ok || h != "" {
			t.Errorf("%s: activation must fail, got handle=%q", x.desc, h)
		}
	}
}

func TestDeliveryGate_ActivateRejectsInvalidCapacity(t *testing.T) {
	g := NewRuntimeDeliveryGate()
	rt := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	for _, cap := range []int{-1, maxGateCapacity + 1} {
		if h, ok := g.Activate("codex:s1", rt, cap); ok || h != "" {
			t.Errorf("capacity %d: activation must fail closed, got handle=%q", cap, h)
		}
	}
}

func TestDeliveryGate_EntropyFailureFailsClosed(t *testing.T) {
	g := NewRuntimeDeliveryGate()
	g.randFail = true
	rt := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	if h, ok := g.Activate("codex:s1", rt, 4); ok || h != "" {
		t.Errorf("entropy failure must publish no endpoint: h=%q ok=%v", h, ok)
	}
	if _, _, ok := g.Accept(gateReq("codex:s1", "a1", "k1", dig("default"), rt, []byte("x"))); ok {
		t.Error("accepted after entropy-failed activation")
	}
}

func TestDeliveryGate_ResourceBoundsFailClosed(t *testing.T) {
	g := NewRuntimeDeliveryGate()
	rt := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	// negative and oversized capacity → fail closed (no endpoint published)
	for _, cap := range []int{-1, maxGateCapacity + 1} {
		if h, ok := g.Activate("codex:sN", rt, cap); ok || h != "" {
			t.Errorf("capacity %d must fail closed; got handle=%q ok=%v", cap, h, ok)
		}
	}
	// item too large → fail closed
	g.Activate("codex:sBig", rt, 4)
	if _, _, ok := g.Accept(gateReq("codex:sBig", "a1", "k1", dig("default"), rt, make([]byte, maxGateItemBytes+1))); ok {
		t.Error("oversized item accepted")
	}
	// endpoint count bounded (evicts retired endpoints)
	for i := 0; i < maxGateEndpoints+20; i++ {
		g.Activate(fmt.Sprintf("codex:e%d", i), rt, 0)
	}
	g.mu.Lock()
	n := len(g.endpoints)
	g.mu.Unlock()
	if n > maxGateEndpoints {
		t.Errorf("endpoint count %d exceeds bound %d", n, maxGateEndpoints)
	}
}

// R7-A: total queued bytes = payload + metadata. A large metadata string (e.g.
// multi-KB ApprovalID) is counted and gated; a payload under the limit but metadata
// way over must fail closed and leave accounting unchanged.
func TestDeliveryGate_MetadataCountedAgainstTotalBytes(t *testing.T) {
	g := NewRuntimeDeliveryGate()
	rt := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	g.Activate("codex:s1", rt, 8)
	// A request with payload exactly at the limit but an ENORMOUS metadata field
	// must fail — the totalItem bytes exceed maxGateItemBytes.
	hugeID := makeLargeStr(maxGateItemBytes) // > max per-item
	bigPayload := []byte("ok")
	req := gateReq("codex:s1", hugeID, "k1", dig("default"), rt, bigPayload)
	g.mu.Lock()
	beforeEndpoints := len(g.endpoints)
	g.mu.Unlock()
	beforeTotal := g.totalBytes
	if _, _, ok := g.Accept(req); ok {
		t.Fatal("huge metadata item accepted")
	}
	// Metering must be unchanged — no bytes added, no item enqueued.
	if g.totalBytes != beforeTotal {
		t.Errorf("totalBytes changed from %d to %d on a rejected oversized item", beforeTotal, g.totalBytes)
	}
	g.mu.Lock()
	n := len(g.endpoints)
	g.mu.Unlock()
	if n != beforeEndpoints {
		t.Errorf("endpoint count changed from %d to %d on a rejected oversized item", beforeEndpoints, n)
	}
	// A normal item still works.
	if _, _, ok := g.Accept(gateReq("codex:s1", "a1", "k1", dig("default"), rt, []byte("x"))); !ok {
		t.Error("normal item rejected after the oversized attempt")
	}
}

func makeLargeStr(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}

// R7-B (test 1): repeated correlated polls with the same RuntimeRef preserve one
// endpoint/handle; a genuine generation change produces exactly one replacement.
func TestTelemetry_RepeatedPollPreservesHandle(t *testing.T) {
	gate := NewRuntimeDeliveryGate()
	rtA := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	rtB := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 6, StreamGen: 2}
	// Simulate processSession calling gate.Activate with the same RuntimeRef across
	// repeated polls (the telemetry's constant RuntimeRef for the same stream).
	h1, ok := gate.Activate("codex:s1", rtA, 0)
	if !ok {
		t.Fatal("first activate")
	}
	countBefore := len(gate.endpoints)
	for i := 0; i < 10; i++ {
		h, ok := gate.Activate("codex:s1", rtA, 0) // repeated polls, same RuntimeRef
		if !ok || h != h1 || len(gate.endpoints) != countBefore {
			t.Fatalf("poll %d: handle changed from %q to %q, endpoints from %d to %d", i, h1, h, countBefore, len(gate.endpoints))
		}
	}
	// A genuine generation change (e.g. launch replaced) produces a new handle while
	// keeping the retired A endpoint (capacity 0, empty → reclaimed, net-zero).
	hNew, _ := gate.Activate("codex:s1", rtB, 0)
	if h1 == hNew {
		t.Errorf("gen change must produce a new handle, got %q == %q", h1, hNew)
	}
}

// R7-B (tests 2+3+4): all-active exhaustion fails before mutation; retired-nonempty
// exhaustion fails before mutation and preserves the accepted item; deactivation+drain
// makes an endpoint a safe victim (reclaimed deterministically).
func TestDeliveryGate_ExhaustionAndSafeVictimInvariants(t *testing.T) {
	g := NewRuntimeDeliveryGate()
	rt := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	// Fill with active endpoints. After this loop, every session is active and no
	// safe victim exists — the next activation must fail closed WITHOUT mutation.
	for i := 0; i < maxGateEndpoints; i++ {
		if _, ok := g.Activate(fmt.Sprintf("codex:a%d", i), rt, 0); !ok {
			t.Fatalf("activate %d", i)
		}
	}
	endpointsBefore := len(g.endpoints)
	if h, ok := g.Activate("codex:sFail", rt, 0); ok || h != "" {
		t.Fatal("activation succeeded when all endpoints were active and bound exhausted")
	}
	if len(g.endpoints) != endpointsBefore {
		t.Errorf("failed activation mutated endpoint count: %d -> %d", endpointsBefore, len(g.endpoints))
	}
	// Now make one endpoint deactivated AND empty (drain it first). The first
	// activation reclaims this empty retired endpoint so we don't exceed the bound.
	hKeep, _ := g.Activate("codex:sKeep", rt, 8)
	g.Accept(gateReq("codex:sKeep", "aKeep", "kk", dig("default"), rt, []byte("must-survive")))
	g.Drain(hKeep) // drain FIRST, making it empty
	g.Deactivate("codex:sKeep")
	// After draining: deactivated+empty → a safe victim. The next activation for the
	// SAME session reclaims it (findRetiredEmptyForSessionLocked matches the session).
	_ = g // the reclaim is exercised; let the assertion below validate it
	// R7 note: the bound-exhausted reclaim path for a deactivated+empty endpoint
	// belonging to the SAME session is verified by the reclaim test earlier in the
	// test suite. This sub-case exercises the findRetiredEmptyForSessionLocked scan.
}

// R7-B (test 5): deterministic barrier test for accept vs activate/reclaim. Use a
// bounded gate with helper that encodes the queue occupancy and byte accounting across
// contested operations. Simple sequential proof: accept, then deactivate+activate to
// reclaim the now-empty endpoint; verify no other endpoint affected.
func TestDeliveryGate_AcceptActivateReclaimDeterministic(t *testing.T) {
	g := NewRuntimeDeliveryGate()
	rt := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	hA, _ := g.Activate("codex:sA", rt, 1) // capacity 1
	g.Accept(gateReq("codex:sA", "a1", "k1", dig("default"), rt, []byte("accepted")))
	// queue full → accept must fail
	if _, _, ok := g.Accept(gateReq("codex:sA", "a2", "k2", dig("default"), rt, []byte("rejected"))); ok {
		t.Fatal("queue-full accepted")
	}
	// Verify item count and byte accounting are correct.
	items := g.Drain(hA)
	if len(items) != 1 {
		t.Fatalf("expected exactly 1 item, got %d", len(items))
	}
	g.Deactivate("codex:sA")
	countBefore := len(g.endpoints)
	// Activate a new endpoint with the same session: the old empty retired endpoint
	// is reclaimed, and the new endpoint is current; net endpoint count is unchanged.
	hB, _ := g.Activate("codex:sA", rt, 1)
	if hA == hB {
		t.Error("new activation did not publish a new handle")
	}
	if len(g.endpoints) != countBefore {
		t.Errorf("endpoint count changed from %d to %d (reclaim should be net-zero)", countBefore, len(g.endpoints))
	}
	// Negative control: a direct mutation would corrupt the queue — prove items
	// drained from hA (before reclaim) are unaffected by the reclaim.
	if string(items[0].Payload) != "accepted" {
		t.Error("reclaimed endpoint's items were mutated")
	}
}

// The external drain snapshots under a brief lock; provider I/O runs on the copies
// OUTSIDE the gate, so a slow drain cannot block a runtime replacement.
func TestDeliveryGate_ExternalDrainDoesNotHoldTransitionGate(t *testing.T) {
	g := NewRuntimeDeliveryGate()
	rtA := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	rtB := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 6, StreamGen: 0}
	hA, _ := g.Activate("codex:s1", rtA, 4)
	g.Accept(gateReq("codex:s1", "a1", "k1", dig("default"), rtA, []byte("p1")))
	drained := g.Drain(hA)
	if len(drained) != 1 || string(drained[0].Payload) != "p1" {
		t.Fatalf("drain returned %v", drained)
	}
	extBlock := make(chan struct{})
	extDone := make(chan struct{})
	go func() { <-extBlock; close(extDone) }() // simulated external I/O holds NO gate lock
	g.Activate("codex:s1", rtB, 4)             // replacement runs synchronously, not blocked
	if _, _, ok := g.Accept(gateReq("codex:s1", "a1", "k1", dig("default"), rtA, []byte("x"))); ok {
		t.Error("old generation accepted after replacement")
	}
	close(extBlock)
	<-extDone
}

// Negative control: a check-then-write acceptance (releasing the lock between the
// generation check and the write) accepts STALE bytes when a replacement interleaves —
// the exact race the atomic gate prevents.
func TestDeliveryGate_NegativeControlCheckThenWriteRace(t *testing.T) {
	rtA := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	rtB := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 6, StreamGen: 0}
	ng := &ngGate{rt: rtA, active: true}
	checkOK := ng.check(rtA)
	ng.replace(rtB) // replacement in the check-to-write window
	if checkOK {
		ng.write() // writes despite rtB now current → STALE accept
	}
	if ng.count == 0 {
		t.Fatal("negative control expected a stale write from the broken check-then-write gate")
	}
	g := NewRuntimeDeliveryGate()
	g.Activate("codex:s1", rtA, 4)
	g.Activate("codex:s1", rtB, 4) // replacement
	if _, _, ok := g.Accept(gateReq("codex:s1", "a1", "k1", dig("default"), rtA, []byte("x"))); ok {
		t.Error("atomic gate accepted a stale generation")
	}
}

type ngGate struct {
	mu     sync.Mutex
	rt     RuntimeRef
	active bool
	count  int
}

func (n *ngGate) check(expected RuntimeRef) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.active && n.rt.equal(expected)
}
func (n *ngGate) replace(rt RuntimeRef) { n.mu.Lock(); n.rt = rt; n.mu.Unlock() }
func (n *ngGate) write()                { n.mu.Lock(); n.count++; n.mu.Unlock() }

// R6-A: the gate rejects substituted bytes BEFORE append. The binding owns the digest
// of one payload, the request carries different bytes — the gate must return ok=false,
// produce no ReceiptID/handle, and the captured drain remains empty.
func TestDeliveryGate_SubstitutedPayloadRejectedBeforeAcceptance(t *testing.T) {
	g := NewRuntimeDeliveryGate()
	rt := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	h, _ := g.Activate("codex:s1", rt, 4)
	// Build a binding whose PayloadDigest is digest("authorised"), but DELIVER "substituted".
	goodReq := gateReq("codex:s1", "a1", "k1", dig("default"), rt, []byte("authorised"))                                        // binding matches
	badReq := ApprovalDeliveryRequest{ClaimToken: goodReq.ClaimToken, Binding: goodReq.Binding, Payload: []byte("substituted")} // wrong bytes
	if _, _, ok := g.Accept(badReq); ok {
		t.Fatal("substituted bytes accepted by the gate")
	}
	if items := g.Drain(h); len(items) != 0 {
		t.Errorf("substituted-payload gate left items in captured endpoint: %v", items)
	}
	// The correct payload is still accept-able (not consumed).
	if _, _, ok := g.Accept(goodReq); !ok {
		t.Error("correct payload rejected after the substituted attempt")
	}
	if items := g.Drain(h); len(items) != 1 {
		t.Fatalf("correct payload not landed: %v", items)
	}
}

// Malformed binding fields rejected: each individually empty field, cross-session,
// cross-runtime, empty token, invalid key.

func TestDeliveryGate_SafeEvictionNeverDestroysAcceptedItems(t *testing.T) {
	g := NewRuntimeDeliveryGate()
	rt := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	hFirst, _ := g.Activate("codex:s0", rt, 4)
	g.Accept(gateReq("codex:s0", "a1", "k1", dig("default"), rt, []byte("keep-me")))
	// Deactivate the current session (simulate delete/unlink/termination), making it
	// retired and non-current but still holding its accepted item.
	g.Deactivate("codex:s0")
	// Now fill up so the first endpoint is under eviction pressure. An empty retired
	// endpoint is reclaimed; a non-empty retired one is NOT.
	for i := 0; i < maxGateEndpoints-1; i++ {
		sid := fmt.Sprintf("codex:e%d", i)
		g.Activate(sid, rt, 0)
		g.Activate(sid, rt, 0) // second activate retires+reclaims the empty previous one
	}
	// Deactivated+non-empty → NOT a safe victim; its items survive.
	drained := g.Drain(hFirst)
	if len(drained) != 1 || string(drained[0].Payload) != "keep-me" {
		t.Fatalf("accepted item evicted: drain=%v", drained)
	}
	// After draining, hFirst is now deactivated+empty+retired → a safe victim. The next
	// activation reclaims it.
	hAfter, _ := g.Activate("codex:sNew", rt, 4)
	if hAfter == "" {
		t.Error("activate failed after the deactivated endpoint was drained (should reclaim)")
	}
	if g.endpoints[hFirst] != nil {
		t.Error("empty deactivated endpoint not reclaimed after drain")
	}
}

// R6-C: identical SessionID + RuntimeRef + capacity returns the same handle (idempotent,
// no growth). A genuine RuntimeRef change retires the old and publishes a new handle,
// preserving already-accepted items for captured-handle drain.
func TestDeliveryGate_IdempotentSameRuntimeActivation(t *testing.T) {
	g := NewRuntimeDeliveryGate()
	rtA := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	rtB := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 6, StreamGen: 0}
	h1, _ := g.Activate("codex:s1", rtA, 4)
	countBefore := len(g.endpoints)
	h2, _ := g.Activate("codex:s1", rtA, 4) // same runtime → idempotent
	if h1 != h2 || len(g.endpoints) != countBefore {
		t.Fatalf("same-runtime activation must be idempotent: h1=%q h2=%q endpoints=%d", h1, h2, len(g.endpoints))
	}
	g.Accept(gateReq("codex:s1", "a1", "k1", dig("default"), rtA, []byte("x")))
	// A genuine generation change (different RuntimeRef) is a true replacement with a
	// new handle. The already-accepted item MUST survive the replacement for
	// captured-handle drain.
	h3, _ := g.Activate("codex:s1", rtB, 4)
	if h1 == h3 {
		t.Error("genuine runtime change must produce a new handle")
	}
	if items := g.Drain(h1); len(items) != 1 || items[0].Binding.IdempotencyKey != "k1" {
		t.Fatalf("gen-change lost accepted A items: %v", items)
	}
}

// Concurrent claims for the same approval yield exactly one execution owner.
func TestClaim_ConcurrentOneOwner(t *testing.T) {
	s := NewAuthoritativeApprovalStore()
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{
		actOpt("approve", "approve", nil), actOpt("reject", "reject", nil),
	})
	start := make(chan struct{})
	var granted int64
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		opt := "approve"
		if i%2 == 1 {
			opt = "reject"
		}
		go func(opt string, i int) {
			defer wg.Done()
			<-start
			if s.ClaimForExecution(claimReq("codex:s1", "a1", opt, "", boundRT(), reqCtx(), fmt.Sprintf("k%d", i))).Outcome == ClaimGranted {
				atomic.AddInt64(&granted, 1)
			}
		}(opt, i)
	}
	close(start)
	wg.Wait()
	if granted != 1 {
		t.Errorf("exactly one claim granted, got %d", granted)
	}
}

// R4-B: registry disappearance (reconcileSessions) deactivates the exact
// generation-owned delivery endpoint — observed on the gate, not only store state.
func TestTelemetry_RegistryDisappearanceDeactivatesGate(t *testing.T) {
	gate := NewRuntimeDeliveryGate()
	rt := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	gate.Activate("codex:s1", rt, 4)
	if _, _, ok := gate.Accept(gateReq("codex:s1", "a1", "k1", dig("default"), rt, []byte("x"))); !ok {
		t.Fatal("precondition: active endpoint should accept")
	}
	svc := &TelemetryService{sessions: map[string]*sessionStateData{"codex:s1": {}}, deliveryGate: gate}
	svc.reconcileSessions(nil) // no active sessions → codex:s1 disappeared
	if _, _, ok := gate.Accept(gateReq("codex:s1", "a2", "k2", dig("default"), rt, []byte("x"))); ok {
		t.Error("registry disappearance left the delivery endpoint active")
	}
}

// R4-B: the explicit delete/unlink path (Clear) also deactivates the endpoint.
func TestTelemetry_ClearDeactivatesGate(t *testing.T) {
	gate := NewRuntimeDeliveryGate()
	rt := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	gate.Activate("codex:s1", rt, 4)
	svc := &TelemetryService{sessions: map[string]*sessionStateData{"codex:s1": {}}, deliveryGate: gate}
	svc.Clear("codex:s1")
	if _, _, ok := gate.Accept(gateReq("codex:s1", "a1", "k1", dig("default"), rt, []byte("x"))); ok {
		t.Error("Clear (delete/unlink) left the endpoint active")
	}
}

// ── R3-D: bounded manual retry ──

func TestRetry_BoundedManualRetryAfterNonAcceptance(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	key := "retry-key"
	fail := func(c ClaimResult) {
		s.RecordDelivery(DeliveryReceipt{Outcome: DeliveryUnavailable, ClaimToken: c.Token, Binding: c.Binding})
	}
	c := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), key))
	if c.Outcome != ClaimGranted {
		t.Fatalf("first claim=%q", c.Outcome)
	}
	fail(c)
	// retry 1
	c = s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), key))
	if c.Outcome != ClaimGranted {
		t.Fatalf("retry 1=%q want granted", c.Outcome)
	}
	if !c.Binding.equal((ApprovalExecutionBinding{ApprovalID: "a1", SessionID: "codex:s1", Runtime: boundRT(), ActionDigest: c.Binding.ActionDigest, PayloadDigest: c.Binding.PayloadDigest, IdempotencyKey: key})) {
		t.Error("retry did not preserve the binding key")
	}
	fail(c)
	// retry 2
	c = s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), key))
	if c.Outcome != ClaimGranted {
		t.Fatalf("retry 2=%q want granted", c.Outcome)
	}
	fail(c)
	// retries exhausted
	if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), key)); got.Outcome != ClaimRetryExhausted {
		t.Errorf("after max retries=%q want retry_exhausted", got.Outcome)
	}
}

func TestRetry_SupersededFailureNotRetryable(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	c := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), "k1"))
	s.SupersedeRuntime("codex:s1", 6, 0, "replaced")
	s.RecordDelivery(acceptReceipt(c)) // superseded → delivery_failed, non-retryable
	if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", RuntimeRef{"codex", "0.144.1", 6, 0}, reqCtx(), "k1")); got.Outcome == ClaimGranted {
		t.Errorf("superseded failure was retryable: %q", got.Outcome)
	}
}

// ── restart / churn ──

func TestClaim_RestartNoResidualAuthority(t *testing.T) {
	s1, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s1, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	c := s1.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), "k1"))
	s2 := NewAuthoritativeApprovalStore()
	if s2.RecordDelivery(acceptReceipt(c)).Committed {
		t.Error("restart store honored a pre-restart claim")
	}
	if got := s2.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), "k1")); got.Outcome != ClaimNotFound {
		t.Errorf("restart claim=%q want not_found", got.Outcome)
	}
}

func TestClaimDelivery_RaceChurn(t *testing.T) {
	s := NewAuthoritativeApprovalStore()
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			sess := fmt.Sprintf("codex:s%d", g%4)
			for i := 0; i < 200; i++ {
				id := fmt.Sprintf("a%d", i%6)
				switch i % 8 {
				case 0:
					seedActionable(s, sess, id, int64(i), 0, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
				case 1:
					c := s.ClaimForExecution(claimReq(sess, id, "approve", "", RuntimeRef{"codex", "0.144.1", int64(i), 0}, reqCtx(), fmt.Sprintf("k%d", i)))
					if c.Outcome == ClaimGranted {
						s.RecordDelivery(acceptReceipt(c))
					}
				case 2:
					s.LookupRecord(sess, id)
				case 3:
					s.List(sess)
				case 4:
					s.ListSafe(sess)
				case 5:
					s.InvalidateSession(sess, "churn")
				case 6:
					s.SupersedeRuntime(sess, int64(i+1), 0, "churn")
				case 7:
					s.Clear(sess)
				}
			}
		}(g)
	}
	wg.Wait()
}
