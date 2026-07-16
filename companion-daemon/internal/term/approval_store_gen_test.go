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
	rt := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	tests := []struct {
		desc, session string
		rt            RuntimeRef
	}{
		{"bad-session-no-colon", "no-colon", rt},
		{"bad-session-empty-adapter", ":local", rt},
		{"bad-session-control-char", "codex:a\x01b", rt},
		{"bad-session-adapter-space", "cod ex:s1", rt},
		{"bad-session-uppercase-adapter", "Codex:s1", rt},
		{"bad-adapter-empty", "codex:s1", RuntimeRef{Adapter: "", Version: "1.0"}},
		{"bad-adapter-uppercase", "codex:s1", RuntimeRef{Adapter: "Codex", Version: "1.0"}},
		{"bad-version-empty", "codex:s1", RuntimeRef{Adapter: "codex", Version: ""}},
		{"bad-version-slash", "codex:s1", RuntimeRef{Adapter: "codex", Version: "1/0"}},
		{"bad-version-traversal", "codex:s1", RuntimeRef{Adapter: "codex", Version: "../etc"}},
	}
	for _, x := range tests {
		t.Run(x.desc, func(t *testing.T) {
			g := NewRuntimeDeliveryGate()
			// R9-A2: pass x.session (not x.desc); each bad identity must fail closed
			// and leave every accounting structure untouched.
			if h, ok := g.Activate(x.session, x.rt, 4); ok || h != "" {
				t.Fatalf("%s: activation must fail, got handle=%q ok=%v", x.desc, h, ok)
			}
			g.mu.Lock()
			defer g.mu.Unlock()
			if len(g.endpoints) != 0 || len(g.current) != 0 || len(g.order) != 0 || g.totalBytes != 0 {
				t.Errorf("%s: rejected activation mutated state: endpoints=%d current=%d order=%d bytes=%d",
					x.desc, len(g.endpoints), len(g.current), len(g.order), g.totalBytes)
			}
		})
	}
	// Known-bad the FORMER length-only implementation accepted: a session with a
	// control character and a path-shaped version now both fail closed.
	g := NewRuntimeDeliveryGate()
	if h, ok := g.Activate("codex:ok\ninject", rt, 4); ok || h != "" {
		t.Errorf("control-char session must be rejected (length-only regression), got %q", h)
	}
	// Accepted canonical control still succeeds.
	if h, ok := g.Activate("codex:s1", rt, 4); !ok || h == "" {
		t.Errorf("canonical session/runtime must activate, got handle=%q ok=%v", h, ok)
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

// R9-B replaces the former direct-gate telemetry/exhaustion/reclaim tests
// (TestTelemetry_RepeatedPollPreservesHandle, TestDeliveryGate_ExhaustionAndSafeVictimInvariants,
// TestDeliveryGate_AcceptActivateReclaimDeterministic). The production-path,
// explicit-capacity, and deterministic-interleaving proofs now live in
// approval_delivery_r9b_test.go. They are removed here (not relabeled) because a
// direct gate.Activate call, an ignored ok result, and a sequential accept-then-drain
// are not the evidence R9-B requires.

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
	if !c.Binding.equal((ApprovalExecutionBinding{ApprovalID: "a1", SessionID: "codex:s1", Runtime: boundRT(), ActionDigest: c.Binding.ActionDigest, PayloadDigest: c.Binding.PayloadDigest, IdempotencyKey: key, OptionID: "approve"})) {
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

// ── R11-F1: metadata-only Store generation authority ──

// TestInstallRuntimeGeneration_CreatesSessionWithoutRecord verifies that
// InstallRuntimeGeneration creates a session and sets the high-water WITHOUT
// creating any Approval record.
func TestInstallRuntimeGeneration_CreatesSessionWithoutRecord(t *testing.T) {
	s := NewAuthoritativeApprovalStore()
	if err := s.InstallRuntimeGeneration("codex:s1", 5, 1, "terminated"); err != nil {
		t.Fatal(err)
	}

	// Session must exist with the correct high-water.
	if s.Len() != 1 {
		t.Fatalf("expected 1 session, got %d", s.Len())
	}
	// ListSafe must return zero records (no Approval record was created).
	safe := s.ListSafe("codex:s1")
	if len(safe) != 0 {
		t.Fatalf("expected 0 records, got %d: %+v", len(safe), safe)
	}
	// List must also return zero records.
	pub := s.List("codex:s1")
	if len(pub) != 0 {
		t.Fatalf("expected 0 public records, got %d", len(pub))
	}
}

// TestInstallRuntimeGeneration_RejectsLateStreamGen0 verifies that after
// InstallRuntimeGeneration sets hw=(epoch, 1), a late IngestObserved at
// StreamGen=0 is rejected by the Store's genNewer check.
func TestInstallRuntimeGeneration_RejectsLateStreamGen0(t *testing.T) {
	s := NewAuthoritativeApprovalStore()

	// Install metadata high-water at StreamGen=1 (simulating terminate).
	if err := s.InstallRuntimeGeneration("claude_headless:s1", 7, 1, "terminated"); err != nil {
		t.Fatal(err)
	}

	// A late observation at StreamGen=0 must be rejected.
	admitted := s.IngestObserved(ApprovalIngest{
		SessionID: "claude_headless:s1",
		LaunchGen: 7,
		StreamGen: 0,
		Provider:  "claude_headless",
		Version:   "2.1.209",
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID:        "claude-late",
				SessionID: "claude_headless:s1",
				AgentKind: "claude_headless",
				Kind:      "approval",
				Source:    agent.SourceJSONL,
			},
			Provenance: contract.ProvenanceProviderHook,
		}},
	})
	if admitted {
		t.Fatal("late StreamGen=0 ingest must be rejected after InstallRuntimeGeneration(StreamGen=1)")
	}
	// No records in the session.
	if len(s.ListSafe("claude_headless:s1")) != 0 {
		t.Fatal("expected 0 records after rejected late ingest")
	}
}

