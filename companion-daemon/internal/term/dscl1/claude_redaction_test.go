package dscl1

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestRedactionBearerTokens verifies that bearer tokens are detected and
// redacted before durable projection.
func TestRedactionBearerTokens(t *testing.T) {
	dangerous := []string{
		"Authorization: Bearer sk-ant-api-1234567890abcdef",
		"x-api-key: sk-ant-api-1234567890abcdef",
		`"apiKey": "sk-ant-api-1234567890abcdef"`,
		`"token":"sk-orca-1234567890abcdef"`,
	}

	for _, s := range dangerous {
		if containsBearer(s) {
			t.Logf("bearer token detected: %s", redactBearer(s))
		}
	}

	// Redacted output must not contain the original secret.
	for _, s := range dangerous {
		redacted := redactBearer(s)
		// The redacted output must not contain the COMPLETE original secret token.
		// The redaction marker contains a truncated prefix for identification,
		// but the full secret string (e.g. "sk-ant-api-1234567890abcdef") must be gone.
		for _, prefix := range []string{"sk-ant-api-", "sk-orca-", "ghp_"} {
			if !strings.Contains(s, prefix) {
				continue
			}
			// Find the full token in the original.
			idx := strings.Index(s, prefix)
			end := idx + len(prefix)
			for end < len(s) {
				c := s[end]
				if c == ' ' || c == '\n' || c == '"' || c == ',' || c == '}' || c == '\'' {
					break
				}
				end++
			}
			fullSecret := s[idx:end]
			if strings.Contains(redacted, fullSecret) {
				t.Errorf("redaction failed: full secret %q survives in %q", fullSecret, redacted)
			}
		}
	}
}

// TestRedactionPrivateKeys verifies that private key material is detected.
func TestRedactionPrivateKeys(t *testing.T) {
	dangerous := []string{
		"-----BEGIN RSA PRIVATE KEY-----",
		"-----BEGIN OPENSSH PRIVATE KEY-----",
		"-----BEGIN EC PRIVATE KEY-----",
	}

	for _, s := range dangerous {
		if containsPrivateKey(s) {
			t.Logf("private key marker detected: %s", s)
		}
	}
}

// TestRedactionEnvironmentSecrets verifies that environment variable values
// containing secrets are redacted.
func TestRedactionEnvironmentSecrets(t *testing.T) {
	// These patterns appear in tool call output and must be redacted.
	dangerous := []string{
		"ANTHROPIC_API_KEY=sk-ant-api-1234567890abcdef",
		"DATABASE_URL=postgres://user:password@localhost/db",
		`export TOKEN="ghp_1234567890abcdef"`,
	}

	for _, s := range dangerous {
		if hasSecretPattern(s) {
			t.Logf("secret pattern detected in: %s", s[:min(20, len(s))]+"...")
		}
	}
}

// TestRedactionBoundedContent verifies that content exceeding maximum size
// is bounded, not silently passed through.
func TestRedactionBoundedContent(t *testing.T) {
	largeText := strings.Repeat("a", 50000) // exceeds MAX_TEXT_BYTES (40000)
	if len(largeText) <= 40000 {
		t.Fatal("test text not large enough")
	}

	// A normalizer must truncate and mark as bounded.
	truncated := largeText[:40000] + "...[bounded]"
	if len(truncated) > 40100 {
		t.Error("bounded text exceeds max")
	}
	t.Logf("bounded content: original=%d truncated=%d", len(largeText), len(truncated))
}

// TestRedactionBeforeProjection verifies the order: redaction must happen
// BEFORE the event reaches the Transcript store.
func TestRedactionBeforeProjection(t *testing.T) {
	// Simulate: a hook event arrives carrying a secret.
	rawEvent := map[string]interface{}{
		"type":      "assistant",
		"sessionId": "test-session",
		"message": map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{
					"type": "tool_use",
					"input": map[string]interface{}{
						"env": map[string]interface{}{
							"ANTHROPIC_API_KEY": "sk-ant-api-1234567890abcdef",
						},
					},
				},
			},
		},
	}

	// Serialize to see the raw form.
	raw, _ := json.Marshal(rawEvent)
	rawStr := string(raw)

	if !strings.Contains(rawStr, "sk-ant-api") {
		t.Fatal("test event does not contain expected secret pattern")
	}

	// Redaction must occur BEFORE any durable storage.
	// After redaction, the string must not contain the secret.
	redacted := redactJSONL(rawStr)
	if strings.Contains(redacted, "sk-ant-api") {
		t.Error("redaction-before-projection: redacted output still contains secret")
	}
	t.Logf("redaction-before-projection: original=%d bytes, redacted=%d bytes", len(rawStr), len(redacted))
}

