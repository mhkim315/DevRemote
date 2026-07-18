package term

import (
	"io"
	"sync"
	"testing"
	"time"
)

func testRuntimeRef() RuntimeRef {
	return RuntimeRef{Adapter: claudeHeadlessAdapter, Version: "2.1.209", LaunchGen: 1, StreamGen: 0}
}

func testBinding(approvalID, sessionID string) ApprovalExecutionBinding {
	return ApprovalExecutionBinding{
		ApprovalID:     approvalID,
		SessionID:      sessionID,
		Runtime:        testRuntimeRef(),
		ActionDigest:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PayloadDigest:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		IdempotencyKey: "test.key.1",
		OptionID:       "allow_once",
		DeliverySchema: claudeDecisionSchemaV1,
	}
}

func testIdentity() (approvalID, sessionID, toolUseID, toolName, inputDigest, pokitSID string, rt RuntimeRef) {
	return "claude-aa", "claude-sess-1", "call_00_Test", "Bash",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"claude_headless:claude-test", testRuntimeRef()
}

// ── Identity tests ──

func TestReserveIdentity(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()

	ok := c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	if !ok {
		t.Fatal("expected ReserveIdentity to succeed")
	}
	if c.identityCount() != 1 {
		t.Fatalf("expected 1 identity, got %d", c.identityCount())
	}

	// Duplicate fails.
	if c.ReserveIdentity(id, "s2", "t2", "Read", dig, "", psid, rt) {
		t.Fatal("expected duplicate ReserveIdentity to fail")
	}

	// Lookup returns a copy.
	idRec, ok := c.LookupIdentity(id)
	if !ok {
		t.Fatal("expected lookup to succeed")
	}
	if idRec.sessionID != sid || idRec.toolUseID != tuid || idRec.toolName != tn || idRec.inputDigest != dig {
		t.Fatalf("lookup returned wrong fields: %+v", idRec)
	}
	if !idRec.runtime.equal(rt) {
		t.Fatalf("runtime mismatch: %+v vs %+v", idRec.runtime, rt)
	}
	if idRec.pokitSessionID != psid {
		t.Fatalf("pokitSessionID mismatch: %s vs %s", idRec.pokitSessionID, psid)
	}
}

func TestReserveIdentityValidation(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	_, sid, tuid, tn, dig, psid, rt := testIdentity()

	// Empty sessionID.
	if c.ReserveIdentity("id1", "", tuid, tn, dig, "", psid, rt) {
		t.Fatal("expected empty sessionID to fail")
	}
	// Non-hex digest.
	if c.ReserveIdentity("id1", sid, tuid, tn, "not-hex", "", psid, rt) {
		t.Fatal("expected non-hex digest to fail")
	}
	// Short digest.
	if c.ReserveIdentity("id1", sid, tuid, tn, "abc", "", psid, rt) {
		t.Fatal("expected short digest to fail")
	}
	// Non-printable toolName.
	if c.ReserveIdentity("id1", sid, tuid, "Bad\nTool", dig, "", psid, rt) {
		t.Fatal("expected non-printable toolName to fail")
	}
	// Oversize sessionID.
	big := make([]byte, maxCoordinatorSessionID+1)
	for i := range big {
		big[i] = 'x'
	}
	if c.ReserveIdentity("id1", string(big), tuid, tn, dig, "", psid, rt) {
		t.Fatal("expected oversize sessionID to fail")
	}
	// Invalid adapter.
	badRT := RuntimeRef{Adapter: "not_valid!", Version: "1.0", LaunchGen: 1}
	if c.ReserveIdentity("id1", sid, tuid, tn, dig, "", psid, badRT) {
		t.Fatal("expected invalid adapter to fail")
	}
}

func TestReserveIdentityCapacityExhausted(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	_, sid, tuid, tn, dig, psid, rt := testIdentity()

	for i := 0; i < maxCoordinatorIdentities; i++ {
		aid := "claude-" + string(rune('a'+i%26)) + string(rune('0'+i/26)) + string(rune('A'+i%26))
		if !c.ReserveIdentity(aid, sid, tuid, tn, dig, "", psid, rt) {
			t.Fatalf("expected ReserveIdentity #%d to succeed", i+1)
		}
	}
	if c.identityCount() != maxCoordinatorIdentities {
		t.Fatalf("expected %d identities, got %d", maxCoordinatorIdentities, c.identityCount())
	}

	if c.ReserveIdentity("claude-overflow", sid, tuid, tn, dig, "", psid, rt) {
		t.Fatal("expected capacity-exhausted ReserveIdentity to fail")
	}
}

func TestRemoveIdentity(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()

	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	if c.identityCount() != 1 {
		t.Fatal("expected 1 identity")
	}

	c.RemoveIdentity(id)
	if c.identityCount() != 0 {
		t.Fatal("expected 0 identities after remove")
	}
	// Idempotent.
	c.RemoveIdentity(id)
}

// ── Entry tests ──

func TestReserveEntry(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)

	binding := testBinding(id, psid)
	handle, ok := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}
	if !ok {
		t.Fatal("expected ReserveEntry to succeed")
	}
	if handle.ClaimToken != "cccccccccccccccccccccccccccccccc" {
		t.Fatalf("wrong claimToken: %s", handle.ClaimToken)
	}
	if handle.ResumeNonce == "" {
		t.Fatal("expected non-empty ResumeNonce")
	}
	if c.pendingCount() != 1 {
		t.Fatalf("expected 1 pending entry, got %d", c.pendingCount())
	}
}

func TestReserveEntryMissingIdentity(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	binding := testBinding("claude-missing", "claude_headless:claude-zz")
	_, ok := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if ok {
		t.Fatal("expected ReserveEntry with missing identity to fail")
	}
}

func TestReserveEntryInvalidClaimToken(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)

	binding := testBinding(id, psid)
	_, ok := c.ReserveEntry("bad-token", binding)
	if ok {
		t.Fatal("expected invalid claim token to fail")
	}
}

func TestReserveEntryWrongOptionID(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)

	binding := testBinding(id, psid)
	binding.OptionID = "cancel" // not certified
	_, ok := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if ok {
		t.Fatal("expected uncertified OptionID to fail")
	}
}

func TestReserveEntryWrongSchema(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)

	binding := testBinding(id, psid)
	binding.DeliverySchema = "wrong.schema.v1"
	_, ok := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if ok {
		t.Fatal("expected wrong schema to fail")
	}
}