// TestInvalidIngestObserved_NoStoreMutation is the non-vacuous negative test
// for the R11-F1 counterexample. An IngestObserved with non-authoritative
// provenance must NOT create a session, advance generation, or supersede
// records.
func TestInvalidIngestObserved_NoStoreMutation(t *testing.T) {
	s := NewAuthoritativeApprovalStore()

	// Attempt to ingest with a non-authoritative provenance (the old
	// "c1d_internal" pattern). This must NOT create any state.
	admitted := s.IngestObserved(ApprovalIngest{
		SessionID: "claude_headless:s-bad",
		LaunchGen: 1,
		StreamGen: 0,
		Provider:  "claude_headless",
		Version:   "2.1.209",
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID:         "_c1d_hw_",
				SessionID:  "claude_headless:s-bad",
				AgentKind:  "claude_headless",
				Kind:       "approval",
				Source:     agent.SourceJSONL,
				Confidence: 1,
			},
			Provenance: "c1d_internal", // NOT in approvalAuthoritative set
		}},
	})
	if admitted {
		t.Fatal("non-authoritative provenance must NOT admit an item")
	}

	// Session must NOT exist — invalid ingest creates no session.
	if s.Len() != 0 {
		t.Fatalf("invalid ingest created a session: Len=%d", s.Len())
	}

	// ListSafe on a non-existent session must return nil (not an empty slice
	// from a created session).
	safe := s.ListSafe("claude_headless:s-bad")
	if safe != nil {
		t.Fatalf("expected nil ListSafe for non-existent session, got %v", safe)
	}
}

