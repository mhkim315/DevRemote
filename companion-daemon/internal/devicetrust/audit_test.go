package devicetrust

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestAuditRecordListRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a := NewFileAuditLog(path)
	a.Record(AuditEvent{DeviceID: "ab01", Action: ActionAuthVerify, Result: ResultGranted, CorrelationID: "beef01"})
	a.Record(AuditEvent{DeviceID: "ab01", Action: ActionSessionKill, SessionID: "controlled_pty:x", Result: ResultOK})
	a.Record(AuditEvent{DeviceID: "ab02", Action: ActionDeviceRevoke, Result: ResultOK})

	got := a.List(0)
	if len(got) != 3 {
		t.Fatalf("List: got %d events want 3", len(got))
	}
	// Chronological order preserved.
	if got[0].Action != ActionAuthVerify || got[2].Action != ActionDeviceRevoke {
		t.Fatalf("order wrong: %+v", got)
	}
	if got[0].Timestamp.IsZero() {
		t.Fatal("timestamp not stamped")
	}
	// Limit returns the most recent N.
	if last := a.List(1); len(last) != 1 || last[0].Action != ActionDeviceRevoke {
		t.Fatalf("List(1)=%+v want most recent", last)
	}
}

// TestAuditRedactionSchema proves the on-disk record can only ever contain the
// six allowed keys — the structural redaction guarantee.
func TestAuditRedactionSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a := NewFileAuditLog(path)
	a.Record(AuditEvent{
		Timestamp: time.Now().UTC(), DeviceID: "aa", Action: ActionAuthVerify,
		SessionID: "controlled_pty:s", Result: ResultGranted, CorrelationID: "cc",
	})
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal line: %v", err)
	}
	allowed := map[string]bool{
		"timestamp": true, "deviceId": true, "action": true,
		"sessionId": true, "result": true, "correlationId": true,
	}
	for k := range m {
		if !allowed[k] {
			t.Fatalf("audit record leaked disallowed key %q", k)
		}
	}
}

func TestAuditIgnoresEmptyAction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a := NewFileAuditLog(path)
	a.Record(AuditEvent{DeviceID: "aa", Result: ResultOK}) // no action → dropped
	if got := a.List(0); len(got) != 0 {
		t.Fatalf("empty-action event was recorded: %+v", got)
	}
}

func TestAuditRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a := NewFileAuditLogWithMax(path, 512) // tiny threshold
	const n = 200
	for i := 0; i < n; i++ {
		a.Record(AuditEvent{DeviceID: "abcd", Action: ActionAuthVerify, Result: ResultGranted, CorrelationID: fmt.Sprintf("%06x", i)})
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("rotation did not create %s.1: %v", path, err)
	}
	if fi, err := os.Stat(path); err == nil && fi.Size() > 512+256 {
		t.Fatalf("current file not bounded after rotation: %d", fi.Size())
	}
	// Single-generation rotation is intentionally lossy: it retains a bounded
	// recent subset (current file + one rotated generation), not the full run.
	got := a.List(0)
	if len(got) == 0 || len(got) >= n {
		t.Fatalf("expected a bounded recent subset, got %d of %d", len(got), n)
	}
	if got[len(got)-1].CorrelationID != fmt.Sprintf("%06x", n-1) {
		t.Fatalf("most recent event lost: last=%q", got[len(got)-1].CorrelationID)
	}
	if got[0].CorrelationID == fmt.Sprintf("%06x", 0) {
		t.Fatal("expected oldest events to be rotated out")
	}
}

func TestAuditFilePerms(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "audit.jsonl")
	a := NewFileAuditLog(path)
	a.Record(AuditEvent{DeviceID: "aa", Action: ActionAuthVerify, Result: ResultGranted})
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("audit file mode %o is group/other accessible", perm)
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := di.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("audit dir mode %o is group/other accessible", perm)
	}
}

// readOwnerOnly fails closed on insecure perms; List must then return nothing.
func TestAuditListFailsClosedOnInsecurePerms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a := NewFileAuditLog(path)
	a.Record(AuditEvent{DeviceID: "aa", Action: ActionAuthVerify, Result: ResultGranted})
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if got := a.List(0); got != nil {
		t.Fatalf("List returned events from a world-readable file: %+v", got)
	}
}

func TestAuditConcurrentRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a := NewFileAuditLog(path)
	const goroutines, per = 8, 25
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < per; i++ {
				a.Record(AuditEvent{DeviceID: "abcd", Action: ActionAuthVerify, Result: ResultGranted})
			}
		}()
	}
	wg.Wait()
	if got := a.List(0); len(got) != goroutines*per {
		t.Fatalf("concurrent record: got %d want %d", len(got), goroutines*per)
	}
}

func TestNopAuditLog(t *testing.T) {
	var a AuditLog = NopAuditLog{}
	a.Record(AuditEvent{Action: ActionAuthVerify, Result: ResultGranted})
	if got := a.List(0); got != nil {
		t.Fatalf("NopAuditLog.List returned %+v", got)
	}
}
