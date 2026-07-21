package devicetrust

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
)

// ── Minimal local audit (M2.5-5) ──
//
// The audit trail records security-relevant actions only. Its redaction
// boundary is STRUCTURAL: AuditEvent has no field capable of holding terminal
// content, input, transcript, private keys, tokens, pairing secrets,
// signatures, or push tokens. If something cannot be expressed by these six
// fields, it is not audited.

// Audit action constants — a closed set. An action names an operation; it never
// carries free-form or sensitive text.
const (
	ActionAuthVerify    = "auth.verify"
	ActionDeviceRevoke  = "device.revoke"
	ActionOwnerRecovery = "owner.recovery"
	ActionSessionStop   = "session.stop"
	ActionSessionKill   = "session.kill"
	ActionWSTicketDeny  = "ws.ticket.deny"
)

// Audit result constants — a closed set.
const (
	ResultGranted = "granted"
	ResultDenied  = "denied"
	ResultOK      = "ok"
	ResultError   = "error"
)

// AuditEvent is one redacted audit record. The six fields below are the entire
// schema — see the package note above on structural redaction.
type AuditEvent struct {
	Timestamp     time.Time `json:"timestamp"`
	DeviceID      string    `json:"deviceId,omitempty"`
	Action        string    `json:"action"`
	SessionID     string    `json:"sessionId,omitempty"`
	Result        string    `json:"result"`
	CorrelationID string    `json:"correlationId,omitempty"`
}

// AuditLog records and lists audit events. Implementations must be safe for
// concurrent use and must never block or fail the caller's security action.
type AuditLog interface {
	Record(AuditEvent)
	List(limit int) []AuditEvent
}

// NopAuditLog discards events. Used in insecure-local-only mode and tests.
type NopAuditLog struct{}

func (NopAuditLog) Record(AuditEvent)     {}
func (NopAuditLog) List(int) []AuditEvent { return nil }

const defaultAuditMaxBytes = 1 << 20 // 1 MiB before single-generation rotation

// maxAuditFieldLen caps identifier fields so a caller-influenced value (e.g. a
// URL session id) cannot bloat the trail.
const maxAuditFieldLen = 256

// auditActions / auditResults are the closed vocabularies. An event whose
// action or result is outside these sets is dropped, never written.
var auditActions = map[string]struct{}{
	ActionAuthVerify: {}, ActionDeviceRevoke: {}, ActionOwnerRecovery: {},
	ActionSessionStop: {}, ActionSessionKill: {}, ActionWSTicketDeny: {},
}

var auditResults = map[string]struct{}{
	ResultGranted: {}, ResultDenied: {}, ResultOK: {}, ResultError: {},
}

// validAuditEvent enforces the closed action/result sets and strict per-field
// identifier syntax. This is what makes the redaction guarantee real: a
// caller-influenced value can never inject arbitrary text (or line breaks) into
// the trail — a violating event is dropped in full, never sanitized into a
// different accepted value.
//
// deviceId and correlationId (device fingerprints and bearer session IDs) are
// always hex. sessionId is a canonical `<adapter>:<local-id>` that may contain
// ':' and Unicode but never a control character. Every field is optional ("").
func validAuditEvent(ev AuditEvent) bool {
	if _, ok := auditActions[ev.Action]; !ok {
		return false
	}
	if _, ok := auditResults[ev.Result]; !ok {
		return false
	}
	return validHexID(ev.DeviceID) && validHexID(ev.CorrelationID) && validSessionID(ev.SessionID)
}

