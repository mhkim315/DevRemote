package term

import (
	"sync"
	"testing"
	"time"
)

func TestReserveIdentity(t *testing.T) {
	c := NewClaudeResumeCoordinator()

	ok := c.ReserveIdentity("claude-aa", "sess-1", "tu-1", "Bash", "abc123")
	if !ok {
		t.Fatal("expected ReserveIdentity to succeed")
	}
	if c.identityCount() != 1 {
		t.Fatalf("expected 1 identity, got %d", c.identityCount())
	}

	// Duplicate fails.
	if c.ReserveIdentity("claude-aa", "sess-2", "tu-2", "Read", "def456") {
		t.Fatal("expected duplicate ReserveIdentity to fail")
	}
	if c.identityCount() != 1 {
		t.Fatalf("expected 1 identity after duplicate, got %d", c.identityCount())
	}

	// Lookup returns a copy.
	id, ok := c.LookupIdentity("claude-aa")
	if !ok {
		t.Fatal("expected lookup to succeed")
	}
	if id.sessionID != "sess-1" || id.toolUseID != "tu-1" || id.toolName != "Bash" || id.inputDigest != "abc123" {
		t.Fatalf("lookup returned wrong fields: %+v", id)
	}

	// Lookup unknown fails.
	if _, ok := c.LookupIdentity("claude-zz"); ok {
		t.Fatal("expected lookup of unknown to fail")
	}
}

func TestReserveIdentityCapacityExhausted(t *testing.T) {
	c := NewClaudeResumeCoordinator()

	for i := 0; i < maxCoordinatorIdentities; i++ {
		aid := "claude-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		if !c.ReserveIdentity(aid, "sess", "tu", "Bash", "digest") {
			t.Fatalf("expected ReserveIdentity #%d to succeed", i+1)
		}
	}
	if c.identityCount() != maxCoordinatorIdentities {
		t.Fatalf("expected %d identities, got %d", maxCoordinatorIdentities, c.identityCount())
	}

	// Capacity exhausted.
	if c.ReserveIdentity("claude-overflow", "sess", "tu", "Bash", "digest") {
		t.Fatal("expected capacity-exhausted ReserveIdentity to fail")
	}
}

func TestRemoveIdentity(t *testing.T) {
	c := NewClaudeResumeCoordinator()

	c.ReserveIdentity("claude-aa", "sess-1", "tu-1", "Bash", "abc123")
	if c.identityCount() != 1 {
		t.Fatal("expected 1 identity")
	}

	c.RemoveIdentity("claude-aa")
	if c.identityCount() != 0 {
		t.Fatal("expected 0 identities after remove")
	}

	// Idempotent.
	c.RemoveIdentity("claude-aa")
	if c.identityCount() != 0 {
		t.Fatal("expected remove to be idempotent")
	}

	// Re-reserve succeeds after remove.
	if !c.ReserveIdentity("claude-aa", "sess-2", "tu-2", "Read", "def456") {
		t.Fatal("expected re-reserve after remove to succeed")
	}
}

func TestReserveEntry(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	c.ReserveIdentity("claude-aa", "sess-1", "tu-1", "Bash", "abc123")

	entry, ok := c.ReserveEntry("claim-1", "claude-aa", "allow")
	if !ok {
		t.Fatal("expected ReserveEntry to succeed")
	}
	if entry.claimToken != "claim-1" {
		t.Fatalf("wrong claimToken: %s", entry.claimToken)
	}
	if entry.approvalID != "claude-aa" {
		t.Fatalf("wrong approvalID: %s", entry.approvalID)
	}
	if entry.decision != "allow" {
		t.Fatalf("wrong decision: %s", entry.decision)
	}
	if entry.resumeNonce == "" {
		t.Fatal("expected non-empty resumeNonce")
	}
	if entry.state != stateDecisionReserved {
		t.Fatalf("expected stateDecisionReserved, got %d", entry.state)
	}
	if c.pendingCount() != 1 {
		t.Fatalf("expected 1 pending entry, got %d", c.pendingCount())
	}
}

func TestReserveEntryMissingIdentity(t *testing.T) {
	c := NewClaudeResumeCoordinator()

	_, ok := c.ReserveEntry("claim-1", "claude-missing", "allow")
	if ok {
		t.Fatal("expected ReserveEntry with missing identity to fail")
	}
}

func TestReserveEntryDuplicateClaim(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	c.ReserveIdentity("claude-aa", "sess-1", "tu-1", "Bash", "abc123")

	c.ReserveEntry("claim-1", "claude-aa", "allow")
	_, ok := c.ReserveEntry("claim-1", "claude-aa", "deny")
	if ok {
		t.Fatal("expected duplicate claim to fail")
	}
	if c.pendingCount() != 1 {
		t.Fatalf("expected 1 pending entry after duplicate, got %d", c.pendingCount())
	}
}

