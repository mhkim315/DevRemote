// M3-auth-1: byte-exact P-256 utilities (SPKI, fingerprint, verification, codecs).
//
// Private-key operations (generate, sign) are in the Android Keystore native
// module (PokitDeviceKey) and are deliberately absent here. These helpers are
// pure and work identically under Hermes and Node.

import { p256 } from '@noble/curves/nist.js';
import { sha256 } from '@noble/hashes/sha2.js';

// Standard SubjectPublicKeyInfo DER prefix for prime256v1 (P-256) uncompressed
// public key — exactly what Go's x509.MarshalPKIXPublicKey emits, and what
// Android Keystore's Certificate.getPublicKey().getEncoded() returns.
const SPKI_P256_PREFIX = Uint8Array.from([
  0x30, 0x59, 0x30, 0x13, 0x06, 0x07, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x02, 0x01,
  0x06, 0x08, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x03, 0x01, 0x07, 0x03, 0x42, 0x00,
]);

// ── device identity (pure derivation from public SPKI DER) ──

export interface DeviceIdentity {
  spkiDer: Uint8Array;       // 91-byte SPKI DER (daemon identity)
  deviceId: string;          // hex(sha256(spkiDer)) = daemon deviceId
}

// deriveIdentity computes the deviceId from a Keystore-provided SPKI DER hex string.
export function deriveIdentity(spkiHex: string): DeviceIdentity {
  const spki = fromHex(spkiHex);
  return { spkiDer: spki, deviceId: deviceFingerprint(spki) };
}

// deviceFingerprint = hex(sha256(spkiDer)) — the daemon's Fingerprint()/deviceId.
export function deviceFingerprint(spki: Uint8Array): string {
  return toHex(sha256(spki));
}

// ── verification (used in M3-auth-3 for host-signature pinning) ──

// verifyDer verifies an ASN.1 DER ECDSA signature over a raw message. The
// KeyStore signs with SHA256withECDSA (single hash), so verification must
// match: p256.verify with prehash:true.
export function verifyDer(sigDer: Uint8Array, message: Uint8Array, pubUncompressed: Uint8Array): boolean {
  // lowS: false — Go's ecdsa.SignASN1 produces both high-S and low-S signatures
  // (~50% each). Noble defaults to rejecting high-S (lowS: true). The protocol
  // transcript already includes nonces and session identity for uniqueness; S
  // malleability does not weaken the host-pin or device-auth security model.
  // BUG-001 Layer 3 / BUG-006A root cause.
  return p256.verify(sigDer, message, pubUncompressed, { format: 'der', prehash: true, lowS: false });
}

// ── strict fail-closed codecs (Hermes/Node identical) ──

export function toHex(u: Uint8Array): string {
  let s = '';
  for (let i = 0; i < u.length; i++) s += u[i].toString(16).padStart(2, '0');
  return s;
}

// fromHex decodes an even-length hex string. Non-hex chars, odd length, and
// mismatched expected byte length all reject — malicious input is never
// silently parsed or truncated. Pass expectedLen <= 0 to skip length check.
export function fromHex(h: string, expectedLen: number = -1): Uint8Array {
  if (h.length % 2 !== 0) throw new Error('hex string must have even length');
  for (let i = 0; i < h.length; i++) {
    const c = h.charCodeAt(i);
    if (!((c >= 48 && c <= 57) || (c >= 65 && c <= 70) || (c >= 97 && c <= 102))) {
      throw new Error(`invalid hex char '${h[i]}' at position ${i}`);
    }
  }
  const out = new Uint8Array(h.length >> 1);
  for (let i = 0; i < out.length; i++) {
    out[i] = (parseInt(h[i * 2], 16) << 4) | parseInt(h[i * 2 + 1], 16);
  }
  if (expectedLen >= 0 && out.length !== expectedLen) {
    throw new Error(`hex decoded ${out.length} bytes, expected ${expectedLen}`);
  }
  return out;
}

const B64 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';

// toBase64 encodes bytes as standard padded base64 (identical to Go base64.StdEncoding).
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

// fromBase64 decodes standard padded base64. Illegal characters (including
// whitespace and URL-safe alt chars '-' '_') are rejected — no silent strip.
// Non-canonical padding is rejected.
export function fromBase64(s: string): Uint8Array {
  if (s.length % 4 !== 0) throw new Error('base64 length must be a multiple of 4');
  // padding must only appear at the end and in the allowed counts.
  const padStart = s.indexOf('=');
  if (padStart >= 0) {
    if (padStart < s.length - 2) throw new Error('base64 padding in wrong position');
    if (s.length - padStart > 2) throw new Error('too many base64 pad chars');
    for (let i = padStart; i < s.length; i++) {
      if (s[i] !== '=') throw new Error('base64 pad chars must only be = at end');
    }
  }
  // canonical re-encode check forced after decode.
  const out = rawBase64Decode(s);
  if (toBase64(out) !== s) throw new Error('base64 is not canonical');
  return out;
}

function rawBase64Decode(s: string): Uint8Array {
  const clean = s.replace(/=+$/, '');
  const out = new Uint8Array((clean.length * 3) >> 2);
  let o = 0;
  let buf = 0;
  let bits = 0;
  for (let i = 0; i < clean.length; i++) {
    const idx = B64.indexOf(clean[i]);
    if (idx < 0) throw new Error(`invalid base64 char '${clean[i]}' at position ${i}`);
    buf = (buf << 6) | idx;
    bits += 6;
    if (bits >= 8) {
      bits -= 8;
      out[o++] = (buf >> bits) & 0xff;
    }
  }
  return out.subarray(0, o);
}
