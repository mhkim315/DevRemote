package main

import (
	"testing"
	"time"

	"devremote/companion-daemon/internal/term"
)

// ── R4: natural-exit drain lifecycle tests ──

// TestDrain_WitnessWaitsForNaturalExit: with drainTimeout>0, a successful
// delivery does not return until the process exits (rt.exited closes).
func TestDrain_WitnessWaitsForNaturalExit(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := catalogMakeSetup(t, "allow_once")
	del := term.NewClaudeManagedApprovalDelivery(svc)
	del.SetPollTimeout(2 * time.Second)
	del.SetDrainTimeout(10 * time.Second) // long enough to observe the race

	var receipt term.DeliveryReceipt
	done := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(done)
		receipt = del.Deliver(term.ApprovalDeliveryRequest{
			ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload,
		})
	}()

	resumeURL, posttoolURL := captureBridgeURLs(t, l)
	inputJSON := `{"command":"echo pokitclaudeapprovalprobe"}`

	// Fire hooks — the fake process does NOT close rt.exited naturally.
	// The delivery must time out on the drain (10s timeout). After the
	// drain timeout, delivery returns with DeliveryAccepted.
	fireResumeHook(t, resumeURL, csid, tuid, tn, inputJSON)
	firePostToolHook(t, posttoolURL, csid, tuid, tn, inputJSON)
	<-done
	elapsed := time.Since(start)

	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("delivery must be accepted, got %s", receipt.Outcome)
	}
	// With the fake process never exiting, drain should have consumed
	// most of the 10s drain timeout. This proves the drain branch ran.
	if elapsed < 8*time.Second {
		t.Fatalf("drain timeout must have been consumed (elapsed=%v, want >=8s)", elapsed)
	}
}

// TestDrain_NaturalExitReturnsImmediately: with drain, if rt.exited is
// already closed, delivery returns immediately after the witness.
func TestDrain_NaturalExitReturnsImmediately(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := catalogMakeSetup(t, "allow_once")
	del := term.NewClaudeManagedApprovalDelivery(svc)
	del.SetPollTimeout(2 * time.Second)
	del.SetDrainTimeout(5 * time.Second)

	done := make(chan struct{})
	go func() {
		defer close(done)
		del.Deliver(term.ApprovalDeliveryRequest{
			ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload,
		})
	}()

	resumeURL, posttoolURL := captureBridgeURLs(t, l)
	inputJSON := `{"command":"echo pokitclaudeapprovalprobe"}`
	_ = inputJSON

	fireResumeHook(t, resumeURL, csid, tuid, tn, inputJSON)
	firePostToolHook(t, posttoolURL, csid, tuid, tn, inputJSON)
	<-done
	// Must complete quickly — fake process exits after hooks, drain
	// should not block.
}

// TestDrain_NonWitnessDoesNotDrain: timeout/mismatch outcomes must NOT
// enter the drain waiter. The delivery fails immediately.
func TestDrain_NonWitnessDoesNotDrain(t *testing.T) {
	_, svc, _, claim, _, _, _, _ := catalogMakeSetup(t, "allow_once")
	del := term.NewClaudeManagedApprovalDelivery(svc)
	del.SetPollTimeout(100 * time.Millisecond) // short timeout → non-witness
	del.SetDrainTimeout(10 * time.Second)

	start := time.Now()
	var receipt term.DeliveryReceipt
	done := make(chan struct{})
	go func() {
		defer close(done)
		receipt = del.Deliver(term.ApprovalDeliveryRequest{
			ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload,
		})
	}()

	// Do NOT fire any hooks — the timer fires first → DeliveryConflict.
	<-done
	elapsed := time.Since(start)

	if receipt.Outcome == term.DeliveryAccepted {
		t.Fatalf("non-witness must not be accepted, got %s", receipt.Outcome)
	}
	// Must complete quickly — drain was never entered.
	if elapsed > 2*time.Second {
		t.Fatalf("non-witness must not consume drain timeout: elapsed=%v", elapsed)
	}
}

// TestDrain_HungProcessDrainTimesOut: a process that never exits must be
// cleaned up after the drain timeout. The delivery still returns
// DeliveryAccepted (the witness was already committed).
func TestDrain_HungProcessDrainTimesOut(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := catalogMakeSetup(t, "allow_once")
	del := term.NewClaudeManagedApprovalDelivery(svc)
	del.SetPollTimeout(2 * time.Second)
	del.SetDrainTimeout(500 * time.Millisecond) // short drain → timeout

	start := time.Now()
	var receipt term.DeliveryReceipt
	done := make(chan struct{})
	go func() {
		defer close(done)
		receipt = del.Deliver(term.ApprovalDeliveryRequest{
			ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload,
		})
	}()

	resumeURL, posttoolURL := captureBridgeURLs(t, l)
	inputJSON := `{"command":"echo pokitclaudeapprovalprobe"}`

	// Witness commits, but the fake process never closes rt.exited.
	// Drain times out after 500ms → delivery returns accepted.
	fireResumeHook(t, resumeURL, csid, tuid, tn, inputJSON)
	firePostToolHook(t, posttoolURL, csid, tuid, tn, inputJSON)
	<-done
	elapsed := time.Since(start)

	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("delivery must be accepted, got %s", receipt.Outcome)
	}
	if elapsed < 400*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("drain timeout mismatch: elapsed=%v, want ~500ms", elapsed)
	}
}
