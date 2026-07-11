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
const pairing = { hostId: 'host-001', hostPubKeyB64: HOST_B64, deviceId: DEV_ID, baseURL: 'http://daemon', origin: 'http://daemon', pairedAt: '2026', role: 'owner' };
const deviceKey = {
  getKeyInfo: async () => ({ provider: 'android_keystore' as const, keyVersion: 1 as const, deviceId: DEV_ID, publicKeySpki: DEV_SPKI, hardwareBacked: true as const, nonExportable: true as const, securityLevel: 'tee' as const }),
  sign: async (msg: Uint8Array) => p256.sign(msg, DEV_PRIV, { format: 'der', prehash: true }),
  getSupport: async () => 'supported' as const, hasKey: async () => true, ensureKey: async () => { throw new Error('x'); }, getPublicKeySpki: async () => DEV_SPKI, deleteKey: async () => {},
};

// Shared auth session — challResp records timestamps; verifyResp reuses them
// so the signed transcript is byte-identical across both phases.
interface AuthSession { capturedCN: Uint8Array; createdAtMS: number; expiresAtMS: number; bootId: string; cid: Uint8Array; sn: Uint8Array; }
let _tokenCounter = 0;

function makeAuthSession(bootId: string, cid: Uint8Array, sn: Uint8Array): AuthSession {
  const now = new Date();
  return { capturedCN: new Uint8Array(32), createdAtMS: now.getTime(), expiresAtMS: now.getTime() + 300_000, bootId, cid, sn };
}

function mockChallenge(ses: AuthSession) {
  mockFetch.mockImplementationOnce(async (_url: string, init: RequestInit) => {
    const body = JSON.parse(init!.body as string);
    const cid = ses.cid;
    for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const cn = new Uint8Array(32); for (let i = 0; i < 64; i += 2) cn[i >> 1] = parseInt(body.clientNonce.substr(i, 2), 16);
    ses.capturedCN.set(cn);
    const t = buildAuthTranscript({ role: 'host', hostId: 'host-001', deviceId: DEV_ID, daemonBootId: ses.bootId, challengeId: cid, clientNonce: ses.capturedCN, serverNonce: ses.sn, createdAtMS: ses.createdAtMS, expiresAtMS: ses.expiresAtMS });
    const sig = p256.sign(t, HOST_PRIV, { format: 'der', prehash: true });
    return { ok: true, json: async () => ({ version: 1, challengeId: toHex(cid), hostId: 'host-001', hostKeyFingerprint: HOST_FP, daemonBootId: ses.bootId, serverNonce: toHex(ses.sn), hostSignature: toHex(sig), issuedAt: new Date(ses.createdAtMS).toISOString(), expiresAt: new Date(ses.expiresAtMS).toISOString() }) } as any;
  });
}

function mockVerify(ses: AuthSession): string {
  const token = (++_tokenCounter).toString(16).padStart(64, '0');
  mockFetch.mockImplementationOnce(async (_url: string, init: RequestInit) => {
    const body = JSON.parse(init!.body as string);
    const sigBytes = new Uint8Array(body.signature.match(/.{2}/g)!.map((h: string) => parseInt(h, 16)));
    const cid = ses.cid;
    const t = buildAuthTranscript({ role: 'device', hostId: 'host-001', deviceId: DEV_ID, daemonBootId: ses.bootId, challengeId: cid, clientNonce: ses.capturedCN, serverNonce: ses.sn, createdAtMS: ses.createdAtMS, expiresAtMS: ses.expiresAtMS });
    const ok = p256.verify(sigBytes, t, DEV_PUB, { format: 'der', prehash: true });
    if (!ok) throw new Error(`device sig mismatch in fixture`);
    return { ok: true, json: async () => ({ token, tokenType: 'Bearer', expiresAt: new Date(Date.now() + 600_000).toISOString(), permissions: ['sessions:read', 'sessions:create'], deviceId: DEV_ID }) } as any;
  });
  return token;
}

// Build fetch mocks that introspect the POST body.
function mockFetchPost(responseFn: (body: unknown) => unknown) {
  mockFetch.mockImplementationOnce(async (_url: string, init: RequestInit) => {
    const body = JSON.parse(init!.body as string);
    return { ok: true, json: async () => responseFn(body) } as any;
  });
}

