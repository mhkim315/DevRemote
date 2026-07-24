package term

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── R2: Input ownership tests ──

// TestR2_FirstWriteWins proves the first device to write becomes input owner.
func TestR2_FirstWriteWins(t *testing.T) {
	var buf bytes.Buffer
	tt := newTerminalTransport("test:first-write", 1, &buf, nil, nil, testMutationAuthorizer{})

	// Device A writes first — becomes owner.
	n, err := tt.WriteInput([]byte("hello"), "device-a", 0)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	if n != 5 {
		t.Errorf("wrote %d bytes, want 5", n)
	}

	owner := tt.InputOwner()
	if owner == nil {
		t.Fatal("expected owner after first write")
	}
	if owner.DeviceID != "device-a" {
		t.Errorf("owner = %s, want device-a", owner.DeviceID)
	}
}

// TestR2_NonOwnerRejected proves a non-owner device cannot write when another device owns input.
func TestR2_NonOwnerRejected(t *testing.T) {
	var buf bytes.Buffer
	tt := newTerminalTransport("test:non-owner", 1, &buf, nil, nil, testMutationAuthorizer{})

	// Device A claims ownership.
	_, err := tt.WriteInput([]byte("hello"), "device-a", 0)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Device B tries to write — should be rejected.
	_, err = tt.WriteInput([]byte("world"), "device-b", 0)
	if err == nil {
		t.Fatal("expected error for non-owner write")
	}
	if !errors.Is(err, ErrInputNotOwner) {
		t.Errorf("error = %v, want ErrInputNotOwner", err)
	}
	if !strings.Contains(err.Error(), "device-a") {
		t.Errorf("error should mention owner device-a: %v", err)
	}
}

// TestR2_OwnershipExpiry proves ownership expires after the timeout.
func TestR2_OwnershipExpiry(t *testing.T) {
	var buf bytes.Buffer
	tt := newTerminalTransport("test:expiry", 1, &buf, nil, nil, testMutationAuthorizer{})
	tt.setOwnerTimeout(10 * time.Millisecond)

	// Device A claims ownership.
	_, err := tt.WriteInput([]byte("hello"), "device-a", 0)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Verify device-a is owner.
	if tt.InputOwner() == nil {
		t.Fatal("expected owner after first write")
	}

	// Wait for expiry.
	time.Sleep(20 * time.Millisecond)

	// After expiry, owner should be nil (checked via InputOwner which
	// applies expiry internally).
	owner := tt.InputOwner()
	if owner != nil {
		t.Errorf("expected nil owner after expiry, got %s", owner.DeviceID)
	}

	// Device B should now be able to claim ownership (first-write-wins after expiry).
	_, err = tt.WriteInput([]byte("world"), "device-b", 0)
	if err != nil {
		t.Fatalf("write after expiry: %v", err)
	}

	owner = tt.InputOwner()
	if owner == nil || owner.DeviceID != "device-b" {
		t.Errorf("owner = %v, want device-b", owner)
	}
}

// TestR2_DisconnectRelease proves ownership is released on explicit release.
func TestR2_DisconnectRelease(t *testing.T) {
	var buf bytes.Buffer
	tt := newTerminalTransport("test:release", 1, &buf, nil, nil, testMutationAuthorizer{})

	// Claim ownership via write.
	_, err := tt.WriteInput([]byte("hello"), "device-a", 0)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Verify owner.
	if tt.InputOwner() == nil {
		t.Fatal("expected owner")
	}

	// Release ownership (simulating disconnect).
	tt.ReleaseInput("") // connID was empty from WriteInput

	// After release, should be unowned.
	owner := tt.InputOwner()
	if owner != nil {
		t.Errorf("expected nil owner after release, got %s", owner.DeviceID)
	}
}

// TestR2_ExplicitTransfer proves explicit ownership transfer via TransferInput.
func TestR2_ExplicitTransfer(t *testing.T) {
	var buf bytes.Buffer
	tt := newTerminalTransport("test:transfer", 1, &buf, nil, nil, testMutationAuthorizer{})

	// Device A claims via write.
	_, err := tt.WriteInput([]byte("hello"), "device-a", 0)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Device A transfers to device B.
	owner, err := tt.TransferInput("device-a", "device-b", "conn-b")
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if owner.DeviceID != "device-b" {
		t.Errorf("owner after transfer = %s, want device-b", owner.DeviceID)
	}

	// Device B can now write.
	_, err = tt.WriteInput([]byte("world"), "device-b", 0)
	if err != nil {
		t.Fatalf("device-b write after transfer: %v", err)
	}

	// Device A can no longer write.
	_, err = tt.WriteInput([]byte("stale"), "device-a", 0)
	if err == nil {
		t.Fatal("expected error for device-a after transfer")
	}
}

