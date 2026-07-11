const mockFetch = jest.fn();
(global as any).fetch = mockFetch;

import { p256 } from '@noble/curves/nist.js';
import { sha256 } from '@noble/hashes/sha2.js';
import { TokenManager, AuthError } from '../src/lib/authClient';
import { toHex, toBase64 } from '../src/lib/crypto';
import { buildAuthTranscript } from '../src/lib/authTranscript';
import { authenticatedFetch } from '../src/lib/authTransport';

const P256_PREFIX = new Uint8Array([0x30, 0x59, 0x30, 0x13, 0x06, 0x07, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x02, 0x01, 0x06, 0x08, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x03, 0x01, 0x07, 0x03, 0x42, 0x00]);
const HOST_PRIV = p256.utils.randomSecretKey();
const HOST_PUB = p256.getPublicKey(HOST_PRIV, false);
const HOST_SPKI = (() => { const s = new Uint8Array(91); s.set(P256_PREFIX, 0); s.set(HOST_PUB, 26); return s; })();
const HOST_B64 = toBase64(HOST_SPKI);
const HOST_FP = toHex(sha256(HOST_SPKI));

const DEV_PRIV = p256.utils.randomSecretKey();
const DEV_PUB = p256.getPublicKey(DEV_PRIV, false);
const DEV_SPKI = (() => { const s = new Uint8Array(91); s.set(P256_PREFIX, 0); s.set(DEV_PUB, 26); return s; })();
const DEV_ID = toHex(sha256(DEV_SPKI));

const pairing = { hostId: 'host-001', hostPubKeyB64: HOST_B64, deviceId: DEV_ID, baseURL: 'http://daemon', pairedAt: '2026', role: 'owner' };
const deviceKey = {
  getKeyInfo: async () => ({ provider: 'android_keystore' as const, keyVersion: 1 as const, deviceId: DEV_ID, publicKeySpki: DEV_SPKI, hardwareBacked: true as const, nonExportable: true as const, securityLevel: 'tee' as const }),
  sign: async (msg: Uint8Array) => p256.sign(msg, DEV_PRIV, { format: 'der', prehash: true }),
  getSupport: async () => 'supported' as const, hasKey: async () => true, ensureKey: async () => { throw new Error('x'); }, getPublicKeySpki: async () => DEV_SPKI, deleteKey: async () => {},
};

function makeChallenge(bootId: string, cid: Uint8Array, sn: Uint8Array, capturedNonceBytes: Uint8Array) {
  const now = new Date(); const issuedAt = new Date(now.getTime()); const expiresAt = new Date(now.getTime() + 300_000);
  const createdAtMS = issuedAt.getTime(); const expiresAtMS = expiresAt.getTime();
  return async (url: string, init: RequestInit) => {
    const body = JSON.parse(init!.body as string);
    const cn = new Uint8Array(32); for (let i = 0; i < 64; i += 2) cn[i >> 1] = parseInt(body.clientNonce.substr(i, 2), 16);
    capturedNonceBytes.set(cn);
    const t = buildAuthTranscript({ role: 'host', hostId: 'host-001', deviceId: DEV_ID, daemonBootId: bootId, challengeId: cid, clientNonce: capturedNonceBytes, serverNonce: sn, createdAtMS, expiresAtMS });
    const sig = p256.sign(t, HOST_PRIV, { format: 'der', prehash: true });
    return { ok: true, json: async () => ({ version: 1, challengeId: toHex(cid), hostId: 'host-001', hostKeyFingerprint: HOST_FP, daemonBootId: bootId, serverNonce: toHex(sn), hostSignature: toHex(sig), issuedAt: issuedAt.toISOString(), expiresAt: expiresAt.toISOString() }) } as any;
  };
}

function makeVerify(_cn: Uint8Array, _bootId: string, _cid: Uint8Array, _sn: Uint8Array) {
  const devToken = 'a'.repeat(64); const bearerExp = new Date(Date.now() + 600_000);
  return async (_url: string, _init: RequestInit) => {
    return { ok: true, json: async () => ({ token: devToken, tokenType: 'Bearer', expiresAt: bearerExp.toISOString(), permissions: ['sessions:read', 'sessions:create'], deviceId: DEV_ID }) } as any;
  };
}

