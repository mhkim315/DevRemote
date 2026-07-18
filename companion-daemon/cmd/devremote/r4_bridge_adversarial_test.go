package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/term"
)

// ── R4 bridge-level adversarial tests ──

// TestR4Bridge_DuplicateResumeHook_WriteOnce sends the identical resume hook
// twice through the real bridge HTTP endpoint. First returns allow (decision
// written exactly once); second returns defer (duplicate, no re-write).
func TestR4Bridge_DuplicateResumeHook_WriteOnce(t *testing.T) {
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

	r1 := fireResumeHook(t, resumeURL, csid, tuid, tn, inputJSON)
	if !bytesEq(r1, term.ClaudeHookResponseBytes("allow")) {
		t.Fatalf("first hook: want allow, got %s", r1)
	}
	r2 := fireResumeHook(t, resumeURL, csid, tuid, tn, inputJSON)
	if !bytesEq(r2, term.ClaudeHookResponseBytes("defer")) {
		t.Fatalf("second hook (duplicate): want defer, got %s", r2)
	}

	firePostToolHook(t, posttoolURL, csid, tuid, tn, inputJSON)
	<-done
	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("delivery must be accepted, got %s", receipt.Outcome)
	}
}

// TestR4Bridge_MissingToolInput_Defer sends a resume hook with tool_input
// omitted, verifies defer, then sends a VALID hook on the SAME claim to
// prove the invalid hook did NOT bind the attempt identity. The valid hook
// succeeding is non-vacuous proof that the claim window is still open.
func TestR4Bridge_MissingToolInput_Defer(t *testing.T) {
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

	// Step 1: invalid hook (missing tool_input) → defer.
	invalidBody := `{"session_id":"` + csid + `","tool_use_id":"` + tuid + `","tool_name":"` + tn + `","hook_event_name":"PreToolUse"}`
	r1 := fireRawResumeHook(t, resumeURL, invalidBody)
	if !bytesEq(r1, term.ClaudeHookResponseBytes("defer")) {
		t.Fatalf("missing tool_input: want defer, got %s", r1)
	}

	// Step 2: SAME claim, VALID hook → allow. Non-vacuous: proves the
	// invalid hook did not consume or bind the ResumeAttemptIdentity.
	inputJSON := `{"command":"echo pokitclaudeapprovalprobe"}`
	r2 := fireResumeHook(t, resumeURL, csid, tuid, tn, inputJSON)
	if !bytesEq(r2, term.ClaudeHookResponseBytes("allow")) {
		t.Fatalf("valid hook after invalid: want allow, got %s", r2)
	}

	// Step 3: PostToolUse witness → delivery accepted.
	firePostToolHook(t, posttoolURL, csid, tuid, tn, inputJSON)
	<-done
	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("delivery must be accepted after valid hook, got %s", receipt.Outcome)
	}
}

func fireRawResumeHook(t *testing.T, url, body string) []byte {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("raw resume hook: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return b
}

// TestR4Bridge_WrongToolName_Defer sends a resume hook with a different
// tool_name, verifies defer, then sends a VALID hook on the SAME claim.
// The valid hook succeeding proves the wrong-tool hook did not bind.
func TestR4Bridge_WrongToolName_Defer(t *testing.T) {
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

	// Step 1: invalid hook (wrong tool_name) → defer.
	r1 := fireResumeHook(t, resumeURL, csid, tuid, "Read", inputJSON)
	if !bytesEq(r1, term.ClaudeHookResponseBytes("defer")) {
		t.Fatalf("wrong tool name: want defer, got %s", r1)
	}

	// Step 2: SAME claim, VALID hook (correct tool_name "Bash") → allow.
	r2 := fireResumeHook(t, resumeURL, csid, tuid, tn, inputJSON)
	if !bytesEq(r2, term.ClaudeHookResponseBytes("allow")) {
		t.Fatalf("valid hook after wrong-tool: want allow, got %s", r2)
	}

	// Step 3: PostToolUse witness → delivery accepted.
	firePostToolHook(t, posttoolURL, csid, tuid, tn, inputJSON)
	<-done
	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("delivery must be accepted after valid hook, got %s", receipt.Outcome)
	}
}