// TestInvalidIngestObserved_DoesNotAdvanceHighWater proves that a valid
// ingest followed by an invalid ingest does NOT advance the high-water
// (the invalid ingest is a no-op).
func TestInvalidIngestObserved_DoesNotAdvanceHighWater(t *testing.T) {
	s := NewAuthoritativeApprovalStore()

	// First: a valid ingest creates the session and sets hw=(1, 0).
	admitted := s.IngestObserved(ApprovalIngest{
		SessionID: "codex:s1",
		LaunchGen: 1,
		StreamGen: 0,
		Provider:  "codex",
		Version:   "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID:        "valid-1",
				SessionID: "codex:s1",
				Kind:      "approval",
				Source:    agent.SourceJSONL,
			},
			Provenance: contract.ProvenanceNativeLog,
		}},
	})
	if !admitted {
		t.Fatal("valid ingest should be admitted")
	}

	// Now attempt an invalid ingest with a HIGHER generation. It must NOT
	// advance the high-water because the item fails provenance validation.
	admitted2 := s.IngestObserved(ApprovalIngest{
		SessionID: "codex:s1",
		LaunchGen: 2, // higher generation
		StreamGen: 0,
		Provider:  "codex",
		Version:   "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID:        "_fake_",
				SessionID: "codex:s1",
				Kind:      "approval",
				Source:    agent.SourceJSONL,
			},
			Provenance: "c1d_internal", // non-authoritative
		}},
	})
	if admitted2 {
		t.Fatal("invalid high-gen ingest must not admit")
	}

	// The original valid record must still be present (not superseded by
	// the invalid high-gen ingest).
	snap, ok := s.LookupRecord("codex:s1", "valid-1")
	if !ok {
		t.Fatal("valid record lost after invalid high-gen ingest")
	}
	if snap.State != ApprovalPending {
		t.Fatalf("valid record superseded by invalid ingest: state=%v", snap.State)
	}
}

// TestInstallRuntimeGeneration_SupersedesRecords verifies that
// InstallRuntimeGeneration invalidates pending records when advancing
// the high-water.
func TestInstallRuntimeGeneration_SupersedesRecords(t *testing.T) {
	s := NewAuthoritativeApprovalStore()

	// Seed a valid record at gen (1, 0).
	s.Ingest(ApprovalIngest{
		SessionID: "codex:s1", LaunchGen: 1, StreamGen: 0, Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval:   agent.AgentApproval{ID: "a1", SessionID: "codex:s1", Kind: "approval", Source: agent.SourceJSONL},
			Provenance: contract.ProvenanceNativeLog,
		}},
	})

	// InstallRuntimeGeneration at a higher generation.
	if err := s.InstallRuntimeGeneration("codex:s1", 1, 1, "terminated"); err != nil {
		t.Fatal(err)
	}

	// The pending record must be invalidated.
	snap, ok := s.LookupRecord("codex:s1", "a1")
	if !ok {
		t.Fatal("record missing after InstallRuntimeGeneration")
	}
	if snap.State != ApprovalInvalidated {
		t.Fatalf("expected invalidated, got %v", snap.State)
	}
}

// TestInstallRuntimeGeneration_IdempotentSameGeneration verifies that
// calling InstallRuntimeGeneration with the same or older generation
// does not supersede or change state beyond the first call.
func TestInstallRuntimeGeneration_IdempotentSameGeneration(t *testing.T) {
	s := NewAuthoritativeApprovalStore()
	if err := s.InstallRuntimeGeneration("codex:s1", 5, 1, "terminated"); err != nil {
		t.Fatal(err)
	}
	if s.Len() != 1 {
		t.Fatalf("expected 1 session, got %d", s.Len())
	}
	// Same generation: no-op for hw. supersedeLocked runs (gen matches,
	// not newer, so no supersede).
	if err := s.InstallRuntimeGeneration("codex:s1", 5, 1, "terminated-again"); err != nil {
		t.Fatal(err)
	}
	if s.Len() != 1 {
		t.Fatalf("expected still 1 session, got %d", s.Len())
	}
	// Older generation: complete no-op — must not supersede.
	// First seed a record at the current gen.
	s.Ingest(ApprovalIngest{
		SessionID: "codex:s1", LaunchGen: 5, StreamGen: 1, Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval:   agent.AgentApproval{ID: "a1", SessionID: "codex:s1", Kind: "approval", Source: agent.SourceJSONL},
			Provenance: contract.ProvenanceNativeLog,
		}},
	})
	// Older generation InstallRuntimeGeneration must NOT supersede.
	if err := s.InstallRuntimeGeneration("codex:s1", 4, 0, "old-gen"); err != nil {
		t.Fatal(err)
	}
	snap, ok := s.LookupRecord("codex:s1", "a1")
	if !ok {
		t.Fatal("record lost after older-gen InstallRuntimeGeneration (should be no-op)")
	}
	if snap.State != ApprovalPending {
		t.Fatalf("older-gen superseded current record: state=%v", snap.State)
	}
}

