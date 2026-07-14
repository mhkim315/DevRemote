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

// A1 remediation-2 store tests (R2-A/B/C): store-authoritative claim, canonical
// digest recompute, approval-bound idempotency, fail-closed capacity, fully-bound
// receipt commit, and runtime linearization.

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

func acceptReceipt(b ApprovalExecutionBinding, token string) DeliveryReceipt {
	return DeliveryReceipt{Outcome: DeliveryAccepted, ClaimToken: token, Binding: b, ReceiptID: newReceiptID()}
}

// ── R2-A: store-authoritative claim ──

func TestClaim_StoreRecomputesDigest_ArbitraryAssertionRejected(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	opt := actOpt("approve", "approve", nil)
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{opt})
	// The store computes the digest; an arbitrary caller assertion that does not
	// match the recomputed digest is rejected.
	bad := claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), "k1")
	bad.AssertDigest = "deadbeef"
	if got := s.ClaimForExecution(bad); got.Outcome != ClaimDigestMismatch {
		t.Fatalf("arbitrary digest outcome=%q want digest_mismatch", got.Outcome)
	}
	// The store's own digest is authoritative and correct.
	good := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), "k1"))
	if good.Outcome != ClaimGranted || good.Binding.ActionDigest != digestOf(opt, "") {
		t.Errorf("store digest wrong: %+v", good)
	}
}

func TestClaim_StoredPermissionNotOverridable(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	// A requester WITHOUT the stored terminal:input permission is rejected. There is
	// no request field to weaken/replace the stored requirement.
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

func TestClaim_InputValidatedAgainstStoredSchema(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{
		actOpt("approve", "approve", nil), // no input
		actOpt("send", "neutral", &agent.InputSchema{Required: true, Placement: "as_payload"}),
	})
	// input for a no-input option → invalid
	if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "x", boundRT(), reqCtx(), "k1")); got.Outcome != ClaimInvalidInput {
		t.Errorf("input for no-input option=%q want invalid_input", got.Outcome)
	}
	// missing required input → invalid
	if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "send", "", boundRT(), reqCtx(), "k2")); got.Outcome != ClaimInvalidInput {
		t.Errorf("missing required input=%q want invalid_input", got.Outcome)
	}
}

func TestClaim_RejectsRuntimeAndActionMismatches(t *testing.T) {
	mk := func() *AuthoritativeApprovalStore {
		s, _ := newTestStore(time.Unix(1000, 0))
		seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
		return s
	}
	cases := []struct {
		name string
		rt   RuntimeRef
		opt  string
		want ClaimOutcome
	}{
		{"stale-launch", RuntimeRef{"codex", "0.144.1", 6, 2}, "approve", ClaimStaleRuntime},
		{"stale-stream", RuntimeRef{"codex", "0.144.1", 5, 3}, "approve", ClaimStaleRuntime},
		{"adapter-mismatch", RuntimeRef{"claude", "0.144.1", 5, 2}, "approve", ClaimRuntimeMismatch},
		{"version-mismatch", RuntimeRef{"codex", "9.9.9", 5, 2}, "approve", ClaimRuntimeMismatch},
		{"unknown-action", boundRT(), "ghost", ClaimUnknownAction},
	}
	for _, c := range cases {
		if got := mk().ClaimForExecution(claimReq("codex:s1", "a1", c.opt, "", c.rt, reqCtx(), "k1")); got.Outcome != c.want {
			t.Errorf("%s: outcome=%q want %q", c.name, got.Outcome, c.want)
		}
	}
}

func TestClaim_ConcurrentApproveVsRejectOneOwner(t *testing.T) {
	s := NewAuthoritativeApprovalStore()
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{
		actOpt("approve", "approve", nil), actOpt("reject", "reject", nil),
	})
	rt := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
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
			if s.ClaimForExecution(claimReq("codex:s1", "a1", opt, "", rt, reqCtx(), fmt.Sprintf("k%d", i))).Outcome == ClaimGranted {
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

// ── R2-B: approval-bound idempotency + capacity fail-closed ──

func TestIdempotency_SameKeyAcrossApprovalsIsConflict(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	seedActionable(s, "codex:s1", "a2", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	// claim a1 with key K and accept it
	c := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), "shared"))
	s.RecordDelivery(acceptReceipt(c.Binding, c.Token))
	// approval a2 reusing key K must NOT get already_accepted → conflict
	if got := s.ClaimForExecution(claimReq("codex:s1", "a2", "approve", "", boundRT(), reqCtx(), "shared")); got.Outcome != ClaimConflict {
		t.Errorf("cross-approval key reuse=%q want conflict", got.Outcome)
	}
	// a1 replay with same key+binding → already_accepted
	if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), "shared")); got.Outcome != ClaimAlreadyAccepted {
		t.Errorf("same-approval replay=%q want already_accepted", got.Outcome)
	}
}

func TestIdempotency_SameKeyDifferentDigestConflict(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{
		actOpt("approve", "approve", nil), actOpt("reject", "reject", nil),
	})
	c := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), "k1"))
	if c.Outcome != ClaimGranted {
		t.Fatalf("claim=%q", c.Outcome)
	}
	// same key, different selected action (different digest) → conflict
	if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "reject", "", boundRT(), reqCtx(), "k1")); got.Outcome != ClaimConflict {
		t.Errorf("same key diff digest=%q want conflict", got.Outcome)
	}
}

