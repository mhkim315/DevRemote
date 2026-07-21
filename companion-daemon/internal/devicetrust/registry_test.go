package devicetrust

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newReg(t *testing.T) (*DeviceRegistry, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "devices.json")
	r, err := NewDeviceRegistry(&FileDeviceStore{Path: path})
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	return r, path
}

func TestRegistry_AddListRevokePersist(t *testing.T) {
	r, path := newReg(t)
	pub := genPubDER(t)

	d, err := r.Add(pub, "My Phone")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if d.Role != RoleOwner {
		t.Fatalf("first device role = %q, want owner", d.Role)
	}
	if _, ok := r.GetActive(d.DeviceID); !ok {
		t.Fatalf("added device not active")
	}

	// Reload from disk — persistence.
	r2, err := NewDeviceRegistry(&FileDeviceStore{Path: path})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got, ok := r2.GetActive(d.DeviceID); !ok || got.Fingerprint != d.Fingerprint {
		t.Fatalf("device not persisted across reload")
	}

	// Revoke + reload — stays revoked, excluded from active lookup.
	if err := r2.Revoke(d.DeviceID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	r3, _ := NewDeviceRegistry(&FileDeviceStore{Path: path})
	if _, ok := r3.GetActive(d.DeviceID); ok {
		t.Fatalf("revoked device still active after reload")
	}
	if _, ok := r3.GetActiveByFingerprint(d.Fingerprint); ok {
		t.Fatalf("revoked device found by fingerprint")
	}
	if list := r3.List(); len(list) != 1 || !list[0].Revoked() {
		t.Fatalf("revoked device not present-but-revoked in list: %+v", list)
	}
}

func TestRegistry_FirstOwnerThenMember(t *testing.T) {
	r, _ := newReg(t)
	a, _ := r.Add(genPubDER(t), "a")
	b, _ := r.Add(genPubDER(t), "b")
	if a.Role != RoleOwner || b.Role != RoleMember {
		t.Fatalf("roles = %q/%q, want owner/member", a.Role, b.Role)
	}
}

func TestRegistry_DuplicateDeterministic(t *testing.T) {
	r, _ := newReg(t)
	pub := genPubDER(t)
	d1, _ := r.Add(pub, "first")
	d2, err := r.Add(pub, "second")
	if err != nil {
		t.Fatalf("dup add: %v", err)
	}
	if d1.DeviceID != d2.DeviceID {
		t.Fatalf("same key produced different device IDs")
	}
	if len(r.List()) != 1 {
		t.Fatalf("duplicate key created a second record")
	}
}

func TestRegistry_RevokedKeyNotResurrected(t *testing.T) {
	r, _ := newReg(t)
	pub := genPubDER(t)
	d, _ := r.Add(pub, "p")
	r.Revoke(d.DeviceID)
	if _, err := r.Add(pub, "p again"); !errors.Is(err, ErrDeviceRevoked) {
		t.Fatalf("re-add of revoked key err = %v, want ErrDeviceRevoked", err)
	}
	if _, ok := r.GetActive(d.DeviceID); ok {
		t.Fatalf("revoked device became active after re-add")
	}
}

func TestRegistry_NonP256Rejected(t *testing.T) {
	r, _ := newReg(t)
	if _, err := r.Add([]byte("not a key"), "x"); !errors.Is(err, ErrNotP256) {
		t.Fatalf("add non-P256 err = %v, want ErrNotP256", err)
	}
	if len(r.List()) != 0 {
		t.Fatalf("invalid key created a device record")
	}
}

func TestRegistry_RevokeUnknown(t *testing.T) {
	r, _ := newReg(t)
	if err := r.Revoke("nope"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("revoke unknown err = %v, want ErrDeviceNotFound", err)
	}
}

func TestRegistry_CorruptStoreFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	if err := os.WriteFile(path, []byte("[ {bad"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := NewDeviceRegistry(&FileDeviceStore{Path: path}); !errors.Is(err, ErrCorruptStore) {
		t.Fatalf("corrupt registry err = %v, want ErrCorruptStore (no silent reset)", err)
	}
	// Corrupt file must not be overwritten to empty.
	if raw, _ := os.ReadFile(path); string(raw) != "[ {bad" {
		t.Fatalf("corrupt registry silently reset/overwritten")
	}
}

func TestRegistry_InsecurePermissionsFailClosed(t *testing.T) {
	r, path := newReg(t)
	r.Add(genPubDER(t), "p") // creates the file
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if _, err := NewDeviceRegistry(&FileDeviceStore{Path: path}); !errors.Is(err, ErrInsecurePermissions) {
		t.Fatalf("insecure-perm registry err = %v, want ErrInsecurePermissions", err)
	}
}

func TestRegistry_PublicDTONoKeyMaterial(t *testing.T) {
	r, _ := newReg(t)
	d, _ := r.Add(genPubDER(t), "phone")
	raw, _ := json.Marshal(d.Public())
	low := strings.ToLower(string(raw))
	if strings.Contains(low, "private") || strings.Contains(low, "publickey") {
		t.Fatalf("public device DTO leaks key material: %s", raw)
	}
	if !strings.Contains(string(raw), d.Fingerprint) {
		t.Fatalf("public device DTO missing fingerprint")
	}
}

// ── Owner Recovery ──

func TestRecoverOwner_Success(t *testing.T) {
	r, path := newReg(t)
	owner, _ := r.Add(genPubDER(t), "Old Owner")
	member, _ := r.Add(genPubDER(t), "Target")

	if owner.Role != RoleOwner || member.Role != RoleMember {
		t.Fatalf("setup: roles = %q/%q, want owner/member", owner.Role, member.Role)
	}

	if err := r.RecoverOwner(owner.DeviceID, member.DeviceID); err != nil {
		t.Fatalf("recover: %v", err)
	}

	// Old owner is now revoked.
	if d, ok := r.GetActive(owner.DeviceID); ok {
		t.Fatalf("old owner still active: %+v", d)
	}
	// Target is now owner.
	d, ok := r.GetActive(member.DeviceID)
	if !ok {
		t.Fatalf("target not active after promotion")
	}
	if d.Role != RoleOwner {
		t.Fatalf("target role = %q, want owner", d.Role)
	}
	// Exactly one active owner.
	ownerCount := 0
	for _, dev := range r.List() {
		if !dev.Revoked() && dev.Role == RoleOwner {
			ownerCount++
		}
	}
	if ownerCount != 1 {
		t.Fatalf("owner count = %d, want 1", ownerCount)
	}

	// Persistence round-trip.
	r2, err := NewDeviceRegistry(&FileDeviceStore{Path: path})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := r2.GetActive(owner.DeviceID); ok {
		t.Fatalf("old owner active after reload")
	}
	if d2, ok := r2.GetActive(member.DeviceID); !ok || d2.Role != RoleOwner {
		t.Fatalf("target not owner after reload: role=%q active=%v", d2.Role, ok)
	}
}

func TestRecoverOwner_OldOwnerNotFound(t *testing.T) {
	r, _ := newReg(t)
	owner, _ := r.Add(genPubDER(t), "Owner")
	member, _ := r.Add(genPubDER(t), "Target")
	_ = owner

	if err := r.RecoverOwner("nonexistent", member.DeviceID); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("err = %v, want ErrDeviceNotFound", err)
	}
	// Target must not be promoted.
	d, _ := r.GetActive(member.DeviceID)
	if d.Role != RoleMember {
		t.Fatalf("target unexpectedly promoted to %q", d.Role)
	}
}

func TestRecoverOwner_TargetNotFound(t *testing.T) {
	r, _ := newReg(t)
	owner, _ := r.Add(genPubDER(t), "Owner")

	if err := r.RecoverOwner(owner.DeviceID, "nonexistent"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("err = %v, want ErrDeviceNotFound", err)
	}
	// Owner must not be revoked.
	d, _ := r.GetActive(owner.DeviceID)
	if d.Revoked() {
		t.Fatalf("owner unexpectedly revoked")
	}
}

func TestRecoverOwner_OldOwnerAlreadyRevoked(t *testing.T) {
	r, _ := newReg(t)
	owner, _ := r.Add(genPubDER(t), "Owner")
	r.Revoke(owner.DeviceID)
	member, _ := r.Add(genPubDER(t), "Member")

	if err := r.RecoverOwner(owner.DeviceID, member.DeviceID); !errors.Is(err, ErrDeviceRevoked) {
		t.Fatalf("err = %v, want ErrDeviceRevoked", err)
	}
}

func TestRecoverOwner_OldOwnerIsMember(t *testing.T) {
	r, _ := newReg(t)
	owner, _ := r.Add(genPubDER(t), "Owner")
	member, _ := r.Add(genPubDER(t), "Member")

	// Try to use member as the "old owner".
	if err := r.RecoverOwner(member.DeviceID, owner.DeviceID); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("err = %v, want ErrNotOwner", err)
	}
}