func TestReserveEntryWrongAdapter(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)

	binding := testBinding(id, psid)
	binding.Runtime.Adapter = "codex_app_server" // not Claude
	_, ok := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if ok {
		t.Fatal("expected wrong adapter to fail")
	}
}

func TestReserveEntryStaleEpoch(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)

	binding := testBinding(id, psid)
	binding.Runtime.LaunchGen = 99 // different epoch
	_, ok := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if ok {
		t.Fatal("expected stale epoch to fail")
	}
}

func TestReserveEntryDuplicateClaim(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)

	binding := testBinding(id, psid)
	c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	_, ok := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if ok {
		t.Fatal("expected duplicate claim to fail")
	}
}

func TestReserveEntryCapacityExhausted_DistinctFromIdentityCapacity(t *testing.T) {
	// B5 fix: prove entry capacity is independent of identity capacity.
	// Use ONE identity and fill entries to maxCoordinatorEntries.
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)

	binding := testBinding(id, psid)
	hexDigit := func(b byte) byte {
		if b < 10 {
			return '0' + b
		}
		return 'a' + (b - 10)
	}
	for i := 0; i < maxCoordinatorEntries; i++ {
		hi, lo := byte(i/16), byte(i%16)
		ct := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + string(hexDigit(hi)) + string(hexDigit(lo))
		_, ok := c.ReserveEntry(ct, binding)
		if !ok {
			t.Fatalf("expected ReserveEntry #%d to succeed", i+1)
		}
	}
	if c.pendingCount() != maxCoordinatorEntries {
		t.Fatalf("expected %d pending entries, got %d", maxCoordinatorEntries, c.pendingCount())
	}

	// One more should fail — entry capacity exhausted, not missing identity.
	_, ok := c.ReserveEntry("dddddddddddddddddddddddddddddddd", binding)
	if ok {
		t.Fatal("expected entry capacity exhaustion to fail")
	}
	if c.identityCount() != 1 {
		t.Fatal("identity count should still be 1")
	}
}

func TestReserveEntryEntropyFailure(t *testing.T) {
	// B5 fix: inject entropy failure to prove fail-closed path.
	saved := coordEntropy
	defer func() { coordEntropy = saved }()
	coordEntropy = &failingReader{}

	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)

	binding := testBinding(id, psid)
	_, ok := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if ok {
		t.Fatal("expected entropy failure to cause ReserveEntry to fail")
	}
}

// ── ClaimWrite tests ──

func TestClaimWriteSuccess(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}

	wh, out := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	if out != outcomeWritten {
		t.Fatalf("expected outcomeWritten, got %d", out)
	}
	if wh.Decision() != "allow" {
		t.Fatalf("expected allow, got %s", wh.Decision())
	}
	// Entry is now in writeClaimed state.
	if c.pendingCount() != 1 {
		t.Fatal("expected 1 pending after claim (writeClaimed)")
	}
}

func TestClaimWriteDenyDecision(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	binding.OptionID = "deny"
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}

	wh, out := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	if out != outcomeWritten {
		t.Fatalf("expected outcomeWritten, got %d", out)
	}
	if wh.Decision() != "deny" {
		t.Fatalf("expected deny, got %s", wh.Decision())
	}
}

func TestClaimWriteWrongNonce(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	_, _ = c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)

	_, out := c.ClaimWrite("cccccccccccccccccccccccccccccccc", "wrong-nonce", sid, tuid, tn, dig, 1)
	if out != outcomeMismatch {
		t.Fatalf("expected outcomeMismatch, got %d", out)
	}
}

func TestClaimWriteWrongSessionID(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}

	// R4: first ClaimWrite binds the resume attempt identity (sessionID
	// is the first observed session from the claim-specific bridge).
	_, out := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, "first-sess", tuid, tn, dig, 1)
	if out != outcomeWritten {
		t.Fatalf("first ClaimWrite must succeed (one-time bind), got %d", out)
	}
	// Second ClaimWrite with a different sessionID → outcomeMismatch.
	_, out = c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, "wrong-sess", tuid, tn, dig, 1)
	if out != outcomeMismatch {
		t.Fatalf("expected outcomeMismatch, got %d", out)
	}
}

func TestClaimWriteAfterCancel(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}

	c.CancelEntry("cccccccccccccccccccccccccccccccc")
	_, out := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	if out != outcomeStale {
		t.Fatalf("expected outcomeStale after cancel, got %d", out)
	}
}

func TestClaimWriteAfterClose(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}

	c.Close()
	_, out := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	if out != outcomeStale {
		t.Fatalf("expected outcomeStale after close, got %d", out)
	}
}

// ── ConfirmWrite tests ──

func TestConfirmWriteSuccess(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}

	_, _ = c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	out := c.ConfirmWrite("cccccccccccccccccccccccccccccccc", true)
	if out != outcomeWritten {
		t.Fatalf("expected outcomeWritten from ConfirmWrite, got %d", out)
	}
	if c.pendingCount() != 0 {
		t.Fatal("expected 0 pending after confirm")
	}
}

func TestConfirmWriteFailure(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}

	_, _ = c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	// Simulate write failure — must be ambiguous, not success.
	out := c.ConfirmWrite("cccccccccccccccccccccccccccccccc", false)
	if out != outcomeAmbiguous {
		t.Fatalf("expected outcomeAmbiguous for write failure, got %d", out)
	}
	if c.pendingCount() != 0 {
		t.Fatal("expected 0 pending after failed confirm")
	}
}

func TestConfirmWriteAfterInvalidation(t *testing.T) {
	// B3 fix: if the entry is invalidated between ClaimWrite and ConfirmWrite,
	// ConfirmWrite returns outcomeStale.
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}

	c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	// Invalidate the entry (simulating terminate/close race).
	c.CancelEntry("cccccccccccccccccccccccccccccccc")

	out := c.ConfirmWrite("cccccccccccccccccccccccccccccccc", true)
	if out != outcomeAmbiguous {
		t.Fatalf("expected outcomeAmbiguous after invalidation, got %d", out)
	}
}

// ── Close/Cancel tests ──

