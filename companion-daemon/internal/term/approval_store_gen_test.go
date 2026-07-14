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

// A1 remediation store tests (B1-B4): atomic ClaimForExecution full-binding,
// canonical digest binding, idempotency, delivery receipt, and lifecycle races.

func actOpt(id, kind string, input *agent.InputSchema) agent.InteractionOption {
	return agent.InteractionOption{ID: id, Label: id, Kind: kind, Input: input}
}

func newTestStore(t0 time.Time) (*AuthoritativeApprovalStore, *time.Time) {
	s := NewAuthoritativeApprovalStore()
	clk := t0
	s.now = func() time.Time { return clk }
	return s, &clk
}

// seedActionable ingests one ACTIONABLE approval (a proven mapping is simulated for
// the test — this proves the store/claim mechanism, NOT a production provider path).
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

func boundRuntime() RuntimeRef {
	return RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
}

func claimReq(sessionID, id, optionID, digest string, rt RuntimeRef, req RequesterContext, key string) ClaimRequest {
	return ClaimRequest{
		SessionID: sessionID, ApprovalID: id, OptionID: optionID, Runtime: rt,
		ActionDigest: digest, Requester: req, RequiredPerm: "terminal:input", IdempotencyKey: key,
	}
}

func digestFor(optID, kind string) string {
	return CanonicalAction{OptionID: optID, Kind: kind, SchemaVersion: ActionSchemaVersion}.Digest()
}

// ── B2: canonical digest ──

func TestDigest_BindsSelectedActionNotOptionList(t *testing.T) {
	a := CanonicalAction{OptionID: "approve", Kind: "approve", SchemaVersion: ActionSchemaVersion}
	b := CanonicalAction{OptionID: "reject", Kind: "reject", SchemaVersion: ActionSchemaVersion}
	if a.Digest() == b.Digest() {
		t.Fatal("different selected actions must have different digests")
	}
	// input substitution changes the digest
	c := a
	c.InputType, c.InputPlacement, c.NormalizedInput = "text", "as_payload", "rm -rf /"
	if c.Digest() == a.Digest() {
		t.Error("input substitution must change the digest")
	}
	// schema version participates
	d := a
	d.SchemaVersion = "a1.action.v2"
	if d.Digest() == a.Digest() {
		t.Error("schema version must change the digest")
	}
	// same fields → identical
	if a.Digest() != (CanonicalAction{OptionID: "approve", Kind: "approve", SchemaVersion: ActionSchemaVersion}).Digest() {
		t.Error("identical canonical actions must have equal digests")
	}
}

// ── B1: atomic claim full binding ──

func TestClaim_GrantsOnceWithOpaqueToken(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	dg := digestFor("approve", "approve")
	res := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", dg, boundRuntime(), reqCtx(), "k1"))
	if res.Outcome != ClaimGranted || res.Token == "" {
		t.Fatalf("claim=%+v", res)
	}
	if res.Token == "a1" || len(res.Token) < 16 {
		t.Error("token must be opaque and not derived from ApprovalID")
	}
	snap, _ := s.LookupRecord("codex:s1", "a1")
	if snap.State != ApprovalExecuting {
		t.Errorf("state=%q want executing", snap.State)
	}
}