describe('TokenManager', () => {
  beforeEach(() => mockFetch.mockReset());

  it('happy path: challenge → verify → bearer', async () => {
    const bootId = 'b1'; const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const sn = new Uint8Array(32); sn[0] = 0xab; const cn = new Uint8Array(32);
    mockFetch.mockImplementationOnce(makeChallenge(bootId, cid, sn, cn));
    mockFetch.mockImplementationOnce(makeVerify(cn, bootId, cid, sn));
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    const tok = await mgr.getValidToken();
    expect(tok).toBe('a'.repeat(64));
  });

  it('singleflight: concurrent calls share one challenge', async () => {
    const bootId = 'b2'; const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const sn = new Uint8Array(32); sn[0] = 0xab; const cn = new Uint8Array(32);
    mockFetch.mockImplementationOnce(makeChallenge(bootId, cid, sn, cn));
    mockFetch.mockImplementationOnce(makeVerify(cn, bootId, cid, sn));
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    const [a, b] = await Promise.all([mgr.getValidToken(), mgr.getValidToken()]);
    expect(a).toBe('a'.repeat(64));
    expect(a).toBe(b);
    expect(mockFetch).toHaveBeenCalledTimes(2); // one challenge, one verify
  });

  it('forceRefresh: invalidate + getValidToken forces re-auth', async () => {
    const bootId = 'b3'; const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const sn = new Uint8Array(32); sn[0] = 0xab; const cn = new Uint8Array(32);
    mockFetch.mockImplementationOnce(makeChallenge(bootId, cid, sn, cn));
    mockFetch.mockImplementationOnce(makeVerify(cn, bootId, cid, sn));
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    expect(await mgr.getValidToken()).toBe('a'.repeat(64));
    // Second call within TTL returns cached.
    mgr.invalidate();
    const bootId2 = 'b4'; const cid2 = new Uint8Array(32); for (let i = 0; i < 32; i++) cid2[i] = 32 - i;
    const sn2 = new Uint8Array(32); sn2[0] = 0xcd; const cn2 = new Uint8Array(32);
    mockFetch.mockImplementationOnce(makeChallenge(bootId2, cid2, sn2, cn2));
    mockFetch.mockImplementationOnce(makeVerify(cn2, bootId2, cid2, sn2));
    expect(await mgr.getValidToken(true)).toBe('a'.repeat(64));
    expect(mockFetch).toHaveBeenCalledTimes(4); // 2 full auth cycles
  });

  it('rejects a host-signature failure', async () => {
    const bootId = 'b5'; const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const sn = new Uint8Array(32);
    const cn = new Uint8Array(32);
    // Challenge succeeds, but return a bad signature. The authenticator will
    // reject it because the host transcript doesn't match the returned sig.
    mockFetch.mockImplementationOnce(async (url: string, init: RequestInit) => {
      const sig = new Uint8Array(70); sig[0] = 0x30; // garbage DER sig
      return { ok: true, json: async () => ({ version: 1, challengeId: toHex(cid), hostId: 'host-001', hostKeyFingerprint: HOST_FP, daemonBootId: bootId, serverNonce: toHex(sn), hostSignature: toHex(sig), issuedAt: new Date().toISOString(), expiresAt: new Date(Date.now() + 300_000).toISOString() }) } as any;
    });
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    await expect(mgr.getValidToken()).rejects.toMatchObject({ code: 'host_pin_fail' });
  });

  it('rejects a verify with missing deviceId', async () => {
    const bootId = 'b6'; const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const sn = new Uint8Array(32); sn[0] = 0xab; const cn = new Uint8Array(32);
    mockFetch.mockImplementationOnce(makeChallenge(bootId, cid, sn, cn));
    mockFetch.mockImplementationOnce(async () => ({ ok: true, json: async () => ({ token: 'a'.repeat(64), tokenType: 'Bearer', expiresAt: new Date(Date.now() + 600_000).toISOString(), permissions: ['sessions:read'] }) }) as any);
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    await expect(mgr.getValidToken()).rejects.toThrow('deviceId');
  });

  it('rejects an unknown permission', async () => {
    const bootId = 'b7'; const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const sn = new Uint8Array(32); sn[0] = 0xab; const cn = new Uint8Array(32);
    mockFetch.mockImplementationOnce(makeChallenge(bootId, cid, sn, cn));
    mockFetch.mockImplementationOnce(async () => ({ ok: true, json: async () => ({ token: 'a'.repeat(64), tokenType: 'Bearer', expiresAt: new Date(Date.now() + 600_000).toISOString(), permissions: ['sessions:read', 'admin'], deviceId: DEV_ID }) }) as any);
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    await expect(mgr.getValidToken()).rejects.toThrow('unknown permission');
  });

  it('rejects a network failure', async () => {
    mockFetch.mockRejectedValue(new Error('connection refused'));
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    await expect(mgr.getValidToken()).rejects.toMatchObject({ code: 'network_error' });
  });
});

describe('authenticatedFetch', () => {
  beforeEach(() => mockFetch.mockReset());

  it('GET 401 → invalidate + force refresh → retry succeeds', async () => {
    const bootId = 'fetch1'; const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const sn = new Uint8Array(32); sn[0] = 0xab; const cn = new Uint8Array(32);
    // First challenge/verify for the initial token.
    mockFetch.mockImplementationOnce(makeChallenge(bootId, cid, sn, cn));
    mockFetch.mockImplementationOnce(makeVerify(cn, bootId, cid, sn));
    // First GET → 401.
    mockFetch.mockImplementationOnce(async () => ({ ok: false, status: 401 }) as any);
    // Force-refresh: second challenge/verify.
    mockFetch.mockImplementationOnce(makeChallenge(bootId + 'x', cid, sn, cn));
    mockFetch.mockImplementationOnce(makeVerify(cn, bootId + 'x', cid, sn));
    // Retry GET succeeds.
    mockFetch.mockImplementationOnce(async () => ({ ok: true, status: 200 }) as any);
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    const res = await authenticatedFetch('http://daemon/api/sessions', { method: 'GET' }, mgr);
    expect(res.status).toBe(200);
  });

  it('POST 401 → no retry (non-idempotent)', async () => {
    const bootId = 'fetch2'; const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const sn = new Uint8Array(32); sn[0] = 0xab; const cn = new Uint8Array(32);
    mockFetch.mockImplementationOnce(makeChallenge(bootId, cid, sn, cn));
    mockFetch.mockImplementationOnce(makeVerify(cn, bootId, cid, sn));
    mockFetch.mockImplementationOnce(async () => ({ ok: false, status: 401 }) as any);
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    await expect(authenticatedFetch('http://daemon/api/sessions', { method: 'POST', body: '{}' }, mgr)).rejects.toThrow('auth rejected');
  });
});