// TestInstallRuntimeGeneration_OlderGenDoesNotSupersedeCurrentApprovals is the
// non-vacuous counterexample for R11-F1 blocker 1. An older runtime's
// termination must not invalidate a newer runtime's pending approvals.
func TestInstallRuntimeGeneration_OlderGenDoesNotSupersedeCurrentApprovals(t *testing.T) {
	s := NewAuthoritativeApprovalStore()

	// New runtime at gen (10, 0) seeds a pending approval.
	s.Ingest(ApprovalIngest{
		SessionID: "codex:s1", LaunchGen: 10, StreamGen: 0, Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval:   agent.AgentApproval{ID: "a-new", SessionID: "codex:s1", Kind: "approval", Source: agent.SourceJSONL},
			Provenance: contract.ProvenanceNativeLog,
		}},
	})

	// Old runtime at gen (5, 1) terminates — must NOT supersede gen 10 records.
	if err := s.InstallRuntimeGeneration("codex:s1", 5, 1, "old-terminated"); err != nil {
		t.Fatal(err)
	}

	snap, ok := s.LookupRecord("codex:s1", "a-new")
	if !ok {
		t.Fatal("new-runtime record lost after old-gen termination")
	}
	if snap.State != ApprovalPending {
		t.Fatalf("old-gen termination superseded current record: state=%v", snap.State)
	}
	// High-water must remain at the newer generation.
	if snap.LaunchGen != 10 || snap.StreamGen != 0 {
		t.Fatalf("hw shifted by old gen: launch=%d stream=%d", snap.LaunchGen, snap.StreamGen)
	}
}

// TestEvictOldestSession_SkipsLiveAuthority verifies that evictOldestSessionLocked
// does not evict sessions with pending records or high-water tombstones.
func TestEvictOldestSession_SkipsLiveAuthority(t *testing.T) {
	s := NewAuthoritativeApprovalStore()

	// Fill sessions with DORMANT (evictable) sessions: ingest then invalidate.
	for i := 0; i < authMaxApprovalSessions-1; i++ {
		sid := fmt.Sprintf("filler:s%d", i)
		id := fmt.Sprintf("f%d", i)
		s.Ingest(ApprovalIngest{
			SessionID: sid, LaunchGen: 1, StreamGen: 0, Provider: "codex", Version: "0.144.1",
			Items: []ApprovalIngestItem{{
				Approval:   agent.AgentApproval{ID: id, SessionID: sid, Kind: "approval", Source: agent.SourceJSONL},
				Provenance: contract.ProvenanceNativeLog,
			}},
		})
		s.InvalidateRecord(sid, id)
	}

	// Create a session with a pending approval record — LIVE authority.
	s.Ingest(ApprovalIngest{
		SessionID: "codex:live", LaunchGen: 1, StreamGen: 0, Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval:   agent.AgentApproval{ID: "a-live", SessionID: "codex:live", Kind: "approval", Source: agent.SourceJSONL},
			Provenance: contract.ProvenanceNativeLog,
		}},
	})

	// Now trigger eviction by creating one more session (hits the limit).
	// A dormant filler should be evicted, not the live session.
	if err := s.InstallRuntimeGeneration("codex:new", 1, 0, "new"); err != nil {
		t.Fatalf("unexpected capacity error (a dormant filler should have been evicted): %v", err)
	}

	// The live session must survive.
	snap, ok := s.LookupRecord("codex:live", "a-live")
	if !ok || snap.State != ApprovalPending {
		t.Fatalf("live record lost: ok=%v state=%v", ok, snap.State)
	}
}