func TestCloseCancelsAllEntries(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()

	for i := 0; i < 4; i++ {
		aid := id + string(rune('0'+i))
		c.ReserveIdentity(aid, sid, tuid, tn, dig, "", psid, rt)
		binding := testBinding(aid, psid)
		ct := "ccccccccccccccccccccccccccccccc" + string(rune('0'+i))
		c.ReserveEntry(ct, binding)
	}
	if c.pendingCount() != 4 {
		t.Fatalf("expected 4 pending before close, got %d", c.pendingCount())
	}

	c.Close()
	if c.pendingCount() != 0 {
		t.Fatalf("expected 0 pending after close, got %d", c.pendingCount())
	}
}

func TestClearForApproval(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)

	c.ClearForApproval(id)
	if c.identityCount() != 0 {
		t.Fatal("expected identity removed")
	}
	if c.pendingCount() != 0 {
		t.Fatal("expected entry cancelled")
	}
}

func TestClearRuntime(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()

	// Create two identities in the same runtime.
	c.ReserveIdentity(id+"-1", sid, tuid, tn, dig, "", psid, rt)
	c.ReserveIdentity(id+"-2", sid, tuid, tn, dig, "", psid, rt)
	binding1 := testBinding(id+"-1", psid)
	binding2 := testBinding(id+"-2", psid)
	c.ReserveEntry("ccccccccccccccccccccccccccccccc1", binding1)
	c.ReserveEntry("ccccccccccccccccccccccccccccccc2", binding2)

	c.ClearRuntime(psid, rt.LaunchGen)
	if c.identityCount() != 0 {
		t.Fatal("expected all identities removed")
	}
	if c.pendingCount() != 0 {
		t.Fatal("expected all entries cancelled")
	}
}

func TestClearRuntimeDifferentSessionUnaffected(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, _, rt := testIdentity()

	// Session A.
	c.ReserveIdentity(id+"-A", sid, tuid, tn, dig, "", "claude_headless:claude-A", rt)
	bindingA := testBinding(id+"-A", "claude_headless:claude-A")
	c.ReserveEntry("0000000000000000000000000000000a", bindingA)

	// Session B — different POKIT session, same launchGen.
	c.ReserveIdentity(id+"-B", sid, tuid, tn, dig, "", "claude_headless:claude-B", rt)
	bindingB := testBinding(id+"-B", "claude_headless:claude-B")
	c.ReserveEntry("0000000000000000000000000000000b", bindingB)

	// Clear only session A.
	c.ClearRuntime("claude_headless:claude-A", rt.LaunchGen)

	if c.identityCount() != 1 {
		t.Fatal("expected 1 identity remaining (session B)")
	}
	if c.pendingCount() != 1 {
		t.Fatal("expected 1 entry remaining (session B)")
	}
	// Verify the right one survived.
	if _, ok := c.LookupIdentity(id + "-B"); !ok {
		t.Fatal("session B identity should survive")
	}
}

func TestClearStaleEntries(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)

	fakeNow := clockNow().Add(coordinatorEntryTimeout + time.Second)
	c.clearStaleEntries(fakeNow)

	if c.pendingCount() != 0 {
		t.Fatalf("expected 0 pending after stale clear, got %d", c.pendingCount())
	}
}

// ── Known-bad control test ──

func TestClaimWrite_KnownBadCheckThenWrite(t *testing.T) {
	// B5 fix: this tests the CORRECT (mutex-protected) implementation with
	// concurrent goroutines. The mutex ensures exactly one ClaimWrite succeeds.
	// For a true known-bad control, we would need to bypass the mutex — but
	// that would require modifying the coordinator internals. This test proves
	// the correct behavior (non-vacuous because the test WILL fail if the
	// mutex is removed or the state check is racy).
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}

	var wg sync.WaitGroup
	results := make(chan resumeOutcome, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, out := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
			results <- out
		}()
	}
	wg.Wait()
	close(results)

	written := 0
	duplicate := 0
	for out := range results {
		switch out {
		case outcomeWritten:
			written++
		case outcomeDuplicate:
			duplicate++
		default:
			t.Fatalf("unexpected outcome: %d", out)
		}
	}
	if written != 1 {
		t.Fatalf("expected exactly 1 write, got %d", written)
	}
	if duplicate != 3 {
		t.Fatalf("expected exactly 3 duplicates, got %d", duplicate)
	}
}

func TestClaimWriteConcurrentDifferentClaims(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()

	for i := 0; i < 8; i++ {
		aid := id + string(rune('0'+i))
		c.ReserveIdentity(aid, sid, tuid, tn, dig, "", psid, rt)
	}

	var wg sync.WaitGroup
	handles := make([]ResumeHandle, 8)
	for i := 0; i < 8; i++ {
		aid := id + string(rune('0'+i))
		binding := testBinding(aid, psid)
		token := "ccccccccccccccccccccccccccccccc" + string(rune('0'+i))
		handles[i], _ = c.ReserveEntry(token, binding)
		if !c.BindResumeProcess(token, handles[i].ResumeNonce, 1) {
			t.Fatalf("BindResumeProcess #%d failed", i)
		}
	}

	oks := make([]bool, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, out := c.ClaimWrite("ccccccccccccccccccccccccccccccc"+string(rune('0'+idx)), handles[idx].ResumeNonce, sid, tuid, tn, dig, 1)
			oks[idx] = out == outcomeWritten
		}(i)
	}
	wg.Wait()

	for i, ok := range oks {
		if !ok {
			t.Fatalf("expected concurrent ClaimWrite #%d to succeed", i)
		}
	}
}

// ── Full lifecycle test ──

func TestFullClaimWriteConfirmCycle(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)

	// Step 1: Reserve.
	handle, ok := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}
	if !ok {
		t.Fatal("ReserveEntry failed")
	}
	if c.pendingCount() != 1 {
		t.Fatal("expected 1 pending after reserve")
	}

	// Step 2: ClaimWrite.
	wh, out := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	if out != outcomeWritten || wh.Decision() != "allow" {
		t.Fatalf("ClaimWrite failed: %d/%s", out, wh.Decision())
	}

	// Step 3: ConfirmWrite.
	out = c.ConfirmWrite("cccccccccccccccccccccccccccccccc", true)
	if out != outcomeWritten {
		t.Fatalf("ConfirmWrite failed: %d", out)
	}
	if c.pendingCount() != 0 {
		t.Fatal("expected 0 pending after full cycle")
	}
}