func TestRecoverOwner_TargetAlreadyOwner(t *testing.T) {
	r, _ := newReg(t)
	owner, _ := r.Add(genPubDER(t), "Owner")

	if err := r.RecoverOwner(owner.DeviceID, owner.DeviceID); !errors.Is(err, ErrAlreadyOwner) {
		t.Fatalf("err = %v, want ErrAlreadyOwner", err)
	}
	// Owner must still be active.
	if _, ok := r.GetActive(owner.DeviceID); !ok {
		t.Fatalf("owner erroneously affected")
	}
}

func TestRecoverOwner_TargetRevoked(t *testing.T) {
	r, _ := newReg(t)
	owner, _ := r.Add(genPubDER(t), "Owner")
	member, _ := r.Add(genPubDER(t), "Member")
	r.Revoke(member.DeviceID)

	if err := r.RecoverOwner(owner.DeviceID, member.DeviceID); !errors.Is(err, ErrDeviceRevoked) {
		t.Fatalf("err = %v, want ErrDeviceRevoked", err)
	}
}

func TestRecoverOwner_RollbackOnSaveFailure(t *testing.T) {
	r, _ := newReg(t)
	owner, _ := r.Add(genPubDER(t), "Old Owner")
	member, _ := r.Add(genPubDER(t), "Target")

	// Simulate save failure: corrupt the underlying file to make it unwritable
	// by removing write permission from the parent directory. This forces
	// saveLocked (which calls writeOwnerOnly → os.WriteFile) to fail.
	r.store = &failingStore{inner: r.store, failOn: "save"}

	err := r.RecoverOwner(owner.DeviceID, member.DeviceID)
	if err == nil {
		t.Fatalf("expected save failure, got nil")
	}

	// In-memory state must be rolled back.
	dOwner, _ := r.GetActive(owner.DeviceID)
	if dOwner.Revoked() {
		t.Fatalf("old owner was not rolled back: revoked=%v", dOwner.Revoked())
	}
	dMember, _ := r.GetActive(member.DeviceID)
	if dMember.Role != RoleMember {
		t.Fatalf("target was not rolled back: role=%q", dMember.Role)
	}
}

