package term

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
)

func protocolRequest(payload []byte) []byte {
	return controlRequest("controlled_pty:input-b-ack", 7, testInputID(), payload)
}

func TestInputB_ExactRawAndDecodedBounds(t *testing.T) {
	raw := protocolRequest([]byte("x"))
	raw = append(raw, []byte(strings.Repeat(" ", inputMaxRawFrame-len(raw)))...)
	if got, decoded, err := parseInputControlRequest(raw); err != nil || got == nil || string(decoded) != "x" {
		t.Fatalf("exact raw bound: req=%+v decoded=%q err=%v", got, decoded, err)
	}
	if _, _, err := parseInputControlRequest(append(raw, ' ')); err == nil || err.Error() != "input_too_large" {
		t.Fatalf("raw bound+1 error=%v, want input_too_large", err)
	}
	if _, decoded, err := parseInputControlRequest(protocolRequest(make([]byte, inputMaxDecoded))); err != nil || len(decoded) != inputMaxDecoded {
		t.Fatalf("exact decoded bound len=%d err=%v", len(decoded), err)
	}
	if _, _, err := parseInputControlRequest(protocolRequest(make([]byte, inputMaxDecoded+1))); err == nil || err.Error() != "input_too_large" {
		t.Fatalf("decoded bound+1 error=%v, want input_too_large", err)
	}
}

func TestInputB_StrictParserRejectsMalformedUnknownAndTrailing(t *testing.T) {
	valid := string(protocolRequest([]byte("x")))
	for _, raw := range []string{
		`{"type":"terminal_input","version":1`,
		strings.Replace(valid, `"payload":`, `"unknown":true,"payload":`, 1),
		valid + ` {}`,
		strings.Replace(valid, testInputID(), strings.Repeat("A", inputIDLen), 1),
	} {
		if _, _, err := parseInputControlRequest([]byte(raw)); err == nil || err.Error() != "invalid_request" {
			t.Fatalf("raw=%q err=%v, want invalid_request", raw, err)
		}
	}
}

func decodeInputOutcome(t *testing.T, raw []byte) inputResult {
	t.Helper()
	var result inputResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestInputB_ClosedOutcomesAndPermissionLimiter(t *testing.T) {
	writer := &inputBWriter{}
	transport := newTerminalTransport("controlled_pty:input-b-ack", 7, writer, nil, nil)
	cache := newInputRecentCache()
	seq := uint64(0)
	msg := protocolRequest([]byte("x"))
	for name, tc := range map[string]struct {
		session      string
		gen          int64
		transportArg *TerminalTransport
		principal    interface{}
		want         string
	}{
		"session_not_found": {session: "controlled_pty:other", gen: 7, transportArg: transport, want: "session_not_found"},
		"stale_generation":  {session: "controlled_pty:input-b-ack", gen: 8, transportArg: transport, want: "stale_generation"},
		"transport_closed":  {session: "controlled_pty:input-b-ack", gen: 7, transportArg: nil, want: "transport_closed"},
	} {
		t.Run(name, func(t *testing.T) {
			result := decodeInputOutcome(t, handleTerminalInput(msg, tc.session, tc.gen, tc.transportArg, nil, &seq, tc.principal, newInputRecentCache(), "conn", newInputPermissionLimiter(time.Now())))
			if result.Outcome != tc.want {
				t.Fatalf("outcome=%q want %q", result.Outcome, tc.want)
			}
		})
	}
	denied := &devicetrust.Principal{Permissions: []string{devicetrust.PermSessionsRead}}
	limiter := newInputPermissionLimiter(time.Now())
	for i := 0; i < 3; i++ {
		result := handleTerminalInput(msg, "controlled_pty:input-b-ack", 7, transport, nil, &seq, denied, cache, "conn", limiter)
		if result == nil || decodeInputOutcome(t, result).Outcome != "permission_denied" {
			t.Fatalf("denial %d = %s", i, result)
		}
	}
	if result := handleTerminalInput(msg, "controlled_pty:input-b-ack", 7, transport, nil, &seq, denied, cache, "conn", limiter); result != nil {
		t.Fatalf("fourth denial bypassed burst limiter: %s", result)
	}
	if !limiter.allow(time.Now().Add(time.Minute)) {
		t.Fatal("limiter did not refill at ten receipts per minute")
	}
}

func TestInputB_CacheCapacityNeverEvictsAcceptedReplay(t *testing.T) {
	cache := newInputRecentCache()
	var digest [32]byte
	for i := 0; i < inputCacheSize; i++ {
		id := strings.Repeat("0", inputIDLen-2) + "ab"
		id = id[:inputIDLen-2] + string("0123456789abcdef"[i/16]) + string("0123456789abcdef"[i%16])
		if !cache.canStore(id) {
			t.Fatalf("cache rejected entry %d before capacity", i)
		}
		cache.store(id, digest, "accepted", uint64(i+1))
	}
	if cache.canStore(strings.Repeat("f", inputIDLen)) {
		t.Fatal("cache accepted a 65th ID and would evict a replay guard")
	}
	first := strings.Repeat("0", inputIDLen-2) + "00"
	if outcome, seq, ok := cache.get(first, digest); !ok || outcome != "accepted" || seq != 1 {
		t.Fatalf("first accepted entry was evicted: %q %d %v", outcome, seq, ok)
	}
	cache.entries[first].expires = time.Now().Add(-time.Second)
	if !cache.canStore(strings.Repeat("f", inputIDLen)) {
		t.Fatal("expired cache entry did not free TTL capacity")
	}
}

func TestInputB_ReplacedCapturedTransportCannotWriteNewGeneration(t *testing.T) {
	oldWriter := &inputBWriter{}
	captured := newTerminalTransport("controlled_pty:input-b-ack", 7, oldWriter, nil, nil)
	// Model same-ID replacement: the old captured handle is retired and a
	// distinct generation exists elsewhere. handleTerminalInput receives only
	// the captured handle, never a session lookup that could select replacement.
	captured.Retire()
	_ = newTerminalTransport("controlled_pty:input-b-ack", 8, &inputBWriter{}, nil, nil)
	seq := uint64(0)
	result := decodeInputOutcome(t, handleTerminalInput(protocolRequest([]byte("old connection")), "controlled_pty:input-b-ack", 7, captured, nil, &seq, nil, newInputRecentCache(), "conn", newInputPermissionLimiter(time.Now())))
	if result.Outcome != "write_failed" {
		t.Fatalf("retired captured transport outcome=%q, want write_failed", result.Outcome)
	}
	if len(oldWriter.wrote) != 0 {
		t.Fatalf("retired generation wrote %q", oldWriter.wrote)
	}
}