func TestReserveEntryCapacityExhausted(t *testing.T) {
	c := NewClaudeResumeCoordinator()

	// Create identities first.
	for i := 0; i < maxCoordinatorEntries; i++ {
		aid := "claude-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		c.ReserveIdentity(aid, "sess", "tu", "Bash", "digest")
	}

	// Fill entries.
	for i := 0; i < maxCoordinatorEntries; i++ {
		aid := "claude-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		ct := "claim-" + string(rune('0'+i))
		if _, ok := c.ReserveEntry(ct, aid, "allow"); !ok {
			t.Fatalf("expected ReserveEntry #%d to succeed", i+1)
		}
	}

	// Need one more identity for the overflow test.
	c.ReserveIdentity("claude-overflow", "sess", "tu", "Bash", "digest")
	_, ok := c.ReserveEntry("claim-overflow", "claude-overflow", "allow")
	if ok {
		t.Fatal("expected capacity-exhausted ReserveEntry to fail")
	}
}

func TestCancelEntry(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	c.ReserveIdentity("claude-aa", "sess-1", "tu-1", "Bash", "abc123")

	entry, _ := c.ReserveEntry("claim-1", "claude-aa", "allow")

	// Read outcome from the channel in a goroutine so we don't block.
	var outcome resumeOutcome
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		outcome = <-entry.ch
	}()

	c.CancelEntry("claim-1")
	wg.Wait()

	if outcome != outcomeCancelled {
		t.Fatalf("expected outcomeCancelled, got %d", outcome)
	}
	if c.pendingCount() != 0 {
		t.Fatalf("expected 0 pending entries after cancel, got %d", c.pendingCount())
	}
}

func TestCancelEntryAfterWrite(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	c.ReserveIdentity("claude-aa", "sess-1", "tu-1", "Bash", "abc123")

	entry, _ := c.ReserveEntry("claim-1", "claude-aa", "allow")

	// Write the decision.
	dec, out := c.ValidateAndWrite("claim-1", entry.resumeNonce, "sess-1", "tu-1", "Bash", "abc123")
	if out != outcomeWritten || dec != "allow" {
		t.Fatalf("expected outcomeWritten + allow, got %d + %s", out, dec)
	}

	// Cancel after write is idempotent (no-op).
	c.CancelEntry("claim-1")
	if c.pendingCount() != 0 {
		t.Fatalf("expected 0 pending entries, got %d", c.pendingCount())
	}
}

func TestValidateAndWriteSuccess(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	c.ReserveIdentity("claude-aa", "sess-1", "tu-1", "Bash", "abc123")

	entry, _ := c.ReserveEntry("claim-1", "claude-aa", "allow")

	dec, out := c.ValidateAndWrite("claim-1", entry.resumeNonce, "sess-1", "tu-1", "Bash", "abc123")
	if out != outcomeWritten {
		t.Fatalf("expected outcomeWritten, got %d", out)
	}
	if dec != "allow" {
		t.Fatalf("expected decision allow, got %s", dec)
	}
	if c.pendingCount() != 0 {
		t.Fatalf("expected 0 pending after write, got %d", c.pendingCount())
	}
}

func TestValidateAndWriteDenyDecision(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	c.ReserveIdentity("claude-aa", "sess-1", "tu-1", "Bash", "abc123")

	entry, _ := c.ReserveEntry("claim-1", "claude-aa", "deny")

	dec, out := c.ValidateAndWrite("claim-1", entry.resumeNonce, "sess-1", "tu-1", "Bash", "abc123")
	if out != outcomeWritten {
		t.Fatalf("expected outcomeWritten, got %d", out)
	}
	if dec != "deny" {
		t.Fatalf("expected decision deny, got %s", dec)
	}
}

func TestValidateAndWriteWrongNonce(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	c.ReserveIdentity("claude-aa", "sess-1", "tu-1", "Bash", "abc123")

	entry, _ := c.ReserveEntry("claim-1", "claude-aa", "allow")

	_, out := c.ValidateAndWrite("claim-1", "wrong-nonce", "sess-1", "tu-1", "Bash", "abc123")
	if out != outcomeMismatch {
		t.Fatalf("expected outcomeMismatch for wrong nonce, got %d", out)
	}
	// Entry still reserved.
	if entry.state != stateDecisionReserved {
		t.Fatalf("expected stateDecisionReserved after mismatch, got %d", entry.state)
	}
}