func TestIdempotency_CapacityFailsClosed(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	// Fill the ledger to capacity with distinct keys (all conflict on a1 after the
	// first grant, but each records its key), forcing capacity.
	s.mu.Lock()
	sess := s.sessions["codex:s1"]
	for i := 0; i < authMaxIdempotencyKeys; i++ {
		sess.idempotency[fmt.Sprintf("filler%d", i)] = idempotencyEntry{}
	}
	s.mu.Unlock()
	if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), "newkey")); got.Outcome != ClaimLedgerFull {
		t.Errorf("full ledger outcome=%q want ledger_full (fail closed)", got.Outcome)
	}
}

func TestIdempotency_NoRetryAfterDeliveryFailure(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	c := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), "k1"))
	// delivery fails
	s.RecordDelivery(DeliveryReceipt{Outcome: DeliveryUnavailable, ClaimToken: c.Token, Binding: c.Binding})
	if snap, _ := s.LookupRecord("codex:s1", "a1"); snap.State != ApprovalDeliveryFailed {
		t.Fatalf("state=%q want delivery_failed", snap.State)
	}
	// A same-key retry does NOT re-execute (record is terminal); frozen no-retry.
	got := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), "k1"))
	if got.Outcome != ClaimAlreadyOwned {
		t.Errorf("retry after failure=%q want already_owned (no re-execution)", got.Outcome)
	}
}

// ── R2-C: fully-bound receipt + linearization ──

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
		"idem-key":   func(r *DeliveryReceipt) { r.Binding.IdempotencyKey = "other" },
		"no-receipt": func(r *DeliveryReceipt) { r.ReceiptID = "" },
	}
	for name, mut := range mutators {
		s, c := mk()
		receipt := acceptReceipt(c.Binding, c.Token)
		mut(&receipt)
		commit := s.RecordDelivery(receipt)
		if commit.Committed {
			t.Errorf("%s mismatch committed a success", name)
		}
	}
	// The fully-correct receipt commits.
	s, c := mk()
	if !s.RecordDelivery(acceptReceipt(c.Binding, c.Token)).Committed {
		t.Error("fully-bound receipt failed to commit")
	}
}

func TestRecordDelivery_SupersededDuringDeliveryCannotCommit(t *testing.T) {
	for _, supersede := range []struct {
		name string
		do   func(s *AuthoritativeApprovalStore)
	}{
		{"launch-replace", func(s *AuthoritativeApprovalStore) { s.SupersedeRuntime("codex:s1", 6, 0, "replaced") }},
		{"stream-change", func(s *AuthoritativeApprovalStore) { s.SupersedeRuntime("codex:s1", 5, 3, "stream") }},
		{"correlation-loss", func(s *AuthoritativeApprovalStore) { s.InvalidateSession("codex:s1", "correlation") }},
	} {
		s, _ := newTestStore(time.Unix(1000, 0))
		seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
		c := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), "k1"))
		if c.Outcome != ClaimGranted {
			t.Fatalf("%s: claim=%q", supersede.name, c.Outcome)
		}
		supersede.do(s) // runtime superseded AFTER claim, BEFORE commit
		commit := s.RecordDelivery(acceptReceipt(c.Binding, c.Token))
		if commit.Committed {
			t.Errorf("%s: superseded delivery committed a stale success", supersede.name)
		}
		if commit.Outcome != DeliveryStaleRuntime {
			t.Errorf("%s: outcome=%q want stale_runtime", supersede.name, commit.Outcome)
		}
	}
}

func TestRecordDelivery_DeleteDuringDeliveryUnavailable(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	c := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), "k1"))
	s.Clear("codex:s1") // delete/unlink between claim and delivery
	if commit := s.RecordDelivery(acceptReceipt(c.Binding, c.Token)); commit.Committed || commit.Outcome != DeliveryUnavailable {
		t.Errorf("delete-during-delivery committed=%v outcome=%q", commit.Committed, commit.Outcome)
	}
}

// ── delivery gate linearization (supplement #4) ──

func TestDeliveryGate_OldGenerationAcceptsNothing(t *testing.T) {
	g := NewRuntimeDeliveryGate()
	rtA := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	rtB := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 6, StreamGen: 0}
	g.SetActive("codex:s1", rtA)
	if !g.AcceptDelivery("codex:s1", rtA) {
		t.Fatal("current runtime should accept")
	}
	// replacement begins → old generation accepts nothing, new does
	g.SetActive("codex:s1", rtB)
	if g.AcceptDelivery("codex:s1", rtA) {
		t.Error("old generation accepted delivery after replacement")
	}
	if !g.AcceptDelivery("codex:s1", rtB) {
		t.Error("new generation should accept")
	}
	// deactivate (delete/unlink/termination) → nothing accepts
	g.Deactivate("codex:s1")
	if g.AcceptDelivery("codex:s1", rtB) {
		t.Error("deactivated runtime accepted delivery")
	}
}

// ── restart / churn ──

func TestClaim_RestartNoResidualAuthority(t *testing.T) {
	s1, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s1, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	c := s1.ClaimForExecution(claimReq("codex:s1", "a1", "approve", "", boundRT(), reqCtx(), "k1"))
	s2 := NewAuthoritativeApprovalStore()
	if s2.RecordDelivery(acceptReceipt(c.Binding, c.Token)).Committed {
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
						s.RecordDelivery(acceptReceipt(c.Binding, c.Token))
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