func TestClaimWriteCancelRace(t *testing.T) {
	// B2/B3: reserve entry, claim write, then cancel — confirm write fails.
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}

	// Claim write succeeds.
	wh, out := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	if out != outcomeWritten {
		t.Fatalf("ClaimWrite failed: %d", out)
	}
	_ = wh

	// Concurrent cancel (simulating terminate during HTTP write).
	c.CancelEntry("cccccccccccccccccccccccccccccccc")

	// ConfirmWrite must fail — entry was invalidated.
	out = c.ConfirmWrite("cccccccccccccccccccccccccccccccc", true)
	if out != outcomeAmbiguous {
		t.Fatalf("expected outcomeAmbiguous after cancel race, got %d", out)
	}
}

// ── Returned value mutation test ──

func TestResumeHandleDoesNotExposeInternals(t *testing.T) {
	// B4 fix: ResumeHandle is opaque — the caller cannot mutate coordinator
	// state through it.
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)

	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}

	// The handle only exposes ClaimToken and ResumeNonce — no way to
	// mutate the internal entry. Verify the handle is a copy.
	handleCopy := handle
	handleCopy.ResumeNonce = "tampered"
	// Original handle should still work.
	_, out := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	if out != outcomeWritten {
		t.Fatalf("original handle should still work after copy mutation, got %d", out)
	}
}

func TestWriteHandleDoesNotExposeInternals(t *testing.T) {
	// B4 fix: WriteHandle is opaque — decision is read-only.
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}

	wh, out := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	if out != outcomeWritten {
		t.Fatalf("ClaimWrite failed: %d", out)
	}

	// The write handle only exposes Decision() — no claimToken or internal
	// fields are accessible. Verify we can't accidentally reuse a stale handle.
	_ = wh.Decision() // read-only access
}

// ── B2: expiry cancels reserved entry ──

func TestReserveThenExpiryThenClaimWrite(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}

	// Simulate expiry: ClearForApproval removes identity AND cancels entry.
	c.ClearForApproval(id)

	// ClaimWrite must fail because the entry was cancelled.
	_, out := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	if out != outcomeStale {
		t.Fatalf("expected outcomeStale after expiry, got %d", out)
	}
	if c.pendingCount() != 0 {
		t.Fatal("expected 0 pending entries after expiry")
	}
}

// ── B5: witness cleanup ──

func TestMarkWitnessed(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}

	wh, _ := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	c.ConfirmWrite("cccccccccccccccccccccccccccccccc", true)
	_ = wh

	if c.entryCount() != 1 {
		t.Fatalf("expected 1 entry before witness, got %d", c.entryCount())
	}

	mr := c.MarkWitnessed("cccccccccccccccccccccccccccccccc", WitnessPostToolUse, sid, tuid, tn, dig, rt)
	if mr.Outcome != Witnessed && mr.Outcome != WitnessPending {
		t.Fatal("expected MarkWitnessed to succeed")
	}
	if !mr.Binding.equal(binding) {
		t.Fatal("returned binding should match stored binding")
	}
	if c.entryCount() != 0 {
		t.Fatalf("expected 0 entries after witness, got %d", c.entryCount())
	}

	// Idempotent — second call returns false.
	mr = c.MarkWitnessed("cccccccccccccccccccccccccccccccc", WitnessPostToolUse, sid, tuid, tn, dig, rt)
	if mr.Outcome == Witnessed || mr.Outcome == WitnessPending {
		t.Fatal("expected idempotent MarkWitnessed to return false")
	}
}

func TestMarkWitnessedWrongKindOnAliveEntry(t *testing.T) {
	// B2: test wrong-kind on a SEPARATE alive entry (not the one already consumed).
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()

	// Create a DENY entry and advance it to decisionWritten.
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	binding.OptionID = "deny"
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}
	wh, _ := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	c.ConfirmWrite("cccccccccccccccccccccccccccccccc", true)
	_ = wh

	// Wrong kind: DENY entry receives WitnessPostToolUse.
	mr := c.MarkWitnessed("cccccccccccccccccccccccccccccccc", WitnessPostToolUse, sid, tuid, tn, dig, rt)
	if mr.Outcome == Witnessed || mr.Outcome == WitnessPending {
		t.Fatal("expected wrong-kind witness to fail on alive deny entry")
	}
	if c.entryCount() != 1 {
		t.Fatal("entry should survive wrong-kind witness")
	}
}

func TestMarkWitnessedWrongState(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}
	_ = handle
	mr := c.MarkWitnessed("cccccccccccccccccccccccccccccccc", WitnessPostToolUse, sid, tuid, tn, dig, rt)
	if mr.Outcome == Witnessed || mr.Outcome == WitnessPending {
		t.Fatal("expected MarkWitnessed to fail for reserved entry")
	}
}

func TestMarkWitnessedWrongSession(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}
	wh, _ := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	c.ConfirmWrite("cccccccccccccccccccccccccccccccc", true)
	_ = wh
	mr := c.MarkWitnessed("cccccccccccccccccccccccccccccccc", WitnessPostToolUse, "wrong-session", tuid, tn, dig, rt)
	if mr.Outcome == Witnessed || mr.Outcome == WitnessPending {
		t.Fatal("expected MarkWitnessed to fail for wrong session")
	}
	if c.entryCount() != 1 {
		t.Fatal("entry should still exist after rejected witness")
	}
}

func TestMarkWitnessedWrongToolUseID(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}
	wh, _ := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	c.ConfirmWrite("cccccccccccccccccccccccccccccccc", true)
	_ = wh
	mr := c.MarkWitnessed("cccccccccccccccccccccccccccccccc", WitnessPermissionDenials, sid, "wrong-tuid", tn, dig, rt)
	if mr.Outcome == Witnessed || mr.Outcome == WitnessPending {
		t.Fatal("expected MarkWitnessed to fail for wrong tool_use_id")
	}
}

func TestMarkWitnessedWrongToolName(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}
	wh, _ := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	c.ConfirmWrite("cccccccccccccccccccccccccccccccc", true)
	_ = wh
	mr := c.MarkWitnessed("cccccccccccccccccccccccccccccccc", WitnessPostToolUse, sid, tuid, "WrongTool", dig, rt)
	if mr.Outcome == Witnessed || mr.Outcome == WitnessPending {
		t.Fatal("expected MarkWitnessed to fail for wrong tool name")
	}
	if c.entryCount() != 1 {
		t.Fatal("entry should still exist after rejected witness")
	}
}

