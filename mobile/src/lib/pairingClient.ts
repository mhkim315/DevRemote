// M3-auth-2A: typed pairing client — conducts the pairing protocol against a
// host daemon. The daemon's protocol (pairing.go) is authoritative.

import type { PokitDeviceKey } from '../../modules/pokit-device-key';
import { toBase64, toHex, fromHex, fromBase64, deviceFingerprint, verifyDer } from './crypto';
import * as Crypto from 'expo-crypto';
import { parsePairingQR } from './qrParser';
import { ensureLegacyKeyRemoved } from './deviceIdentity';

export type PairingStatus =
  | 'malformed_qr' | 'qr_expired' | 'unsafe_endpoint'
  | 'network_error' | 'phase1_rejected' | 'host_fingerprint_mismatch'
  | 'phase2_rejected' | 'host_proof_invalid' | 'operator_rejected'
  | 'pairing_expired' | 'approved';

export interface PairingResult {
  status: PairingStatus;
  hostId?: string; hostPubKeyB64?: string;
  deviceId?: string; fingerprint?: string; role?: string;
  baseURL?: string; errorDetail?: string;
}

// buildPairingTranscript matches the daemon's buildPairingTranscript byte-exact.
export function buildPairingTranscript(
  phoneNonce: Uint8Array, hostNonce: Uint8Array,
  hostPubDER: Uint8Array, sessionId: string,
  hostId = '', daemonBootId = '', challengeId = '', expiresAt = '',
): Uint8Array {
  const prefix = new TextEncoder().encode('pokit-pair-v1:');
  const sid = new TextEncoder().encode(sessionId);
  const binding = [hostId, daemonBootId, challengeId, expiresAt].map(v => new TextEncoder().encode(v));
  const boundLength = binding.every(v => v.length === 0) ? 0 : binding.reduce((n, v) => n + 1 + v.length, 0);
  const out = new Uint8Array(prefix.length + phoneNonce.length + hostNonce.length + hostPubDER.length + sid.length + boundLength);
  let o = 0;
  out.set(prefix, o); o += prefix.length;
  out.set(phoneNonce, o); o += phoneNonce.length;
  out.set(hostNonce, o); o += hostNonce.length;
  out.set(hostPubDER, o); o += hostPubDER.length;
  out.set(sid, o);
  o += sid.length;
  for (const value of binding) {
    if (boundLength === 0) break;
    out[o++] = 0;
    out.set(value, o); o += value.length;
  }
  return out;
}

// Go's time.RFC3339 emits whole seconds when the timestamp has no fractional
// component. Pairing transcripts must use that exact representation, not the
// always-millisecond JavaScript ISO form.
export function formatGoRFC3339(date: Date): string {
  return date.toISOString().replace(/\.\d{3}Z$/, 'Z');
}

// siblingURL derives /pair/confirm and /pair/result from a QR endpoint that
// already ends in "/pair" (the daemon's QR canonical endpoint).
function siblingURL(endpoint: string, path: string): string {
  const base = endpoint.slice(0, -'/pair'.length);
  return base + path;
}

// pairingPOST returns res.json(), checking the HTTP status first.
// Pairing V1 contract: LAN HTTP only, no redirects, exact /pair path.
async function pairingPOST(url: string, body: unknown): Promise<any> {
  const res = await fetch(url, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    redirect: 'error', // Pairing V1: no HTTP redirects
  });
  if (!res.ok) return { error: `server returned ${res.status}` };
  try { return await res.json(); } catch { return { error: 'non-JSON response' }; }
}

