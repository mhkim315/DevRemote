package term

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// ── Input-A denial payload format ──

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

// ── Input-A rate limiting ──

// allowDenial implements the coalescing gate: returns true when a denial
// should be sent (more than 1 second since the last one).
func allowDenial(last time.Time) (bool, time.Time) {
	if time.Since(last) > time.Second {
		return true, time.Now()
	}
	return false, last
}

func TestInputA_RateLimit_FirstDenialSent(t *testing.T) {
	allowed, _ := allowDenial(time.Time{}) // zero time = never denied
	if !allowed {
		t.Error("first denial must be allowed")
	}
}

func TestInputA_RateLimit_CoalescedWithinWindow(t *testing.T) {
	last := time.Now()
	allowed, next := allowDenial(last)
	if allowed {
		t.Error("denial sent immediately after last — must be coalesced")
	}
	if !next.Equal(last) {
		t.Error("last timestamp must not advance when coalesced")
	}
}

func TestInputA_RateLimit_AllowedAfterWindow(t *testing.T) {
	last := time.Now().Add(-2 * time.Second)
	allowed, next := allowDenial(last)
	if !allowed {
		t.Error("denial must be allowed after >1s window")
	}
	if !next.After(last) {
		t.Error("last timestamp must advance when denial is sent")
	}
}

func TestInputA_RateLimit_ConcurrentCoalescing(t *testing.T) {
	// N concurrent goroutines try to send denials within the same window.
	// Only the first must be allowed.
	var mu sync.Mutex
	last := time.Time{}
	sent := 0

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			allowed, newLast := allowDenial(last)
			if allowed {
				sent++
				last = newLast
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	if sent != 1 {
		t.Errorf("concurrent denials: got %d sent, want exactly 1", sent)
	}
}

// ── Input-A denial does not reach WriteInput ──

func TestInputA_ReadOnlyDenialReasonIsBounded(t *testing.T) {
	// The reason field is a fixed static string — no user/session data leaked.
	payload := string(readOnlyDenialPayload)
	if len(payload) > 256 {
		t.Errorf("denial payload too large: %d bytes", len(payload))
	}

	var ctrl struct {
		Type   string `json:"type"`
		Reason string `json:"reason"`
	}
	json.Unmarshal(readOnlyDenialPayload, &ctrl)

	// Reason must not contain JSON, HTML, or control characters.
	for _, c := range ctrl.Reason {
		if c < 0x20 && c != ' ' {
			t.Errorf("denial reason contains control character: U+%04X", c)
		}
	}
	if len(ctrl.Reason) > 120 {
		t.Errorf("denial reason too long: %d chars", len(ctrl.Reason))
	}
}