describe('TokenManager', () => {
  beforeEach(() => mockFetch.mockReset());

  it('happy path: challenge → verify → bearer', async () => {
    const ses = makeAuthSession('b1', new Uint8Array(32), new Uint8Array(32));
    mockChallenge(ses);
    const tok = mockVerify(ses);
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    expect(await mgr.getValidToken()).toBe(tok);
  });

  it('singleflight: concurrent calls share one challenge', async () => {
    const ses = makeAuthSession('b2', new Uint8Array(32), new Uint8Array(32));
    mockChallenge(ses);
    const tok = mockVerify(ses);
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    const [a, b] = await Promise.all([mgr.getValidToken(), mgr.getValidToken()]);
    expect(a).toBe(tok); expect(a).toBe(b);
    expect(mockFetch).toHaveBeenCalledTimes(2);
  });

  it('forceRefresh: getValidToken(true) forces re-auth', async () => {
    const ses1 = makeAuthSession('b3', new Uint8Array(32), new Uint8Array(32));
    mockChallenge(ses1);
    const tokA = mockVerify(ses1);
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    expect(await mgr.getValidToken()).toBe(tokA);
    // forceRefresh → new challenge + verify.
    const ses2 = makeAuthSession('b4', new Uint8Array(32), new Uint8Array(32));
    mockChallenge(ses2);
    const tokB = mockVerify(ses2);
    expect(await mgr.getValidToken(true)).toBe(tokB);
    expect(tokB).not.toBe(tokA);
    expect(mockFetch).toHaveBeenCalledTimes(4);
  });

  it('host signature failure', async () => {
    mockFetch.mockImplementationOnce(async () => ({ ok: true, json: async () => ({ version: 1, challengeId: 'a'.repeat(64), hostId: 'host-001', hostKeyFingerprint: HOST_FP, daemonBootId: 'b5', serverNonce: 'b'.repeat(64), hostSignature: toHex(new Uint8Array(70).fill(0x30)), issuedAt: new Date().toISOString(), expiresAt: new Date(Date.now() + 300_000).toISOString() }) } as any));
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    await expect(mgr.getValidToken()).rejects.toMatchObject({ code: 'host_pin_fail' });
  });

  it('verify with missing deviceId', async () => {
    const ses = makeAuthSession('b6', new Uint8Array(32), new Uint8Array(32));
    mockChallenge(ses);
    mockFetch.mockImplementationOnce(async () => ({ ok: true, json: async () => ({ token: '00'.repeat(32), tokenType: 'Bearer', expiresAt: new Date(Date.now() + 600_000).toISOString(), permissions: ['sessions:read'] }) } as any));
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    await expect(mgr.getValidToken()).rejects.toMatchObject({ code: 'verify_rejected' });
  });

  it('unknown permission', async () => {
    const ses = makeAuthSession('b7', new Uint8Array(32), new Uint8Array(32));
    mockChallenge(ses);
    mockFetch.mockImplementationOnce(async () => ({ ok: true, json: async () => ({ token: '00'.repeat(32), tokenType: 'Bearer', expiresAt: new Date(Date.now() + 600_000).toISOString(), permissions: ['sessions:read', 'admin'], deviceId: DEV_ID }) } as any));
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    await expect(mgr.getValidToken()).rejects.toMatchObject({ code: 'verify_rejected' });
  });

  it('network failure', async () => {
    mockFetch.mockRejectedValue(new Error('connection refused'));
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    await expect(mgr.getValidToken()).rejects.toMatchObject({ code: 'network_error' });
  });
});