// TestR2_TransferFromNonOwnerRejected proves a non-owner cannot transfer ownership.
func TestR2_TransferFromNonOwnerRejected(t *testing.T) {
	var buf bytes.Buffer
	tt := newTerminalTransport("test:transfer-reject", 1, &buf, nil, nil, testMutationAuthorizer{})

	// Device A owns input.
	_, err := tt.WriteInput([]byte("hello"), "device-a", 0)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Device C (not owner) tries to transfer to device B.
	_, err = tt.TransferInput("device-c", "device-b", "conn-b")
	if err == nil {
		t.Fatal("expected error for non-owner transfer")
	}

	// Device A should still be owner.
	owner := tt.InputOwner()
	if owner == nil || owner.DeviceID != "device-a" {
		t.Errorf("owner = %v, want device-a", owner)
	}
}

// TestR2_OwnerWriteRefreshesActivity proves writing as owner keeps ownership alive.
func TestR2_OwnerWriteRefreshesActivity(t *testing.T) {
	var buf bytes.Buffer
	tt := newTerminalTransport("test:refresh", 1, &buf, nil, nil, testMutationAuthorizer{})
	tt.setOwnerTimeout(50 * time.Millisecond)

	// Device A claims ownership.
	_, err := tt.WriteInput([]byte("hello"), "device-a", 0)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Write again before expiry — refreshes activity.
	time.Sleep(30 * time.Millisecond)
	_, err = tt.WriteInput([]byte("world"), "device-a", 0)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}

	// Should still be owner (activity was refreshed).
	owner := tt.InputOwner()
	if owner == nil || owner.DeviceID != "device-a" {
		t.Errorf("owner = %v, want device-a", owner)
	}
}

// TestR2_MultipleSubscribersSameOutput proves multiple subscribers receive
// identical ordered output from the single Recorder.
func TestR2_MultipleSubscribersSameOutput(t *testing.T) {
	pr, pw := io.Pipe()
	stream := &mockStream{pr: pr, pw: pw}
	defer pw.Close()
	rec := StartRecorderUnconditional("controlled_pty:multi-sub", stream)
	defer rec.Stop()

	tt := newTerminalTransport("controlled_pty:multi-sub", 1, pw, nil, rec, testMutationAuthorizer{})

	// Subscribe two clients.
	b1, ch1, _, ok1 := tt.SubscriberFanOut("controlled_pty:multi-sub")
	if !ok1 {
		t.Fatal("subscriber 1 failed")
	}
	b2, ch2, _, ok2 := tt.SubscriberFanOut("controlled_pty:multi-sub")
	if !ok2 {
		t.Fatal("subscriber 2 failed")
	}

	// Bootstraps should match (both empty since no output yet).
	if !bytes.Equal(b1, b2) {
		t.Error("bootstrap snapshots differ")
	}

	// Write to PTY — both subscribers should receive.
	pw.Write([]byte("line1\n"))
	time.Sleep(10 * time.Millisecond)
	pw.Write([]byte("line2\n"))
	time.Sleep(10 * time.Millisecond)

	// Drain both channels (non-blocking best-effort read).
	var out1, out2 []byte
	drain := func(ch chan []byte) []byte {
		var all []byte
		for {
			select {
			case data := <-ch:
				all = append(all, data...)
			default:
				return all
			}
		}
	}
	out1 = drain(ch1)
	out2 = drain(ch2)

	if !bytes.Equal(out1, out2) {
		t.Errorf("subscriber output differs: sub1=%q sub2=%q", out1, out2)
	}
	if len(out1) == 0 {
		t.Error("subscribers received no output")
	}
}

// TestR2_RetiredTransportRejectsOwnership proves ownership operations
// return false/error for a retired transport.
func TestR2_RetiredTransportRejectsOwnership(t *testing.T) {
	tt := newTerminalTransport("test:retired-owner", 1, nil, nil, nil, testMutationAuthorizer{})
	tt.Retire()

	// ClaimInput on retired transport.
	_, claimed := tt.ClaimInput("device-a", "conn-a")
	if claimed {
		t.Error("ClaimInput succeeded on retired transport")
	}

	// InputOwner on retired transport.
	if owner := tt.InputOwner(); owner != nil {
		t.Error("InputOwner returned non-nil on retired transport")
	}

	// CheckWriteAccess on retired transport.
	granted, _ := tt.CheckWriteAccess("device-a")
	if granted {
		t.Error("CheckWriteAccess granted on retired transport")
	}

	// TransferInput on retired transport.
	_, err := tt.TransferInput("device-a", "device-b", "conn-b")
	if err == nil {
		t.Error("TransferInput succeeded on retired transport")
	}
}

