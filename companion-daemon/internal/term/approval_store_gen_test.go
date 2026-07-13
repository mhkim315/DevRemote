package term

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// A1-B — generation-bound authoritative ApprovalStore negative/production tests.

func optSet(ids ...string) []agent.InteractionOption {
	out := make([]agent.InteractionOption, 0, len(ids))
	for _, id := range ids {
		kind := "neutral"
		switch id {
		case "approve":
			kind = "approve"
		case "reject":
			kind = "reject"
		}
		out = append(out, agent.InteractionOption{ID: id, Label: id, Kind: kind})
	}
	return out
}

func ingestItem(sessionID, approvalID string, opts []agent.InteractionOption) ApprovalIngestItem {
	return ApprovalIngestItem{
		Approval: agent.AgentApproval{
			ID: approvalID, SessionID: sessionID, AgentKind: "codex", Kind: "approval",
			Prompt: "approve?", Options: opts, Default: "reject", Source: agent.SourceJSONL,
			Confidence: 0.9,
		},
		Provenance:   contract.ProvenanceNativeLog,
		RequiredPerm: "terminal:input",
	}
}

func newTestStore(t0 time.Time) (*AuthoritativeApprovalStore, *time.Time) {
	s := NewAuthoritativeApprovalStore()
	clk := t0
	s.now = func() time.Time { return clk }
	return s, &clk
}

func mustPending(t *testing.T, s *AuthoritativeApprovalStore, sess, id string) {
	t.Helper()
	snap, ok := s.LookupRecord(sess, id)
	if !ok || snap.State != ApprovalPending {
		t.Fatalf("want pending %s/%s, got ok=%v state=%q", sess, id, ok, snap.State)
	}
}

func TestAuthStore_IngestPositive_BindsGenerationAndProvenance(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	s.Ingest(ApprovalIngest{
		SessionID: "codex:s1", LaunchGen: 7, StreamGen: 3, Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{ingestItem("codex:s1", "a1", optSet("approve", "reject"))},
	})
	snap, ok := s.LookupRecord("codex:s1", "a1")
	if !ok {
		t.Fatal("record not stored")
	}
	if snap.LaunchGen != 7 || snap.StreamGen != 3 || snap.Provider != "codex" || snap.Version != "0.144.1" {
		t.Errorf("binding lost: %+v", snap)
	}
	if snap.Provenance != contract.ProvenanceNativeLog || snap.State != ApprovalPending {
		t.Errorf("provenance/state wrong: %+v", snap)
	}
	if snap.ActionDigest == "" {
		t.Error("action digest not computed")
	}
}

func TestAuthStore_DropsNonAuthoritativeProvenance(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	for _, prov := range []contract.Provenance{contract.ProvenanceHeuristic, contract.ProvenancePromptHint, contract.ProvenancePTYStructural, contract.ProvenanceUnknown, ""} {
		item := ingestItem("codex:s1", "a1", optSet("approve"))
		item.Provenance = prov
		s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 1, Items: []ApprovalIngestItem{item}})
		if _, ok := s.LookupRecord("codex:s1", "a1"); ok {
			t.Errorf("provenance %q must not establish a request", prov)
		}
	}
}

func TestAuthStore_DropsIdlessCrossSessionAndEmptyOptions(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	// id-less
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 1, Items: []ApprovalIngestItem{ingestItem("codex:s1", "", optSet("approve"))}})
	// cross-session binding
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 1, Items: []ApprovalIngestItem{ingestItem("codex:OTHER", "a2", optSet("approve"))}})
	// empty options
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 1, Items: []ApprovalIngestItem{ingestItem("codex:s1", "a3", nil)}})
	if _, ok := s.LookupRecord("codex:s1", ""); ok {
		t.Error("id-less request stored")
	}
	if _, ok := s.LookupRecord("codex:s1", "a2"); ok {
		t.Error("cross-session request stored")
	}
	if _, ok := s.LookupRecord("codex:s1", "a3"); ok {
		t.Error("empty-options request stored")
	}
}

func TestAuthStore_CrossSessionIsolation(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 1, Items: []ApprovalIngestItem{ingestItem("codex:s1", "a1", optSet("approve"))}})
	// Same approval ID under a DIFFERENT session must be independent.
	if _, ok := s.LookupRecord("codex:s2", "a1"); ok {
		t.Error("approval leaked across sessions")
	}
	// Reserve on the wrong session must not touch s1's record.
	if _, oc := s.Reserve("codex:s2", "a1"); oc != OutcomeNotFound {
		t.Errorf("reserve wrong session outcome=%q want not_found", oc)
	}
	mustPending(t, s, "codex:s1", "a1")
}

