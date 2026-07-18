package main

import (
	"testing"
	"time"

	"devremote/companion-daemon/internal/term"
)

// ── R4: natural-exit drain lifecycle tests (channel barriers) ──

// getResumeProc returns the last process spawned by the fake launcher
// (the resume process created by ResumeForApproval).
func getResumeProc(l *compFakeLauncher) *compPipeProc {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.procs) == 0 {
		return nil
	}
	return l.procs[len(l.procs)-1]
}

// TestDrain_WitnessEntersWaiter: with drainTimeout>0, a successful
// witness delivery calls the drain waiter. The drain waiter blocks
// until released by a channel signal, proving the drain runs AFTER
// witness commit and BEFORE delivery returns.
func TestDrain_WitnessEntersWaiter(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := catalogMakeSetup(t, "allow_once")
	del := term.NewClaudeManagedApprovalDelivery(svc)
	del.SetPollTimeout(2 * time.Second)
	del.SetDrainTimeout(1)

	waiterEntered := make(chan struct{})
	waiterRelease := make(chan struct{})
	drainCalled := make(chan struct{}, 1)

	del.SetDrainWaiter(func(exited <-chan struct{}, timeout time.Duration) bool {
		drainCalled <- struct{}{}
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

	<-waiterEntered

	// Delivery must NOT have returned yet.
	select {
	case <-done:
		t.Fatal("delivery returned before drain waiter was released")
	default:
	}

	close(waiterRelease)
	<-done
	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("delivery must be accepted, got %s", receipt.Outcome)
	}
}

// TestDrain_NonWitnessSkipsWaiter: timeout/mismatch outcomes never call
// the drain waiter.
func TestDrain_NonWitnessSkipsWaiter(t *testing.T) {
	_, svc, _, claim, _, _, _, _ := catalogMakeSetup(t, "allow_once")
	del := term.NewClaudeManagedApprovalDelivery(svc)
	del.SetPollTimeout(100 * time.Millisecond)
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

// TestDrain_TerminateAfterWaiter: the deferred rt.terminate() (which
// calls Kill+Wait) runs AFTER the drain waiter releases. Proved by
// reading the REAL compPipeProc.KillCount/WaitCount before and after.
func TestDrain_TerminateAfterWaiter(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := catalogMakeSetup(t, "allow_once")
	del := term.NewClaudeManagedApprovalDelivery(svc)
	del.SetPollTimeout(2 * time.Second)
	del.SetDrainTimeout(1)

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

	<-waiterEntered

	// Assert Kill+Wait NOT called yet (waiter blocks terminate).
	proc := getResumeProc(l)
	if proc == nil {
		t.Fatal("resume process not found")
	}
	proc.CountMu.Lock()
	kcBefore := proc.KillCount
	wcBefore := proc.WaitCount
	proc.CountMu.Unlock()
	if kcBefore > 0 || wcBefore > 0 {
		t.Fatalf("Kill/Wait called before drain released: kill=%d wait=%d", kcBefore, wcBefore)
	}

	// Release → terminate runs → Kill+Wait each exactly once.
	close(waiterRelease)
	<-done

	proc.CountMu.Lock()
	kcAfter := proc.KillCount
	wcAfter := proc.WaitCount
	proc.CountMu.Unlock()
	if kcAfter != 1 {
		t.Fatalf("Kill must be called exactly once after drain, got %d", kcAfter)
	}
	if wcAfter != 1 {
		t.Fatalf("Wait must be called exactly once after drain, got %d", wcAfter)
	}
	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("delivery must be accepted, got %s", receipt.Outcome)
	}
}

// TestDrain_HungProcessTimeoutResult: drain waiter returning false
// (timeout) still produces DeliveryAccepted. Kill+Wait are called
// exactly once by the deferred terminate().
func TestDrain_HungProcessTimeoutResult(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := catalogMakeSetup(t, "allow_once")
	del := term.NewClaudeManagedApprovalDelivery(svc)
	del.SetPollTimeout(2 * time.Second)
	del.SetDrainTimeout(1)

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

	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("delivery must be accepted after drain timeout, got %s", receipt.Outcome)
	}

	// Kill+Wait each exactly once (deferred terminate after drain).
	proc := getResumeProc(l)
	if proc == nil {
		t.Fatal("resume process not found")
	}
	proc.CountMu.Lock()
	kc := proc.KillCount
	wc := proc.WaitCount
	proc.CountMu.Unlock()
	if kc != 1 {
		t.Fatalf("Kill must be exactly 1 after drain timeout, got %d", kc)
	}
	if wc != 1 {
		t.Fatalf("Wait must be exactly 1 after drain timeout, got %d", wc)
	}
}
