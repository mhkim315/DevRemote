// M3-auth-3A: device bearer session manager.
//
// Singleflight: multiple concurrent getValidToken() calls share one in-flight
// challenge/verify promise. 401 force-refresh: invalidate() causes the next
// call to re-authenticate even if the current token is still within the TTL
// window.

import type { PokitDeviceKey } from '../../modules/pokit-device-key';
import type { StoredPairing } from './pairingStore';
import * as Crypto from 'expo-crypto';
import { toHex, fromHex, fromBase64, verifyDer } from './crypto';
import { buildAuthTranscript } from './authTranscript';

const PERMISSION_VOCABULARY = new Set([
  'sessions:read', 'sessions:create', 'sessions:stop', 'sessions:kill',
  'history:delete', 'terminal:input',
]);

export interface BearerState {
  token: string;
  expiresAt: Date;
  permissions: string[];
}

export type AuthErrorCode =
  | 'network_error' | 'host_pin_fail' | 'challenge_rejected'
  | 'verify_rejected' | 'expired' | 'revoked';

export class AuthError extends Error {
  code: AuthErrorCode; statusCode?: number;
  constructor(code: AuthErrorCode, message?: string, statusCode?: number) {
    super(message || code); this.name = 'AuthError'; this.code = code; this.statusCode = statusCode;
  }
}

// TokenManager owns the bearer lifecycle for one paired host.
export class TokenManager {
  private state: BearerState | null = null;
  private inFlight: Promise<BearerState> | null = null;
  private pairing: StoredPairing | null;
  private deviceKey: PokitDeviceKey | null;
  private baseURL: string;

  constructor(pairing: StoredPairing | null, deviceKey: PokitDeviceKey | null, baseURL: string) {
    this.pairing = pairing; this.deviceKey = deviceKey; this.baseURL = baseURL;
  }

  // refreshAfter401 handles a 401 for `requestToken` atomically:
  // - if requestToken IS the current bearer → force a fresh challenge + return new token
  // - if requestToken is stale (already replaced) → return the latest token as-is
  // This prevents a stale 401 from destroying a freshly-refreshed bearer.
  async refreshAfter401(requestToken: string): Promise<string> {
    if (this.state && this.state.token === requestToken) {
      this.state = null;
      return this.getValidToken(true);
    }
    return this.getValidToken(false);
  }

  // getValidToken returns a fresh bearer. Callers get one shared in-flight
  // promise; a 401 caller invalidates first, then calls with forceRefresh.
  async getValidToken(forceRefresh?: boolean): Promise<string> {
    if (forceRefresh) this.state = null;
    if (this.state && this.state.expiresAt.getTime() > Date.now() + 30_000) {
      return this.state.token;
    }
    // Singleflight: if a challenge is in progress, join it.
    if (this.inFlight) return (await this.inFlight).token;
    this.inFlight = this.authenticate();
    try {
      const s = await this.inFlight;
      this.state = s;
      return s.token;
    } finally { this.inFlight = null; }
  }

  private async authenticate(): Promise<BearerState> {
    if (!this.pairing || !this.deviceKey) throw new AuthError('network_error', 'not paired');
    let hostPubDER: Uint8Array;
    try { hostPubDER = fromBase64(this.pairing.hostPubKeyB64); } catch { throw new AuthError('host_pin_fail', 'invalid host key'); }
    const { publicKeySpki, deviceId } = await this.deviceKey.getKeyInfo().catch(() => { throw new AuthError('network_error', 'device identity unavailable'); });

    // 1. Challenge.
    const clientNonce = await Crypto.getRandomBytesAsync(32);
    const chalResp = await authPost(this.baseURL + '/api/device-auth/challenge', { version: 1, hostId: this.pairing.hostId, deviceId, clientNonce: toHex(clientNonce) });
    validateChallengeResponse(chalResp, this.pairing.hostId);
    let challengeId: Uint8Array, serverNonce: Uint8Array, hostSig: Uint8Array;
    try {
      challengeId = fromHex(chalResp.challengeId, 32);
      serverNonce = fromHex(chalResp.serverNonce, 32);
      hostSig = fromHex(chalResp.hostSignature);
    } catch { throw new AuthError('challenge_rejected', 'malformed challenge field'); }
    const bootId = chalResp.daemonBootId;
    if (typeof bootId !== 'string' || bootId.length > 128) throw new AuthError('challenge_rejected', 'invalid boot id');
    const issuedAt = new Date(chalResp.issuedAt);
    const expiresAt = new Date(chalResp.expiresAt);
    if (isNaN(issuedAt.getTime()) || isNaN(expiresAt.getTime()) || issuedAt >= expiresAt) throw new AuthError('challenge_rejected', 'invalid challenge timestamps');
    const createdAtMS = issuedAt.getTime();
    const expiresAtMS = expiresAt.getTime();

    // 2. Verify HOST signature against the pinned host key.
    const hostPubPoint = hostPubDER.length === 91 ? hostPubDER.subarray(26) : hostPubDER;
    const hostTranscript = buildAuthTranscript({ role: 'host', hostId: this.pairing.hostId, deviceId, daemonBootId: bootId, challengeId, clientNonce, serverNonce, createdAtMS, expiresAtMS });
    if (!verifyDer(hostSig, hostTranscript, hostPubPoint)) throw new AuthError('host_pin_fail', 'host signature verification failed');

    // 3. Sign device transcript + verify.
    const deviceTranscript = buildAuthTranscript({ role: 'device', hostId: this.pairing.hostId, deviceId, daemonBootId: bootId, challengeId, clientNonce, serverNonce, createdAtMS, expiresAtMS });
    const deviceSig = await this.deviceKey.sign(deviceTranscript).catch(() => { throw new AuthError('network_error', 'device signing failed'); });
    const verifyResp = await authPost(this.baseURL + '/api/device-auth/verify', { version: 1, challengeId: chalResp.challengeId, deviceId, signature: toHex(deviceSig) });
    return validateVerifyResponse(verifyResp, deviceId);
  }
}

