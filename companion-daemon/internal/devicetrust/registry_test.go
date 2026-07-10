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
