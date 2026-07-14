package term

import (
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
		ReceiptID: newReceiptID(), DeliveredPayloadDigest: payloadDigest(c.Payload),
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
	weak := RequesterContext{DeviceID: "d", BearerSessionID: "b", Permissions: []string{"sessions:read"}}
	if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), weak, "k1")); got.Outcome != ClaimUnauthorized {
		t.Errorf("weak requester outcome=%q want unauthorized", got.Outcome)
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
		ReceiptID: newReceiptID(), DeliveredPayloadDigest: payloadDigest([]byte("substituted\n")),
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

// ── R3-C: gate linearization ──

type barrierSink struct {
	started chan struct{}
	proceed chan struct{}
	count   *int32
}

func (b *barrierSink) Enqueue(p []byte) string {
	if b.started != nil {
		b.started <- struct{}{}
		<-b.proceed
	}
	atomic.AddInt32(b.count, 1)
	return "rid"
}

func TestDeliveryGate_OldGenerationAcceptsNothing(t *testing.T) {
	g := NewRuntimeDeliveryGate()
	var count int32
	sink := &barrierSink{count: &count}
	rtA := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	rtB := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 6, StreamGen: 0}
	g.Activate("codex:s1", rtA, sink)
	if _, ok := g.Accept("codex:s1", rtA, []byte("x")); !ok {
		t.Fatal("current runtime should accept")
	}
	g.Activate("codex:s1", rtB, sink) // replacement
	if _, ok := g.Accept("codex:s1", rtA, []byte("x")); ok {
		t.Error("old generation accepted after replacement")
	}
	if _, ok := g.Accept("codex:s1", rtB, []byte("x")); !ok {
		t.Error("new generation should accept")
	}
	g.Deactivate("codex:s1")
	if _, ok := g.Accept("codex:s1", rtB, []byte("x")); ok {
		t.Error("deactivated runtime accepted")
	}
	// no sink → unavailable
	g.Activate("codex:s2", rtA, nil)
	if _, ok := g.Accept("codex:s2", rtA, []byte("x")); ok {
		t.Error("nil sink accepted")
	}
}

// The accept enqueue happens UNDER the gate lock, so a replacement cannot interleave
// between the current-generation check and the enqueue. Barrier-controlled, no sleeps.
func TestDeliveryGate_AcceptAtomicVsReplacement(t *testing.T) {
	g := NewRuntimeDeliveryGate()
	var count int32
	sink := &barrierSink{started: make(chan struct{}), proceed: make(chan struct{}), count: &count}
	rtA := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	rtB := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 6, StreamGen: 0}
	g.Activate("codex:s1", rtA, sink)

	acceptedCh := make(chan bool, 1)
	go func() {
		_, ok := g.Accept("codex:s1", rtA, []byte("bytes")) // holds gate lock inside Enqueue
		acceptedCh <- ok
	}()
	<-sink.started // accept is now inside the lock, mid-enqueue

	replaced := make(chan struct{})
	go func() {
		g.Activate("codex:s1", rtB, sink) // blocks on the gate lock until accept releases it
		close(replaced)
	}()

	// Negative control: bypassing the gate (calling the sink directly) accepts bytes
	// regardless of generation — proving the gate is what provides the property.
	direct := &barrierSink{count: &count}
	before := atomic.LoadInt32(&count)
	direct.Enqueue([]byte("bypass"))
	if atomic.LoadInt32(&count) != before+1 {
		t.Fatal("negative control: direct sink enqueue should always count")
	}

	sink.proceed <- struct{}{} // let the in-lock enqueue complete
	if ok := <-acceptedCh; !ok {
		t.Fatal("accept that began while rtA was current must succeed atomically")
	}
	<-replaced // replacement applied only after accept released the lock

	// After replacement, the old generation accepts nothing.
	if _, ok := g.Accept("codex:s1", rtA, []byte("x")); ok {
		t.Error("old generation accepted after replacement completed")
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