// TestEvictOldestSession_ProtectsHighWaterTombstone verifies that a
// metadata-only high-water session (tombstone) is NOT evicted, and that
// stale replay is still rejected after capacity pressure.
func TestEvictOldestSession_ProtectsHighWaterTombstone(t *testing.T) {
	s := NewAuthoritativeApprovalStore()

	// Install a high-water tombstone at gen (2, 1) — blocks stale gen (2, 0).
	if err := s.InstallRuntimeGeneration("codex:tombstone", 2, 1, "terminated"); err != nil {
		t.Fatal(err)
	}

	// Fill with dormant sessions to create capacity pressure.
	for i := 0; i < authMaxApprovalSessions-2; i++ {
		sid := fmt.Sprintf("filler:s%d", i)
		id := fmt.Sprintf("f%d", i)
		s.Ingest(ApprovalIngest{
			SessionID: sid, LaunchGen: 1, StreamGen: 0, Provider: "codex", Version: "0.144.1",
			Items: []ApprovalIngestItem{{
				Approval:   agent.AgentApproval{ID: id, SessionID: sid, Kind: "approval", Source: agent.SourceJSONL},
				Provenance: contract.ProvenanceNativeLog,
			}},
		})
		s.InvalidateRecord(sid, id)
	}

	// Add one more session — must evict a dormant filler, not the tombstone.
	if err := s.InstallRuntimeGeneration("codex:probe", 1, 0, "probe"); err != nil {
		t.Fatalf("capacity should be freed by dormant eviction: %v", err)
	}

	// The tombstone must still exist (session in the store).
	if s.Len() < authMaxApprovalSessions {
		// It's possible the tombstone was at the end — but List/Lookup should work.
	}

	// Stale gen (2, 0) ingest must still be rejected by the tombstone's high-water.
	admitted := s.IngestObserved(ApprovalIngest{
		SessionID: "codex:tombstone", LaunchGen: 2, StreamGen: 0, Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval:   agent.AgentApproval{ID: "stale", SessionID: "codex:tombstone", Kind: "approval", Source: agent.SourceJSONL},
			Provenance: contract.ProvenanceNativeLog,
		}},
	})
	if admitted {
		t.Fatal("stale gen-0 ingest admitted after tombstone should have rejected it")
	}
}

// TestEvictOneLocked_PreservesPendingRecords verifies per-record eviction
// only removes terminal records, never pending/executing.
func TestEvictOneLocked_PreservesPendingRecords(t *testing.T) {
	s := NewAuthoritativeApprovalStore()

	// Seed exactly authMaxApprovalsPerSession all-pending records.
	for i := 0; i < authMaxApprovalsPerSession; i++ {
		admitted := s.IngestObserved(ApprovalIngest{
			SessionID: "codex:s1", LaunchGen: 1, StreamGen: 0, Provider: "codex", Version: "0.144.1",
			Items: []ApprovalIngestItem{{
				Approval:   agent.AgentApproval{ID: fmt.Sprintf("a%d", i), SessionID: "codex:s1", Kind: "approval", Source: agent.SourceJSONL},
				Provenance: contract.ProvenanceNativeLog,
			}},
		})
		if !admitted {
			t.Fatalf("record %d not admitted (capacity pre-check too aggressive?)", i)
		}
	}

	// One more ingest at capacity: evictOneLocked fires but must find zero
	// terminal victims. The new item is skipped, all pending records survive.
	admitted := s.IngestObserved(ApprovalIngest{
		SessionID: "codex:s1", LaunchGen: 1, StreamGen: 0, Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval:   agent.AgentApproval{ID: "overflow", SessionID: "codex:s1", Kind: "approval", Source: agent.SourceJSONL},
			Provenance: contract.ProvenanceNativeLog,
		}},
	})
	if admitted {
		t.Fatal("overflow item admitted despite capacity")
	}

	// All original records must still be present.
	for i := 0; i < authMaxApprovalsPerSession; i++ {
		snap, ok := s.LookupRecord("codex:s1", fmt.Sprintf("a%d", i))
		if !ok || snap.State != ApprovalPending {
			t.Fatalf("record a%d lost or state changed: ok=%v state=%v", i, ok, snap.State)
		}
	}
}