func TestClaim_RejectsFullBindingMismatches(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	dg := digestFor("approve", "approve")
	rt := boundRuntime()

	cases := []struct {
		name string
		req  ClaimRequest
		want ClaimOutcome
	}{
		{"cross-session", claimReq("codex:OTHER", "a1", "approve", dg, rt, reqCtx(), ""), ClaimNotFound},
		{"unknown-approval", claimReq("codex:s1", "nope", "approve", dg, rt, reqCtx(), ""), ClaimNotFound},
		{"unknown-action", claimReq("codex:s1", "a1", "ghost", dg, rt, reqCtx(), ""), ClaimUnknownAction},
		{"stale-launch", claimReq("codex:s1", "a1", "approve", dg, RuntimeRef{"codex", "0.144.1", 6, 2}, reqCtx(), ""), ClaimStaleRuntime},
		{"stale-stream", claimReq("codex:s1", "a1", "approve", dg, RuntimeRef{"codex", "0.144.1", 5, 3}, reqCtx(), ""), ClaimStaleRuntime},
		{"adapter-mismatch", claimReq("codex:s1", "a1", "approve", dg, RuntimeRef{"claude", "0.144.1", 5, 2}, reqCtx(), ""), ClaimRuntimeMismatch},
		{"version-mismatch", claimReq("codex:s1", "a1", "approve", dg, RuntimeRef{"codex", "9.9.9", 5, 2}, reqCtx(), ""), ClaimRuntimeMismatch},
		{"no-requester", claimReq("codex:s1", "a1", "approve", dg, rt, RequesterContext{}, ""), ClaimUnauthorized},
		{"missing-perm", claimReq("codex:s1", "a1", "approve", dg, rt, RequesterContext{DeviceID: "d", BearerSessionID: "b", Permissions: []string{"sessions:read"}}, ""), ClaimUnauthorized},
		{"empty-digest", claimReq("codex:s1", "a1", "approve", "", rt, reqCtx(), ""), ClaimUnknownAction},
	}
	for _, c := range cases {
		// fresh record per case so a prior claim doesn't consume it
		seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
		if got := s.ClaimForExecution(c.req); got.Outcome != c.want {
			t.Errorf("%s: outcome=%q want %q", c.name, got.Outcome, c.want)
		}
	}
}

func TestClaim_NonActionableRejected(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	// non-actionable ingest (production default)
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 5, StreamGen: 2, Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{Approval: agent.AgentApproval{ID: "a1", SessionID: "codex:s1", Kind: "approval"}, Provenance: contract.ProvenanceNativeLog, Actionable: false, RequiredPerm: "terminal:input"}}})
	res := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", digestFor("approve", "approve"), boundRuntime(), reqCtx(), ""))
	if res.Outcome != ClaimNotActionable {
		t.Errorf("non-actionable claim=%q want not_actionable", res.Outcome)
	}
}

func TestClaim_ExpiryFailsClosed(t *testing.T) {
	s, clk := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	*clk = time.Unix(1000, 0).Add(authApprovalExpiry + time.Second)
	if got := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", digestFor("approve", "approve"), boundRuntime(), reqCtx(), "")); got.Outcome != ClaimExpired {
		t.Errorf("expired claim=%q want expired", got.Outcome)
	}
}

func TestClaim_DuplicateNoSecondOwner(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	dg := digestFor("approve", "approve")
	first := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", dg, boundRuntime(), reqCtx(), ""))
	if first.Outcome != ClaimGranted {
		t.Fatalf("first=%q", first.Outcome)
	}
	// second claim (no key) sees executing → already_owned, no new token
	second := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", dg, boundRuntime(), reqCtx(), ""))
	if second.Outcome != ClaimAlreadyOwned || second.Token != "" {
		t.Errorf("second=%+v want already_owned no token", second)
	}
}

func TestClaim_ConcurrentApproveVsRejectOneOwner(t *testing.T) {
	s := NewAuthoritativeApprovalStore() // real clock; race detector active
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{
		actOpt("approve", "approve", nil), actOpt("reject", "reject", nil),
	})
	rt := RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	start := make(chan struct{})
	var granted int64
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		opt, kind := "approve", "approve"
		if i%2 == 1 {
			opt, kind = "reject", "reject"
		}
		go func(opt, kind string) {
			defer wg.Done()
			<-start
			if s.ClaimForExecution(claimReq("codex:s1", "a1", opt, digestFor(opt, kind), rt, reqCtx(), "")).Outcome == ClaimGranted {
				atomic.AddInt64(&granted, 1)
			}
		}(opt, kind)
	}
	close(start)
	wg.Wait()
	if granted != 1 {
		t.Errorf("exactly one claim must be granted, got %d", granted)
	}
}

