// M3-auth-1: byte-exact P-256 crypto for the mobile device-auth client.
//
// Every byte here must match the Go daemon (companion-daemon/internal/
// devicetrust). Software keys via @noble/curves (no native module); the M2.5
// threat model explicitly defers hardware attestation and the daemon does not
// verify it. Codecs are pure (no Buffer/btoa) so they behave identically under
// Hermes/React Native and Node/jest.

import { p256 } from '@noble/curves/nist.js';
import { sha256 } from '@noble/hashes/sha2.js';

// Standard SubjectPublicKeyInfo DER prefix for a prime256v1 (P-256) uncompressed
// public key — exactly what Go's x509.MarshalPKIXPublicKey emits. Followed by
// the 65-byte uncompressed point (0x04 || X(32) || Y(32)) → 91 bytes total.
const SPKI_P256_PREFIX = Uint8Array.from([
  0x30, 0x59, 0x30, 0x13, 0x06, 0x07, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x02, 0x01,
  0x06, 0x08, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x03, 0x01, 0x07, 0x03, 0x42, 0x00,
]);

// ── keys ──

export function generatePrivateKey(): Uint8Array {
  return p256.utils.randomSecretKey();
}

// publicKeyUncompressed returns 0x04 || X(32) || Y(32) (65 bytes).
export function publicKeyUncompressed(privateKey: Uint8Array): Uint8Array {
  return p256.getPublicKey(privateKey, false);
}

// spkiDer wraps an uncompressed P-256 point in the SPKI DER encoding the daemon
// expects (ParseP256PublicKey). Input must be the 65-byte uncompressed point.
export function spkiDer(pubUncompressed: Uint8Array): Uint8Array {
  const out = new Uint8Array(SPKI_P256_PREFIX.length + pubUncompressed.length);
  out.set(SPKI_P256_PREFIX, 0);
  out.set(pubUncompressed, SPKI_P256_PREFIX.length);
  return out;
}

// deviceFingerprint = hex(sha256(spkiDer)) — the daemon's Fingerprint()/deviceId.
export function deviceFingerprint(spki: Uint8Array): string {
  return toHex(sha256(spki));
}

// ── device identity (pure derivation; storage lives in deviceIdentity.ts) ──

export interface DeviceIdentity {
  privateKey: Uint8Array;
  publicKeyUncompressed: Uint8Array;
  spkiDer: Uint8Array;
  deviceId: string; // hex fingerprint = daemon deviceId
}

// deriveIdentity computes the public identity from a private scalar. Pure, so it
// is unit-testable without the native secure store.
export function deriveIdentity(privateKey: Uint8Array): DeviceIdentity {
  const pub = publicKeyUncompressed(privateKey);
  const spki = spkiDer(pub);
  return { privateKey, publicKeyUncompressed: pub, spkiDer: spki, deviceId: deviceFingerprint(spki) };
}

// ── signatures (ASN.1 DER, as Go's ecdsa.VerifyASN1 requires) ──
//
// `prehash: true` makes @noble hash the message with sha256 internally, matching
// Go's ecdsa signing/verifying over sha256(message). The signature is
// deterministic (RFC 6979).

// signDer signs a message with P-256 and returns an ASN.1 DER signature.
export function signDer(privateKey: Uint8Array, message: Uint8Array): Uint8Array {
  return p256.sign(message, privateKey, { format: 'der', prehash: true });
}

// verifyDer verifies an ASN.1 DER signature over a message.
export function verifyDer(sigDer: Uint8Array, message: Uint8Array, pubUncompressed: Uint8Array): boolean {
  return p256.verify(sigDer, message, pubUncompressed, { format: 'der', prehash: true });
}

// ── pure codecs (Hermes/Node identical; base64 is standard with padding to
// match Go base64.StdEncoding used by JSON []byte fields) ──

export function toHex(u: Uint8Array): string {
  let s = '';
  for (let i = 0; i < u.length; i++) s += u[i].toString(16).padStart(2, '0');
  return s;
}

export function fromHex(h: string): Uint8Array {
  const out = new Uint8Array(h.length >> 1);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(h.substr(i * 2, 2), 16);
  return out;
}

const B64 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';

export function toBase64(u: Uint8Array): string {
  let out = '';
  let i = 0;
  for (; i + 3 <= u.length; i += 3) {
    const n = (u[i] << 16) | (u[i + 1] << 8) | u[i + 2];
    out += B64[(n >> 18) & 63] + B64[(n >> 12) & 63] + B64[(n >> 6) & 63] + B64[n & 63];
  }
  const rem = u.length - i;
  if (rem === 1) {
    const n = u[i] << 16;
    out += B64[(n >> 18) & 63] + B64[(n >> 12) & 63] + '==';
  } else if (rem === 2) {
    const n = (u[i] << 16) | (u[i + 1] << 8);
    out += B64[(n >> 18) & 63] + B64[(n >> 12) & 63] + B64[(n >> 6) & 63] + '=';
  }
  return out;
}

export function fromBase64(s: string): Uint8Array {
  const clean = s.replace(/[^A-Za-z0-9+/]/g, '');
  const out = new Uint8Array((clean.length * 3) >> 2);
  let o = 0;
  let buf = 0;
  let bits = 0;
  for (let i = 0; i < clean.length; i++) {
    buf = (buf << 6) | B64.indexOf(clean[i]);
    bits += 6;
    if (bits >= 8) {
      bits -= 8;
      out[o++] = (buf >> bits) & 0xff;
    }
  }
  return out.subarray(0, o);
}