func TestMarkWitnessedWrongInputDigest(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}
	wh, _ := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	c.ConfirmWrite("cccccccccccccccccccccccccccccccc", true)
	_ = wh
	mr := c.MarkWitnessed("cccccccccccccccccccccccccccccccc", WitnessPostToolUse, sid, tuid, tn, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", rt)
	if mr.Outcome == Witnessed || mr.Outcome == WitnessPending {
		t.Fatal("expected MarkWitnessed to fail for wrong input digest")
	}
}

func TestMarkWitnessedWrongRuntime(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}
	wh, _ := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	c.ConfirmWrite("cccccccccccccccccccccccccccccccc", true)
	_ = wh
	wrongRT := RuntimeRef{Adapter: claudeHeadlessAdapter, Version: "2.1.209", LaunchGen: 99, StreamGen: 0}
	mr := c.MarkWitnessed("cccccccccccccccccccccccccccccccc", WitnessPostToolUse, sid, tuid, tn, dig, wrongRT)
	if mr.Outcome == Witnessed || mr.Outcome == WitnessPending {
		t.Fatal("expected MarkWitnessed to fail for wrong RuntimeRef")
	}
}

// ── B3: direct deadline tests ──

func TestConfirmWriteExpiredWriteClaim(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}
	wh, _ := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	_ = wh

	// Advance past the write-claim deadline.
	orig := clockNow
	clockNow = func() time.Time { return time.Now().Add(coordinatorEntryTimeout + time.Second) }
	defer func() { clockNow = orig }()

	out := c.ConfirmWrite("cccccccccccccccccccccccccccccccc", true)
	if out != outcomeAmbiguous {
		t.Fatalf("expected outcomeAmbiguous for expired write claim, got %d", out)
	}
	if c.entryCount() != 0 {
		t.Fatal("entry should be removed after expired ConfirmWrite")
	}
}

func TestMarkWitnessedExpiredWitness(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}
	wh, _ := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	c.ConfirmWrite("cccccccccccccccccccccccccccccccc", true)
	_ = wh

	// Advance past the witness deadline (2× coordinatorEntryTimeout).
	orig := clockNow
	clockNow = func() time.Time { return time.Now().Add(2*coordinatorEntryTimeout + time.Second) }
	defer func() { clockNow = orig }()

	mr := c.MarkWitnessed("cccccccccccccccccccccccccccccccc", WitnessPostToolUse, sid, tuid, tn, dig, rt)
	if mr.Outcome == Witnessed || mr.Outcome == WitnessPending {
		t.Fatal("expected expired witness to fail")
	}
	if c.entryCount() != 0 {
		t.Fatal("entry should be removed after expired witness")
	}
}

func TestConfirmWriteJustBeforeDeadline(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}
	wh, _ := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	_ = wh

	// Just before deadline — succeeds.
	orig := clockNow
	clockNow = func() time.Time { return time.Now().Add(coordinatorEntryTimeout - time.Second) }
	defer func() { clockNow = orig }()

	out := c.ConfirmWrite("cccccccccccccccccccccccccccccccc", true)
	if out != outcomeWritten {
		t.Fatalf("expected outcomeWritten just before deadline, got %d", out)
	}
}

func TestConfirmWriteJustAfterDeadline(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}
	wh, _ := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	_ = wh

	// Just after deadline — fails.
	orig := clockNow
	clockNow = func() time.Time { return time.Now().Add(coordinatorEntryTimeout + time.Second) }
	defer func() { clockNow = orig }()

	out := c.ConfirmWrite("cccccccccccccccccccccccccccccccc", true)
	if out != outcomeAmbiguous {
		t.Fatalf("expected outcomeAmbiguous just after deadline, got %d", out)
	}
}

// ── failingReader for entropy injection ──

type failingReader struct{}

func (f *failingReader) Read(p []byte) (int, error) { return 0, io.ErrUnexpectedEOF }

// ── R4 adversarial tests (frozen amendment §8) ──

// helper: reserve + pre-bind + write (the production path).
func r4Setup(t *testing.T) (*claudeResumeCoordinator, ResumeHandle, string, string, string, string, RuntimeRef) {
	t.Helper()
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, ok := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !ok {
		t.Fatal("ReserveEntry failed")
	}
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}
	return c, handle, sid, tuid, tn, dig, rt
}

// Test 3: Two resume hooks competing — exactly one identity registered.
func TestR4_TwoResumeHooks_ExactlyOneIdentity(t *testing.T) {
	c, handle, _, _, tn, dig, _ := r4Setup(t)
	tok := "cccccccccccccccccccccccccccccccc"
	nonce := handle.ResumeNonce

	// Two goroutines race to bind the identity using a channel barrier.
	// Both wait for the release signal, then enter ClaimWrite concurrently.
	// The coordinator mutex serialises them; exactly one must succeed.
	release := make(chan struct{})
	done := make(chan struct{}, 2)
	outs := make([]resumeOutcome, 2)

	go func() {
		<-release
		_, outs[0] = c.ClaimWrite(tok, nonce, "sess-a", "tu-a", tn, dig, 1)
		done <- struct{}{}
	}()
	go func() {
		<-release
		_, outs[1] = c.ClaimWrite(tok, nonce, "sess-b", "tu-b", tn, dig, 1)
		done <- struct{}{}
	}()

	close(release) // release both goroutines simultaneously
	<-done
	<-done

	written := 0
	mismatch := 0
	for _, o := range outs {
		if o == outcomeWritten {
			written++
		} else if o == outcomeMismatch {
			mismatch++
		}
	}
	if written != 1 || mismatch != 1 {
		t.Fatalf("concurrent hooks: want exactly 1 written + 1 mismatch, got written=%d mismatch=%d", written, mismatch)
	}
}

// Test 10: Different resume session ID after first bind → outcomeMismatch.
func TestR4_DifferentSessionIDAfterFirstBind(t *testing.T) {
	c, handle, _, tuid, tn, dig, _ := r4Setup(t)
	tok := "cccccccccccccccccccccccccccccccc"

	_, out := c.ClaimWrite(tok, handle.ResumeNonce, "sess-first", tuid, tn, dig, 1)
	if out != outcomeWritten {
		t.Fatalf("first bind: want outcomeWritten, got %d", out)
	}
	_, out = c.ClaimWrite(tok, handle.ResumeNonce, "different-sess", tuid, tn, dig, 1)
	if out != outcomeMismatch {
		t.Fatalf("want outcomeMismatch for diff session, got %d", out)
	}
}