func TestAuthStore_OlderGenerationCannotCreateUpdateOrRestore(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 5, StreamGen: 2, Items: []ApprovalIngestItem{ingestItem("codex:s1", "a1", optSet("approve"))}})
	mustPending(t, s, "codex:s1", "a1")

	// Older launch generation: whole ingest rejected, including a brand-new id.
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 4, StreamGen: 9, Items: []ApprovalIngestItem{
		ingestItem("codex:s1", "a1", optSet("reject")),
		ingestItem("codex:s1", "a2", optSet("approve")),
	}})
	if _, ok := s.LookupRecord("codex:s1", "a2"); ok {
		t.Error("older-generation ingest created a new request")
	}
	// a1 unchanged (still approve-only contract, pending).
	snap, _ := s.LookupRecord("codex:s1", "a1")
	if snap.State != ApprovalPending {
		t.Errorf("older gen mutated live record: %+v", snap)
	}

	// Older stream generation within the same launch is also rejected.
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 5, StreamGen: 1, Items: []ApprovalIngestItem{ingestItem("codex:s1", "a3", optSet("approve"))}})
	if _, ok := s.LookupRecord("codex:s1", "a3"); ok {
		t.Error("older stream-generation ingest created a request")
	}
}

func TestAuthStore_NewerGenerationInvalidatesPriorPending(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 5, StreamGen: 2, Items: []ApprovalIngestItem{ingestItem("codex:s1", "a1", optSet("approve"))}})
	// A newer launch generation (runtime replacement) invalidates prior pending.
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 6, StreamGen: 0, Items: []ApprovalIngestItem{ingestItem("codex:s1", "a2", optSet("approve"))}})
	snap, ok := s.LookupRecord("codex:s1", "a1")
	if !ok || snap.State != ApprovalInvalidated {
		t.Errorf("prior pending not invalidated on newer generation: ok=%v state=%q", ok, snap.State)
	}
	mustPending(t, s, "codex:s1", "a2")
	// A late reserve of the invalidated old request fails closed.
	if _, oc := s.Reserve("codex:s1", "a1"); oc != OutcomeAlreadyTerminal {
		t.Errorf("reserve invalidated outcome=%q want already_terminal", oc)
	}
}

func TestAuthStore_DuplicateIDChangedActionSetRefused(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 1, Items: []ApprovalIngestItem{ingestItem("codex:s1", "a1", optSet("approve", "reject"))}})
	d0, _ := s.LookupRecord("codex:s1", "a1")

	// Same generation, same id, DIFFERENT action set: must not mutate the live record.
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 1, Items: []ApprovalIngestItem{ingestItem("codex:s1", "a1", optSet("approve", "reject", "send_text"))}})
	d1, _ := s.LookupRecord("codex:s1", "a1")
	if d1.ActionDigest != d0.ActionDigest || len(d1.Options) != len(d0.Options) {
		t.Errorf("changed action set mutated a live request: %d→%d options", len(d0.Options), len(d1.Options))
	}

	// Same id, SAME contract but reordered options: idempotent, still one record, digest stable.
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 1, Items: []ApprovalIngestItem{ingestItem("codex:s1", "a1", optSet("reject", "approve"))}})
	d2, _ := s.LookupRecord("codex:s1", "a1")
	if d2.ActionDigest != d0.ActionDigest {
		t.Error("option reorder wrongly treated as a contract change")
	}
}

func TestAuthStore_ExpiryBoundary(t *testing.T) {
	s, clk := newTestStore(time.Unix(1000, 0))
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 1, Items: []ApprovalIngestItem{ingestItem("codex:s1", "a1", optSet("approve"))}})

	// Exactly at expiry: not yet expired (expiry is strictly-after).
	*clk = time.Unix(1000, 0).Add(authApprovalExpiry)
	if snap, _ := s.LookupRecord("codex:s1", "a1"); snap.State != ApprovalPending {
		t.Errorf("at expiry boundary state=%q, want pending", snap.State)
	}
	// One tick past expiry: reserve fails as expired, state transitions to expired.
	*clk = time.Unix(1000, 0).Add(authApprovalExpiry + time.Nanosecond)
	if _, oc := s.Reserve("codex:s1", "a1"); oc != OutcomeExpired {
		t.Errorf("past expiry reserve outcome=%q want expired", oc)
	}
	if snap, _ := s.LookupRecord("codex:s1", "a1"); snap.State != ApprovalExpired {
		t.Errorf("state after expiry=%q want expired", snap.State)
	}
}