// validHexID accepts a bounded hex string (or ""). Non-hex input — a JWT, a
// bearer token, terminal text — is rejected, dropping the whole event.
func validHexID(s string) bool {
	if len(s) > maxAuditFieldLen {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

// validSessionID accepts "" or a canonical session ID of the form
// "<adapter>:<local-id>", where adapter is [a-z_]+ and local-id is bounded and
// free of whitespace and control characters (Unicode letters/digits allowed).
// This deliberately rejects free-form text — a bearer token, a JWT, a shell
// command, an escape sequence — so nothing but a real canonical session
// identifier can ever be recorded, even if a caller passes attacker input.
func validSessionID(s string) bool {
	if s == "" {
		return true
	}
	if len(s) > maxAuditFieldLen {
		return false
	}
	i := strings.IndexByte(s, ':')
	if i <= 0 || i == len(s)-1 {
		return false // require a non-empty adapter and local id
	}
	for _, r := range s[:i] {
		if !((r >= 'a' && r <= 'z') || r == '_') {
			return false
		}
	}
	for _, r := range s[i+1:] {
		if r < 0x20 || r == 0x7f || unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

// FileAuditLog is an append-only JSONL audit trail with owner-only (0600)
// permissions and a 0700 parent directory. Writes are best-effort: an audit I/O
// failure never propagates to the security action that produced the event.
type FileAuditLog struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
}

var (
	_ AuditLog = (*FileAuditLog)(nil)
	_ AuditLog = NopAuditLog{}
)

// NewFileAuditLog creates a file audit log at path with the default rotation
// threshold.
func NewFileAuditLog(path string) *FileAuditLog {
	return &FileAuditLog{path: path, maxBytes: defaultAuditMaxBytes}
}

// NewFileAuditLogWithMax lets tests inject a small rotation threshold.
func NewFileAuditLogWithMax(path string, maxBytes int64) *FileAuditLog {
	if maxBytes <= 0 {
		maxBytes = defaultAuditMaxBytes
	}
	return &FileAuditLog{path: path, maxBytes: maxBytes}
}

// Record appends one event. Events failing the closed-vocabulary / identifier
// validation are dropped entirely (never partially sanitized). Marshal and I/O
// failures are dropped silently so auditing can never break authentication or
// lifecycle. The write path refuses symlinks/non-regular files and enforces
// owner-only (0600) permissions before appending.
func (l *FileAuditLog) Record(ev AuditEvent) {
	if !validAuditEvent(ev) {
		return
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	line, err := json.Marshal(ev)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.ensureDirLocked(); err != nil {
		return
	}
	// Secure the CURRENT file BEFORE any rotation, so a file that gets renamed
	// to <path>.1 is already owner-only (a rotated generation must never inherit
	// broader permissions). Rejects symlinks / non-regular files up front.
	if !l.secureExistingLocked() {
		return
	}
	// Rotate the (now-secured) file if needed; fail closed if rotation cannot
	// complete safely rather than appending past the size cap.
	if !l.rotateIfNeededLocked(int64(len(line) + 1)) {
		return
	}
	// O_NOFOLLOW closes the Lstat→open TOCTOU window: if the final path
	// component is (or becomes) a symlink, the open fails rather than following
	// it. Supported on darwin and linux (the daemon's target platforms). A new
	// file is created atomically at 0600 (0600 has no group/other bits for the
	// umask to leave visible).
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// secureExistingLocked refuses to write through a symlink or non-regular file
// and tightens an existing over-permissive regular audit file to 0600 (verifying
// the result). Returns false when the path cannot be made safe, so the caller
// fails closed. Must hold l.mu.
func (l *FileAuditLog) secureExistingLocked() bool {
	fi, err := os.Lstat(l.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true // no file yet — OpenFile creates it 0600
		}
		return false
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
		return false // never write through a symlink or non-regular file
	}
	if fi.Mode().Perm()&0o077 != 0 {
		// User-owned regular file with broader permissions: correct to 0600,
		// then verify. If correction fails or does not stick, fail closed.
		if err := os.Chmod(l.path, 0o600); err != nil {
			return false
		}
		if again, err := os.Lstat(l.path); err != nil || again.Mode().Perm()&0o077 != 0 {
			return false
		}
	}
	return true
}

// List returns up to limit most-recent events in chronological order, spanning
// the rotated (.1) generation and the current file.
func (l *FileAuditLog) List(limit int) []AuditEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	var all []AuditEvent
	all = append(all, readAuditFile(l.path+".1")...)
	all = append(all, readAuditFile(l.path)...)
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all
}

func (l *FileAuditLog) ensureDirLocked() error {
	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// MkdirAll does not tighten an existing dir; enforce 0700.
	return os.Chmod(dir, 0o700)
}

// rotateIfNeededLocked renames the current file to <path>.1 (single generation)
// when appending would exceed maxBytes. The caller must have already secured
// the current file (owner-only regular file), so the rename produces an
// owner-only <path>.1. Returns false when rotation was required but could not
// complete safely, so the caller fails closed instead of appending. Overwriting
// rename replaces any pre-existing (possibly insecure) <path>.1.
func (l *FileAuditLog) rotateIfNeededLocked(incoming int64) bool {
	fi, err := os.Stat(l.path)
	if err != nil {
		return true // no current file — nothing to rotate
	}
	if fi.Size()+incoming <= l.maxBytes {
		return true // under threshold
	}
	rotated := l.path + ".1"
	if err := os.Rename(l.path, rotated); err != nil {
		return false
	}
	// Verify the rotated generation is a regular, owner-only file. (rename does
	// not follow symlinks, so a swapped-in symlink is renamed as-is and caught
	// here.) Correct broadened bits defensively; fail closed if it cannot be
	// made a secure regular file.
	rfi, err := os.Lstat(rotated)
	if err != nil || rfi.Mode()&os.ModeSymlink != 0 || !rfi.Mode().IsRegular() {
		return false
	}
	if rfi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(rotated, 0o600); err != nil {
			return false
		}
		if again, err := os.Lstat(rotated); err != nil || again.Mode().Perm()&0o077 != 0 {
			return false
		}
	}
	return true
}

// readAuditFile parses one JSONL audit file, failing closed (empty) on missing
// files or insecure permissions.
func readAuditFile(path string) []AuditEvent {
	raw, err := readOwnerOnly(path)
	if err != nil {
		return nil
	}
	var out []AuditEvent
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev AuditEvent
		if json.Unmarshal(line, &ev) == nil {
			out = append(out, ev)
		}
	}
	return out
}
