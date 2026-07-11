package devicetrust

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"testing"
)

// TestMobileVectors proves byte-exact compatibility with the mobile device-auth
// client (M3-auth-1). The SPKI DER, deviceId fingerprint, message, and ASN.1
// signature below are produced by @noble/curves in the mobile jest test
// (mobile/__tests__/crypto.test.ts) from the fixed private scalar
// 0102..20. The daemon must accept them unchanged: any drift on either side
// fails here and there.
func TestMobileVectors(t *testing.T) {
	const (
		spkiHex = "3059301306072a8648ce3d020106082a8648ce3d03010703420004" +
			"515c3d6eb9e396b904d3feca7f54fdcd0cc1e997bf375dca515ad0a6c3b4035f" +
			"4536be3a50f318fbf9a5475902a221502bef0d57e08c53b2cc0a56f17d9f9354"
		wantFingerprint = "f1d59449b727165de732bf283338122b99628a615918fedc67d878fffcf47da7"
		message         = "pokit-mobile-vector-v1"
		sigDerHex       = "3044022001a7c80dc4afea65966c4e138bf1038a9bf21ee38ffdc09604533a12f6ecaf78" +
			"022035097449d58db7da4d8c95a8e86424273820699df463920dd60e9a7361c74bdd"
	)

	spki, err := hex.DecodeString(spkiHex)
	if err != nil {
		t.Fatalf("decode spki: %v", err)
	}

	// The mobile-produced SPKI DER parses as a P-256 key.
	pub, err := ParseP256PublicKey(spki)
	if err != nil {
		t.Fatalf("ParseP256PublicKey rejected mobile SPKI DER: %v", err)
	}

	// The deviceId the mobile derives (hex(sha256(SPKI))) matches Fingerprint.
	if got := Fingerprint(spki); got != wantFingerprint {
		t.Fatalf("Fingerprint = %s, want %s (mobile deviceId mismatch)", got, wantFingerprint)
	}

	// The mobile ASN.1 DER signature over sha256(message) verifies.
	sig, err := hex.DecodeString(sigDerHex)
	if err != nil {
		t.Fatalf("decode sig: %v", err)
	}
	digest := sha256.Sum256([]byte(message))
	if !ecdsa.VerifyASN1(pub, digest[:], sig) {
		t.Fatal("ecdsa.VerifyASN1 rejected the mobile-produced signature")
	}
}

// TestNativeSignatureInterop proves the daemon accepts a NON-DETERMINISTIC
// P-256 SHA256withECDSA / ASN.1 DER signature — the exact wire format the
// Android Keystore provider (M3-auth-1A) emits. It does not assert fixed
// signature bytes (native ECDSA is randomized) and transfers no private
// material; it only proves the public verification contract:
//   - a valid signature over a message verifies against the SPKI-derived key
//   - a modified message is rejected
//   - a different key is rejected
//   - a mutated signature is rejected
func TestNativeSignatureInterop(t *testing.T) {
	// A fresh key stands in for the Keystore key; only its public SPKI + a
	// signature it produced ever cross to the verifier — as on device.
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	spki, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal spki: %v", err)
	}
	if len(spki) != 91 {
		t.Fatalf("SPKI length %d, want 91", len(spki))
	}
	pub, err := ParseP256PublicKey(spki)
	if err != nil {
		t.Fatalf("ParseP256PublicKey: %v", err)
	}

	message := []byte("pokit-device-auth-v1 native challenge transcript")
	digest := sha256.Sum256(message)
	// SHA256withECDSA on the device = sign the SHA-256 digest, ASN.1 DER output.
	sig, err := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	// Valid signature verifies.
	if !ecdsa.VerifyASN1(pub, digest[:], sig) {
		t.Fatal("valid native-shaped signature was rejected")
	}
	// Modified message rejected.
	badDigest := sha256.Sum256([]byte("tampered message"))
	if ecdsa.VerifyASN1(pub, badDigest[:], sig) {
		t.Fatal("signature verified against a modified message")
	}
	// Different key rejected.
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if ecdsa.VerifyASN1(&other.PublicKey, digest[:], sig) {
		t.Fatal("signature verified against a different key")
	}
	// Mutated signature rejected.
	mutated := append([]byte(nil), sig...)
	mutated[len(mutated)-1] ^= 0x01
	if ecdsa.VerifyASN1(pub, digest[:], mutated) {
		t.Fatal("mutated signature verified")
	}

	// Two signatures over the same message differ (non-deterministic), yet both
	// verify — confirming the daemon must not rely on fixed signature bytes.
	sig2, _ := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	if hex.EncodeToString(sig) == hex.EncodeToString(sig2) {
		t.Fatal("expected non-deterministic ECDSA signatures to differ")
	}
	if !ecdsa.VerifyASN1(pub, digest[:], sig2) {
		t.Fatal("second native-shaped signature was rejected")
	}
}
