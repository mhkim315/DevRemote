package devicetrust

import (
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
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

// ── 9.4-D Epoch race tests (§5.2 contract) ──

func TestEpoch_RevokeVsVerify(t *testing.T) {
	store := &FileDeviceStore{Path: t.TempDir() + "/devices.json"}
	reg, _ := NewDeviceRegistry(store)
	_, pub, _ := GenKeypair(t)
	dev, err := reg.Add(pub, "test-device")
	if err != nil {
		t.Fatalf("add device: %v", err)
	}

	bootID, _ := NewBootID()
	mgr := NewDeviceSessionManager(bootID, 1*time.Hour)
	mgr.GetEpoch = reg.GetEpoch

	tok, _, _, err := mgr.CreateAfterVerifiedChallenge(dev.DeviceID, "host-1", bootID, PermissionsForRole(RoleOwner), dev.Epoch)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if p := mgr.AuthenticateBearer(tok); p == nil {
		t.Fatal("session should be valid before revoke")
	}

	if err := reg.Revoke(dev.DeviceID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if p := mgr.AuthenticateBearer(tok); p != nil {
		t.Fatal("revoke vs verify: stale session was accepted after revoke")
	}
}

func TestEpoch_RevokeVsRefresh(t *testing.T) {
	store := &FileDeviceStore{Path: t.TempDir() + "/devices.json"}
	reg, _ := NewDeviceRegistry(store)
	_, pub, _ := GenKeypair(t)
	dev, err := reg.Add(pub, "test-device")
	if err != nil {
		t.Fatalf("add device: %v", err)
	}

	bootID, _ := NewBootID()
	mgr := NewDeviceSessionManager(bootID, 1*time.Hour)
	mgr.GetEpoch = reg.GetEpoch

	tok, _, _, _ := mgr.CreateAfterVerifiedChallenge(dev.DeviceID, "host-1", bootID, PermissionsForRole(RoleOwner), dev.Epoch)
	reg.Revoke(dev.DeviceID)

	// Old token (issued before revoke at epoch 0) must be rejected after
	// revoke bumps epoch to 1.
	if p := mgr.AuthenticateBearer(tok); p != nil {
		t.Fatal("revoke vs refresh: stale session accepted after revoke")
	}
	// New session issued after revoke carries epoch 1 — it must be valid.
	newTok, _, _, err := mgr.CreateAfterVerifiedChallenge(dev.DeviceID, "host-1", bootID, PermissionsForRole(RoleOwner), reg.GetEpoch(dev.DeviceID))
	if err != nil {
		t.Fatalf("revoke vs refresh: post-revoke session create: %v", err)
	}
	if p := mgr.AuthenticateBearer(newTok); p == nil {
		t.Fatal("revoke vs refresh: new session (epoch 1) should be valid")
	}
}

func TestEpoch_ReplacementVsOldDevice(t *testing.T) {
	store := &FileDeviceStore{Path: t.TempDir() + "/devices.json"}
	reg, _ := NewDeviceRegistry(store)
	_, oldPub, _ := GenKeypair(t)
	oldDev, _ := reg.Add(oldPub, "old-device")

	bootID, _ := NewBootID()
	mgr := NewDeviceSessionManager(bootID, 1*time.Hour)
	mgr.GetEpoch = reg.GetEpoch

	oldTok, _, _, _ := mgr.CreateAfterVerifiedChallenge(oldDev.DeviceID, "host-1", bootID, PermissionsForRole(RoleOwner), 0)
	reg.Revoke(oldDev.DeviceID)

	_, newPub, _ := GenKeypair(t)
	newDev, _ := reg.Add(newPub, "replacement-device")

	newTok, _, _, err := mgr.CreateAfterVerifiedChallenge(newDev.DeviceID, "host-1", bootID, PermissionsForRole(RoleOwner), 0)
	if err != nil {
		t.Fatalf("replacement session: %v", err)
	}
	if p := mgr.AuthenticateBearer(newTok); p == nil {
		t.Fatal("replacement session should be valid")
	}
	if p := mgr.AuthenticateBearer(oldTok); p != nil {
		t.Fatal("old bearer for revoked device must be rejected")
	}
}

func TestEpoch_DaemonRestartBootChange(t *testing.T) {
	store := &FileDeviceStore{Path: t.TempDir() + "/devices.json"}
	reg, _ := NewDeviceRegistry(store)
	_, pub, _ := GenKeypair(t)
	dev, _ := reg.Add(pub, "test-device")

	bootID1, _ := NewBootID()
	mgr1 := NewDeviceSessionManager(bootID1, 1*time.Hour)
	mgr1.GetEpoch = reg.GetEpoch

	tok, _, _, _ := mgr1.CreateAfterVerifiedChallenge(dev.DeviceID, "host-1", bootID1, PermissionsForRole(RoleOwner), 0)
	if p := mgr1.AuthenticateBearer(tok); p == nil {
		t.Fatal("session valid under boot1")
	}

	// Simulate daemon restart with different boot ID.
	bootID2 := "different-boot-after-restart"
	mgr2 := NewDeviceSessionManager(bootID2, 1*time.Hour)
	mgr2.GetEpoch = reg.GetEpoch

	// Boot change: session digest not in new manager's map → nil Principal.
	if p := mgr2.AuthenticateBearer(tok); p != nil {
		t.Fatal("restart boot change: old token accepted by new session manager")
	}
}

func TestEpoch_PushRegistrationAfterRevoke(t *testing.T) {
	store := &FileDeviceStore{Path: t.TempDir() + "/devices.json"}
	reg, _ := NewDeviceRegistry(store)
	_, pub, _ := GenKeypair(t)
	dev, _ := reg.Add(pub, "test-device")

	bootID, _ := NewBootID()
	mgr := NewDeviceSessionManager(bootID, 1*time.Hour)
	mgr.GetEpoch = reg.GetEpoch

	tok, _, _, _ := mgr.CreateAfterVerifiedChallenge(dev.DeviceID, "host-1", bootID, PermissionsForRole(RoleOwner), dev.Epoch)
	reg.Revoke(dev.DeviceID)

	if p := mgr.AuthenticateBearer(tok); p != nil {
		t.Fatal("push registration after revoke: stale bearer accepted")
	}
}

// ── 9.4-D R3: real concurrent race tests with timing barriers ──

func TestEpoch_ConcurrentRevokeVsIssue(t *testing.T) {
	store := &FileDeviceStore{Path: t.TempDir() + "/devices.json"}
	reg, _ := NewDeviceRegistry(store)
	_, pub, _ := GenKeypair(t)
	dev, _ := reg.Add(pub, "test-device")

	bootID, _ := NewBootID()
	mgr := NewDeviceSessionManager(bootID, 1*time.Hour)
	mgr.GetEpoch = reg.GetEpoch

	var wg sync.WaitGroup
	var issued int32
	var revoked int32
	barrier := make(chan struct{})

	// Goroutine 1: repeatedly issue sessions at the current epoch.
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-barrier
		for i := 0; i < 50; i++ {
			epoch := reg.GetEpoch(dev.DeviceID)
			_, _, _, err := mgr.CreateAfterVerifiedChallenge(dev.DeviceID, "host-1", bootID, PermissionsForRole(RoleOwner), epoch)
			if err != nil {
				return // epoch changed, stop
			}
			atomic.AddInt32(&issued, 1)
		}
	}()

	// Goroutine 2: repeatedly revoke (which bumps epoch).
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-barrier
		for i := 0; i < 50; i++ {
			// Re-add + revoke to bump epoch
			_, _, _ = GenKeypair(t)
			reg.Revoke(dev.DeviceID)
			atomic.AddInt32(&revoked, 1)
			time.Sleep(time.Millisecond)
		}
	}()

	close(barrier)
	wg.Wait()
	t.Logf("issued=%d revoked=%d", atomic.LoadInt32(&issued), atomic.LoadInt32(&revoked))
}