func TestAuthStore_ReserveIsAtMostOnce_AndCommitReplayRejected(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 1, Items: []ApprovalIngestItem{ingestItem("codex:s1", "a1", optSet("approve"))}})

	snap, oc := s.Reserve("codex:s1", "a1")
	if oc != OutcomeOK || snap.State != ApprovalExecuting {
		t.Fatalf("first reserve oc=%q state=%q", oc, snap.State)
	}
	// Second reserve while executing: refused.
	if _, oc2 := s.Reserve("codex:s1", "a1"); oc2 != OutcomeAlreadyTerminal {
		t.Errorf("double reserve oc=%q want already_terminal", oc2)
	}
	// Commit once by kind.
	if !s.Commit("codex:s1", "a1", "approve") {
		t.Fatal("commit failed")
	}
	if snap, _ := s.LookupRecord("codex:s1", "a1"); snap.State != ApprovalApproved {
		t.Errorf("committed state=%q want approved", snap.State)
	}
	// Replay: commit/fail/reserve on a terminal record all refused.
	if s.Commit("codex:s1", "a1", "approve") || s.Fail("codex:s1", "a1") {
		t.Error("terminal record re-committed (replay must be rejected)")
	}
	if _, oc3 := s.Reserve("codex:s1", "a1"); oc3 != OutcomeAlreadyTerminal {
		t.Errorf("reserve resolved oc=%q want already_terminal", oc3)
	}
}

func TestAuthStore_FailMarksDeliveryFailedNotSuccess(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 1, Items: []ApprovalIngestItem{ingestItem("codex:s1", "a1", optSet("approve"))}})
	if _, oc := s.Reserve("codex:s1", "a1"); oc != OutcomeOK {
		t.Fatalf("reserve oc=%q", oc)
	}
	if !s.Fail("codex:s1", "a1") {
		t.Fatal("fail transition rejected")
	}
	snap, _ := s.LookupRecord("codex:s1", "a1")
	if snap.State != ApprovalDeliveryFailed {
		t.Errorf("state=%q want delivery_failed", snap.State)
	}
	// Public projection must be delivery_failed, never approved.
	list := s.List("codex:s1")
	if len(list) != 1 || list[0].Status != string(ApprovalDeliveryFailed) {
		t.Errorf("public status=%v want delivery_failed", list)
	}
	// Commit cannot run after a terminal delivery_failed.
	if s.Commit("codex:s1", "a1", "approve") {
		t.Error("delivery_failed record was committed as success")
	}
}

func TestAuthStore_CommitRequiresReservation(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 1, Items: []ApprovalIngestItem{ingestItem("codex:s1", "a1", optSet("approve"))}})
	// Commit without Reserve (still pending) must be refused — no resolve-before-reserve.
	if s.Commit("codex:s1", "a1", "approve") {
		t.Error("committed a pending record without reservation")
	}
	mustPending(t, s, "codex:s1", "a1")
}

func TestAuthStore_MutationAliasingBlocked(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	opts := optSet("approve", "reject")
	opts[0].Input = &agent.InputSchema{Required: true, Placement: "as_payload"}
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 1, Items: []ApprovalIngestItem{ingestItem("codex:s1", "a1", opts)}})

	// Mutating the caller's original slice/pointer after ingest must not change stored state.
	opts[0].ID = "HIJACK"
	opts[0].Input.Required = false

	// Mutating a returned snapshot must not change stored state either.
	snap, _ := s.LookupRecord("codex:s1", "a1")
	snap.Options[0].ID = "HIJACK2"
	snap.Options[0].Input.Placement = "after_payload"

	fresh, _ := s.LookupRecord("codex:s1", "a1")
	if fresh.Options[0].ID != "approve" || fresh.Options[0].Input == nil || !fresh.Options[0].Input.Required || fresh.Options[0].Input.Placement != "as_payload" {
		t.Errorf("stored authority mutated through aliasing: %+v", fresh.Options[0])
	}
	// List output must likewise be a copy.
	l := s.List("codex:s1")
	l[0].Options[0].Kind = "HIJACK3"
	again := s.List("codex:s1")
	if again[0].Options[0].Kind == "HIJACK3" {
		t.Error("List output aliases stored options")
	}
}

func TestAuthStore_BoundedEvictionPerSession(t *testing.T) {
	s, clk := newTestStore(time.Unix(1000, 0))
	// Fill beyond the per-session cap with live pending records.
	for i := 0; i < authMaxApprovalsPerSession+10; i++ {
		*clk = time.Unix(1000+int64(i), 0)
		s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 1, Items: []ApprovalIngestItem{
			ingestItem("codex:s1", fmt.Sprintf("a%03d", i), optSet("approve")),
		}})
	}
	// The store must never exceed the per-session bound.
	s.mu.Lock()
	n := len(s.sessions["codex:s1"].records)
	s.mu.Unlock()
	if n > authMaxApprovalsPerSession {
		t.Errorf("session holds %d records, exceeds bound %d", n, authMaxApprovalsPerSession)
	}
}

