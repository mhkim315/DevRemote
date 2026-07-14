package term

import "testing"

// A1-A — contract-freeze proof. These tests prove the frozen approval state
// machine and outcome vocabulary are CLOSED, TOTAL, and internally consistent.
// They lock the vocabulary the later packets (A1-B..A1-E) bind to; a change that
// silently widens a closed set, breaks terminality (at-most-once), or lets an
// unknown value read as success will fail here.

func TestApprovalContract_StateSetClosedAndTerminalityConsistent(t *testing.T) {
	all := []ApprovalState{
		ApprovalPending, ApprovalExecuting, ApprovalApproved, ApprovalRejected,
		ApprovalResolved, ApprovalDeliveryFailed, ApprovalExpired, ApprovalInvalidated,
	}
	// Exactly these eight are valid; nothing outside the set is.
	if len(approvalStateValid) != len(all) {
		t.Fatalf("valid-state set size = %d, want %d (closed set drifted)", len(approvalStateValid), len(all))
	}
	for _, s := range all {
		if !IsValidApprovalState(s) {
			t.Errorf("state %q missing from closed valid set", s)
		}
	}
	if IsValidApprovalState("bogus") {
		t.Error("unknown state accepted as valid (must fail closed)")
	}

	// Non-terminal states are exactly {pending, executing}; the rest are terminal
	// and admit no outgoing transition (this is what enforces at-most-once).
	nonTerminal := map[ApprovalState]bool{ApprovalPending: true, ApprovalExecuting: true}
	for _, s := range all {
		wantTerminal := !nonTerminal[s]
		if IsTerminalApprovalState(s) != wantTerminal {
			t.Errorf("state %q terminal=%v, want %v", s, IsTerminalApprovalState(s), wantTerminal)
		}
		if wantTerminal {
			for _, to := range all {
				if CanTransitionApproval(s, to) {
					t.Errorf("terminal state %q must not transition to %q", s, to)
				}
			}
		}
	}
}

func TestApprovalContract_TransitionsAreExactlyTheFrozenPaths(t *testing.T) {
	// The complete set of legal edges. Any edge not listed must be rejected.
	legal := map[ApprovalState]map[ApprovalState]bool{
		ApprovalPending:   {ApprovalExecuting: true, ApprovalExpired: true, ApprovalInvalidated: true},
		ApprovalExecuting: {ApprovalApproved: true, ApprovalRejected: true, ApprovalResolved: true, ApprovalDeliveryFailed: true},
	}
	all := []ApprovalState{
		ApprovalPending, ApprovalExecuting, ApprovalApproved, ApprovalRejected,
		ApprovalResolved, ApprovalDeliveryFailed, ApprovalExpired, ApprovalInvalidated,
	}
	for _, from := range all {
		for _, to := range all {
			want := legal[from][to]
			if CanTransitionApproval(from, to) != want {
				t.Errorf("CanTransitionApproval(%q,%q)=%v, want %v", from, to, CanTransitionApproval(from, to), want)
			}
		}
	}
	// Unknown states never transition.
	if CanTransitionApproval("bogus", ApprovalApproved) || CanTransitionApproval(ApprovalPending, "bogus") {
		t.Error("transition involving unknown state must be false")
	}
	// There is no path back into pending and no executing→invalidated.
	if CanTransitionApproval(ApprovalExecuting, ApprovalPending) {
		t.Error("executing must not return to pending")
	}
	if CanTransitionApproval(ApprovalExecuting, ApprovalInvalidated) {
		t.Error("executing must not be silently invalidated (must fail closed to delivery_failed)")
	}
}

func TestApprovalContract_KindMapsToTerminalStateByKindNotID(t *testing.T) {
	cases := map[string]ApprovalState{
		"approve": ApprovalApproved,
		"reject":  ApprovalRejected,
		"cancel":  ApprovalRejected,
		"neutral": ApprovalResolved,
		"open":    ApprovalResolved,
		"":        ApprovalResolved,
		"y":       ApprovalResolved, // an action ID-looking value is NOT a kind → neutral
	}
	for kind, want := range cases {
		if got := terminalStateForKind(kind); got != want {
			t.Errorf("terminalStateForKind(%q)=%q, want %q", kind, got, want)
		}
	}
}

func TestApprovalContract_PublicProjectionIsBoundedAndClosed(t *testing.T) {
	// executing is internal-only and must project to pending, never a terminal value.
	if projectPublicStatus(ApprovalExecuting) != string(ApprovalPending) {
		t.Errorf("executing must project to pending, got %q", projectPublicStatus(ApprovalExecuting))
	}
	// Every internal state projects into the closed public set.
	for s := range approvalStateValid {
		pub := projectPublicStatus(s)
		if !IsPublicApprovalStatus(pub) {
			t.Errorf("state %q projects to %q which is not a valid public status", s, pub)
		}
	}
	// An unknown internal state must fail closed to a non-actionable terminal value.
	if got := projectPublicStatus("bogus"); got != string(ApprovalExpired) {
		t.Errorf("unknown internal state must project to expired (fail closed), got %q", got)
	}
	// The public set is exactly the seven consumer-justified values (no executing).
	if len(publicApprovalStatusValid) != 7 {
		t.Fatalf("public status set size = %d, want 7", len(publicApprovalStatusValid))
	}
	if IsPublicApprovalStatus("executing") {
		t.Error("executing must not be part of the public status vocabulary")
	}
}

func TestDeliveryOutcome_ClosedAndOnlyAcceptedSucceeds(t *testing.T) {
	all := []DeliveryOutcome{
		DeliveryAccepted, DeliveryAlreadyAccepted, DeliveryStaleRuntime,
		DeliveryRuntimeMismatch, DeliveryUnavailable, DeliveryConflict, DeliveryRejected,
	}
	if len(deliveryOutcomeValid) != len(all) {
		t.Fatalf("delivery outcome set size = %d, want %d", len(deliveryOutcomeValid), len(all))
	}
	for _, o := range all {
		if !IsValidDeliveryOutcome(o) {
			t.Errorf("outcome %q missing from closed set", o)
		}
		// Only accepted/already_accepted permit a successful commit.
		wantSuccess := o == DeliveryAccepted || o == DeliveryAlreadyAccepted
		if deliverySucceeded(o) != wantSuccess {
			t.Errorf("outcome %q success=%v want %v", o, deliverySucceeded(o), wantSuccess)
		}
	}
	if IsValidDeliveryOutcome("bogus") || deliverySucceeded("bogus") {
		t.Error("unknown delivery outcome must be invalid and never succeed")
	}
}