// failingStore wraps a DeviceStore and injects failures for Load or Save.
type failingStore struct {
	inner  DeviceStore
	failOn string
}

func (f *failingStore) Load() ([]Device, error) {
	if f.failOn == "load" {
		return nil, errors.New("injected load failure")
	}
	return f.inner.Load()
}

func (f *failingStore) Save(recs []Device) error {
	if f.failOn == "save" {
		return errors.New("injected save failure")
	}
	return f.inner.Save(recs)
}

func TestRecoverOwner_ConcurrentOnlyOneWinner(t *testing.T) {
	r, _ := newReg(t)
	owner, _ := r.Add(genPubDER(t), "Owner")
	m1, _ := r.Add(genPubDER(t), "Member-1")
	m2, _ := r.Add(genPubDER(t), "Member-2")

	// Two goroutines try to recover to different targets concurrently.
	// Only one should succeed because RecoverOwner takes a write lock and
	// verifies the old owner is still active owner at entry.
	var wg sync.WaitGroup
	var won1, won2 bool
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := r.RecoverOwner(owner.DeviceID, m1.DeviceID); err == nil {
			won1 = true
		}
	}()
	go func() {
		defer wg.Done()
		if err := r.RecoverOwner(owner.DeviceID, m2.DeviceID); err == nil {
			won2 = true
		}
	}()
	wg.Wait()

	if won1 && won2 {
		t.Fatalf("both recoveries claimed success — atomicity violated")
	}
	if !won1 && !won2 {
		t.Fatalf("neither recovery succeeded")
	}
	// Exactly one owner.
	ownerCount := 0
	for _, d := range r.List() {
		if !d.Revoked() && d.Role == RoleOwner {
			ownerCount++
		}
	}
	if ownerCount != 1 {
		t.Fatalf("owner count = %d, want 1", ownerCount)
	}
	// Old owner must be revoked.
	if _, ok := r.GetActive(owner.DeviceID); ok {
		t.Fatalf("old owner still active after recovery")
	}
}

func TestRegistry_ConcurrentOps(t *testing.T) {
	r, _ := newReg(t)
	// Seed a device to revoke concurrently.
	seed, _ := r.Add(genPubDER(t), "seed")

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pub := genPubDER(t)
			r.Add(pub, "d")
			r.GetActiveByFingerprint(Fingerprint(pub))
			r.List()
			r.TouchLastSeen(seed.DeviceID)
			r.Revoke(seed.DeviceID)
		}()
	}
	wg.Wait()
	if _, ok := r.GetActive(seed.DeviceID); ok {
		t.Fatalf("seed device should be revoked after concurrent ops")
	}
	if len(r.List()) == 0 {
		t.Fatalf("registry empty after concurrent adds")
	}
}
