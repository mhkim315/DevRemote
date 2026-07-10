package devicetrust

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func genPubDER(t *testing.T) []byte {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return der
}

func TestHostIdentity_CreateLoadStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	store := &FileKeyStore{Path: path}

	a, err := LoadOrCreateHostIdentity(store)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	b, err := LoadOrCreateHostIdentity(store)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if a.HostID != b.HostID || a.Fingerprint() != b.Fingerprint() {
		t.Fatalf("identity not stable across load: %s/%s vs %s/%s", a.HostID, a.Fingerprint(), b.HostID, b.Fingerprint())
	}
	if a.HostID == "" || a.KeyVersion != 1 {
		t.Fatalf("bad identity fields: %+v", a)
	}
}

func TestHostIdentity_DistinctInstalls(t *testing.T) {
	a, _ := LoadOrCreateHostIdentity(&FileKeyStore{Path: filepath.Join(t.TempDir(), "id.json")})
	b, _ := LoadOrCreateHostIdentity(&FileKeyStore{Path: filepath.Join(t.TempDir(), "id.json")})
	if a.HostID == b.HostID || a.Fingerprint() == b.Fingerprint() {
		t.Fatalf("distinct installs produced the same identity")
	}
}

func TestHostIdentity_SignVerifyRoundTrip(t *testing.T) {
	id, _ := LoadOrCreateHostIdentity(&FileKeyStore{Path: filepath.Join(t.TempDir(), "id.json")})
	msg := []byte("pair-challenge-nonce")
	sig, err := id.Sign(msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if !VerifySignature(id.PublicKeyDER(), msg, sig) {
		t.Fatalf("valid signature failed to verify")
	}
	if VerifySignature(id.PublicKeyDER(), []byte("tampered"), sig) {
		t.Fatalf("signature verified against a different message")
	}
	if VerifySignature(genPubDER(t), msg, sig) {
		t.Fatalf("signature verified against a different public key")
	}
}

func TestHostIdentity_OwnerOnlyModeAndAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "id.json")
	if _, err := LoadOrCreateHostIdentity(&FileKeyStore{Path: path}); err != nil {
		t.Fatalf("create: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("identity file mode = %o, want 0600", fi.Mode().Perm())
	}
	// No leftover temp files from the atomic write.
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".pokit-tmp-") {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}

func TestHostIdentity_CorruptFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "id.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := LoadOrCreateHostIdentity(&FileKeyStore{Path: path})
	if !errors.Is(err, ErrCorruptStore) {
		t.Fatalf("corrupt load err = %v, want ErrCorruptStore (fail closed, no reset)", err)
	}
	// The corrupt file must NOT have been overwritten with a fresh identity.
	raw, _ := os.ReadFile(path)
	if string(raw) != "{not valid json" {
		t.Fatalf("corrupt identity was silently overwritten")
	}
}

func TestHostIdentity_InsecurePermissionsFailClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "id.json")
	if _, err := LoadOrCreateHostIdentity(&FileKeyStore{Path: path}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	_, err := (&FileKeyStore{Path: path}).Load()
	if !errors.Is(err, ErrInsecurePermissions) {
		t.Fatalf("insecure-perm load err = %v, want ErrInsecurePermissions", err)
	}
}

func TestHostIdentity_PublicDTONoPrivateKey(t *testing.T) {
	id, _ := LoadOrCreateHostIdentity(&FileKeyStore{Path: filepath.Join(t.TempDir(), "id.json")})
	raw, err := json.Marshal(id.Public())
	if err != nil {
		t.Fatalf("marshal public: %v", err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "private") {
		t.Fatalf("public host identity DTO leaks a private field: %s", raw)
	}
	// Sanity: fingerprint present.
	if !strings.Contains(string(raw), id.Fingerprint()) {
		t.Fatalf("public DTO missing fingerprint")
	}
}
