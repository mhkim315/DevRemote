// M3-auth-2A: typed pairing client — conducts the pairing protocol against a
// host daemon. The daemon's protocol is authoritative (pairing.go); this is the
// mobile side. Stateless — no UI, no storage; callers own persistence + UX.

import type { PokitDeviceKey } from '../../modules/pokit-device-key';
import { toBase64, toHex, fromBase64, deviceFingerprint, verifyDer } from './crypto';
import { parsePairingQR, type PairingQRPayload } from './qrParser';
// checkedFetch is inlined here (pairing needs no bearer)

export type PairingStatus =
  | 'malformed_qr'
  | 'qr_expired'
  | 'unsafe_endpoint'
  | 'network_error'
  | 'phase1_rejected'
  | 'host_fingerprint_mismatch'
  | 'phase2_rejected'
  | 'host_proof_invalid'
  | 'operator_rejected'
  | 'pairing_expired'
  | 'approved';

export interface PairingResult {
  status: PairingStatus;
  // Populated only on approved:
  hostId?: string;
  hostPubKeyB64?: string;
  deviceId?: string;
  fingerprint?: string;
  role?: string;
  baseURL?: string;
  errorDetail?: string;
}

// buildPairingTranscript matches the daemon's buildPairingTranscript byte-exact.
export function buildPairingTranscript(
  phoneNonce: Uint8Array, hostNonce: Uint8Array,
  hostPubDER: Uint8Array, sessionId: string,
): Uint8Array {
  const prefix = new TextEncoder().encode('pokit-pair-v1:');
  const sid = new TextEncoder().encode(sessionId);
  const out = new Uint8Array(prefix.length + phoneNonce.length + hostNonce.length + hostPubDER.length + sid.length);
  let o = 0;
  out.set(prefix, o); o += prefix.length;
  out.set(phoneNonce, o); o += phoneNonce.length;
  out.set(hostNonce, o); o += hostNonce.length;
  out.set(hostPubDER, o); o += hostPubDER.length;
  out.set(sid, o);
  return out;
}

// conductPairing runs the full protocol phases using a QR payload and the
// platform DeviceKey provider. Returns a typed result; the caller owns
// persistence of the result on success.
export async function conductPairing(
  qrRaw: unknown, deviceKey: PokitDeviceKey,
): Promise<PairingResult> {
  // 0. Parse + validate QR.
  const qr = parsePairingQR(qrRaw);
  if ('error' in qr) return { status: 'malformed_qr', errorDetail: qr.error };
  if (qr.expiresAt <= new Date()) return { status: 'qr_expired' };

  // 0b. Device identity.
  let identity: { deviceId: string; publicKeySpki: Uint8Array };
  try {
    identity = await deviceKey.getKeyInfo();
  } catch {
    return { status: 'network_error', errorDetail: 'device identity unavailable' };
  }

  const hostPubDER = qr.hostPubKeyDer;
  const sessionId = qr.sessionId;

  // 1. Phase 1: POST /pair.
  const phoneNonce = crypto.getRandomValues(new Uint8Array(32));
  let phase1Resp: { hostNonce?: string; hostPublicKey?: string; hostFingerprint?: string; error?: string };
  try {
    phase1Resp = await pairingFetch(qr.endpoint + '/pair', {
      publicKey: toBase64(identity.publicKeySpki),
      displayName: 'Pokit Mobile',
      phoneNonce: toBase64(phoneNonce),
      bootstrapToken: qr.bootstrapToken,
    });
  } catch {
    return { status: 'network_error', errorDetail: 'phase 1 request failed' };
  }
  if (phase1Resp.error || !phase1Resp.hostNonce || !phase1Resp.hostPublicKey || !phase1Resp.hostFingerprint) {
    return { status: 'phase1_rejected', errorDetail: phase1Resp.error || 'host response incomplete' };
  }
  let hostNonce: Uint8Array;
  let hostPubKeyFromPhase1: Uint8Array;
  try {
    hostNonce = fromBase64(phase1Resp.hostNonce);
    hostPubKeyFromPhase1 = fromBase64(phase1Resp.hostPublicKey);
  } catch {
    return { status: 'phase1_rejected', errorDetail: 'invalid host nonce or public key' };
  }

  // 2. Pin-check: the host public key fingerprint must match the QR.
  if (deviceFingerprint(hostPubKeyFromPhase1) !== qr.fingerprint) {
    return { status: 'host_fingerprint_mismatch' };
  }

  // 3. Phase 2: POST /pair/confirm.
  const transcript = buildPairingTranscript(phoneNonce, hostNonce, hostPubDER, sessionId);
  let phoneSignature: Uint8Array;
  try {
    phoneSignature = await deviceKey.sign(transcript);
  } catch {
    return { status: 'network_error', errorDetail: 'device signing failed' };
  }
  let phase2Resp: { status?: string; hostProof?: string; error?: string };
  try {
    phase2Resp = await pairingFetch(qr.endpoint + '/pair/confirm', {
      phoneSignature: toBase64(phoneSignature),
    });
  } catch {
    return { status: 'network_error', errorDetail: 'phase 2 request failed' };
  }
  if (phase2Resp.error || phase2Resp.status !== 'proof_verified') {
    return { status: 'phase2_rejected', errorDetail: phase2Resp.error || 'proof not verified' };
  }

  // 4. Host-proof: verify the host signed the SAME transcript.
  if (phase2Resp.hostProof) {
    try {
      const hostProofDER = fromBase64(phase2Resp.hostProof);
      if (!verifyDer(hostProofDER, transcript, hostPubKeyFromPhase1)) {
        return { status: 'host_proof_invalid' };
      }
    } catch {
      return { status: 'host_proof_invalid', errorDetail: 'host proof decode failed' };
    }
  } else {
    return { status: 'host_proof_invalid', errorDetail: 'host proof missing' };
  }

  // 5. Poll for operator approval.
  const deadline = qr.expiresAt.getTime();
  for (;;) {
    if (Date.now() > deadline) return { status: 'pairing_expired' };
    let poll: { status: string; deviceId?: string; fingerprint?: string; role?: string };
    try {
      poll = await pairingGet(qr.endpoint + '/pair/result');
    } catch {
      await sleep(2000);
      continue;
    }
    switch (poll.status) {
      case 'approved':
        return {
          status: 'approved',
          hostId: qr.hostId,
          hostPubKeyB64: qr.hostPubKeyB64,
          deviceId: poll.deviceId || identity.deviceId,
          fingerprint: poll.fingerprint,
          role: poll.role,
          baseURL: baseURLFromEndpoint(qr.endpoint),
        };
      case 'rejected':
        return { status: 'operator_rejected' };
      case 'expired':
        return { status: 'pairing_expired' };
    }
    await sleep(2000);
  }
}

// ── minimal checked-fetch for pairing (no bearer) ──

async function pairingFetch(url: string, body: unknown): Promise<any> {
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  return res.json();
}

async function pairingGet(url: string): Promise<any> {
  const res = await fetch(url);
  return res.json();
}

function sleep(ms: number): Promise<void> { return new Promise(r => setTimeout(r, ms)); }

function baseURLFromEndpoint(endpoint: string): string {
  try {
    const u = new URL(endpoint);
    return `https://<tunnel>`; // caller replaces with the actual tunnel/URL
  } catch { return ''; }
  // The tunnel URL is known locally (set via ConnectScreen); pairing just
  // records the host identity. The caller wires the base URL separately.
}
