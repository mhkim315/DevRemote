package main

import (
	"testing"
	"time"

	"devremote/companion-daemon/internal/term"
)

// ── R5: PreToolUse/PostToolUse hook body boundary tests ──

// TestR5_PreToolUse_RejectsPostOnlyFields: PreToolUse with PostToolUse-only
// fields (tool_response) must defer. A subsequent VALID PreToolUse on the
// SAME claim must succeed, and delivery must be accepted — proving the
// invalid hook did NOT consume the bind slot.
func TestR5_PreToolUse_RejectsPostOnlyFields(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := catalogMakeSetup(t, "allow_once")
	del := term.NewClaudeManagedApprovalDelivery(svc)
	del.SetDrainTimeout(0)
	del.SetPollTimeout(2 * time.Second)

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

	// Step 1: PreToolUse with PostToolUse-only field → defer.
	bodyWithPost := `{"session_id":"` + csid + `","tool_use_id":"` + tuid + `","tool_name":"` + tn + `","tool_input":` + inputJSON + `,"hook_event_name":"PreToolUse","tool_response":"should be rejected"}`
	r := fireRawResumeHook(t, resumeURL, bodyWithPost)
	if !bytesEq(r, term.ClaudeHookResponseBytes("defer")) {
		t.Fatalf("PreToolUse w/ Post field: want defer, got %s", r)
	}

	// Step 2: VALID PreToolUse on SAME claim → allow (non-vacuous).
	fireResumeHook(t, resumeURL, csid, tuid, tn, inputJSON)

	// Step 3: VALID PostToolUse witness → delivery accepted.
	firePostToolHook(t, posttoolURL, csid, tuid, tn, inputJSON)
	<-done
	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("delivery must be accepted after valid hooks, got %s", receipt.Outcome)
	}
}

// TestR5_PostToolUse_AcceptsOwnFields: PostToolUse body with tool_response
// and duration_ms is accepted and commits the witness.
func TestR5_PostToolUse_AcceptsOwnFields(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := catalogMakeSetup(t, "allow_once")
	del := term.NewClaudeManagedApprovalDelivery(svc)
	del.SetDrainTimeout(0)
	del.SetPollTimeout(2 * time.Second)

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

	// PostToolUse with REAL body shape (tool_response + duration_ms).
	postBody := `{"session_id":"` + csid + `","tool_use_id":"` + tuid + `","tool_name":"` + tn + `","tool_input":` + inputJSON + `,"hook_event_name":"PostToolUse","tool_response":"pokitclaudeapprovalprobe","duration_ms":350,"cwd":"/tmp","transcript_path":"","permission_mode":"default","effort":"medium"}`
	fireRawResumeHook(t, posttoolURL, postBody)
	<-done
	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("delivery must be accepted, got %s", receipt.Outcome)
	}
}

// TestR5_PostToolUse_RejectsUnknownField: PostToolUse with unknown field
// strict-decode fails. A subsequent VALID PostToolUse on the SAME claim
// must commit the witness — proving the invalid hook did NOT consume it.
func TestR5_PostToolUse_RejectsUnknownField(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := catalogMakeSetup(t, "allow_once")
	del := term.NewClaudeManagedApprovalDelivery(svc)
	del.SetDrainTimeout(0)
	del.SetPollTimeout(2 * time.Second)

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

	// Step 1: PostToolUse with UNKNOWN field → strict decode rejects.
	postBody := `{"session_id":"` + csid + `","tool_use_id":"` + tuid + `","tool_name":"` + tn + `","tool_input":` + inputJSON + `,"hook_event_name":"PostToolUse","unknown_field":"intruder"}`
	fireRawResumeHook(t, posttoolURL, postBody)

	// Step 2: VALID PostToolUse on SAME claim → delivery accepted.
	firePostToolHook(t, posttoolURL, csid, tuid, tn, inputJSON)
	<-done
	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("delivery must be accepted after valid PostToolUse, got %s", receipt.Outcome)
	}
}

// TestR5_PostToolUse_WrongEventType: PostToolUse endpoint with wrong
// hook_event_name → 200 no-op. A subsequent valid PostToolUse commits.
func TestR5_PostToolUse_WrongEventType(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := catalogMakeSetup(t, "allow_once")
	del := term.NewClaudeManagedApprovalDelivery(svc)
	del.SetDrainTimeout(0)
	del.SetPollTimeout(2 * time.Second)

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

	// Step 1: PostToolUse with wrong event type → 200 no-op.
	postBody := `{"session_id":"` + csid + `","tool_use_id":"` + tuid + `","tool_name":"` + tn + `","tool_input":` + inputJSON + `,"hook_event_name":"PreToolUse"}`
	fireRawResumeHook(t, posttoolURL, postBody)

	// Step 2: VALID PostToolUse → delivery accepted.
	firePostToolHook(t, posttoolURL, csid, tuid, tn, inputJSON)
	<-done
	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("delivery must be accepted after valid PostToolUse, got %s", receipt.Outcome)
	}
}

// TestR5_PostToolUse_DuplicateKey: PostToolUse with duplicate JSON key
// → strict decode rejects → no witness. A subsequent valid PostToolUse
// commits the witness.
func TestR5_PostToolUse_DuplicateKey(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := catalogMakeSetup(t, "allow_once")
	del := term.NewClaudeManagedApprovalDelivery(svc)
	del.SetDrainTimeout(0)
	del.SetPollTimeout(2 * time.Second)

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

	// Step 1: PostToolUse with duplicate key → strict decode rejects.
	postBody := `{"session_id":"` + csid + `","tool_use_id":"` + tuid + `","tool_name":"` + tn + `","tool_name":"duplicate_key","hook_event_name":"PostToolUse"}`
	fireRawResumeHook(t, posttoolURL, postBody)

	// Step 2: VALID PostToolUse → delivery accepted.
	firePostToolHook(t, posttoolURL, csid, tuid, tn, inputJSON)
	<-done
	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("delivery must be accepted after valid PostToolUse, got %s", receipt.Outcome)
	}
}
