package devicetrust

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// sentinels are values that must NEVER appear in the audit file. They resemble
// secrets and injection payloads an attacker might try to smuggle through a
// caller-influenced field.
var auditSentinels = map[string]string{
	"newline":        "line1\nline2",
	"carriage":       "a\rb",
	"escape":         "\x1b[2J\x1b[31mred",
	"nul":            "a\x00b",
	"bearer":         "Bearer " + "sk-" + strings.Repeat("a", 32),
	"jwt":            "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.sig",
	"terminal-text":  "$ cat /etc/passwd; rm -rf /",
	"oversized":      strings.Repeat("a", maxAuditFieldLen+1),
	"forged-nonhex":  "not-a-hex-id",
	"unicode-hex-ok": "0123456789abcdefABCDEF", // control: this one IS valid hex
}

// TestAuditRejectsForgedActionAndResult drops events with an out-of-vocabulary
// action or result.
func TestAuditRejectsForgedActionAndResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a := NewFileAuditLog(path)
	a.Record(AuditEvent{DeviceID: "aa", Action: "forged.action", Result: ResultOK})
	a.Record(AuditEvent{DeviceID: "aa", Action: ActionAuthVerify, Result: "forged-result"})
	a.Record(AuditEvent{DeviceID: "aa", Action: "", Result: ""})
	if got := a.List(0); len(got) != 0 {
		t.Fatalf("forged action/result was recorded: %+v", got)
	}
}

// TestAuditDropsMaliciousIdentifierFields proves that control characters,
// oversized values, and non-hex tokens in identifier fields drop the whole
// event, and that none of the sentinel bytes ever land on disk.
func TestAuditDropsMaliciousIdentifierFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a := NewFileAuditLog(path)

	for name, payload := range auditSentinels {
		if name == "unicode-hex-ok" {
			continue // valid hex — exercised as the positive control below
		}
		// Try the payload in each identifier position through Record directly.
		a.Record(AuditEvent{DeviceID: payload, Action: ActionAuthVerify, Result: ResultDenied})
		a.Record(AuditEvent{DeviceID: "aa", Action: ActionSessionKill, Result: ResultOK, SessionID: payload})
		a.Record(AuditEvent{DeviceID: "aa", Action: ActionAuthVerify, Result: ResultGranted, CorrelationID: payload})
	}

	// Nothing should have been written.
	if _, err := os.Stat(path); err == nil {
		raw, _ := os.ReadFile(path)
		if len(raw) != 0 {
			t.Fatalf("malicious events reached disk: %q", raw)
		}
	}
	if got := a.List(0); len(got) != 0 {
		t.Fatalf("malicious events were recorded: %+v", got)
	}

	// Positive control: valid canonical events still write and read back.
	a.Record(AuditEvent{DeviceID: "0123456789abcdef", Action: ActionAuthVerify, Result: ResultGranted, CorrelationID: "beef"})
	a.Record(AuditEvent{DeviceID: "0123456789abcdef", Action: ActionSessionKill, Result: ResultOK, SessionID: "controlled_pty:shell-1"})
	got := a.List(0)
	if len(got) != 2 {
		t.Fatalf("valid events not writable after strict validation: %d", len(got))
	}

	// And the raw file must contain none of the sentinel byte sequences.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for name, payload := range auditSentinels {
		if name == "unicode-hex-ok" {
			continue
		}
		// Compare on the trimmed sentinel (control bytes can't appear anyway).
		probe := strings.TrimRight(payload, "\x00")
		if probe != "" && strings.Contains(string(raw), probe) {
			t.Fatalf("sentinel %q leaked into audit file", name)
		}
	}
	// Session-token-shaped and terminal-text sentinels specifically absent.
	for _, s := range []string{"Bearer ", "eyJ", "rm -rf", "\x1b["} {
		if strings.Contains(string(raw), s) {
			t.Fatalf("secret/terminal sentinel %q leaked into audit file", s)
		}
	}
}

// ── Filesystem security (BLOCKER 3) ──

func TestAuditNewFileIs0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	NewFileAuditLog(path).Record(AuditEvent{DeviceID: "aa", Action: ActionAuthVerify, Result: ResultGranted})
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("new audit file mode %o want 0600", perm)
	}
}