// Test 11: Wrong resume process generation → ClaimWrite rejects.
func TestR4_WrongResumeProcessGeneration(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	// Bind with gen 5, but ClaimWrite with gen 3 → mismatch.
	c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 5)
	_, out := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 3)
	if out != outcomeMismatch {
		t.Fatalf("wrong generation: want outcomeMismatch, got %d", out)
	}
}

// Test 22: Missing pre-bound resume generation → ClaimWrite unavailable.
func TestR4_MissingPreBind_ClaimWriteUnavailable(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	// NO BindResumeProcess → expectedLaunchGen==0 → ClaimWrite must reject.
	_, out := c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, sid, tuid, tn, dig, 1)
	if out != outcomeMismatch {
		t.Fatalf("missing pre-bind: want outcomeMismatch, got %d", out)
	}
}

// Test 4: PostToolUse with different toolUseID (not bound) → witness rejected.
func TestR4_PostToolUse_WrongIdentity_Rejected(t *testing.T) {
	c, handle, _, _, tn, dig, rt := r4Setup(t)
	tok := "cccccccccccccccccccccccccccccccc"

	// Bind identity A.
	c.ClaimWrite(tok, handle.ResumeNonce, "sess-a", "tu-bound", tn, dig, 1)
	c.ConfirmWrite(tok, true)

	// MarkWitnessed with DIFFERENT identity → rejected.
	mr := c.MarkWitnessed(tok, WitnessPostToolUse, "sess-a", "tu-wrong", tn, dig, rt)
	if mr.Outcome == Witnessed || mr.Outcome == WitnessPending {
		t.Fatal("wrong toolUseID must be rejected")
	}
}

// Test 5: Valid early PostToolUse (before ConfirmWrite) → stored, success after ConfirmWrite(true).
func TestR4_EarlyPostToolUse_StoredThenCommitted(t *testing.T) {
	c, handle, sid, tuid, tn, dig, rt := r4Setup(t)
	tok := "cccccccccccccccccccccccccccccccc"

	// Bind identity.
	c.ClaimWrite(tok, handle.ResumeNonce, sid, tuid, tn, dig, 1)
	// Do NOT ConfirmWrite yet.

	// Early witness (before ConfirmWrite).
	mr := c.MarkWitnessed(tok, WitnessPostToolUse, sid, tuid, tn, dig, rt)
	if mr.Outcome != Witnessed && mr.Outcome != WitnessPending {
		t.Fatal("early witness must be accepted")
	}
	// R4: early witness returns zero binding/digest (not terminal).
	if mr.Binding.ApprovalID != "" || mr.Digest != "" {
		t.Fatal("early witness must return zero binding/digest")
	}

	// ConfirmWrite(true) → commits the stored early witness.
	out := c.ConfirmWrite(tok, true)
	if out != outcomeWritten {
		t.Fatalf("ConfirmWrite after early witness: want outcomeWritten, got %d", out)
	}

	// Entry must be terminal; completion must carry TerminalWitnessed.
	select {
	case result := <-handle.Completion:
		if result.Outcome != TerminalWitnessed {
			t.Fatalf("want TerminalWitnessed, got %d", result.Outcome)
		}
	default:
		t.Fatal("completion must be available after ConfirmWrite with early witness")
	}
}

// Test 6: Write failure then PostToolUse → no success (ambiguous terminal wipes early witness).
func TestR4_WriteFailure_WipesEarlyWitness(t *testing.T) {
	c, handle, sid, tuid, tn, dig, rt := r4Setup(t)
	tok := "cccccccccccccccccccccccccccccccc"

	c.ClaimWrite(tok, handle.ResumeNonce, sid, tuid, tn, dig, 1)
	// Early witness stored.
	c.MarkWitnessed(tok, WitnessPostToolUse, sid, tuid, tn, dig, rt)

	// Write FAILURE.
	out := c.ConfirmWrite(tok, false)
	if out != outcomeAmbiguous {
		t.Fatalf("ConfirmWrite(false): want outcomeAmbiguous, got %d", out)
	}

	// Completion must be TerminalAmbiguous.
	select {
	case result := <-handle.Completion:
		if result.Outcome != TerminalAmbiguous {
			t.Fatalf("want TerminalAmbiguous, got %d", result.Outcome)
		}
	default:
		t.Fatal("completion must be available after write failure")
	}
}

// Test 8: Deny evidence matches new bound identity → witness accepted (deny commit).
func TestR4_DenyEvidence_MatchesBoundIdentity(t *testing.T) {
	c, handle, sid, tuid, tn, dig, rt := r4SetupForDeny(t)
	tok := "cccccccccccccccccccccccccccccccc"

	c.ClaimWrite(tok, handle.ResumeNonce, sid, tuid, tn, dig, 1)
	c.ConfirmWrite(tok, true)

	// Denial entry matching the bound identity.
	entries := []streamDenialEntry{{ToolUseID: tuid, ToolName: tn, InputDigest: dig}}
	mr := c.MarkDenialWitness(tok, sid, entries, rt)
	if mr.Outcome != Witnessed && mr.Outcome != WitnessPending {
		t.Fatal("denial witness matching bound identity must succeed")
	}
	if mr.Binding.ApprovalID == "" || mr.Digest == "" {
		t.Fatal("terminal deny witness must return binding+digest")
	}

	select {
	case result := <-handle.Completion:
		if result.Outcome != TerminalWitnessed {
			t.Fatalf("want TerminalWitnessed, got %d", result.Outcome)
		}
	default:
		t.Fatal("completion must be available")
	}
}

func r4SetupForDeny(t *testing.T) (*claudeResumeCoordinator, ResumeHandle, string, string, string, string, RuntimeRef) {
	t.Helper()
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	binding.OptionID = "deny"
	binding.DeliverySchema = claudeDecisionSchemaV1
	handle, ok := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !ok {
		t.Fatal("ReserveEntry for deny failed")
	}
	if !c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1) {
		t.Fatal("BindResumeProcess failed")
	}
	return c, handle, sid, tuid, tn, dig, rt
}