// ── B2: idempotency ──

func TestClaim_IdempotencySameKeySameDigestAlreadyAccepted(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("send", "neutral", &agent.InputSchema{Placement: "as_payload"})})
	ca := CanonicalAction{OptionID: "send", Kind: "neutral", SchemaVersion: ActionSchemaVersion, InputType: "text", InputPlacement: "as_payload", NormalizedInput: "ls"}
	dg := ca.Digest()
	c := s.ClaimForExecution(claimReq("codex:s1", "a1", "send", dg, boundRuntime(), reqCtx(), "key-1"))
	if c.Outcome != ClaimGranted {
		t.Fatalf("claim=%q", c.Outcome)
	}
	// deliver accepted → key marked accepted
	commit := s.RecordDelivery("codex:s1", "a1", c.Token, DeliveryReceipt{Outcome: DeliveryAccepted, ActionDigest: dg})
	if !commit.Committed {
		t.Fatal("expected committed")
	}
	// replay same key+digest → already_accepted, no new owner
	replay := s.ClaimForExecution(claimReq("codex:s1", "a1", "send", dg, boundRuntime(), reqCtx(), "key-1"))
	if replay.Outcome != ClaimAlreadyAccepted {
		t.Errorf("replay=%q want already_accepted", replay.Outcome)
	}
}

func TestClaim_IdempotencySameKeyDifferentDigestConflict(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{
		actOpt("approve", "approve", nil), actOpt("reject", "reject", nil),
	})
	c := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", digestFor("approve", "approve"), boundRuntime(), reqCtx(), "key-1"))
	if c.Outcome != ClaimGranted {
		t.Fatalf("claim=%q", c.Outcome)
	}
	// same key, DIFFERENT digest (different selected action) → conflict
	conflict := s.ClaimForExecution(claimReq("codex:s1", "a1", "reject", digestFor("reject", "reject"), boundRuntime(), reqCtx(), "key-1"))
	if conflict.Outcome != ClaimConflict {
		t.Errorf("conflict=%q want conflict", conflict.Outcome)
	}
}

// ── B3/B4: delivery receipt + commit safety ──

func TestRecordDelivery_OnlyAcceptedCommits(t *testing.T) {
	dg := digestFor("approve", "approve")
	for _, tc := range []struct {
		outcome   DeliveryOutcome
		committed bool
		state     ApprovalState
	}{
		{DeliveryAccepted, true, ApprovalApproved},
		{DeliveryAlreadyAccepted, true, ApprovalApproved},
		{DeliveryStaleRuntime, false, ApprovalDeliveryFailed},
		{DeliveryRuntimeMismatch, false, ApprovalDeliveryFailed},
		{DeliveryUnavailable, false, ApprovalDeliveryFailed},
		{DeliveryRejected, false, ApprovalDeliveryFailed},
	} {
		s, _ := newTestStore(time.Unix(1000, 0))
		seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
		c := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", dg, boundRuntime(), reqCtx(), ""))
		commit := s.RecordDelivery("codex:s1", "a1", c.Token, DeliveryReceipt{Outcome: tc.outcome, ActionDigest: dg})
		if commit.Committed != tc.committed {
			t.Errorf("%s: committed=%v want %v", tc.outcome, commit.Committed, tc.committed)
		}
		snap, _ := s.LookupRecord("codex:s1", "a1")
		if snap.State != tc.state {
			t.Errorf("%s: state=%q want %q", tc.outcome, snap.State, tc.state)
		}
	}
}

