package main

import (
	"testing"
	"time"

	"devremote/companion-daemon/internal/term"
)

// ── R5: PreToolUse/PostToolUse hook body boundary tests ──

// TestR5_PreToolUse_RejectsPostOnlyFields: PreToolUse (initial hook) with
// tool_response or duration_ms must be deferred — those fields are only
// valid in PostToolUse bodies.
func TestR5_PreToolUse_RejectsPostOnlyFields(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := catalogMakeSetup(t, "allow_once")
	del := term.NewClaudeManagedApprovalDelivery(svc)
	del.SetPollTimeout(2 * time.Second)

	done := make(chan struct{})
	go func() {
		defer close(done)
		del.Deliver(term.ApprovalDeliveryRequest{
			ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload,
		})
	}()

	resumeURL, _ := captureBridgeURLs(t, l)
	inputJSON := `{"command":"echo pokitclaudeapprovalprobe"}`

	// PreToolUse body with PostToolUse-only field tool_response → defer.
	bodyWithPost := `{"session_id":"` + csid + `","tool_use_id":"` + tuid + `","tool_name":"` + tn + `","tool_input":` + inputJSON + `,"hook_event_name":"PreToolUse","tool_response":"should be rejected"}`
	r := fireRawResumeHook(t, resumeURL, bodyWithPost)
	if !bytesEq(r, term.ClaudeHookResponseBytes("defer")) {
		t.Fatalf("PreToolUse w/ Post field: want defer, got %s", r)
	}

	// Subsequent VALID PreToolUse on SAME claim → allow (proves invalid
	// hook did not consume the bind slot).
	r2 := fireResumeHook(t, resumeURL, csid, tuid, tn, inputJSON)
	if !bytesEq(r2, term.ClaudeHookResponseBytes("allow")) {
		t.Fatalf("valid PreToolUse after invalid: want allow, got %s", r2)
	}
	<-done
}

// TestR5_PostToolUse_AcceptsOwnFields: PostToolUse body with
// tool_response and duration_ms is accepted (these are legitimate
// PostToolUse fields).
func TestR5_PostToolUse_AcceptsOwnFields(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := catalogMakeSetup(t, "allow_once")
	del := term.NewClaudeManagedApprovalDelivery(svc)
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

	// Valid PreToolUse → allow.
	r1 := fireResumeHook(t, resumeURL, csid, tuid, tn, inputJSON)
	if !bytesEq(r1, term.ClaudeHookResponseBytes("allow")) {
		t.Fatalf("want allow, got %s", r1)
	}

	// PostToolUse with REAL body shape (tool_response + duration_ms present,
	// prompt_id absent). This must be accepted and witness the claim.
	postBody := `{"session_id":"` + csid + `","tool_use_id":"` + tuid + `","tool_name":"` + tn + `","tool_input":` + inputJSON + `,"hook_event_name":"PostToolUse","tool_response":"pokitclaudeapprovalprobe","duration_ms":350,"cwd":"/tmp","transcript_path":"","permission_mode":"default","effort":"medium"}`
	r2 := fireRawResumeHook(t, posttoolURL, postBody)
	if !bytesEq(r2, []byte{}) {
		t.Fatalf("PostToolUse with own fields: want empty 200 body, got %s", r2)
	}
	<-done
	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("delivery must be accepted, got %s", receipt.Outcome)
	}
}

// TestR5_PostToolUse_RejectsUnknownField: PostToolUse body with an unknown
// field must not witness (strict decode rejects → 200 no-op).
func TestR5_PostToolUse_RejectsUnknownField(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := catalogMakeSetup(t, "allow_once")
	del := term.NewClaudeManagedApprovalDelivery(svc)
	del.SetPollTimeout(2 * time.Second)

	done := make(chan struct{})
	go func() {
		defer close(done)
		del.Deliver(term.ApprovalDeliveryRequest{
			ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload,
		})
	}()

	resumeURL, posttoolURL := captureBridgeURLs(t, l)
	inputJSON := `{"command":"echo pokitclaudeapprovalprobe"}`

	// Valid PreToolUse → allow.
	fireResumeHook(t, resumeURL, csid, tuid, tn, inputJSON)

	// PostToolUse with UNKNOWN field → strict decode rejects → no witness.
	postBody := `{"session_id":"` + csid + `","tool_use_id":"` + tuid + `","tool_name":"` + tn + `","tool_input":` + inputJSON + `,"hook_event_name":"PostToolUse","unknown_field":"intruder"}`
	fireRawResumeHook(t, posttoolURL, postBody)
	<-done

	// Delivery must timeout/fail — witness was never committed.
	if svc.Coordinator().EntryCount() != 0 {
		t.Fatal("unknown field must not create a coordinator entry")
	}
}

// TestR5_PostToolUse_WrongEventType: PostToolUse endpoint with
// hook_event_name other than "PostToolUse" → handler returns 200 no-op
// (no witness).
func TestR5_PostToolUse_WrongEventType(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := catalogMakeSetup(t, "allow_once")
	del := term.NewClaudeManagedApprovalDelivery(svc)
	del.SetPollTimeout(2 * time.Second)

	done := make(chan struct{})
	go func() {
		defer close(done)
		del.Deliver(term.ApprovalDeliveryRequest{
			ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload,
		})
	}()

	resumeURL, posttoolURL := captureBridgeURLs(t, l)
	inputJSON := `{"command":"echo pokitclaudeapprovalprobe"}`

	// Valid PreToolUse → allow.
	fireResumeHook(t, resumeURL, csid, tuid, tn, inputJSON)

	// Post w/ wrong event type → 200 no-op.
	postBody := `{"session_id":"` + csid + `","tool_use_id":"` + tuid + `","tool_name":"` + tn + `","tool_input":` + inputJSON + `,"hook_event_name":"PreToolUse"}`
	fireRawResumeHook(t, posttoolURL, postBody)
	<-done

	if svc.Coordinator().EntryCount() != 0 {
		t.Fatal("wrong event type must not create a coordinator entry")
	}
}
