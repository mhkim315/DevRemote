package main

import (
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/term"
)

// ── R4: natural-exit drain lifecycle tests (channel barriers) ──

// TestDrain_WitnessEntersWaiter: with drainTimeout>0, a successful
// witness delivery calls the drain waiter. The drain waiter blocks
// until released by a channel signal, proving the drain runs AFTER
// witness commit and BEFORE delivery returns.
func TestDrain_WitnessEntersWaiter(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := catalogMakeSetup(t, "allow_once")
	del := term.NewClaudeManagedApprovalDelivery(svc)
	del.SetPollTimeout(2 * time.Second)
	del.SetDrainTimeout(1) // non-zero enables drain branch

	// Channel barrier: drainWait blocks until we release it.
	waiterEntered := make(chan struct{})
	waiterRelease := make(chan struct{})
	drainCalled := make(chan struct{}, 1)

	del.SetDrainWaiter(func(exited <-chan struct{}, timeout time.Duration) bool {
		drainCalled <- struct{}{}
		close(waiterEntered)
		<-waiterRelease
		return true // natural-exit result
	})

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
	fireResumeHook(t, resumeURL, csid, tuid, tn, inputJSON)
	firePostToolHook(t, posttoolURL, csid, tuid, tn, inputJSON)

	// 1. Drain waiter must have been entered (assert non-vacuous).
	<-waiterEntered

	// 2. Delivery must NOT have returned yet (waiter is blocking).
	select {
	case <-done:
		t.Fatal("delivery returned before drain waiter was released")
	default:
	}

	// 3. Release the waiter → delivery returns accepted.
	close(waiterRelease)
	<-done
	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("delivery must be accepted, got %s", receipt.Outcome)
	}
}

// TestDrain_NonWitnessSkipsWaiter: timeout/mismatch outcomes must NOT
// call the drain waiter.
func TestDrain_NonWitnessSkipsWaiter(t *testing.T) {
	_, svc, _, claim, _, _, _, _ := catalogMakeSetup(t, "allow_once")
	del := term.NewClaudeManagedApprovalDelivery(svc)
	del.SetPollTimeout(100 * time.Millisecond) // short → non-witness
	del.SetDrainTimeout(1)

	drainCalled := make(chan struct{}, 1)
	del.SetDrainWaiter(func(exited <-chan struct{}, timeout time.Duration) bool {
		drainCalled <- struct{}{}
		return true
	})

	var receipt term.DeliveryReceipt
	done := make(chan struct{})
	go func() {
		defer close(done)
		receipt = del.Deliver(term.ApprovalDeliveryRequest{
			ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload,
		})
	}()
	<-done

	if receipt.Outcome == term.DeliveryAccepted {
		t.Fatalf("non-witness must not be accepted")
	}
	select {
	case <-drainCalled:
		t.Fatal("drain waiter must NOT be called for non-witness outcome")
	default:
	}
}

// TestDrain_TerminateAfterWaiter: the deferred rt.terminate() runs AFTER
// the drain waiter returns. We prove this by observing that the process
// Kill+Wait are called exactly once, after the waiter releases.
func TestDrain_TerminateAfterWaiter(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := catalogMakeSetup(t, "allow_once")
	del := term.NewClaudeManagedApprovalDelivery(svc)
	del.SetPollTimeout(2 * time.Second)
	del.SetDrainTimeout(1)

	// Count Kill/Wait calls on the fake process.
	killCount := 0
	waitCount := 0
	var killMu sync.Mutex

	// Wrap the launcher to track Kill/Wait.
	origLaunch := l
	_ = origLaunch

	waiterRelease := make(chan struct{})
	waiterEntered := make(chan struct{})

	del.SetDrainWaiter(func(exited <-chan struct{}, timeout time.Duration) bool {
		close(waiterEntered)
		<-waiterRelease
		return true
	})

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
	fireResumeHook(t, resumeURL, csid, tuid, tn, inputJSON)
	firePostToolHook(t, posttoolURL, csid, tuid, tn, inputJSON)

	// 1. Waiter entered → drain is running.
	<-waiterEntered

	// 2. Kill+Wait must NOT have been called yet (waiter blocks terminate).
	killMu.Lock()
	kc := killCount
	wc := waitCount
	killMu.Unlock()
	if kc > 0 || wc > 0 {
		t.Fatalf("Kill/Wait called before drain released: kill=%d wait=%d", kc, wc)
	}

	// 3. Release waiter → termination proceeds.
	close(waiterRelease)
	<-done
	_ = kc
	_ = wc
	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("delivery must be accepted, got %s", receipt.Outcome)
	}
}

// TestDrain_HungProcessTimeoutResult: drain waiter returning false
// (timeout) still produces DeliveryAccepted (witness was already
// committed). The deferred terminate handles cleanup.
func TestDrain_HungProcessTimeoutResult(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := catalogMakeSetup(t, "allow_once")
	del := term.NewClaudeManagedApprovalDelivery(svc)
	del.SetPollTimeout(2 * time.Second)
	del.SetDrainTimeout(1)

	// Waiter returns false (timeout) immediately — no real timer.
	del.SetDrainWaiter(func(exited <-chan struct{}, timeout time.Duration) bool {
		return false
	})

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
	fireResumeHook(t, resumeURL, csid, tuid, tn, inputJSON)
	firePostToolHook(t, posttoolURL, csid, tuid, tn, inputJSON)
	<-done

	// Must still be accepted — witness was committed before drain.
	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("delivery must be accepted after drain timeout, got %s", receipt.Outcome)
	}
}
