package devicetrust

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

const hostIdentityVersion = 1

// HostIdentity is the daemon's stable cryptographic identity. The private key
// stays in memory; it is never exposed via a public DTO, log, or error.
type HostIdentity struct {
	HostID     string
	KeyVersion int
	CreatedAt  time.Time
	priv       *ecdsa.PrivateKey
}

// PublicHostIdentity is the safe, private-key-free view for callers/UI.
type PublicHostIdentity struct {
	HostID       string    `json:"hostId"`
	KeyVersion   int       `json:"keyVersion"`
	Fingerprint  string    `json:"fingerprint"`
	PublicKeyDER []byte    `json:"publicKey"`
	CreatedAt    time.Time `json:"createdAt"`
}

// PublicKeyDER returns the canonical PKIX/DER encoding of the public key.
func (h *HostIdentity) PublicKeyDER() []byte {
	der, _ := x509.MarshalPKIXPublicKey(&h.priv.PublicKey)
	return der
}

// Fingerprint is the SHA-256 hex of the canonical public key encoding.
func (h *HostIdentity) Fingerprint() string { return Fingerprint(h.PublicKeyDER()) }

// Public returns the private-key-free identity view.
func (h *HostIdentity) Public() PublicHostIdentity {
	return PublicHostIdentity{
		HostID:       h.HostID,
		KeyVersion:   h.KeyVersion,
		Fingerprint:  h.Fingerprint(),
		PublicKeyDER: h.PublicKeyDER(),
		CreatedAt:    h.CreatedAt,
	}
}

// Sign returns an ASN.1 ECDSA signature over SHA-256(msg).
func (h *HostIdentity) Sign(msg []byte) ([]byte, error) {
	digest := sha256.Sum256(msg)
	return ecdsa.SignASN1(rand.Reader, h.priv, digest[:])
}

// Fingerprint returns the SHA-256 hex of a canonical public key DER.
func Fingerprint(pubDER []byte) string {
	sum := sha256.Sum256(pubDER)
	return hex.EncodeToString(sum[:])
}

// VerifySignature verifies an ASN.1 ECDSA signature over msg using a canonical
// P-256 public key DER. Returns false on any parse/verify failure.
func VerifySignature(pubDER, msg, sig []byte) bool {
	pub, err := ParseP256PublicKey(pubDER)
	if err != nil {
		return false
	}
	digest := sha256.Sum256(msg)
	return ecdsa.VerifyASN1(pub, digest[:], sig)
}

// ParseP256PublicKey decodes and validates a canonical P-256 public key DER.
func ParseP256PublicKey(pubDER []byte) (*ecdsa.PublicKey, error) {
	pk, err := x509.ParsePKIXPublicKey(pubDER)
	if err != nil {
		return nil, ErrNotP256
	}
	ec, ok := pk.(*ecdsa.PublicKey)
	if !ok || ec.Curve != elliptic.P256() {
		return nil, ErrNotP256
	}
	return ec, nil
}

// hostIdentityData is the serialized storage form (owner-only file / keystore).
// It holds private-key material and must never be exposed publicly.
type hostIdentityData struct {
	Version         int       `json:"version"`
	HostID          string    `json:"hostId"`
	KeyVersion      int       `json:"keyVersion"`
	CreatedAt       time.Time `json:"createdAt"`
	PublicKeyDER    []byte    `json:"publicKey"`
	PrivateKeyPKCS8 []byte    `json:"privateKey"`
}

// KeyStore persists the host identity. FileKeyStore is the MVP; a Keychain
// implementation can replace it without touching identity/pairing/auth code.
type KeyStore interface {
	// Load returns the stored identity, or ErrNoIdentity if none exists.
	Load() (*hostIdentityData, error)
	Save(*hostIdentityData) error
}

// LoadOrCreateHostIdentity loads the persisted identity, or creates and
// persists one on first run. Corruption or insecure storage fails closed and
// never silently overwrites an existing identity.
func LoadOrCreateHostIdentity(store KeyStore) (*HostIdentity, error) {
	data, err := store.Load()
	if errors.Is(err, ErrNoIdentity) {
		return createHostIdentity(store)
	}
	if err != nil {
		return nil, err // insecure permissions / corruption → fail closed
	}
	priv, perr := x509.ParsePKCS8PrivateKey(data.PrivateKeyPKCS8)
	if perr != nil {
		return nil, fmt.Errorf("%w: private key unreadable", ErrCorruptStore)
	}
	ec, ok := priv.(*ecdsa.PrivateKey)
	if !ok || ec.Curve != elliptic.P256() {
		return nil, fmt.Errorf("%w: private key not P-256", ErrCorruptStore)
	}
	return &HostIdentity{HostID: data.HostID, KeyVersion: data.KeyVersion, CreatedAt: data.CreatedAt, priv: ec}, nil
}

func createHostIdentity(store KeyStore) (*HostIdentity, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return nil, err
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, err
	}
	h := &HostIdentity{HostID: hex.EncodeToString(idBytes), KeyVersion: 1, CreatedAt: time.Now().UTC(), priv: priv}
	data := &hostIdentityData{
		Version:         hostIdentityVersion,
		HostID:          h.HostID,
		KeyVersion:      h.KeyVersion,
		CreatedAt:       h.CreatedAt,
		PublicKeyDER:    pubDER,
		PrivateKeyPKCS8: pkcs8,
	}
	if err := store.Save(data); err != nil {
		return nil, err
	}
	return h, nil
}

// FileKeyStore is the MVP owner-only file KeyStore.
type FileKeyStore struct{ Path string }

func (f *FileKeyStore) Load() (*hostIdentityData, error) {
	raw, err := readOwnerOnly(f.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoIdentity
		}
		return nil, err // insecure permissions → fail closed
	}
	var d hostIdentityData
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorruptStore, err)
	}
	if d.Version != hostIdentityVersion || len(d.PrivateKeyPKCS8) == 0 || len(d.PublicKeyDER) == 0 {
		return nil, fmt.Errorf("%w: missing identity fields", ErrCorruptStore)
	}
	return &d, nil
}

func (f *FileKeyStore) Save(d *hostIdentityData) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return writeOwnerOnly(f.Path, raw)
}