func TestAuditCorrectsPreexisting0644(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	// Pre-create an empty user-owned regular file with broad permissions.
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	a := NewFileAuditLog(path)
	a.Record(AuditEvent{DeviceID: "aa", Action: ActionAuthVerify, Result: ResultGranted})
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("pre-existing file not corrected to owner-only: mode %o", perm)
	}
	// The append still succeeded after correction.
	if got := a.List(0); len(got) != 1 {
		t.Fatalf("append after permission correction failed: %d events", len(got))
	}
}

func TestAuditRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real-target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	path := filepath.Join(dir, "audit.jsonl")
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	a := NewFileAuditLog(path)
	a.Record(AuditEvent{DeviceID: "aa", Action: ActionAuthVerify, Result: ResultGranted})
	// Nothing must have been written through the symlink to the target.
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if len(raw) != 0 {
		t.Fatalf("audit wrote through a symlink: %q", raw)
	}
}

func TestAuditRejectsNonRegularFile(t *testing.T) {
	dir := t.TempDir()
	// A directory at the audit path is a non-regular file.
	path := filepath.Join(dir, "audit.jsonl")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	a := NewFileAuditLog(path)
	a.Record(AuditEvent{DeviceID: "aa", Action: ActionAuthVerify, Result: ResultGranted}) // must not panic/write

	// A FIFO is also non-regular (skip if the platform/CI cannot create one).
	fifo := filepath.Join(dir, "fifo.jsonl")
	if err := syscall.Mkfifo(fifo, 0o600); err == nil {
		af := NewFileAuditLog(fifo)
		done := make(chan struct{})
		go func() {
			af.Record(AuditEvent{DeviceID: "aa", Action: ActionAuthVerify, Result: ResultGranted})
			close(done)
		}()
		<-done // must return without blocking on the FIFO
	}
}

func TestAuditAppendsAfterSecurityValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a := NewFileAuditLog(path)
	for i := 0; i < 5; i++ {
		a.Record(AuditEvent{DeviceID: "aa", Action: ActionAuthVerify, Result: ResultGranted})
	}
	if got := a.List(0); len(got) != 5 {
		t.Fatalf("normal append broke after security validation: %d", len(got))
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode drifted from 0600: %o", fi.Mode().Perm())
	}
}

// TestAuditRotationSecuresGeneration proves a pre-existing insecure (0644)
// current file is corrected to 0600 BEFORE rotation, so the rotated generation
// (<path>.1) is never left group/other-readable.
// TestAuditSessionIDRejectsUnicodeWhitespaceAndFormat proves the session-ID
// validator rejects Unicode whitespace and format-control code points, not just
// ASCII controls, so no invisible/zero-width characters can enter the trail.
func TestAuditSessionIDRejectsUnicodeWhitespaceAndFormat(t *testing.T) {
	for _, bad := range []string{
		"controlled_pty:a\u00a0b", // NO-BREAK SPACE (Zs)
		"controlled_pty:a\u2028b", // LINE SEPARATOR (Zl)
		"controlled_pty:a\u200bb", // ZERO WIDTH SPACE (Cf)
		"controlled_pty:a\ufeffb", // ZERO WIDTH NO-BREAK SPACE / BOM (Cf)
	} {
		if validSessionID(bad) {
			t.Fatalf("session id with hidden character accepted: %q", bad)
		}
	}
	if !validSessionID("controlled_pty:shell-1783690564296039000") {
		t.Fatal("canonical ASCII session id wrongly rejected")
	}
}

func TestAuditRotationSecuresGeneration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	// Seed an over-threshold, world-readable current file.
	seed := []byte(strings.Repeat("x", 600) + "\n")
	if err := os.WriteFile(path, seed, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := NewFileAuditLogWithMax(path, 512) // any append now forces rotation
	a.Record(AuditEvent{DeviceID: "aa", Action: ActionAuthVerify, Result: ResultGranted})

	// Rotation must have occurred and the rotated generation must be owner-only.
	rfi, err := os.Lstat(path + ".1")
	if err != nil {
		t.Fatalf("expected rotated generation: %v", err)
	}
	if !rfi.Mode().IsRegular() {
		t.Fatalf("rotated generation is not a regular file: %v", rfi.Mode())
	}
	if perm := rfi.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("rotated generation mode %o is group/other accessible", perm)
	}
	// The fresh current file is also 0600, and the new event is present.
	cfi, err := os.Stat(path)
	if err != nil || cfi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("current file after rotation not owner-only: %v perm=%o", err, cfi.Mode().Perm())
	}
	if got := a.List(0); len(got) == 0 || got[len(got)-1].Action != ActionAuthVerify {
		t.Fatalf("append after secure rotation missing: %+v", got)
	}
}