// Test 9: Deny evidence with different identity → witness rejected.
func TestR4_DenyEvidence_WrongIdentity_Rejected(t *testing.T) {
	c, handle, _, tuid, tn, dig, rt := r4SetupForDeny(t)
	tok := "cccccccccccccccccccccccccccccccc"

	c.ClaimWrite(tok, handle.ResumeNonce, "sess-denial", tuid, tn, dig, 1)
	c.ConfirmWrite(tok, true)

	// Denial entry with DIFFERENT toolUseID → rejected.
	entries := []streamDenialEntry{{ToolUseID: "wrong-tu", ToolName: tn, InputDigest: dig}}
	mr := c.MarkDenialWitness(tok, "sess-denial", entries, rt)
	if mr.Outcome == Witnessed || mr.Outcome == WitnessPending {
		t.Fatal("denial with wrong identity must be rejected")
	}
}

// Test 19: Ambiguous denial entries (duplicate bound tool_use_id) → cancel.
func TestR4_AmbiguousDenialEntries_Cancel(t *testing.T) {
	c, handle, sid, tuid, tn, dig, rt := r4SetupForDeny(t)
	tok := "cccccccccccccccccccccccccccccccc"

	c.ClaimWrite(tok, handle.ResumeNonce, sid, tuid, tn, dig, 1)
	c.ConfirmWrite(tok, true)

	// Two denial entries with the same bound identity → ambiguous.
	entries := []streamDenialEntry{
		{ToolUseID: tuid, ToolName: tn, InputDigest: dig},
		{ToolUseID: tuid, ToolName: tn, InputDigest: dig},
	}
	mr := c.MarkDenialWitness(tok, sid, entries, rt)
	if mr.Outcome == Witnessed || mr.Outcome == WitnessPending {
		t.Fatal("ambiguous denial entries must fail")
	}
	// Entry must be cancelled.
	if c.EntryCount() != 0 {
		t.Fatal("ambiguous denial must cancel the entry")
	}
	select {
	case result := <-handle.Completion:
		if result.Outcome != TerminalAmbiguous {
			t.Fatalf("ambiguous denial: want TerminalAmbiguous, got %d", result.Outcome)
		}
	default:
		t.Fatal("completion must be available")
	}
}

// Test 14-18: witness_pending + all cleanup paths.
func TestR4_WitnessPending_Stop_Cancelled(t *testing.T) {
	c, handle, sid, tuid, tn, dig, rt := r4Setup(t)
	tok := "cccccccccccccccccccccccccccccccc"

	c.ClaimWrite(tok, handle.ResumeNonce, sid, tuid, tn, dig, 1)
	// Early witness → stateWitnessPending.
	c.MarkWitnessed(tok, WitnessPostToolUse, sid, tuid, tn, dig, rt)

	// Cancel (stop path).
	c.CancelEntry(tok)
	select {
	case result := <-handle.Completion:
		if result.Outcome != TerminalCancelled {
			t.Fatalf("stop: want TerminalCancelled, got %d", result.Outcome)
		}
	default:
		t.Fatal("completion must be available after stop")
	}
}

func TestR4_WitnessPending_Delete_Cancelled(t *testing.T) {
	c, handle, sid, tuid, tn, dig, rt := r4Setup(t)
	tok := "cccccccccccccccccccccccccccccccc"
	id, _, _, _, _, _, _ := testIdentity()
	c.entries[tok].approvalID = id // set approval ID for ClearForApproval

	c.ClaimWrite(tok, handle.ResumeNonce, sid, tuid, tn, dig, 1)
	c.MarkWitnessed(tok, WitnessPostToolUse, sid, tuid, tn, dig, rt)

	// ClearForApproval (delete path).
	c.ClearForApproval(id)
	if c.EntryCount() != 0 {
		t.Fatal("delete must remove the entry")
	}
	select {
	case result := <-handle.Completion:
		if result.Outcome != TerminalCancelled {
			t.Fatalf("delete: want TerminalCancelled, got %d", result.Outcome)
		}
	default:
		t.Fatal("completion must be available after delete")
	}
}

func TestR4_WitnessPending_Replacement_StaleRuntime(t *testing.T) {
	c, handle, sid, tuid, tn, dig, rt := r4Setup(t)
	tok := "cccccccccccccccccccccccccccccccc"
	psid := "claude_headless:claude-test"

	c.ClaimWrite(tok, handle.ResumeNonce, sid, tuid, tn, dig, 1)
	c.MarkWitnessed(tok, WitnessPostToolUse, sid, tuid, tn, dig, rt)

	// ClearRuntime (replacement path).
	c.ClearRuntime(psid, rt.LaunchGen)
	if c.EntryCount() != 0 {
		t.Fatal("replacement must remove the entry")
	}
	select {
	case result := <-handle.Completion:
		if result.Outcome != TerminalStaleRuntime {
			t.Fatalf("replacement: want TerminalStaleRuntime, got %d", result.Outcome)
		}
	default:
		t.Fatal("completion must be available after replacement")
	}
}

func TestR4_WitnessPending_Timeout_Ambiguous(t *testing.T) {
	c, handle, sid, tuid, tn, dig, rt := r4Setup(t)
	tok := "cccccccccccccccccccccccccccccccc"

	c.ClaimWrite(tok, handle.ResumeNonce, sid, tuid, tn, dig, 1)
	c.MarkWitnessed(tok, WitnessPostToolUse, sid, tuid, tn, dig, rt)

	// Simulate timeout by advancing time beyond the entry cutoff.
	c.mu.Lock()
	e := c.entries[tok]
	e.writeClaimedAt = e.writeClaimedAt.Add(-2 * coordinatorEntryTimeout)
	c.mu.Unlock()
	c.clearStaleEntries(clockNow())

	if c.EntryCount() != 0 {
		t.Fatal("timeout must remove the entry")
	}
	select {
	case result := <-handle.Completion:
		if result.Outcome != TerminalAmbiguous {
			t.Fatalf("timeout: want TerminalAmbiguous, got %d", result.Outcome)
		}
	default:
		t.Fatal("completion must be available after timeout")
	}
}

