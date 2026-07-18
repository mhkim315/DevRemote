package main

import (
	"testing"
	"time"

	"devremote/companion-daemon/internal/term"
)

// TestR4Bridge_GracefulShutdown_PostToolUseCompletes proves bridge.close()
// uses graceful Shutdown (not hard Close). PostToolUse enters →
// MarkWitnessed publishes completion → delivery terminates → bridge.close
// gracefully drains → handler still completes HTTP 200 (no EOF).
//
// Before the fix, server.Close() killed the in-flight HTTP connection.
func TestR4Bridge_GracefulShutdown_PostToolUseCompletes(t *testing.T) {
	l, svc, _, claim, _, csid, tuid, tn := catalogMakeSetup(t, "allow_once")
	del := term.NewClaudeManagedApprovalDelivery(svc)
	del.SetPollTimeout(2 * time.Second)

	inputJSON := `{"command":"echo pokitclaudeapprovalprobe"}`

	// claimDone closes when delivery finishes (terminate → bridge.close).
	claimDone := make(chan struct{})
	// postToolDone closes when PostToolUse handler returns.
	postToolDone := make(chan struct{})

	var receipt term.DeliveryReceipt
	go func() {
		defer close(claimDone)
		receipt = del.Deliver(term.ApprovalDeliveryRequest{
			ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload,
		})
	}()

	// Delivery spawns the resume process; capture its bridge URLs.
	resumeURL, posttoolURL := captureBridgeURLs(t, l)

	r1 := fireResumeHook(t, resumeURL, csid, tuid, tn, inputJSON)
	if !bytesEq(r1, term.ClaudeHookResponseBytes("allow")) {
		t.Fatalf("want allow, got %s", r1)
	}

	// Fire PostToolUse from a goroutine — the delivery goroutine will
	// receive the witness and call rt.terminate → bridge.close DURING
	// the PostToolUse handler's HTTP response write.
	go func() {
		firePostToolHook(t, posttoolURL, csid, tuid, tn, inputJSON)
		close(postToolDone)
	}()

	<-claimDone    // delivery finished (bridge.close called during handler)
	<-postToolDone // PostToolUse still completed (graceful drain, no EOF)

	if receipt.Outcome != term.DeliveryAccepted {
		t.Fatalf("delivery must be accepted, got %s", receipt.Outcome)
	}
}
