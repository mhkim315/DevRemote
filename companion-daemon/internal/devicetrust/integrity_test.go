package devicetrust

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// ── BLOCKER 1: returned values must not share internal mutable state ──

func TestRegistry_ReturnedDevicesAreIsolated(t *testing.T) {
	r, _ := newReg(t)
	pub := genPubDER(t)
	added, err := r.Add(pub, "p")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	// Mutate the returned copy's slice + pointer fields.
	added.PublicKeyDER[0] ^= 0xFF
	rev := time.Unix(1, 0).UTC()
	added.RevokedAt = &rev

	got, ok := r.GetActive(added.DeviceID)
	if !ok {
		t.Fatalf("device became inactive via returned-value mutation")
	}
	if !bytes.Equal(got.PublicKeyDER, pub) {
		t.Fatalf("mutating returned PublicKeyDER changed the registry key")
	}
	if got.Revoked() {
		t.Fatalf("mutating returned RevokedAt revoked the internal device")
	}

	// List() results must be isolated too.
	list := r.List()
	list[0].PublicKeyDER[0] ^= 0xFF
	rev2 := time.Unix(2, 0).UTC()
	list[0].RevokedAt = &rev2
	got2, _ := r.GetActive(added.DeviceID)
	if !bytes.Equal(got2.PublicKeyDER, pub) || got2.Revoked() {
		t.Fatalf("mutating List() result changed the registry")
	}
}

// ── BLOCKER 2: identity load fails closed on inconsistency ──

func TestHostIdentity_KeypairMismatchFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "id.json")
	p1, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	p2, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pkcs8, _ := x509.MarshalPKCS8PrivateKey(p1)
	wrongPub, _ := x509.MarshalPKIXPublicKey(&p2.PublicKey)
	data := &hostIdentityData{
		Version: hostIdentityVersion, HostID: "h", KeyVersion: 1,
		CreatedAt: time.Now().UTC(), PublicKeyDER: wrongPub, PrivateKeyPKCS8: pkcs8,
	}
	if err := (&FileKeyStore{Path: path}).Save(data); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := LoadOrCreateHostIdentity(&FileKeyStore{Path: path}); !errors.Is(err, ErrCorruptStore) {
		t.Fatalf("keypair mismatch err = %v, want ErrCorruptStore", err)
	}
}

func TestHostIdentity_BadMetadataFailsClosed(t *testing.T) {
	mk := func(mut func(d *hostIdentityData)) *hostIdentityData {
		priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		pkcs8, _ := x509.MarshalPKCS8PrivateKey(priv)
		pub, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
		d := &hostIdentityData{Version: hostIdentityVersion, HostID: "h", KeyVersion: 1, CreatedAt: time.Now().UTC(), PublicKeyDER: pub, PrivateKeyPKCS8: pkcs8}
		mut(d)
		return d
	}
	cases := map[string]func(*hostIdentityData){
		"empty hostId":   func(d *hostIdentityData) { d.HostID = "" },
		"zero keyVer":    func(d *hostIdentityData) { d.KeyVersion = 0 },
		"zero createdAt": func(d *hostIdentityData) { d.CreatedAt = time.Time{} },
	}
	for name, mut := range cases {
		path := filepath.Join(t.TempDir(), "id.json")
		(&FileKeyStore{Path: path}).Save(mk(mut))
		if _, err := LoadOrCreateHostIdentity(&FileKeyStore{Path: path}); !errors.Is(err, ErrCorruptStore) {
			t.Fatalf("%s: err = %v, want ErrCorruptStore", name, err)
		}
	}
}

// ── BLOCKER 2: registry load fails closed on inconsistency ──

func validDevice(t *testing.T) Device {
	t.Helper()
	pub := genPubDER(t)
	fp := Fingerprint(pub)
	now := time.Now().UTC()
	return Device{Version: deviceRecordVersion, DeviceID: fp, PublicKeyDER: pub, Fingerprint: fp, DisplayName: "d", Role: RoleOwner, CreatedAt: now, LastSeenAt: now}
}

func TestRegistry_CorruptRecordFailsClosed(t *testing.T) {
	cases := map[string]func(*Device){
		"fingerprint mismatch": func(d *Device) { d.Fingerprint = "deadbeef" },
		"deviceId mismatch":    func(d *Device) { d.DeviceID = "not-the-fingerprint" },
		"invalid role":         func(d *Device) { d.Role = "superuser" },
		"zero createdAt":       func(d *Device) { d.CreatedAt = time.Time{} },
		"zero lastSeenAt":      func(d *Device) { d.LastSeenAt = time.Time{} },
		"non-P256 key":         func(d *Device) { d.PublicKeyDER = []byte("garbage") },
	}
	for name, mut := range cases {
		path := filepath.Join(t.TempDir(), "devices.json")
		d := validDevice(t)
		mut(&d)
		if err := (&FileDeviceStore{Path: path}).Save([]Device{d}); err != nil {
			t.Fatalf("%s: save: %v", name, err)
		}
		if _, err := NewDeviceRegistry(&FileDeviceStore{Path: path}); !errors.Is(err, ErrCorruptStore) {
			t.Fatalf("%s: registry load err = %v, want ErrCorruptStore", name, err)
		}
	}
}

func TestRegistry_DuplicateRecordFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	d := validDevice(t)
	if err := (&FileDeviceStore{Path: path}).Save([]Device{d, d}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := NewDeviceRegistry(&FileDeviceStore{Path: path}); !errors.Is(err, ErrCorruptStore) {
		t.Fatalf("duplicate record load err = %v, want ErrCorruptStore (not silent drop)", err)
	}
}