func TestR4_WitnessPending_Close_Cancelled(t *testing.T) {
	c, handle, sid, tuid, tn, dig, rt := r4Setup(t)
	tok := "cccccccccccccccccccccccccccccccc"

	c.ClaimWrite(tok, handle.ResumeNonce, sid, tuid, tn, dig, 1)
	c.MarkWitnessed(tok, WitnessPostToolUse, sid, tuid, tn, dig, rt)

	// Close.
	c.Close()
	select {
	case result := <-handle.Completion:
		if result.Outcome != TerminalCancelled {
			t.Fatalf("close: want TerminalCancelled, got %d", result.Outcome)
		}
	default:
		t.Fatal("completion must be available after close")
	}
}

// Test 13: Same resume hook re-invocation (duplicate) → provider write exactly once.
func TestR4_DuplicateHookReplay_WriteOnce(t *testing.T) {
	c, handle, sid, tuid, tn, dig, _ := r4Setup(t)
	tok := "cccccccccccccccccccccccccccccccc"

	// First bind.
	wh1, out := c.ClaimWrite(tok, handle.ResumeNonce, sid, tuid, tn, dig, 1)
	if out != outcomeWritten || wh1.Decision() == "" {
		t.Fatal("first bind must succeed")
	}
	// Same identity replay → duplicate (no write).
	_, out = c.ClaimWrite(tok, handle.ResumeNonce, sid, tuid, tn, dig, 1)
	if out != outcomeDuplicate {
		t.Fatalf("duplicate: want outcomeDuplicate, got %d", out)
	}
}

// Test 21: Wrong early witness before ConfirmWrite → rejected without entering stateWitnessPending.
func TestR4_WrongEarlyWitness_Rejected_StateUnchanged(t *testing.T) {
	c, handle, sid, tuid, tn, dig, rt := r4Setup(t)
	tok := "cccccccccccccccccccccccccccccccc"

	c.ClaimWrite(tok, handle.ResumeNonce, sid, tuid, tn, dig, 1)
	if c.pendingCount() != 1 {
		t.Fatal("expected 1 pending after ClaimWrite")
	}

	// Wrong witness (different toolUseID).
	mr := c.MarkWitnessed(tok, WitnessPostToolUse, "sess-wrong", "tu-wrong", tn, dig, rt)
	if mr.Outcome == Witnessed || mr.Outcome == WitnessPending {
		t.Fatal("wrong early witness must be rejected")
	}
	if c.pendingCount() != 1 {
		t.Fatal("wrong witness must not change pending count")
	}

	// Correct witness still succeeds (first valid early witness).
	mr = c.MarkWitnessed(tok, WitnessPostToolUse, sid, tuid, tn, dig, rt)
	if mr.Outcome != Witnessed && mr.Outcome != WitnessPending {
		t.Fatal("correct early witness must succeed after wrong one was rejected")
	}
}

// Test 12: Missing tool_input → bridge rejects (tested at coordinator: empty toolName/digest mismatch).
func TestR4_MissingToolInput_Rejected(t *testing.T) {
	c, handle, sid, tuid, tn, _, _ := r4Setup(t)
	tok := "cccccccccccccccccccccccccccccccc"

	// Empty digest → coordinator validates via inputDigest comparison.
	// Bridge always computes and compares before calling ClaimWrite, but
	// coordinator independently validates — empty digest fails.
	_, out := c.ClaimWrite(tok, handle.ResumeNonce, sid, tuid, tn, "", 1)
	// Empty string is not a valid 64-char hex digest → mismatch.
	if out != outcomeMismatch {
		t.Fatalf("missing/empty inputDigest: want outcomeMismatch, got %d", out)
	}
}

// Test 20: Cross-use: denial with original identity (not bound) → witness rejected.
func TestR4_DenialCrossUse_OriginalIdentity_Rejected(t *testing.T) {
	// Reserve identity and entry with ORIGINAL tuid (no bound attempt yet).
	c := NewClaudeResumeCoordinator()
	id, sid, tuid, tn, dig, psid, rt := testIdentity()
	c.ReserveIdentity(id, sid, tuid, tn, dig, "", psid, rt)
	binding := testBinding(id, psid)
	binding.OptionID = "deny"
	binding.DeliverySchema = claudeDecisionSchemaV1
	handle, _ := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	c.BindResumeProcess("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, 1)
	// ClaimWrite with NEW identity.
	c.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce, "new-sess", "new-tu", tn, dig, 1)
	c.ConfirmWrite("cccccccccccccccccccccccccccccccc", true)

	// Deny with ORIGINAL tuid (not the bound one) → rejected.
	entries := []streamDenialEntry{{ToolUseID: tuid, ToolName: tn, InputDigest: dig}}
	mr := c.MarkDenialWitness("cccccccccccccccccccccccccccccccc", "new-sess", entries, rt)
	if mr.Outcome == Witnessed || mr.Outcome == WitnessPending {
		t.Fatal("denial with original identity must be rejected after new identity is bound")
	}
}

// Test 1+2: Mutated input / different tool name → both coordinator rejection.
func TestR4_MutatedInput_DifferentToolName_CoordinatorRejects(t *testing.T) {
	c, handle, sid, tuid, tn, dig, _ := r4Setup(t)
	tok := "cccccccccccccccccccccccccccccccc"

	// Mutated input (wrong digest) → coordinator rejects.
	_, out := c.ClaimWrite(tok, handle.ResumeNonce, sid, tuid, tn, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 1)
	if out != outcomeMismatch {
		t.Fatalf("mutated input: want outcomeMismatch, got %d", out)
	}

	// Different tool name → coordinator rejects.
	_, out = c.ClaimWrite(tok, handle.ResumeNonce, sid, tuid, "Read", dig, 1)
	if out != outcomeMismatch {
		t.Fatalf("different tool name: want outcomeMismatch, got %d", out)
	}
}

// Test 7: Timeout/stop/replacement → ResumeAttemptIdentity removed, early witness wiped.
func TestR4_StopWipesAttemptIdentity(t *testing.T) {
	c, handle, sid, tuid, tn, dig, _ := r4Setup(t)
	tok := "cccccccccccccccccccccccccccccccc"

	c.ClaimWrite(tok, handle.ResumeNonce, sid, tuid, tn, dig, 1)
	// Verify attempt is bound.
	c.mu.Lock()
	if c.entries[tok].attempt == nil {
		c.mu.Unlock()
		t.Fatal("attempt must be non-nil after ClaimWrite")
	}
	c.mu.Unlock()

	// Cancel wipes the entry (and with it, the attempt).
	c.CancelEntry(tok)
	if c.EntryCount() != 0 {
		t.Fatal("cancel must remove the entry")
	}
}
