package term

import (
	"bytes"
	"testing"
)

// ── PA4.3: Terminal transport generation-gated isolation ──

// TestPA4_3_WriteInputGatedByGeneration proves WriteInput succeeds when
// transport is active and silently drops when retired (stale generation).
func TestPA4_3_WriteInputGatedByGeneration(t *testing.T) {
	var buf bytes.Buffer
	tt := newTerminalTransport("test:gen-gated", 1, &buf, nil)

	n, err := tt.WriteInput([]byte("hello"))
	if err != nil {
		t.Fatalf("WriteInput: %v", err)
	}
	if n != 5 {
		t.Errorf("wrote %d bytes, want 5", n)
	}

	// RetireIfGeneration with matching gen retires the transport.
	tt.RetireIfGeneration(1)
	if !tt.IsRetired() {
		t.Fatal("transport not retired")
	}

	// Stale write after retirement silently drops (fail-closed).
	n, err = tt.WriteInput([]byte("stale"))
	if err != nil {
		t.Fatalf("WriteInput after retire: %v", err)
	}
	if n != 0 {
		t.Errorf("stale write wrote %d bytes, want 0", n)
	}
}

// TestPA4_3_RetireIfGenerationRejectsStaleGen proves a mismatched
// generation does NOT retire the transport.
func TestPA4_3_RetireIfGenerationRejectsStaleGen(t *testing.T) {
	var buf bytes.Buffer
	tt := newTerminalTransport("test:stale-retire", 2, &buf, nil)

	// Stale gen=1 does nothing.
	tt.RetireIfGeneration(1)
	if tt.IsRetired() {
		t.Fatal("transport retired by stale gen=1, want gen=2 required")
	}

	// Current gen=2 retires.
	tt.RetireIfGeneration(2)
	if !tt.IsRetired() {
		t.Fatal("transport not retired by current gen=2")
	}
}

// TestPA4_3_ResizeGatedByGeneration proves Resize is a no-op when
// transport is retired.
func TestPA4_3_ResizeGatedByGeneration(t *testing.T) {
	tt := newTerminalTransport("test:resize", 1, nil, nil)

	// Active transport: Resize returns nil (no-op without resizer).
	if err := tt.Resize(24, 80); err != nil {
		t.Fatalf("Resize active: %v", err)
	}

	// Retire and verify Resize still returns nil (no-op, not error).
	tt.RetireIfGeneration(1)
	if err := tt.Resize(24, 80); err != nil {
		t.Fatalf("Resize retired: %v", err)
	}
}

// TestPA4_3_RetireIsIdempotent proves multiple Retire calls are safe.
func TestPA4_3_RetireIsIdempotent(t *testing.T) {
	tt := newTerminalTransport("test:idempotent", 1, nil, nil)

	tt.RetireIfGeneration(1)
	tt.RetireIfGeneration(1) // second retire — no panic
	if !tt.IsRetired() {
		t.Fatal("transport not retired")
	}
}

// TestPA4_3_WriteInputFailClosed proves nil writer silently drops
// input (returns 0, nil — fail-closed, no panic).
func TestPA4_3_WriteInputFailClosed(t *testing.T) {
	tt := newTerminalTransport("test:nil-writer", 1, nil, nil)
	n, err := tt.WriteInput([]byte("x"))
	if err != nil {
		t.Fatalf("WriteInput: %v", err)
	}
	if n != 0 {
		t.Errorf("WriteInput with nil writer wrote %d bytes, want 0", n)
	}
}

// TestPA4_3_NewTerminalTransportAssignsGeneration proves the
// constructor stores the exact generation.
func TestPA4_3_NewTerminalTransportAssignsGeneration(t *testing.T) {
	tt := newTerminalTransport("test:gen-assign", 42, nil, nil)
	if tt.generation != 42 {
		t.Errorf("generation=%d, want 42", tt.generation)
	}
}

func TestPA4_3_SubscriberFanOutRetiredRejected(t *testing.T) {
	tt := newTerminalTransport("test:fanout-retired", 1, nil, nil)
	tt.RetireIfGeneration(1)
	_, _, ok := tt.SubscriberFanOut("test:fanout-retired")
	if ok {
		t.Error("SubscriberFanOut succeeded on retired transport")
	}
}

func TestPA4_3_SubscriberFanOutWrongSessionRejected(t *testing.T) {
	tt := newTerminalTransport("test:fanout-session", 1, nil, nil)
	_, _, ok := tt.SubscriberFanOut("wrong-session-id")
	if ok {
		t.Error("SubscriberFanOut succeeded with wrong sessionID")
	}
}