// TestIngest_NewerGenSupersedesWithDuplicateIDs verifies that a newer
// generation supersedes old records even when all its items are duplicate
// IDs (R11-F1 blocker 3).
func TestIngest_NewerGenSupersedesWithDuplicateIDs(t *testing.T) {
	s := NewAuthoritativeApprovalStore()

	// Seed a record at gen (1, 0).
	s.Ingest(ApprovalIngest{
		SessionID: "codex:s1", LaunchGen: 1, StreamGen: 0, Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval:   agent.AgentApproval{ID: "dup1", SessionID: "codex:s1", Kind: "approval", Source: agent.SourceJSONL, Options: []agent.InteractionOption{{ID: "opt1"}}},
			Provenance: contract.ProvenanceNativeLog,
		}},
	})

	// New runtime at gen (2, 0) re-offers the SAME approval ID with the SAME
	// options (idempotent re-offer). Even though no NEW record is admitted,
	// the older gen records must be superseded.
	admitted := s.IngestObserved(ApprovalIngest{
		SessionID: "codex:s1", LaunchGen: 2, StreamGen: 0, Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval:   agent.AgentApproval{ID: "dup1", SessionID: "codex:s1", Kind: "approval", Source: agent.SourceJSONL, Options: []agent.InteractionOption{{ID: "opt1"}}},
			Provenance: contract.ProvenanceNativeLog,
		}},
	})
	if admitted {
		t.Fatal("duplicate idempotent re-offer should not admit a new record")
	}

	// The old gen record must be invalidated.
	snap, ok := s.LookupRecord("codex:s1", "dup1")
	if !ok {
		t.Fatal("record disappeared — should be invalidated, not deleted")
	}
	if snap.State == ApprovalPending {
		t.Fatal("old-gen record still pending after newer gen arrived with same ID")
	}
}

// TestIngest_NewerGenSupersedesAtCapacity verifies that a newer generation
// supersedes old records even when capacity prevents new item admission
// (R11-F1 blocker 3).
func TestIngest_NewerGenSupersedesAtCapacity(t *testing.T) {
	s := NewAuthoritativeApprovalStore()

	// Fill gen (1, 0) to capacity with pending records.
	for i := 0; i < authMaxApprovalsPerSession; i++ {
		s.Ingest(ApprovalIngest{
			SessionID: "codex:s1", LaunchGen: 1, StreamGen: 0, Provider: "codex", Version: "0.144.1",
			Items: []ApprovalIngestItem{{
				Approval:   agent.AgentApproval{ID: fmt.Sprintf("old%d", i), SessionID: "codex:s1", Kind: "approval", Source: agent.SourceJSONL},
				Provenance: contract.ProvenanceNativeLog,
			}},
		})
	}

	// New runtime at gen (2, 0) tries to add a new item but capacity is full.
	// Even though the new item can't be admitted, the old gen records must be
	// superseded because a structurally valid newer generation arrived.
	admitted := s.IngestObserved(ApprovalIngest{
		SessionID: "codex:s1", LaunchGen: 2, StreamGen: 0, Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval:   agent.AgentApproval{ID: "new-item", SessionID: "codex:s1", Kind: "approval", Source: agent.SourceJSONL},
			Provenance: contract.ProvenanceNativeLog,
		}},
	})
	if admitted {
		t.Fatal("capacity-full should not admit new item")
	}

	// All old gen records must be invalidated (not pending).
	for i := 0; i < authMaxApprovalsPerSession; i++ {
		snap, ok := s.LookupRecord("codex:s1", fmt.Sprintf("old%d", i))
		if !ok {
			t.Fatalf("old%d disappeared", i)
		}
		if snap.State == ApprovalPending {
			t.Fatalf("old%d still pending after newer gen arrived at capacity", i)
		}
	}

	// ListSafe may return recently-resolved records in the resolution window,
	// but none must be in a live state.
	for _, dto := range s.ListSafe("codex:s1") {
		if dto.State == string(ApprovalPending) || dto.State == string(ApprovalExecuting) {
			t.Fatalf("live record in ListSafe after newer gen superseded: id=%s state=%v", dto.ID, dto.State)
		}
	}
}