// ── strict DTO validators ──

function validateChallengeResponse(r: any, expectedHostId: string) {
  if (r.version !== 1) throw new AuthError('challenge_rejected', 'unsupported version');
  if (r.hostId !== expectedHostId) throw new AuthError('host_pin_fail', 'host id mismatch');
  if (typeof r.challengeId !== 'string' || r.challengeId.length !== 64) throw new AuthError('challenge_rejected', 'invalid challengeId');
  if (typeof r.serverNonce !== 'string' || r.serverNonce.length !== 64) throw new AuthError('challenge_rejected', 'invalid serverNonce');
  {
    const hostSig = fromHex(r.hostSignature ?? '');
    if (hostSig.length < 68 || hostSig.length > 74) throw new AuthError('challenge_rejected', 'invalid hostSignature length');
  }
  if (typeof r.daemonBootId !== 'string' || r.daemonBootId.length === 0) throw new AuthError('challenge_rejected', 'invalid bootId');
  if (typeof r.hostKeyFingerprint !== 'string' || r.hostKeyFingerprint.length !== 64) throw new AuthError('challenge_rejected', 'invalid host fingerprint');
  if (typeof r.issuedAt !== 'string' || typeof r.expiresAt !== 'string') throw new AuthError('challenge_rejected', 'missing timestamp');
}

function validateVerifyResponse(r: any, expectedDeviceId: string): BearerState {
  if (typeof r.token !== 'string' || !/^[0-9a-f]{64}$/.test(r.token)) throw new AuthError('verify_rejected', 'invalid token');
  if (r.tokenType !== 'Bearer') throw new AuthError('verify_rejected', 'invalid tokenType');
  if (typeof r.expiresAt !== 'string') throw new AuthError('verify_rejected', 'missing expiresAt');
  const expiresAt = new Date(r.expiresAt);
  if (isNaN(expiresAt.getTime()) || expiresAt <= new Date()) throw new AuthError('verify_rejected', 'bearer already expired');
  if (typeof r.deviceId !== 'string' || r.deviceId !== expectedDeviceId) throw new AuthError('verify_rejected', `deviceId mismatch: expected ${expectedDeviceId}`);
  const perms = Array.isArray(r.permissions) ? r.permissions as string[] : [];
  if (perms.length === 0 || perms.length > 20) throw new AuthError('verify_rejected', 'invalid permission count');
  for (const p of perms) {
    if (typeof p !== 'string' || !PERMISSION_VOCABULARY.has(p)) throw new AuthError('verify_rejected', `unknown permission: ${p}`);
  }
  return { token: r.token, expiresAt, permissions: perms };
}

async function authPost(url: string, body: unknown): Promise<any> {
  let res: Response;
  try { res = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }); }
  catch { throw new AuthError('network_error', 'auth request failed'); }
  if (!res.ok) {
    if (res.status === 401 || res.status === 403) throw new AuthError('host_pin_fail', `auth rejected: ${res.status}`, res.status);
    if (res.status === 429) throw new AuthError('network_error', 'rate limited', res.status);
    throw new AuthError('network_error', `auth server returned ${res.status}`, res.status);
  }
  try { return await res.json(); } catch { throw new AuthError('challenge_rejected', 'non-JSON auth response'); }
}

// Export authenticate for the happy-path jest (TokenManager delegates to it internally)
export async function authenticate(
  pairing: StoredPairing, deviceKey: PokitDeviceKey, baseURL: string,
): Promise<BearerState> {
  return new TokenManager(pairing, deviceKey, baseURL)['authenticate']();
}
