// M3-auth-3A: device bearer lifecycle on top of the daemon's M2.5-3/4
// challenge-auth endpoints. Stateless; caller owns pairing metadata and the
// DeviceKey provider. Bearer is in-memory only — a restart re-challenges.

import type { PokitDeviceKey } from '../../modules/pokit-device-key';
import type { StoredPairing } from './pairingStore';
import * as Crypto from 'expo-crypto';
import { toHex, fromHex, fromBase64, verifyDer } from './crypto';
import { buildAuthTranscript } from './authTranscript';

export interface BearerState {
  token: string;
  expiresAt: Date;
  permissions: string[];
  sessionId: string;
}

export type AuthErrorCode =
  | 'network_error'
  | 'host_pin_fail'
  | 'challenge_rejected'
  | 'verify_rejected'
  | 'expired'
  | 'revoked';

export class AuthError extends Error {
  code: AuthErrorCode;
  statusCode?: number;
  constructor(code: AuthErrorCode, message?: string, statusCode?: number) {
    super(message || code);
    this.name = 'AuthError';
    this.code = code;
    this.statusCode = statusCode;
  }
}

const CHALLENGE_TTL_MS = 5 * 60 * 1000; // daemon hardcoded 5min

// authenticate runs the full challenge/verify flow against a paired daemon.
export async function authenticate(
  pairing: StoredPairing, deviceKey: PokitDeviceKey, baseURL: string,
): Promise<BearerState> {
  // pairingStore persists hostPubKey as canonical base64 (P-256 SPKI).
  let hostPubDER: Uint8Array;
  try { hostPubDER = fromBase64(pairing.hostPubKeyB64); } catch { throw new AuthError('host_pin_fail', 'invalid host key'); }
  const { publicKeySpki, deviceId } = await deviceKey.getKeyInfo().catch(() => { throw new AuthError('network_error', 'device identity unavailable'); });

  // 1. Challenge.
  const clientNonce = await Crypto.getRandomBytesAsync(32);
  const challengeResp = await authPost(baseURL + '/api/device-auth/challenge', {
    version: 1, hostId: pairing.hostId, deviceId, clientNonce: toHex(clientNonce),
  });
  if (!challengeResp.challengeId || !challengeResp.serverNonce || !challengeResp.hostSignature || !challengeResp.daemonBootId) {
    throw new AuthError('challenge_rejected', 'incomplete challenge response');
  }
  // Validate field lengths.
  const challengeId = fromHex(challengeResp.challengeId, 32);
  const serverNonce = fromHex(challengeResp.serverNonce, 32);
  const hostSig = fromHex(challengeResp.hostSignature);
  const bootId = challengeResp.daemonBootId;
  if (typeof bootId !== 'string' || bootId.length > 128) throw new AuthError('challenge_rejected', 'invalid boot id');
  if (challengeResp.hostId !== pairing.hostId) throw new AuthError('host_pin_fail', 'host id mismatch');
  const expiresAt = new Date(challengeResp.expiresAt);
  if (isNaN(expiresAt.getTime()) || expiresAt <= new Date()) throw new AuthError('expired', 'challenge expired');

  // 2. Verify HOST signature against the pinned host key.
  const createdAtMS = expiresAt.getTime() - CHALLENGE_TTL_MS;
  const hostTranscript = buildAuthTranscript({
    role: 'host', hostId: pairing.hostId, deviceId,
    daemonBootId: bootId, challengeId, clientNonce, serverNonce,
    createdAtMS, expiresAtMS: expiresAt.getTime(),
  });
  // verifyDer needs the 65-byte uncompressed point (drop 26B SPKI prefix).
  const hostPubPoint = hostPubDER.length === 91 ? hostPubDER.subarray(26) : hostPubDER;
  if (!verifyDer(hostSig, hostTranscript, hostPubPoint)) {
    throw new AuthError('host_pin_fail', 'host signature verification failed');
  }

  // 3. Sign device transcript + verify.
  const deviceTranscript = buildAuthTranscript({
    role: 'device', hostId: pairing.hostId, deviceId,
    daemonBootId: bootId, challengeId, clientNonce, serverNonce,
    createdAtMS, expiresAtMS: expiresAt.getTime(),
  });
  const deviceSig = await deviceKey.sign(deviceTranscript).catch(() => { throw new AuthError('network_error', 'device signing failed'); });
  const verifyResp = await authPost(baseURL + '/api/device-auth/verify', {
    version: 1, challengeId: challengeResp.challengeId, deviceId, signature: toHex(deviceSig),
  });
  if (!verifyResp.token || verifyResp.tokenType !== 'Bearer' || !verifyResp.expiresAt) {
    throw new AuthError('verify_rejected', 'incomplete verify response');
  }
  const tokExpires = new Date(verifyResp.expiresAt);
  if (isNaN(tokExpires.getTime()) || tokExpires <= new Date()) throw new AuthError('verify_rejected', 'bearer already expired');
  const perms = Array.isArray(verifyResp.permissions) ? verifyResp.permissions as string[] : [];
  if (verifyResp.deviceId && verifyResp.deviceId !== deviceId) throw new AuthError('verify_rejected', 'device id mismatch in verify');

  return { token: verifyResp.token, expiresAt: tokExpires, permissions: perms, sessionId: tokExpires.toISOString() };
}

// getValidToken returns a valid bearer token, refreshing if within 30s of expiry.
export async function getValidToken(
  pairing: StoredPairing | null, deviceKey: PokitDeviceKey | null, baseURL: string,
  state: { current: BearerState | null },
): Promise<string> {
  if (!pairing || !deviceKey) throw new AuthError('network_error', 'not paired');
  if (state.current && state.current.expiresAt.getTime() > Date.now() + 30_000) {
    return state.current.token;
  }
  state.current = await authenticate(pairing, deviceKey, baseURL);
  return state.current.token;
}

async function authPost(url: string, body: unknown): Promise<any> {
  let res: Response;
  try { res = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }); }
  catch { throw new AuthError('network_error', 'auth request failed'); }
  if (!res.ok) {
    if (res.status === 401) throw new AuthError('challenge_rejected', 'unauthorized', res.status);
    if (res.status === 403) throw new AuthError('host_pin_fail', 'forbidden', res.status);
    throw new AuthError('network_error', `auth server returned ${res.status}`, res.status);
  }
  try { return await res.json(); } catch { throw new AuthError('challenge_rejected', 'non-JSON auth response'); }
}