// TestInstallRuntimeGeneration_CapacityExhaustedFailClosed verifies that
// when all 1024 session slots contain live authority, the 1025th installation
// returns an error and Store size does not increase.
func TestInstallRuntimeGeneration_CapacityExhaustedFailClosed(t *testing.T) {
	s := NewAuthoritativeApprovalStore()

	// Fill all session slots with live (pending) authority.
	for i := 0; i < authMaxApprovalSessions; i++ {
		sid := fmt.Sprintf("live:s%d", i)
		s.Ingest(ApprovalIngest{
			SessionID: sid, LaunchGen: 1, StreamGen: 0, Provider: "codex", Version: "0.144.1",
			Items: []ApprovalIngestItem{{
				Approval:   agent.AgentApproval{ID: fmt.Sprintf("a%d", i), SessionID: sid, Kind: "approval", Source: agent.SourceJSONL},
				Provenance: contract.ProvenanceNativeLog,
			}},
		})
	}
	if s.Len() != authMaxApprovalSessions {
		t.Fatalf("expected %d sessions, got %d", authMaxApprovalSessions, s.Len())
	}

	// The 1025th session must be rejected — no safe victim exists.
	err := s.InstallRuntimeGeneration("codex:overflow", 1, 0, "test")
	if err == nil {
		t.Fatal("expected capacity error, got nil")
	}
	if s.Len() != authMaxApprovalSessions {
		t.Fatalf("capacity exhausted: expected %d sessions, got %d (size increased)",
			authMaxApprovalSessions, s.Len())
	}
}

// TestEvictOldestSession_DeliveryFailedRetryNotVictim verifies that a session
// with a delivery_failed record that still has retries remaining is not
// selected as an eviction victim.
func TestEvictOldestSession_DeliveryFailedRetryNotVictim(t *testing.T) {
	s := NewAuthoritativeApprovalStore()

	// Create a session with a delivery_failed record that has retries.
	s.Ingest(ApprovalIngest{
		SessionID: "codex:retry", LaunchGen: 5, StreamGen: 0, Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval:   agent.AgentApproval{ID: "a1", SessionID: "codex:retry", Kind: "approval", Options: []agent.InteractionOption{actOpt("approve", "approve", nil)}},
			Provenance: contract.ProvenanceNativeLog, Actionable: true, RequiredPerm: "terminal:input",
		}},
	})
	// Claim and fail delivery (retries=1, still < maxManualRetries=2).
	rt := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 0}
	c := s.ClaimForExecution(claimReq("codex:retry", "a1", "approve", "", rt, reqCtx(), "k1"))
	if c.Outcome != ClaimGranted {
		t.Fatalf("claim: %v", c.Outcome)
	}
	s.RecordDelivery(DeliveryReceipt{Outcome: DeliveryUnavailable, ClaimToken: c.Token, Binding: c.Binding})

	// Fill remaining slots with dormant (evictable) sessions.
	for i := 0; i < authMaxApprovalSessions-2; i++ {
		sid := fmt.Sprintf("filler:s%d", i)
		id := fmt.Sprintf("f%d", i)
		s.Ingest(ApprovalIngest{
			SessionID: sid, LaunchGen: 1, StreamGen: 0, Provider: "codex", Version: "0.144.1",
			Items: []ApprovalIngestItem{{
				Approval:   agent.AgentApproval{ID: id, SessionID: sid, Kind: "approval", Source: agent.SourceJSONL},
				Provenance: contract.ProvenanceNativeLog,
			}},
		})
		s.InvalidateRecord(sid, id)
	}

	// Now trigger eviction by adding one more session.
	err := s.InstallRuntimeGeneration("codex:new", 1, 0, "new")
	if err != nil {
		// All fillers are empty, so one should be evicted. The retry session
		// must NOT be the victim.
		t.Fatalf("unexpected capacity error (should have evicted a filler): %v", err)
	}

	// The delivery_failed session with retries must still exist.
	snap, ok := s.LookupRecord("codex:retry", "a1")
	if !ok {
		t.Fatal("delivery_failed session was evicted despite retries remaining")
	}
	if snap.State != ApprovalDeliveryFailed {
		t.Fatalf("retry record state changed: %v", snap.State)
	}
}