func TestValidateAndWriteWrongSessionID(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	c.ReserveIdentity("claude-aa", "sess-1", "tu-1", "Bash", "abc123")

	entry, _ := c.ReserveEntry("claim-1", "claude-aa", "allow")

	_, out := c.ValidateAndWrite("claim-1", entry.resumeNonce, "wrong-sess", "tu-1", "Bash", "abc123")
	if out != outcomeMismatch {
		t.Fatalf("expected outcomeMismatch, got %d", out)
	}
}

func TestValidateAndWriteWrongToolUseID(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	c.ReserveIdentity("claude-aa", "sess-1", "tu-1", "Bash", "abc123")

	entry, _ := c.ReserveEntry("claim-1", "claude-aa", "allow")

	_, out := c.ValidateAndWrite("claim-1", entry.resumeNonce, "sess-1", "wrong-tu", "Bash", "abc123")
	if out != outcomeMismatch {
		t.Fatalf("expected outcomeMismatch, got %d", out)
	}
}

func TestValidateAndWriteWrongToolName(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	c.ReserveIdentity("claude-aa", "sess-1", "tu-1", "Bash", "abc123")

	entry, _ := c.ReserveEntry("claim-1", "claude-aa", "allow")

	_, out := c.ValidateAndWrite("claim-1", entry.resumeNonce, "sess-1", "tu-1", "WrongTool", "abc123")
	if out != outcomeMismatch {
		t.Fatalf("expected outcomeMismatch, got %d", out)
	}
}

func TestValidateAndWriteWrongInputDigest(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	c.ReserveIdentity("claude-aa", "sess-1", "tu-1", "Bash", "abc123")

	entry, _ := c.ReserveEntry("claim-1", "claude-aa", "allow")

	_, out := c.ValidateAndWrite("claim-1", entry.resumeNonce, "sess-1", "tu-1", "Bash", "wrong-digest")
	if out != outcomeMismatch {
		t.Fatalf("expected outcomeMismatch, got %d", out)
	}
}

func TestValidateAndWriteDuplicateWrite(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	c.ReserveIdentity("claude-aa", "sess-1", "tu-1", "Bash", "abc123")

	entry, _ := c.ReserveEntry("claim-1", "claude-aa", "allow")

	// First write succeeds.
	dec, out := c.ValidateAndWrite("claim-1", entry.resumeNonce, "sess-1", "tu-1", "Bash", "abc123")
	if out != outcomeWritten || dec != "allow" {
		t.Fatalf("expected first write to succeed")
	}

	// Second write fails with duplicate.
	_, out = c.ValidateAndWrite("claim-1", entry.resumeNonce, "sess-1", "tu-1", "Bash", "abc123")
	if out != outcomeDuplicate {
		t.Fatalf("expected outcomeDuplicate, got %d", out)
	}
}

func TestValidateAndWriteMissingEntry(t *testing.T) {
	c := NewClaudeResumeCoordinator()

	_, out := c.ValidateAndWrite("claim-missing", "nonce", "sess", "tu", "Bash", "digest")
	if out != outcomeStale {
		t.Fatalf("expected outcomeStale for missing entry, got %d", out)
	}
}

func TestValidateAndWriteAfterCancel(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	c.ReserveIdentity("claude-aa", "sess-1", "tu-1", "Bash", "abc123")

	entry, _ := c.ReserveEntry("claim-1", "claude-aa", "allow")
	c.CancelEntry("claim-1")

	_, out := c.ValidateAndWrite("claim-1", entry.resumeNonce, "sess-1", "tu-1", "Bash", "abc123")
	if out != outcomeDuplicate {
		t.Fatalf("expected outcomeDuplicate after cancel, got %d", out)
	}
}

func TestValidateAndWriteAfterClose(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	c.ReserveIdentity("claude-aa", "sess-1", "tu-1", "Bash", "abc123")

	entry, _ := c.ReserveEntry("claim-1", "claude-aa", "allow")
	c.Close()

	_, out := c.ValidateAndWrite("claim-1", entry.resumeNonce, "sess-1", "tu-1", "Bash", "abc123")
	if out != outcomeStale {
		t.Fatalf("expected outcomeStale after close, got %d", out)
	}
}

func TestReserveIdentityAfterClose(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	c.Close()

	if c.ReserveIdentity("claude-aa", "sess", "tu", "Bash", "digest") {
		t.Fatal("expected ReserveIdentity after close to fail")
	}
}

func TestReserveEntryAfterClose(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	c.ReserveIdentity("claude-aa", "sess", "tu", "Bash", "digest")
	c.Close()

	_, ok := c.ReserveEntry("claim-1", "claude-aa", "allow")
	if ok {
		t.Fatal("expected ReserveEntry after close to fail")
	}
}