func TestRecordDelivery_RejectsWrongTokenOrDigest(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	dg := digestFor("approve", "approve")
	c := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", dg, boundRuntime(), reqCtx(), ""))
	// wrong token
	if r := s.RecordDelivery("codex:s1", "a1", "forged-token", DeliveryReceipt{Outcome: DeliveryAccepted, ActionDigest: dg}); r.Committed || r.Outcome != DeliveryRejected {
		t.Errorf("wrong token committed=%v outcome=%q", r.Committed, r.Outcome)
	}
	// wrong digest
	if r := s.RecordDelivery("codex:s1", "a1", c.Token, DeliveryReceipt{Outcome: DeliveryAccepted, ActionDigest: "otherdigest"}); r.Committed || r.Outcome != DeliveryConflict {
		t.Errorf("wrong digest committed=%v outcome=%q", r.Committed, r.Outcome)
	}
	// record still executing (not falsely committed)
	if snap, _ := s.LookupRecord("codex:s1", "a1"); snap.State != ApprovalExecuting {
		t.Errorf("state=%q want executing (no false commit)", snap.State)
	}
}

func TestRecordDelivery_DeleteBetweenClaimAndDeliveryUnavailable(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	dg := digestFor("approve", "approve")
	c := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", dg, boundRuntime(), reqCtx(), ""))
	s.Clear("codex:s1") // delete/unlink between claim and delivery
	if r := s.RecordDelivery("codex:s1", "a1", c.Token, DeliveryReceipt{Outcome: DeliveryAccepted, ActionDigest: dg}); r.Committed || r.Outcome != DeliveryUnavailable {
		t.Errorf("delete-then-deliver committed=%v outcome=%q want unavailable", r.Committed, r.Outcome)
	}
}

func TestRecordDelivery_ReplayAfterCommitRejected(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	dg := digestFor("approve", "approve")
	c := s.ClaimForExecution(claimReq("codex:s1", "a1", "approve", dg, boundRuntime(), reqCtx(), ""))
	if !s.RecordDelivery("codex:s1", "a1", c.Token, DeliveryReceipt{Outcome: DeliveryAccepted, ActionDigest: dg}).Committed {
		t.Fatal("first commit failed")
	}
	// replay the receipt on a now-terminal record → rejected, no re-commit
	if r := s.RecordDelivery("codex:s1", "a1", c.Token, DeliveryReceipt{Outcome: DeliveryAccepted, ActionDigest: dg}); r.Committed {
		t.Error("terminal record re-committed (stale receipt replay)")
	}
}

// ── restart / eviction / churn ──

func TestClaim_RestartNoResidualAuthority(t *testing.T) {
	s1, _ := newTestStore(time.Unix(1000, 0))
	seedActionable(s1, "codex:s1", "a1", 5, 2, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
	c := s1.ClaimForExecution(claimReq("codex:s1", "a1", "approve", digestFor("approve", "approve"), boundRuntime(), reqCtx(), "k"))
	// fresh store (restart): the opaque token from s1 is meaningless
	s2 := NewAuthoritativeApprovalStore()
	if r := s2.RecordDelivery("codex:s1", "a1", c.Token, DeliveryReceipt{Outcome: DeliveryAccepted, ActionDigest: digestFor("approve", "approve")}); r.Committed {
		t.Error("restart store honored a pre-restart claim token")
	}
	if got := s2.ClaimForExecution(claimReq("codex:s1", "a1", "approve", digestFor("approve", "approve"), boundRuntime(), reqCtx(), "k")); got.Outcome != ClaimNotFound {
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
				switch i % 7 {
				case 0:
					seedActionable(s, sess, id, int64(i), 0, "codex", "0.144.1", []agent.InteractionOption{actOpt("approve", "approve", nil)})
				case 1:
					c := s.ClaimForExecution(claimReq(sess, id, "approve", digestFor("approve", "approve"), RuntimeRef{"codex", "0.144.1", int64(i), 0}, reqCtx(), fmt.Sprintf("k%d", i)))
					if c.Outcome == ClaimGranted {
						s.RecordDelivery(sess, id, c.Token, DeliveryReceipt{Outcome: DeliveryUnavailable, ActionDigest: c.Digest})
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
					s.Clear(sess)
				}
			}
		}(g)
	}
	wg.Wait()
}