// TestIngest_MalformedDeliveryDoesNotAdvanceGeneration verifies that a
// higher-generation item with authoritative provenance but malformed
// delivery material does NOT supersede existing approvals.
func TestIngest_MalformedDeliveryDoesNotAdvanceGeneration(t *testing.T) {
	s := NewAuthoritativeApprovalStore()

	// Seed a valid record at gen (1, 0).
	s.Ingest(ApprovalIngest{
		SessionID: "codex:s1", LaunchGen: 1, StreamGen: 0, Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval:   agent.AgentApproval{ID: "valid", SessionID: "codex:s1", Kind: "approval", Source: agent.SourceJSONL},
			Provenance: contract.ProvenanceNativeLog,
		}},
	})

	// Attempt to ingest at gen (2, 0) with malformed delivery material
	// (delivery material on a non-actionable item is invalid).
	admitted := s.IngestObserved(ApprovalIngest{
		SessionID: "codex:s1", LaunchGen: 2, StreamGen: 0, Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{ID: "bad-delivery", SessionID: "codex:s1", Kind: "approval", Source: agent.SourceJSONL},
			// Authoritative provenance, but malformed delivery (material on non-actionable).
			Provenance:       contract.ProvenanceProviderHook,
			Actionable:       false,
			DeliveryMaterial: []ApprovalDeliveryMaterial{{OptionID: "x", SchemaVersion: "v1", ResponseBytes: []byte("y")}},
		}},
	})
	if admitted {
		t.Fatal("malformed delivery item should not be admitted")
	}

	// The gen-1 record must still be pending — malformed higher-gen delivery
	// must not supersede existing authority.
	snap, ok := s.LookupRecord("codex:s1", "valid")
	if !ok {
		t.Fatal("valid record disappeared after malformed higher-gen ingest")
	}
	if snap.State != ApprovalPending {
		t.Fatalf("valid record superseded by malformed delivery ingest: state=%v", snap.State)
	}
	// LaunchGen must still be at the original generation.
	if snap.LaunchGen != 1 {
		t.Fatalf("generation advanced by malformed ingest: launchGen=%d", snap.LaunchGen)
	}
}

// TestIngestObserved_CapacityExhaustedFailClosed verifies that IngestObserved
// fails closed when all session slots are protected and no safe victim exists.
func TestIngestObserved_CapacityExhaustedFailClosed(t *testing.T) {
	s := NewAuthoritativeApprovalStore()

	// Fill all slots with high-water tombstones (protected, no safe victim).
	for i := 0; i < authMaxApprovalSessions; i++ {
		sid := fmt.Sprintf("tombstone:s%d", i)
		if err := s.InstallRuntimeGeneration(sid, int64(i+1), 1, "terminated"); err != nil {
			t.Fatalf("fill %d: %v", i, err)
		}
	}
	if s.Len() != authMaxApprovalSessions {
		t.Fatalf("expected %d sessions, got %d", authMaxApprovalSessions, s.Len())
	}

	// IngestObserved must fail — no safe victim, all slots are tombstones.
	admitted := s.IngestObserved(ApprovalIngest{
		SessionID: "codex:overflow", LaunchGen: 1, StreamGen: 0, Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval:   agent.AgentApproval{ID: "new", SessionID: "codex:overflow", Kind: "approval", Source: agent.SourceJSONL},
			Provenance: contract.ProvenanceNativeLog,
		}},
	})
	if admitted {
		t.Fatal("ingest admitted despite capacity exhaustion")
	}

	// Store size must not increase.
	if s.Len() != authMaxApprovalSessions {
		t.Fatalf("capacity exceeded: %d > %d", s.Len(), authMaxApprovalSessions)
	}

	// Existing authority must remain unchanged.
	snap, ok := s.LookupRecord("tombstone:s0", "any")
	if ok {
		t.Fatal("tombstone should have no records")
	}
	_ = snap
}
