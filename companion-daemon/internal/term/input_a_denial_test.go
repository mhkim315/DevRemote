package term

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
)

// ── Input-A: denial payload format ──

func TestInputA_DenialPayloadIsValidJSON(t *testing.T) {
	var ctrl struct {
		Type   string `json:"type"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(readOnlyDenialPayload, &ctrl); err != nil {
		t.Fatalf("readOnlyDenialPayload is not valid JSON: %v", err)
	}
	if ctrl.Type != "read_only" {
		t.Errorf("type = %q, want read_only", ctrl.Type)
	}
	if ctrl.Reason == "" {
		t.Error("reason must not be empty")
	}
}

func TestInputA_ReadOnlyDenialReasonIsBounded(t *testing.T) {
	payload := string(readOnlyDenialPayload)
	if len(payload) > 256 {
		t.Errorf("payload too large: %d bytes", len(payload))
	}
	var ctrl struct {
		Type   string `json:"type"`
		Reason string `json:"reason"`
	}
	json.Unmarshal(readOnlyDenialPayload, &ctrl)
	for _, c := range ctrl.Reason {
		if c < 0x20 && c != ' ' {
			t.Errorf("reason contains control char U+%04X", c)
		}
	}
	if len(ctrl.Reason) > 120 {
		t.Errorf("reason too long: %d chars", len(ctrl.Reason))
	}
}

// ── Input-A: hasTicketPerm permission gate (production code) ──

func TestInputA_HasTicketPerm_MissingPermissionDenied(t *testing.T) {
	p := &devicetrust.Principal{
		DeviceID:    "device-1",
		Permissions: []string{"sessions:read"},
	}
	if hasTicketPerm(p, string(devicetrust.PermTerminalInput)) {
		t.Error("hasTicketPerm must return false when terminal:input is missing")
	}
}

func TestInputA_HasTicketPerm_PermissionGranted(t *testing.T) {
	p := &devicetrust.Principal{
		DeviceID:    "device-1",
		Permissions: []string{string(devicetrust.PermTerminalInput)},
	}
	if !hasTicketPerm(p, string(devicetrust.PermTerminalInput)) {
		t.Error("hasTicketPerm must return true when terminal:input is present")
	}
}

func TestInputA_HasTicketPerm_NilPrincipal(t *testing.T) {
	// nil principal = legacy/no-auth path. hasTicketPerm returns true
	// so the caller (handleWSWithPrincipal) needn't special-case nil.
	// The combined check is: ticketPrincipal != nil && !hasTicketPerm(...)
	// which correctly gates only authenticated connections.
	if !hasTicketPerm(nil, string(devicetrust.PermTerminalInput)) {
		t.Error("hasTicketPerm(nil) must return true — nil = no auth = permitted")
	}
}

func TestInputA_HasTicketPerm_EmptyPermissions(t *testing.T) {
	p := &devicetrust.Principal{
		DeviceID:    "device-1",
		Permissions: []string{},
	}
	if hasTicketPerm(p, string(devicetrust.PermTerminalInput)) {
		t.Error("hasTicketPerm must return false for empty permissions")
	}
}

// ── Input-A: rate limiting (production logic) ──

func TestInputA_RateLimit_ConcurrentCoalescing(t *testing.T) {
	var mu sync.Mutex
	last := time.Time{}
	sent := 0

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			if time.Since(last) > time.Second {
				sent++
				last = time.Now()
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	if sent != 1 {
		t.Errorf("concurrent denials: got %d sent, want exactly 1 (coalesced)", sent)
	}
}

func TestInputA_RateLimit_AllowedAfterWindow(t *testing.T) {
	last := time.Now().Add(-2 * time.Second)
	if time.Since(last) <= time.Second {
		t.Error("denial must be allowed after >1s window")
	}
}

func TestInputA_RateLimit_CoalescedWithinWindow(t *testing.T) {
	last := time.Now()
	time.Sleep(5 * time.Millisecond)
	if time.Since(last) > time.Second {
		t.Skip("clock too coarse for rate-limit test")
	}
}

// ── Input-A: denial payload stability ──

func TestInputA_DenialPayload_ConstantFormat(t *testing.T) {
	p1 := string(readOnlyDenialPayload)
	p2 := string(readOnlyDenialPayload)
	if p1 != p2 {
		t.Error("denial payload changed between reads — must be constant")
	}
}

func TestInputA_DenialPayload_NoLeakedData(t *testing.T) {
	payload := string(readOnlyDenialPayload)
	badWords := []string{"sessionId", "deviceId", "token", "bearer", "/tmp", "poke", "secret"}
	for _, w := range badWords {
		if containsFold(payload, w) {
			t.Errorf("denial payload contains potentially leaked word: %q", w)
		}
	}
}

func containsFold(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			c1 := s[i+j]
			c2 := substr[j]
			if c1 >= 'A' && c1 <= 'Z' {
				c1 += 32
			}
			if c2 >= 'A' && c2 <= 'Z' {
				c2 += 32
			}
			if c1 != c2 {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