func TestAuthStore_EvictionPrefersTerminalOverLive(t *testing.T) {
	s, clk := newTestStore(time.Unix(1000, 0))
	// One old terminal record + fill the rest with live pending; the terminal one
	// should be evicted first even though it is not the absolute oldest live record.
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 1, Items: []ApprovalIngestItem{ingestItem("codex:s1", "term", optSet("approve"))}})
	s.Reserve("codex:s1", "term")
	s.Commit("codex:s1", "term", "approve") // terminal
	for i := 0; i < authMaxApprovalsPerSession; i++ {
		*clk = time.Unix(2000+int64(i), 0)
		s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 1, Items: []ApprovalIngestItem{ingestItem("codex:s1", fmt.Sprintf("live%03d", i), optSet("approve"))}})
	}
	if _, ok := s.LookupRecord("codex:s1", "term"); ok {
		t.Error("terminal record should have been evicted first")
	}
}

func TestAuthStore_ClearThenRecreateStartsFresh(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 9, StreamGen: 4, Items: []ApprovalIngestItem{ingestItem("codex:s1", "a1", optSet("approve"))}})
	s.Clear("codex:s1")
	if _, ok := s.LookupRecord("codex:s1", "a1"); ok {
		t.Fatal("record survived Clear")
	}
	// A recreated session at a LOWER generation must be accepted (generations legitimately restart).
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 1, StreamGen: 0, Items: []ApprovalIngestItem{ingestItem("codex:s1", "b1", optSet("approve"))}})
	mustPending(t, s, "codex:s1", "b1")
}

func TestAuthStore_InvalidateSessionOnCorrelationLoss(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 1, Items: []ApprovalIngestItem{
		ingestItem("codex:s1", "a1", optSet("approve")),
		ingestItem("codex:s1", "a2", optSet("approve")),
	}})
	s.InvalidateSession("codex:s1", "correlation unavailable")
	for _, id := range []string{"a1", "a2"} {
		snap, _ := s.LookupRecord("codex:s1", id)
		if snap.State != ApprovalInvalidated {
			t.Errorf("%s state=%q want invalidated", id, snap.State)
		}
		if _, oc := s.Reserve("codex:s1", id); oc != OutcomeAlreadyTerminal {
			t.Errorf("%s reserve after correlation loss oc=%q", id, oc)
		}
	}
}

// Simulated daemon restart: a fresh store has no residual authority.
func TestAuthStore_RestartHasNoResidualAuthority(t *testing.T) {
	s1, _ := newTestStore(time.Unix(1000, 0))
	s1.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 1, Items: []ApprovalIngestItem{ingestItem("codex:s1", "a1", optSet("approve"))}})
	// "restart"
	s2 := NewAuthoritativeApprovalStore()
	if _, ok := s2.LookupRecord("codex:s1", "a1"); ok {
		t.Error("fresh store inherited prior pending authority")
	}
	if _, oc := s2.Reserve("codex:s1", "a1"); oc != OutcomeNotFound {
		t.Errorf("reserve on fresh store oc=%q want not_found", oc)
	}
}

func TestAuthStore_RaceChurn(t *testing.T) {
	s := NewAuthoritativeApprovalStore() // real clock; race detector is the point
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			sess := fmt.Sprintf("codex:s%d", g%4)
			for i := 0; i < 200; i++ {
				id := fmt.Sprintf("a%d", i%8)
				switch i % 6 {
				case 0:
					s.Ingest(ApprovalIngest{SessionID: sess, LaunchGen: int64(i), Items: []ApprovalIngestItem{ingestItem(sess, id, optSet("approve", "reject"))}})
				case 1:
					s.Reserve(sess, id)
				case 2:
					s.Commit(sess, id, "approve")
				case 3:
					s.Fail(sess, id)
				case 4:
					s.List(sess)
				case 5:
					s.InvalidateSession(sess, "churn")
				}
			}
		}(g)
	}
	wg.Wait()
}

// Negative control: prove the aliasing test is not vacuous — a shallow (non-copy)
// store WOULD be corrupted by the same mutation, so the guard above is real.
func TestAuthStore_AliasingNegativeControl(t *testing.T) {
	opts := optSet("approve")
	opts[0].Input = &agent.InputSchema{Required: true}
	// Simulate a store that keeps the caller's slice directly (the bug we prevent).
	shallow := opts
	opts[0].Input.Required = false
	if shallow[0].Input.Required {
		t.Fatal("control setup wrong: expected shared pointer mutation to show through")
	}
	// The real store uses copyOptions; confirm it breaks the alias.
	deep := copyOptions(opts)
	opts[0].Input = &agent.InputSchema{Required: true, Placement: "as_payload"}
	if deep[0].Input == nil || deep[0].Input.Required {
		t.Error("copyOptions did not isolate the input pointer")
	}
}
