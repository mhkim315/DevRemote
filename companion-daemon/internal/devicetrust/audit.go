package devicetrust

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
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
	ActionAuthVerify   = "auth.verify"
	ActionDeviceRevoke = "device.revoke"
	ActionSessionStop  = "session.stop"
	ActionSessionKill  = "session.kill"
	ActionWSTicketDeny = "ws.ticket.deny"
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

// Record appends one event. Empty/unknown actions and marshal/I-O failures are
// dropped silently so auditing can never break authentication or lifecycle.
func (l *FileAuditLog) Record(ev AuditEvent) {
	if ev.Action == "" {
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
	l.rotateIfNeededLocked(int64(len(line) + 1))
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
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
// when appending would exceed maxBytes.
func (l *FileAuditLog) rotateIfNeededLocked(incoming int64) {
	fi, err := os.Stat(l.path)
	if err != nil {
		return // no file yet
	}
	if fi.Size()+incoming <= l.maxBytes {
		return
	}
	_ = os.Rename(l.path, l.path+".1")
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