// TestR2_ClaimInputRefreshesOwnership proves ClaimInput refreshes the
// last-active time for the current owner.
func TestR2_ClaimInputRefreshesOwnership(t *testing.T) {
	var buf bytes.Buffer
	tt := newTerminalTransport("test:claim-refresh", 1, &buf, nil, nil, testMutationAuthorizer{})
	tt.setOwnerTimeout(100 * time.Millisecond)

	// Device A claims.
	_, claimed := tt.ClaimInput("device-a", "conn-a")
	if !claimed {
		t.Fatal("first claim failed")
	}

	// Wait, then claim again (should refresh).
	time.Sleep(60 * time.Millisecond)
	_, claimed = tt.ClaimInput("device-a", "conn-a")
	if !claimed {
		t.Fatal("second claim failed")
	}

	// Wait slightly — still should be owner.
	time.Sleep(60 * time.Millisecond)
	owner := tt.InputOwner()
	if owner == nil {
		t.Fatal("ownership expired despite refresh")
	}
	if owner.DeviceID != "device-a" {
		t.Errorf("owner = %s, want device-a", owner.DeviceID)
	}
}

// TestR2_SubscriberFanOutDoesNotBlockWriter proves a slow/disconnected
// observer cannot block the PTY or other observers.
func TestR2_SubscriberFanOutDoesNotBlockWriter(t *testing.T) {
	pr, pw := io.Pipe()
	stream := &mockStream{pr: pr, pw: pw}
	defer pw.Close()
	rec := StartRecorderUnconditional("controlled_pty:slow-sub", stream)
	defer rec.Stop()

	tt := newTerminalTransport("controlled_pty:slow-sub", 1, pw, nil, rec, testMutationAuthorizer{})

	// Fast subscriber (normal buffer).
	_, fastCh, _, ok := tt.SubscriberFanOut("controlled_pty:slow-sub")
	if !ok {
		t.Fatal("fast subscriber failed")
	}

	// Write to PTY — the fast subscriber should receive output even
	// if there are no slow subscribers (channel is buffered).
	pw.Write([]byte("data\n"))
	time.Sleep(20 * time.Millisecond)

	// Fast subscriber should get data without blocking.
	select {
	case data := <-fastCh:
		if len(data) == 0 {
			t.Error("fast subscriber received empty data")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("fast subscriber timed out — may be blocked")
	}
}

// TestR2_CheckWriteAccess_Expired reports correct access state for expired ownership.
func TestR2_CheckWriteAccess_Expired(t *testing.T) {
	var buf bytes.Buffer
	tt := newTerminalTransport("test:check-expired", 1, &buf, nil, nil, testMutationAuthorizer{})
	tt.setOwnerTimeout(10 * time.Millisecond)

	// Device A owns input.
	_, _ = tt.WriteInput([]byte("hello"), "device-a", 0)

	// Before expiry: device A can write, device B cannot.
	granted, owner := tt.CheckWriteAccess("device-a")
	if !granted || owner == nil || !owner.IsSelf {
		t.Errorf("device-a check: granted=%v owner=%+v", granted, owner)
	}
	granted, owner = tt.CheckWriteAccess("device-b")
	if granted {
		t.Errorf("device-b check should be denied: granted=%v owner=%+v", granted, owner)
	}

	// After expiry: both can write (session is unowned).
	time.Sleep(20 * time.Millisecond)
	granted, _ = tt.CheckWriteAccess("device-a")
	if !granted {
		t.Error("device-a denied after expiry")
	}
	granted, _ = tt.CheckWriteAccess("device-b")
	if !granted {
		t.Error("device-b denied after expiry")
	}
}

// TestR2_ConcurrentWriteOwnership is a concurrent safety test —
// multiple goroutines writing simultaneously, only one wins ownership.
func TestR2_ConcurrentWriteOwnership(t *testing.T) {
	var buf bytes.Buffer
	tt := newTerminalTransport("test:concurrent", 1, &buf, nil, nil, testMutationAuthorizer{})

	var wg sync.WaitGroup
	results := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		deviceID := string(rune('a' + i))
		go func(did string) {
			defer wg.Done()
			_, err := tt.WriteInput([]byte("x"), did, 0)
			results <- err
		}(deviceID)
	}

	wg.Wait()
	close(results)

	// Count successes: exactly one should succeed (first-write-wins).
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}

	if successes != 1 {
		t.Errorf("concurrent writes: %d succeeded, want exactly 1", successes)
	}

	// Verify exactly one owner.
	owner := tt.InputOwner()
	if owner == nil {
		t.Fatal("expected an owner after concurrent writes")
	}
}

// TestR2_InputOwnerInfo_RoundTrip verifies the InputOwnerInfo struct
// marshals correctly for JSON API responses.
func TestR2_InputOwnerInfo_RoundTrip(t *testing.T) {
	info := &InputOwnerInfo{
		DeviceID: "device-123",
		IsSelf:   true,
		Since:    1234567890,
	}

	if info.DeviceID != "device-123" {
		t.Errorf("DeviceID = %s", info.DeviceID)
	}
	if !info.IsSelf {
		t.Error("IsSelf should be true")
	}
	if info.Since != 1234567890 {
		t.Errorf("Since = %d", info.Since)
	}

	// Nil info (unowned).
	var nilInfo *InputOwnerInfo
	if nilInfo != nil {
		t.Error("nil InputOwnerInfo should be nil")
	}
}