describe('authenticatedFetch', () => {
  beforeEach(() => mockFetch.mockReset());

  it('GET 401 → refresh → retry with new Authorization', async () => {
    const ses1 = makeAuthSession('f1', new Uint8Array(32), new Uint8Array(32));
    mockChallenge(ses1);
    const tokA = mockVerify(ses1);
    // First GET uses tokA, returns 401.
    mockFetch.mockImplementationOnce(async (url: string, init: RequestInit) => {
      const auth = (init!.headers as Record<string, string>)['Authorization'];
      if (auth === `Bearer ${tokA}`) return { ok: false, status: 401 } as any;
      throw new Error(`unexpected auth: ${auth}`);
    });
    // refreshAfter401 → new session.
    const ses2 = makeAuthSession('f1x', new Uint8Array(32), new Uint8Array(32));
    mockChallenge(ses2);
    const tokB = mockVerify(ses2);
    expect(tokB).not.toBe(tokA);
    // Retry GET uses tokB, returns 200.
    mockFetch.mockImplementationOnce(async (url: string, init: RequestInit) => {
      const auth = (init!.headers as Record<string, string>)['Authorization'];
      if (auth === `Bearer ${tokB}`) return { ok: true, status: 200 } as any;
      throw new Error(`retry auth mismatch: expected ${tokB}, got ${auth}`);
    });
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    const res = await authenticatedFetch('http://daemon/api', { method: 'GET' }, mgr);
    expect(res.status).toBe(200);
  });

  it('POST 401 → no retry', async () => {
    const ses = makeAuthSession('f2', new Uint8Array(32), new Uint8Array(32));
    mockChallenge(ses);
    mockVerify(ses);
    mockFetch.mockImplementationOnce(async () => ({ ok: false, status: 401 }) as any);
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    await expect(authenticatedFetch('http://daemon/api', { method: 'POST', body: '{}' }, mgr)).rejects.toMatchObject({ code: 'host_pin_fail' });
  });

  it('GET 401 → refresh → 401 → typed failure', async () => {
    const ses1 = makeAuthSession('d1', new Uint8Array(32), new Uint8Array(32));
    mockChallenge(ses1);
    mockVerify(ses1);
    mockFetch.mockImplementationOnce(async () => ({ ok: false, status: 401 }) as any);
    // Refresh.
    const ses2 = makeAuthSession('d1x', new Uint8Array(32), new Uint8Array(32));
    mockChallenge(ses2);
    mockVerify(ses2);
    // Retry also 401.
    mockFetch.mockImplementationOnce(async () => ({ ok: false, status: 401 }) as any);
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    await expect(authenticatedFetch('http://daemon/api', { method: 'GET' }, mgr)).rejects.toMatchObject({ code: 'host_pin_fail' });
  });

  // refreshAfter401 atomically: if requestToken IS stale → returns latest without re-auth.
  it('stale 401 returns the already-fresh bearer without a third auth', async () => {
    const sesA = makeAuthSession('sa', new Uint8Array(32), new Uint8Array(32));
    mockChallenge(sesA);
    const tokA = mockVerify(sesA);
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    await mgr.getValidToken(); // seed tokA

    // Advance to tokB.
    const sesB = makeAuthSession('sb', new Uint8Array(32), new Uint8Array(32));
    mockChallenge(sesB);
    const tokB = mockVerify(sesB);
    await mgr.getValidToken(true); // force refresh → tokB

    // Stale 401 for tokA → refreshAfter401 returns tokB (no third auth).
    const reused = await mgr.refreshAfter401(tokA);
    expect(reused).toBe(tokB);
  });
});

describe('bearer token format', () => {
  beforeEach(() => mockFetch.mockReset());

  it('rejects 63-char token', async () => {
    const ses = makeAuthSession('tf1', new Uint8Array(32), new Uint8Array(32));
    mockChallenge(ses);
    mockFetch.mockImplementationOnce(async () => ({ ok: true, json: async () => ({ token: '0'.repeat(63), tokenType: 'Bearer', expiresAt: new Date(Date.now() + 600_000).toISOString(), permissions: ['sessions:read'], deviceId: DEV_ID }) } as any));
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    await expect(mgr.getValidToken()).rejects.toMatchObject({ code: 'verify_rejected' });
  });

  it('rejects uppercase token', async () => {
    const ses = makeAuthSession('tf2', new Uint8Array(32), new Uint8Array(32));
    mockChallenge(ses);
    mockFetch.mockImplementationOnce(async () => ({ ok: true, json: async () => ({ token: 'F'.repeat(64), tokenType: 'Bearer', expiresAt: new Date(Date.now() + 600_000).toISOString(), permissions: ['sessions:read'], deviceId: DEV_ID }) } as any));
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    await expect(mgr.getValidToken()).rejects.toMatchObject({ code: 'verify_rejected' });
  });

  it('rejects token with newline', async () => {
    const ses = makeAuthSession('tf3', new Uint8Array(32), new Uint8Array(32));
    mockChallenge(ses);
    mockFetch.mockImplementationOnce(async () => ({ ok: true, json: async () => ({ token: '0'.repeat(32) + '\n' + '0'.repeat(32), tokenType: 'Bearer', expiresAt: new Date(Date.now() + 600_000).toISOString(), permissions: ['sessions:read'], deviceId: DEV_ID }) } as any));
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    await expect(mgr.getValidToken()).rejects.toMatchObject({ code: 'verify_rejected' });
  });
});

describe('mock sanity', () => {
  beforeEach(() => mockFetch.mockReset());
  it('fetch is mockFetch and mockResolvedValueOnce fires', async () => {
    mockFetch.mockResolvedValueOnce({ ok: true, status: 200, json: async () => ({ x: 1 }) } as any);
    const res = await fetch('http://x');
    expect((await res.json()).x).toBe(1);
    expect(mockFetch).toHaveBeenCalledTimes(1);
  });
});