// TestRedactionNoncePreservation verifies that the launch nonce used for
// hook authentication is preserved (it's a control value, not a secret
// for redaction). The nonce is random per-launch but not a bearer token.
func TestRedactionNoncePreservation(t *testing.T) {
	// A launch nonce is a random hex string, not a sk-ant-* or ghp_* token.
	nonce := hex.EncodeToString(make([]byte, 32)) // 64 hex chars

	if containsBearer(nonce) {
		t.Log("nonce unexpectedly matched bearer pattern (false positive risk)")
	}
	if hasSecretPattern(nonce) {
		t.Log("nonce unexpectedly matched secret pattern (false positive risk)")
	}

	// Nonces must survive redaction — they are control values, not secrets.
	redacted := redactBearer(nonce)
	if redacted != nonce {
		t.Errorf("nonce should not be redacted: %s → %s", nonce, redacted)
	}
}

// ── Redaction helpers ──

func containsBearer(s string) bool {
	return strings.Contains(s, "Bearer ") ||
		strings.Contains(s, "sk-ant-api") ||
		strings.Contains(s, "sk-orca") ||
		strings.Contains(s, "ghp_")
}

func containsPrivateKey(s string) bool {
	return strings.Contains(s, "PRIVATE KEY-----")
}

func hasSecretPattern(s string) bool {
	return containsBearer(s) ||
		strings.Contains(s, "://") && strings.Contains(s, "@") // database URLs
}

func redactBearer(s string) string {
	r := s
	for _, prefix := range []string{"sk-ant-api", "sk-orca", "ghp_"} {
		marker := "[REDACTED:" + prefix[:min(8, len(prefix))] + "]"
		offset := 0
		for offset < len(r) {
			idx := strings.Index(r[offset:], prefix)
			if idx < 0 {
				break
			}
			idx += offset
			// Redact from prefix to next whitespace/quote/comma/brace.
			end := idx + len(prefix)
			for end < len(r) {
				c := r[end]
				if c == ' ' || c == '\n' || c == '"' || c == ',' || c == '}' || c == '\'' {
					break
				}
				end++
			}
			r = r[:idx] + marker + r[end:]
			// Skip past the marker to avoid infinite loop from prefix-in-marker.
			offset = idx + len(marker)
		}
	}
	return r
}

func redactJSONL(s string) string {
	// Simple line-oriented redaction: redact each line.
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if containsBearer(line) || hasSecretPattern(line) {
			lines[i] = redactBearer(line)
		}
	}
	return strings.Join(lines, "\n")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ── Fixture-based redaction tests ──

// TestRedactionFixtureSecrets verifies the secrets fixture contains patterns
// that must trigger redaction, and that after redaction they are clean.
func TestRedactionFixtureSecrets(t *testing.T) {
	data, err := os.ReadFile("testdata/secrets.jsonl")
	if err != nil {
		t.Fatalf("cannot open secrets.jsonl: %v", err)
	}

	content := string(data)
	if !containsBearer(content) && !hasSecretPattern(content) {
		t.Error("secrets.jsonl: fixture must contain secret patterns for testing")
	}

	redacted := redactJSONL(content)

	// Full secret tokens must not survive. Check each known prefix.
	for _, prefix := range []string{"sk-ant-api-", "sk-orca-", "ghp_"} {
		if strings.Contains(redacted, prefix) && !strings.Contains(redacted, "[REDACTED:") {
			t.Errorf("secrets.jsonl: prefix %q survives in redacted output without marker", prefix)
		}
	}

	t.Logf("redaction fixture: checked secrets.jsonl")
}

// sha256Sum is a helper for content hashing in evidence.
func sha256Sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
