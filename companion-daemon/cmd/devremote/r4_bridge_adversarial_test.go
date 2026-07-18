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
// omitted from the JSON body. Must defer; must not bind an attempt.
func TestR4Bridge_MissingToolInput_Defer(t *testing.T) {
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
	body := `{"session_id":"` + csid + `","tool_use_id":"` + tuid + `","tool_name":"` + tn + `","hook_event_name":"PreToolUse"}`
	r := fireRawResumeHook(t, resumeURL, body)
	if !bytesEq(r, term.ClaudeHookResponseBytes("defer")) {
		t.Fatalf("missing tool_input: want defer, got %s", r)
	}
	<-done
	if svc.Coordinator().EntryCount() != 0 {
		t.Fatal("missing tool_input must not create a coordinator entry")
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
// tool_name than the original. Must defer; must not bind an attempt.
func TestR4Bridge_WrongToolName_Defer(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, _ := catalogMakeSetup(t, "allow_once")
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
	r := fireResumeHook(t, resumeURL, csid, tuid, "Read", inputJSON)
	if !bytesEq(r, term.ClaudeHookResponseBytes("defer")) {
		t.Fatalf("wrong tool name: want defer, got %s", r)
	}
	<-done
	if svc.Coordinator().EntryCount() != 0 {
		t.Fatal("wrong tool name must not create a coordinator entry")
	}
}
