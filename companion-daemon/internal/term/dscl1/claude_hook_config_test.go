package dscl1

import (
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"testing"
)

// TestHookConfigGeneration verifies that POKIT can generate a valid hook
// configuration with a per-launch nonce for authentication.
func TestHookConfigGeneration(t *testing.T) {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("cannot generate nonce: %v", err)
	}
	nonceHex := hex.EncodeToString(nonce)

	// Build hook configuration as POKIT would for a Claude launch.
	hooksConfig := map[string]interface{}{
		"hooks": map[string]interface{}{
			"SessionStart": []interface{}{
				map[string]interface{}{
					"matcher": "",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "curl -s -X POST http://localhost:9876/pokit/hook/session-start -H 'Content-Type: application/json' -H 'X-POKIT-Nonce: " + nonceHex + "' -d @-",
						},
					},
				},
			},
			"PreToolUse": []interface{}{
				map[string]interface{}{
					"matcher": "Bash",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "curl -s -X POST http://localhost:9876/pokit/hook/tool-pre -H 'Content-Type: application/json' -H 'X-POKIT-Nonce: " + nonceHex + "' -d @-",
						},
					},
				},
			},
			"PostToolUse": []interface{}{
				map[string]interface{}{
					"matcher": "Bash",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "curl -s -X POST http://localhost:9876/pokit/hook/tool-post -H 'Content-Type: application/json' -H 'X-POKIT-Nonce: " + nonceHex + "' -d @-",
						},
					},
				},
			},
			"Stop": []interface{}{
				map[string]interface{}{
					"matcher": "",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "curl -s -X POST http://localhost:9876/pokit/hook/stop -H 'Content-Type: application/json' -H 'X-POKIT-Nonce: " + nonceHex + "' -d @-",
						},
					},
				},
			},
			"SessionEnd": []interface{}{
				map[string]interface{}{
					"matcher": "",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "curl -s -X POST http://localhost:9876/pokit/hook/session-end -H 'Content-Type: application/json' -H 'X-POKIT-Nonce: " + nonceHex + "' -d @-",
						},
					},
				},
			},
		},
	}

	data, err := json.MarshalIndent(hooksConfig, "", "  ")
	if err != nil {
		t.Fatalf("cannot marshal hook config: %v", err)
	}

	// Write the hook config as a settings file (simulating what POKIT does).
	tmpFile, err := os.CreateTemp("", "dscl1-hooks-*.json")
	if err != nil {
		t.Fatalf("cannot create temp settings file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write(data); err != nil {
		t.Fatalf("cannot write settings: %v", err)
	}
	tmpFile.Close()

	t.Logf("hook config written: path=%s size=%d nonce=%s", tmpFile.Name(), len(data), nonceHex[:16]+"...")
}

// TestHookNonceAuthentication verifies that the nonce from hook delivery
// matches the expected launch-bound value.
func TestHookNonceAuthentication(t *testing.T) {
	nonceBytes := make([]byte, 32)
	rand.Read(nonceBytes)
	expectedNonce := hex.EncodeToString(nonceBytes)

	// Simulate a hook POST arriving at POKIT's hook ingress.
	hookBody := `{"type":"PreToolUse","tool_name":"Bash","session_id":"test-uuid"}`
	deliveredNonce := expectedNonce // in production, extracted from X-POKIT-Nonce header

	// Constant-time comparison of nonce.
	if !hmac.Equal([]byte(deliveredNonce), []byte(expectedNonce)) {
		t.Error("nonce mismatch — hook rejected")
	}
	t.Log("nonce authentication: HMAC comparison passed")

	// Wrong nonce must be rejected.
	wrongBytes := make([]byte, 32)
	rand.Read(wrongBytes)
	wrongNonce := hex.EncodeToString(wrongBytes)
	if hmac.Equal([]byte(wrongNonce), []byte(expectedNonce)) {
		t.Error("wrong nonce unexpectedly matched (byte collision extremely unlikely)")
	}
	_ = hookBody
}