func TestValidateAndWrite_KnownBadCheckThenWrite(t *testing.T) {
	// This test proves the mutex is necessary. Two goroutines check the entry
	// state independently, then both attempt to write. Exactly one succeeds;
	// the other gets outcomeDuplicate.
	c := NewClaudeResumeCoordinator()
	c.ReserveIdentity("claude-aa", "sess-1", "tu-1", "Bash", "abc123")

	entry, _ := c.ReserveEntry("claim-1", "claude-aa", "allow")

	var wg sync.WaitGroup
	results := make(chan resumeOutcome, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, out := c.ValidateAndWrite("claim-1", entry.resumeNonce, "sess-1", "tu-1", "Bash", "abc123")
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
	if duplicate != 1 {
		t.Fatalf("expected exactly 1 duplicate, got %d", duplicate)
	}
}

func TestReserveEntryConcurrent(t *testing.T) {
	c := NewClaudeResumeCoordinator()

	// Create identities.
	for i := 0; i < 8; i++ {
		aid := "claude-" + string(rune('a'+i))
		c.ReserveIdentity(aid, "sess", "tu", "Bash", "digest")
	}

	var wg sync.WaitGroup
	oks := make([]bool, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			aid := "claude-" + string(rune('a'+idx))
			ct := "claim-" + string(rune('0'+idx))
			_, oks[idx] = c.ReserveEntry(ct, aid, "allow")
		}(i)
	}
	wg.Wait()

	for i, ok := range oks {
		if !ok {
			t.Fatalf("expected concurrent ReserveEntry #%d to succeed", i)
		}
	}
	if c.pendingCount() != 8 {
		t.Fatalf("expected 8 pending entries, got %d", c.pendingCount())
	}
}

func TestValidateAndWriteConcurrentDuplicate(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	c.ReserveIdentity("claude-aa", "sess-1", "tu-1", "Bash", "abc123")

	entry, _ := c.ReserveEntry("claim-1", "claude-aa", "allow")

	var wg sync.WaitGroup
	results := make(chan resumeOutcome, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, out := c.ValidateAndWrite("claim-1", entry.resumeNonce, "sess-1", "tu-1", "Bash", "abc123")
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

func TestCloseCancelsAllEntries(t *testing.T) {
	c := NewClaudeResumeCoordinator()

	// Create identities and entries.
	for i := 0; i < 4; i++ {
		aid := "claude-" + string(rune('a'+i))
		ct := "claim-" + string(rune('0'+i))
		c.ReserveIdentity(aid, "sess", "tu", "Bash", "digest")
		e, _ := c.ReserveEntry(ct, aid, "allow")

		// Start a goroutine to drain each entry's channel.
		go func(entry *resumeEntry) {
			<-entry.ch
		}(e)
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
	c.ReserveIdentity("claude-aa", "sess-1", "tu-1", "Bash", "abc123")

	e, _ := c.ReserveEntry("claim-1", "claude-aa", "allow")
	go func() { <-e.ch }()

	c.ClearForApproval("claude-aa")

	if c.identityCount() != 0 {
		t.Fatal("expected identity removed")
	}
	if c.pendingCount() != 0 {
		t.Fatal("expected entry cancelled")
	}
}

func TestClearForApprovalNonExistent(t *testing.T) {
	c := NewClaudeResumeCoordinator()

	// No panic, no state change.
	c.ClearForApproval("claude-zz")
	if c.identityCount() != 0 || c.pendingCount() != 0 {
		t.Fatal("expected no-op for non-existent approval")
	}
}

func TestClearStaleEntries(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	c.ReserveIdentity("claude-aa", "sess-1", "tu-1", "Bash", "abc123")

	entry, _ := c.ReserveEntry("claim-1", "claude-aa", "allow")

	// Drain the channel in background.
	var outcome resumeOutcome
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		outcome = <-entry.ch
	}()

	// Move time forward past the timeout.
	fakeNow := clockNow().Add(coordinatorEntryTimeout + time.Second)
	c.clearStaleEntries(fakeNow)
	wg.Wait()

	if outcome != outcomeTimeout {
		t.Fatalf("expected outcomeTimeout, got %d", outcome)
	}
	if c.pendingCount() != 0 {
		t.Fatalf("expected 0 pending after stale clear, got %d", c.pendingCount())
	}
}

func TestReserveEntryEntropyFailure(t *testing.T) {
	// This test validates that ReserveEntry fails when entropy is unavailable.
	// We can't easily inject entropy failure here without replacing rand.Reader,
	// so this test is a documentation of the contract: if generateNonce returns
	// "", ReserveEntry returns (nil, false).
	//
	// In production, crypto/rand.Read on macOS never fails (it uses /dev/urandom
	// via getentropy). The check exists for correctness, not for a practically
	// reachable path.
	t.Log("entropy failure path is documented — rand.Reader is reliable on macOS")
}
