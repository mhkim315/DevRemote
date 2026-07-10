package devicetrust

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAuthTranscript_Determinism(t *testing.T) {
	nowMS := time.Now().UTC().UnixMilli()
	expMS := nowMS + 300_000
	t1 := AuthTranscript{
		Role: "device", HostID: "h1", DeviceID: "d1", DaemonBootID: "boot1",
		ChallengeID: bytes32("c"), ClientNonce: bytes32("cn"), ServerNonce: bytes32("sn"),
		CreatedAtMS: nowMS, ExpiresAtMS: expMS,
	}
	b1 := t1.Build()
	b2 := t1.Build()
	if string(b1) != string(b2) {
		t.Fatalf("transcript not deterministic: len1=%d len2=%d", len(b1), len(b2))
	}
}

func TestAuthTranscript_DifferentRoleDiverges(t *testing.T) {
	nowMS := time.Now().UTC().UnixMilli()
	expMS := nowMS + 300_000
	base := AuthTranscript{Role: "host", HostID: "h", DeviceID: "d", DaemonBootID: "b",
		ChallengeID: bytes32("c"), ClientNonce: bytes32("cn"), ServerNonce: bytes32("sn"),
		CreatedAtMS: nowMS, ExpiresAtMS: expMS,
	}
	host := base
	host.Role = "host"
	dev := base
	dev.Role = "device"
	if string(host.Build()) == string(dev.Build()) {
		t.Fatalf("host and device transcripts must differ")
	}
}

func TestAuthTranscript_Validate(t *testing.T) {
	nowMS := time.Now().UTC().UnixMilli()
	valid := AuthTranscript{Role: "host", HostID: "h", DeviceID: "d", DaemonBootID: "b",
		ChallengeID: bytes32("c"), ClientNonce: bytes32("cn"), ServerNonce: bytes32("sn"),
		CreatedAtMS: nowMS, ExpiresAtMS: nowMS + 1,
	}
	cases := map[string]func(*AuthTranscript){
		"bad role":     func(tx *AuthTranscript) { tx.Role = "admin" },
		"empty hostId": func(tx *AuthTranscript) { tx.HostID = "" },
		"short nonce":  func(tx *AuthTranscript) { tx.ClientNonce = make([]byte, 16) },
		"zero created": func(tx *AuthTranscript) { tx.CreatedAtMS = 0 },
		"zero expires": func(tx *AuthTranscript) { tx.ExpiresAtMS = 0 },
	}
	for name, mut := range cases {
		tx := valid
		mut(&tx)
		if err := tx.Validate(); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid transcript rejected: %v", err)
	}
}

func TestChallengeStore_Lifecycle(t *testing.T) {
	cs := NewChallengeStore()
	now := time.Now().UTC()
	ch := &PendingChallenge{
		DeviceID: "d1", ClientNonce: bytes32("cn"), ServerNonce: bytes32("sn"),
		HostID: "h1", DaemonBootID: "boot1",
		IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute),
	}
	cid, err := cs.Insert(ch)
	if err != nil || len(cid) != 32 {
		t.Fatalf("Insert: err=%v len=%d", err, len(cid))
	}

	// Consume with correct deviceId.
	got, err := cs.Consume(cid, []byte("d1"))
	if err != nil || !got.Consumed {
		t.Fatalf("Consume: err=%v consumed=%v", err, got != nil && got.Consumed)
	}

	// Second consume must fail (already consumed + deleted).
	if _, err := cs.Consume(cid, []byte("d1")); err == nil {
		t.Fatalf("second consume succeeded on consumed challenge")
	}
}

func TestChallengeStore_WrongDeviceFails(t *testing.T) {
	cs := NewChallengeStore()
	ch := &PendingChallenge{
		DeviceID: "d1", ClientNonce: bytes32("cn"), ServerNonce: bytes32("sn"),
		HostID: "h1", DaemonBootID: "boot1",
		IssuedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	}
	cid, _ := cs.Insert(ch)
	// Consume with wrong deviceId fails (constant-time compare).
	if _, err := cs.Consume(cid, []byte("wrong-device")); err == nil {
		t.Fatalf("consume with wrong device should fail")
	}
	// The challenge must still exist (Consume doesn't count device mismatch).
	if _, err := cs.Find(cid); err != nil {
		t.Fatalf("challenge should still be findable after wrong device: %v", err)
	}
	// Correct device can consume.
	if _, err := cs.Consume(cid, []byte("d1")); err != nil {
		t.Fatalf("correct device: %v", err)
	}
}

func TestChallengeStore_ExpiredDeleted(t *testing.T) {
	cs := NewChallengeStore()
	ch := &PendingChallenge{
		DeviceID: "d1", ClientNonce: bytes32("cn"), ServerNonce: bytes32("sn"),
		HostID: "h1", DaemonBootID: "boot1",
		IssuedAt:  time.Now().UTC().Add(-10 * time.Minute),
		ExpiresAt: time.Now().UTC().Add(-1 * time.Minute),
	}
	cid, _ := cs.Insert(ch)
	if _, err := cs.Consume(cid, []byte("d1")); err == nil {
		t.Fatalf("expired challenge consumed")
	}
}

func TestChallengeStore_AttemptThresholdDeletes(t *testing.T) {
	cs := NewChallengeStore()
	ch := &PendingChallenge{
		DeviceID: "d1", ClientNonce: bytes32("cn"), ServerNonce: bytes32("sn"),
		HostID: "h1", DaemonBootID: "boot1",
		IssuedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	}
	cid, _ := cs.Insert(ch)
	// RecordFailure increments the attempt counter (Consume no longer counts
	// deviceId mismatches as failures).
	for i := 0; i < 5; i++ {
		cs.RecordFailure(cid)
	}
	if _, err := cs.Find(cid); err == nil {
		t.Fatalf("challenge should be deleted after 5 failed attempts")
	}
}

// Secret safety: challenge IDs, nonces, and clientNonces must be absent from
// serialized public DTOs and error messages.
func TestChallengeStore_NoSecretsInErrors(t *testing.T) {
	cs := NewChallengeStore()
	ch := &PendingChallenge{
		DeviceID: "d1", ClientNonce: bytes32("cn"), ServerNonce: bytes32("sn"),
		HostID: "h1", DaemonBootID: "boot1",
		IssuedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	}
	cid, _ := cs.Insert(ch)
	_, err := cs.Consume(cid, []byte("wrong"))
	msg := err.Error()
	// The error must not contain the raw challenge ID, nonces, or boot ID.
	for _, secret := range []string{string(cid), string(ch.ClientNonce), string(ch.ServerNonce), ch.DaemonBootID} {
		if strings.Contains(msg, secret) {
			t.Fatalf("error message leaked a secret: %q", msg)
		}
	}
}

// No raw token/nonce/signature in DTOs.
func TestChallengeStore_DTONoSecrets(t *testing.T) {
	ch := &PendingChallenge{
		DeviceID: "d1", ClientNonce: bytes32("cn"), ServerNonce: bytes32("sn"),
		HostID: "h1", DaemonBootID: "boot1",
		IssuedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	}
	raw, _ := json.Marshal(ch)
	low := strings.ToLower(string(raw))
	for _, secret := range []string{"clientNonce", "serverNonce", "daemonBootId", "consumed"} {
		if strings.Contains(low, secret) {
			t.Fatalf("DTO leaked secret field %q: %s", secret, string(raw))
		}
	}
}

func bytes32(s string) []byte {
	b := make([]byte, 32)
	copy(b, s)
	return b
}