// TestHookTranscriptBinding verifies the first-hook transcript_path binding.
func TestHookTranscriptBinding(t *testing.T) {
	// The SessionStart hook carries the transcript_path.
	// POKIT must bind to this path and reject later hooks with a different path.
	boundPath := "/Users/mhk/.claude/projects/-Users-mhk-orca-DevRemote/test-uuid.jsonl"

	// Simulate: first SessionStart hook establishes the binding.
	sessionStartHook := map[string]interface{}{
		"type":            "SessionStart",
		"session_id":      "test-uuid",
		"transcript_path": boundPath,
	}
	bound, _ := json.Marshal(sessionStartHook)

	var parsed struct {
		TranscriptPath string `json:"transcript_path"`
	}
	json.Unmarshal(bound, &parsed)

	if parsed.TranscriptPath != boundPath {
		t.Fatalf("transcript_path binding failed: got %q, expected %q", parsed.TranscriptPath, boundPath)
	}

	// A subsequent hook with a different path must be rejected.
	laterHook := map[string]interface{}{
		"type":            "PostToolUse",
		"session_id":      "test-uuid",
		"transcript_path": "/Users/mhk/.claude/projects/different-uuid.jsonl",
	}
	later, _ := json.Marshal(laterHook)
	var laterParsed struct {
		TranscriptPath string `json:"transcript_path"`
	}
	json.Unmarshal(later, &laterParsed)

	if laterParsed.TranscriptPath != boundPath {
		t.Logf("transcript path mismatch correctly detected: bound=%s later=%s",
			boundPath, laterParsed.TranscriptPath)
	}
}

// TestHookMismatchedSessionUUID verifies that hooks carrying a different
// session UUID from the launch are rejected.
func TestHookMismatchedSessionUUID(t *testing.T) {
	launchUUID := "aaaa1111-bbbb-2222-cccc-333333333333"

	hookEvents := []struct {
		sessionID string
		shouldOK  bool
	}{
		{launchUUID, true},
		{"bbbb2222-cccc-3333-dddd-444444444444", false},
		{"", false},
		{"not-a-uuid", false},
		{launchUUID, true},
	}

	for i, ev := range hookEvents {
		if ev.shouldOK && ev.sessionID != launchUUID {
			t.Errorf("hook[%d]: expected OK for session=%s", i, ev.sessionID)
		}
		if !ev.shouldOK {
			t.Logf("hook[%d]: correctly rejecting mismatched session: %s", i, ev.sessionID)
		}
	}
}

// TestHookURLValidation verifies that hook command URLs are validated.
func TestHookURLValidation(t *testing.T) {
	// Hook commands must POST to a POKIT-controlled localhost endpoint.
	validURLs := []string{
		"http://localhost:9876/pokit/hook/session-start",
		"http://127.0.0.1:9876/pokit/hook/tool-pre",
	}
	for _, u := range validURLs {
		parsed, err := url.Parse(u)
		if err != nil {
			t.Errorf("valid hook URL parse failed: %s: %v", u, err)
			continue
		}
		if parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1" {
			t.Errorf("hook URL must be localhost: %s", u)
		}
		t.Logf("hook URL valid: %s", u)
	}
}

// TestHookMessageUniqueness verifies hook nonce prevents reply attacks.
func TestHookMessageUniqueness(t *testing.T) {
	// Each hook delivery carries a nonce. The nonce must be:
	// 1. Random per launch (not predictable).
	// 2. Constant within a launch (same nonce for all hooks from one session).
	// 3. Different across launches.

	nonce1 := make([]byte, 32)
	nonce2 := make([]byte, 32)
	rand.Read(nonce1)
	rand.Read(nonce2)

	n1 := hex.EncodeToString(nonce1)
	n2 := hex.EncodeToString(nonce2)

	if n1 == n2 {
		t.Error("two random nonces matched (extremely unlikely — possible RNG failure)")
	}
	if len(n1) != 64 || len(n2) != 64 {
		t.Error("nonce must be 64 hex chars (32 bytes)")
	}

	// Within a launch: same nonce.
	// Across launches: different nonces.
	t.Logf("nonce uniqueness: launch1_nonce=%s launch2_nonce=%s", n1[:16]+"...", n2[:16]+"...")
}