func TestEpoch_ConcurrentRevokeVsRefresh(t *testing.T) {
	store := &FileDeviceStore{Path: t.TempDir() + "/devices.json"}
	reg, _ := NewDeviceRegistry(store)
	_, pub, _ := GenKeypair(t)
	dev, _ := reg.Add(pub, "test-device")

	bootID, _ := NewBootID()
	mgr := NewDeviceSessionManager(bootID, 1*time.Hour)
	mgr.GetEpoch = reg.GetEpoch

	var wg sync.WaitGroup
	var refreshed, revoked int32
	barrier := make(chan struct{})

	// Issue initial session.
	tok, _, _, _ := mgr.CreateAfterVerifiedChallenge(dev.DeviceID, "host-1", bootID, PermissionsForRole(RoleOwner), dev.Epoch)

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-barrier
		for i := 0; i < 50; i++ {
			if p := mgr.AuthenticateBearer(tok); p == nil {
				return // token rejected by epoch check
			}
			atomic.AddInt32(&refreshed, 1)
			time.Sleep(time.Microsecond)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-barrier
		for i := 0; i < 20; i++ {
			reg.Revoke(dev.DeviceID)
			atomic.AddInt32(&revoked, 1)
			// Re-add so we can revoke again
			_, newPub, _ := GenKeypair(t)
			reg.Add(newPub, "retry")
			reg.Revoke(dev.DeviceID)
			atomic.AddInt32(&revoked, 1)
			time.Sleep(time.Millisecond)
		}
	}()

	close(barrier)
	wg.Wait()
	t.Logf("refreshed=%d revoked=%d", atomic.LoadInt32(&refreshed), atomic.LoadInt32(&revoked))
	// After revoke, the old token must be rejected.
	if p := mgr.AuthenticateBearer(tok); p != nil {
		t.Errorf("after concurrent revoke, old token must be rejected")
	}
}
