package term

import (
	"testing"
)

// emptyPayloadDigest is the digest of a zero-length payload (nil or []byte{}).
var emptyPayloadDigest = func() string { return payloadDigest(nil) }()

// allowOnceActionDigest is the digest of the fixed "allow_once" action with no
// input — the exact input the test writes so the gate's metadata validator
// accepts it. Must match validOpName in the gate validator.
var allowOnceActionDigest = func() string {
	return CanonicalAction{OptionID: "allow_once", Kind: "allow_once", SchemaVersion: "a1.1"}.Digest()
}()

// TestRuntimeDeliveryGate_StaleGenerationRefused proves that a write claim with
// a stale (mismatched) RuntimeRef is refused by the gate.
func TestRuntimeDeliveryGate_StaleGenerationRefused(t *testing.T) {
	gate := NewRuntimeDeliveryGate()

	ref1 := RuntimeRef{Adapter: "codex_app_server", Version: "codex-cli-0.144.1", LaunchGen: 1}
	ref2 := RuntimeRef{Adapter: "codex_app_server", Version: "codex-cli-0.144.1", LaunchGen: 2}

	handle1, ok := gate.Activate("codex:devremote-session", ref1, 1)
	if !ok {
		t.Fatal("first activation failed")
	}
	if handle1 == "" {
		t.Fatal("handle empty on activation")
	}

	// Deliver with matching generation 1 — must succeed.
	req1 := ApprovalDeliveryRequest{
		Binding: ApprovalExecutionBinding{
			ApprovalID: "approval1", SessionID: "codex:devremote-session",
			Runtime: ref1, ActionDigest: allowOnceActionDigest,
			PayloadDigest: emptyPayloadDigest, IdempotencyKey: "ik-1",
		},
		ClaimToken: "claimtok1",
	}
	receipt1, _, ok1 := gate.Accept(req1)
	if !ok1 || receipt1.Outcome != DeliveryAccepted {
		t.Fatalf("first accept failed: ok=%v outcome=%s", ok1, receipt1.Outcome)
	}

	// Replace with generation 2.
	handle2, ok := gate.Activate("codex:devremote-session", ref2, 1)
	if !ok {
		t.Fatal("replacement activation failed")
	}
	if handle2 == "" || handle2 == handle1 {
		t.Fatalf("replacement handle=%v, want non-empty and different from %v", handle2, handle1)
	}

	// Stale generation 1 — MUST be refused.
	reqStale := ApprovalDeliveryRequest{
		Binding: ApprovalExecutionBinding{
			ApprovalID: "approval2", SessionID: "codex:devremote-session",
			Runtime: ref1, ActionDigest: allowOnceActionDigest,
			PayloadDigest: emptyPayloadDigest, IdempotencyKey: "ik-2",
		},
		ClaimToken: "claimtok2",
	}
	_, _, ok = gate.Accept(reqStale)
	if ok {
		t.Fatal("STALE-GENERATION NOT REFUSED: accept with old LaunchGen succeeded")
	}
	t.Log("STALE_GENERATION_REFUSED ok")

	// Known-bad control: matching generation 2 still works.
	reqCurrent := ApprovalDeliveryRequest{
		Binding: ApprovalExecutionBinding{
			ApprovalID: "approval3", SessionID: "codex:devremote-session",
			Runtime: ref2, ActionDigest: allowOnceActionDigest,
			PayloadDigest: emptyPayloadDigest, IdempotencyKey: "ik-3",
		},
		ClaimToken: "claimtok3",
	}
	receipt3, _, okCurrent := gate.Accept(reqCurrent)
	if !okCurrent || receipt3.Outcome != DeliveryAccepted {
		t.Fatalf("current-gen accept failed: ok=%v outcome=%s", okCurrent, receipt3.Outcome)
	}
}

// TestRuntimeDeliveryGate_ReplaceMidFlight_StaleRejected exercises the
// deterministic race: a blocked Accept races with a replacement and loses.
func TestRuntimeDeliveryGate_ReplaceMidFlight_StaleRejected(t *testing.T) {
	gate := NewRuntimeDeliveryGate()

	ref1 := RuntimeRef{Adapter: "codex_app_server", Version: "codex-cli-0.144.1", LaunchGen: 1}
	ref2 := RuntimeRef{Adapter: "codex_app_server", Version: "codex-cli-0.144.1", LaunchGen: 2}

	_, ok := gate.Activate("codex:devremote-session", ref1, 1)
	if !ok {
		t.Fatal("first activation failed")
	}

	ready := make(chan struct{})
	goDone := make(chan struct{})
	var acceptOK bool

	gate.acceptEntryHook = func() {
		close(ready)
		<-goDone
	}

	go func() {
		defer close(goDone)
		req := ApprovalDeliveryRequest{
			Binding: ApprovalExecutionBinding{
				ApprovalID: "approvalA", SessionID: "codex:devremote-session",
				Runtime: ref1, ActionDigest: allowOnceActionDigest,
				PayloadDigest: emptyPayloadDigest, IdempotencyKey: "ik-A",
			},
			ClaimToken: "claimtokA",
		}
		_, _, ok := gate.Accept(req)
		acceptOK = ok
	}()

	<-ready
	_, ok = gate.Activate("codex:devremote-session", ref2, 1)
	if !ok {
		t.Fatal("replacement activation failed")
	}
	close(goDone)
	<-goDone
	gate.acceptEntryHook = nil

	if acceptOK {
		t.Fatal("REPLACE-MID-FLIGHT NOT REJECTED: stale-gen Accept succeeded")
	}
	t.Log("REPLACE_MID_FLIGHT_STALE_REJECTED ok")
}