export async function conductPairing(
  qrRaw: unknown, deviceKey: PokitDeviceKey,
): Promise<PairingResult> {
  const qr = parsePairingQR(qrRaw);
  if ('error' in qr) return { status: 'malformed_qr', errorDetail: qr.error };
  if (qr.expiresAt <= new Date()) return { status: 'qr_expired' };

  // Device identity. PROVISION the hardware key so a fresh install or a post-wipe
  // re-pair works: remove any legacy JS software key (fail-closed), then
  // ensureKey() creates-or-returns the Secure Enclave / Keystore identity. An
  // inaccessible/incompatible EXISTING key stays a typed failure — ensureKey does
  // not delete or regenerate a pre-existing identity — so we never silently
  // rotate a paired key. Runs AFTER strict QR validation, BEFORE any network I/O.
  let identity: { deviceId: string; publicKeySpki: Uint8Array };
  try {
    await ensureLegacyKeyRemoved();
    identity = await deviceKey.ensureKey();
  } catch {
    return { status: 'network_error', errorDetail: 'device identity unavailable' };
  }

  const hostPubDER = qr.hostPubKeyDer; // 91B SPKI validated by QR parser
  const sessionId = qr.sessionId;

  // 1. Phase 1 — POST to endpoint (QR endpoint IS the full URL "http://host:port/pair").
  const phoneNonce = await Crypto.getRandomBytesAsync(32);
  let p1: any;
  try {
    p1 = await pairingPOST(qr.endpoint, {
      publicKey: toBase64(identity.publicKeySpki),
      displayName: 'Pokit Mobile',
      phoneNonce: toBase64(phoneNonce),
      bootstrapToken: qr.bootstrapToken,
      protocolVersion: qr.protocolVersion,
      hostId: qr.hostId,
      daemonBootId: qr.daemonBootId,
      challengeId: qr.challengeId,
      expiresAt: formatGoRFC3339(qr.expiresAt),
    });
  } catch { return { status: 'network_error', errorDetail: 'phase 1 request failed' }; }
  if (p1.error || !p1.hostNonce || !p1.hostPublicKey) {
    return { status: 'phase1_rejected', errorDetail: p1.error || 'incomplete' };
  }
  // Decode daemon-returned JSON byte[] fields (base64 on the wire).
  let hostNonce: Uint8Array;
  let hostPubKeyFromPhase1: Uint8Array;
  try {
    hostNonce = fromBase64(p1.hostNonce);
    hostPubKeyFromPhase1 = fromBase64(p1.hostPublicKey);
  } catch { return { status: 'phase1_rejected', errorDetail: 'invalid host nonce/pub' }; }

  // 2. Pin-check: host fingerprint must match QR.
  if (deviceFingerprint(hostPubKeyFromPhase1) !== qr.fingerprint) {
    return { status: 'host_fingerprint_mismatch' };
  }

  // 3. Phase 2 — POST /pair/confirm.
  const expiresAt = formatGoRFC3339(qr.expiresAt);
  const transcript = buildPairingTranscript(phoneNonce, hostNonce, hostPubDER, sessionId, qr.hostId, qr.daemonBootId, qr.challengeId, expiresAt);
  let phoneSig: Uint8Array;
  try { phoneSig = await deviceKey.sign(transcript); } catch {
    return { status: 'network_error', errorDetail: 'device signing failed' };
  }
  let p2: any;
  try {
    p2 = await pairingPOST(siblingURL(qr.endpoint, '/pair/confirm'), {
      phoneSignature: toBase64(phoneSig),
      protocolVersion: qr.protocolVersion,
      hostId: qr.hostId,
      daemonBootId: qr.daemonBootId,
      challengeId: qr.challengeId,
      expiresAt,
    });
  } catch { return { status: 'network_error', errorDetail: 'phase 2 request failed' }; }
  if (p2.error || p2.status !== 'proof_verified') {
    return { status: 'phase2_rejected', errorDetail: p2.error || 'proof not verified' };
  }

  // 4. Host-proof — daemon returns hostProof as HEX.
  if (p2.hostProof) {
    try {
      const hostProofDER = fromHex(p2.hostProof);
      // verifyDer needs the 65-byte uncompressed point (drop 26B SPKI prefix).
      const hostPubPoint = hostPubKeyFromPhase1.length === 91 ? hostPubKeyFromPhase1.subarray(26) : hostPubKeyFromPhase1;
      // verifyDer has prehash:true — hashes transcript internally once
      // to match daemon's Sign(sha256(transcript)).
      if (!verifyDer(hostProofDER, transcript, hostPubPoint)) {
        return { status: 'host_proof_invalid' };
      }
    } catch { return { status: 'host_proof_invalid', errorDetail: 'host proof decode failed' }; }
  } else {
    return { status: 'host_proof_invalid', errorDetail: 'host proof missing' };
  }

  // 5. Poll for operator approval. Redirect on the result path is TERMINAL:
  //    a detected redirect poisons the poll loop — it may continue running
  //    until expiry but must never return 'approved'.
  const resultURL = siblingURL(qr.endpoint, '/pair/result') + '?session=' + encodeURIComponent(sessionId);
  const deadline = qr.expiresAt.getTime();
  let redirectDetected = false;
  for (;;) {
    if (Date.now() > deadline) return { status: 'pairing_expired' };
    let poll: any;
    try {
      const res = await fetch(resultURL, { redirect: 'error' }); // Pairing V1: no HTTP redirects
      if (!res.ok) { await delay(2000); continue; }
      poll = await res.json();
    } catch {
      // fetch with redirect:'error' throws TypeError on redirect.
      // Mark the session as poison — no subsequent response may produce approval.
      redirectDetected = true;
      await delay(2000); continue;
    }
    // Redirect-poisoned polls can never yield approval.
    if (redirectDetected && poll?.status === 'approved') continue;
    switch (poll.status) {
      case 'approved': {
        if (poll.deviceId !== identity.deviceId) {
          return { status: 'network_error', errorDetail: 'approved deviceId does not match the presented identity' };
        }
        if (poll.fingerprint !== identity.deviceId) {
          return { status: 'network_error', errorDetail: 'approved fingerprint does not match' };
        }
        if (poll.role !== 'owner' && poll.role !== 'member') {
          return { status: 'network_error', errorDetail: `unknown role: ${poll.role}` };
        }
        return {
          status: 'approved',
          hostId: qr.hostId, hostPubKeyB64: qr.hostPubKeyB64,
          deviceId: poll.deviceId, fingerprint: poll.fingerprint, role: poll.role,
          baseURL: qr.endpoint,
        };
      }
      case 'rejected': return { status: 'operator_rejected' };
      case 'expired': return { status: 'pairing_expired' };
    }
    await delay(2000);
  }
}

function delay(ms: number): Promise<void> { return new Promise(r => setTimeout(r, ms)); }
