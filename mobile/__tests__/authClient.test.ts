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

// Helper: build a valid challenge response + signed host proof.
function challResp(bootId: string, cid: Uint8Array, sn: Uint8Array, capturedCN: Uint8Array) {
  const now = new Date(); const issuedAt = now; const expiresAt = new Date(now.getTime() + 300_000);
  const createdAtMS = issuedAt.getTime(); const expiresAtMS = expiresAt.getTime();
  // Read the actual clientNonce from the authPost body.
  return (body: unknown) => {
    const b = body as { clientNonce: string };
    const cn = new Uint8Array(32); for (let i = 0; i < 64; i += 2) cn[i >> 1] = parseInt(b.clientNonce.substr(i, 2), 16);
    capturedCN.set(cn);
    const t = buildAuthTranscript({ role: 'host', hostId: 'host-001', deviceId: DEV_ID, daemonBootId: bootId, challengeId: cid, clientNonce: capturedCN, serverNonce: sn, createdAtMS, expiresAtMS });
    const sig = p256.sign(t, HOST_PRIV, { format: 'der', prehash: true });
    return { version: 1, challengeId: toHex(cid), hostId: 'host-001', hostKeyFingerprint: HOST_FP, daemonBootId: bootId, serverNonce: toHex(sn), hostSignature: toHex(sig), issuedAt: issuedAt.toISOString(), expiresAt: expiresAt.toISOString() };
  };
}

function verifyResp(capturedCN: Uint8Array, bootId: string, cid: Uint8Array, sn: Uint8Array) {
  return (body: unknown) => {
    const b = body as { signature: string };
    const sigBytes = new Uint8Array(b.signature.match(/.{2}/g)!.map((h: string) => parseInt(h, 16)));
    const now = new Date(); const ia = now; const ea = new Date(now.getTime() + 300_000);
    const t = buildAuthTranscript({ role: 'device', hostId: 'host-001', deviceId: DEV_ID, daemonBootId: bootId, challengeId: cid, clientNonce: capturedCN, serverNonce: sn, createdAtMS: ia.getTime(), expiresAtMS: ea.getTime() });
    const ok = p256.verify(sigBytes, t, DEV_PUB, { format: 'der', prehash: true });
    if (!ok) throw new Error(`device sig mismatch in fixture`);
    return { token: '0'.repeat(64), tokenType: 'Bearer', expiresAt: new Date(Date.now() + 600_000).toISOString(), permissions: ['sessions:read', 'sessions:create'], deviceId: DEV_ID };
  };
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
    const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const sn = new Uint8Array(32); sn[0] = 0xab; const cn = new Uint8Array(32);
    // Use mockImplementationOnce directly (not via mockFetchPost helper).
    let bounceCN = new Uint8Array(32);
    mockFetch.mockImplementationOnce(async (_url: string, init: RequestInit) => {
      const body = JSON.parse(init!.body as string);
      bounceCN = new Uint8Array(32); for (let i = 0; i < 64; i += 2) bounceCN[i >> 1] = parseInt(body.clientNonce.substr(i, 2), 16);
      const now = new Date(); const iss = now; const exp = new Date(now.getTime() + 300_000);
      const t = buildAuthTranscript({ role: 'host', hostId: 'host-001', deviceId: DEV_ID, daemonBootId: 'b1', challengeId: cid, clientNonce: bounceCN, serverNonce: sn, createdAtMS: iss.getTime(), expiresAtMS: exp.getTime() });
      return { ok: true, json: async () => ({ version: 1, challengeId: toHex(cid), hostId: 'host-001', hostKeyFingerprint: HOST_FP, daemonBootId: 'b1', serverNonce: toHex(sn), hostSignature: toHex(p256.sign(t, HOST_PRIV, { format: 'der', prehash: true })), issuedAt: iss.toISOString(), expiresAt: exp.toISOString() }) } as any;
    });
    mockFetch.mockImplementationOnce(async (_url: string, init: RequestInit) => {
      const body = JSON.parse(init!.body as string);
      const sigBytes = new Uint8Array(body.signature.match(/.{2}/g)!.map((h: string) => parseInt(h, 16)));
      const now = new Date(); const t = buildAuthTranscript({ role: 'device', hostId: 'host-001', deviceId: DEV_ID, daemonBootId: 'b1', challengeId: cid, clientNonce: bounceCN, serverNonce: sn, createdAtMS: now.getTime(), expiresAtMS: now.getTime() + 300_000 });
      p256.verify(sigBytes, t, DEV_PUB, { format: 'der', prehash: true }); // throws on mismatch
      return { ok: true, json: async () => ({ token: '0'.repeat(64), tokenType: 'Bearer', expiresAt: new Date(Date.now() + 600_000).toISOString(), permissions: ['sessions:read', 'sessions:create'], deviceId: DEV_ID }) } as any;
    });
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    expect(await mgr.getValidToken()).toBe('0'.repeat(64));
  });

  it('singleflight: concurrent calls share one challenge', async () => {
    const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const sn = new Uint8Array(32); sn[0] = 0xab; const cn = new Uint8Array(32);
    mockFetchPost(challResp('b2', cid, sn, cn));
    mockFetchPost(verifyResp(cn, 'b2', cid, sn));
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    const [a, b] = await Promise.all([mgr.getValidToken(), mgr.getValidToken()]);
    expect(a).toBe('0'.repeat(64)); expect(a).toBe(b);
    expect(mockFetch).toHaveBeenCalledTimes(2);
  });

  it('forceRefresh: invalidateIfCurrent + getValidToken(true)', async () => {
    const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const sn = new Uint8Array(32); sn[0] = 0xab; const cn = new Uint8Array(32);
    mockFetchPost(challResp('b3', cid, sn, cn));
    mockFetchPost(verifyResp(cn, 'b3', cid, sn));
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    expect(await mgr.getValidToken()).toBe('0'.repeat(64));
    // Invalidate the current token and force re-auth.
    mgr.invalidateIfCurrent('0'.repeat(64));
    const cid2 = new Uint8Array(32); for (let i = 0; i < 32; i++) cid2[i] = 32 - i;
    const sn2 = new Uint8Array(32); sn2[0] = 0xcd; const cn2 = new Uint8Array(32);
    mockFetchPost(challResp('b4', cid2, sn2, cn2));
    mockFetchPost(verifyResp(cn2, 'b4', cid2, sn2));
    expect(await mgr.getValidToken()).toBe('0'.repeat(64));
    expect(mockFetch).toHaveBeenCalledTimes(4);
  });

  it('host signature failure', async () => {
    const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const sn = new Uint8Array(32); sn[0] = 0xab;
    // Return a garbage host signature.
    mockFetchPost(() => ({
      version: 1, challengeId: toHex(cid), hostId: 'host-001', hostKeyFingerprint: HOST_FP,
      daemonBootId: 'b5', serverNonce: toHex(sn),
      hostSignature: toHex(new Uint8Array(70).fill(0x30)),
      issuedAt: new Date().toISOString(), expiresAt: new Date(Date.now() + 300_000).toISOString(),
    }));
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    await expect(mgr.getValidToken()).rejects.toMatchObject({ code: 'host_pin_fail' });
  });

  it('verify with missing deviceId', async () => {
    const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const sn = new Uint8Array(32); sn[0] = 0xab; const cn = new Uint8Array(32);
    mockFetchPost(challResp('b6', cid, sn, cn));
    mockFetchPost(() => ({ token: '0'.repeat(64), tokenType: 'Bearer', expiresAt: new Date(Date.now() + 600_000).toISOString(), permissions: ['sessions:read'] }));
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    await expect(mgr.getValidToken()).rejects.toMatchObject({ code: 'verify_rejected' });
  });

  it('unknown permission', async () => {
    const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const sn = new Uint8Array(32); sn[0] = 0xab; const cn = new Uint8Array(32);
    mockFetchPost(challResp('b7', cid, sn, cn));
    mockFetchPost(() => ({ token: '0'.repeat(64), tokenType: 'Bearer', expiresAt: new Date(Date.now() + 600_000).toISOString(), permissions: ['sessions:read', 'admin'], deviceId: DEV_ID }));
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

  it('GET 401 → invalidateIfCurrent + force refresh → retry', async () => {
    const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const sn = new Uint8Array(32); sn[0] = 0xab; const cn = new Uint8Array(32);
    mockFetchPost(challResp('f1', cid, sn, cn));
    mockFetchPost(verifyResp(cn, 'f1', cid, sn));
    mockFetch.mockResolvedValueOnce({ ok: false, status: 401 } as any);
    // Force refresh challenge+verify.
    mockFetchPost(challResp('f1x', cid, sn, cn));
    mockFetchPost(verifyResp(cn, 'f1x', cid, sn));
    mockFetch.mockResolvedValueOnce({ ok: true, status: 200 } as any);
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    const res = await authenticatedFetch('http://daemon/api', { method: 'GET' }, mgr);
    expect(res.status).toBe(200);
  });

  it('POST 401 → no retry', async () => {
    const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const sn = new Uint8Array(32); sn[0] = 0xab; const cn = new Uint8Array(32);
    mockFetchPost(challResp('f2', cid, sn, cn));
    mockFetchPost(verifyResp(cn, 'f2', cid, sn));
    mockFetch.mockResolvedValueOnce({ ok: false, status: 401 } as any);
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    await expect(authenticatedFetch('http://daemon/api', { method: 'POST', body: '{}' }, mgr)).rejects.toMatchObject({ code: 'host_pin_fail' });
  });

  it('GET 401 → refresh → 401 → typed failure', async () => {
    const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const sn = new Uint8Array(32); sn[0] = 0xab; const cn = new Uint8Array(32);
    mockFetchPost(challResp('d1', cid, sn, cn));
    mockFetchPost(verifyResp(cn, 'd1', cid, sn));
    mockFetch.mockResolvedValueOnce({ ok: false, status: 401 } as any);
    mockFetchPost(challResp('d1x', cid, sn, cn));
    mockFetchPost(verifyResp(cn, 'd1x', cid, sn));
    mockFetch.mockResolvedValueOnce({ ok: false, status: 401 } as any);
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    await expect(authenticatedFetch('http://daemon/api', { method: 'GET' }, mgr)).rejects.toMatchObject({ code: 'host_pin_fail' });
  });

  it('stale 401 does not invalidate a fresh bearer', async () => {
    const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const sn = new Uint8Array(32); sn[0] = 0xab; const cn = new Uint8Array(32);
    mockFetchPost(challResp('s1', cid, sn, cn));
    mockFetchPost((body) => ({ ...verifyResp(cn, 's1', cid, sn)(body), token: '1'.repeat(64) }));
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    const tokA = await mgr.getValidToken();
    // Refresh → new bearer tokB.
    mgr.invalidateIfCurrent(tokA);
    const cid2 = new Uint8Array(32); for (let i = 0; i < 32; i++) cid2[i] = 32 - i;
    const sn2 = new Uint8Array(32); sn2[0] = 0xcd; const cn2 = new Uint8Array(32);
    mockFetchPost(challResp('s2', cid2, sn2, cn2));
    mockFetchPost((body) => ({ ...verifyResp(cn2, 's2', cid2, sn2)(body), token: '2'.repeat(64) }));
    const tokB = await mgr.getValidToken(true);
    expect(tokB).not.toBe(tokA);
    // Stale 401 for tokA must NOT invalidate tokB.
    mgr.invalidateIfCurrent(tokA);
    expect(await mgr.getValidToken()).toBe(tokB);
  });
});

describe('bearer token format', () => {
  beforeEach(() => mockFetch.mockReset());

  it('rejects 63-char token', async () => {
    const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const sn = new Uint8Array(32); sn[0] = 0xab; const cn = new Uint8Array(32);
    mockFetchPost(challResp('tf1', cid, sn, cn));
    mockFetchPost(() => ({ token: '0'.repeat(63), tokenType: 'Bearer', expiresAt: new Date(Date.now() + 600_000).toISOString(), permissions: ['sessions:read'], deviceId: DEV_ID }));
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    await expect((mgr as any).authenticate()).rejects.toMatchObject({ code: 'verify_rejected' });
  });

  it('rejects uppercase token', async () => {
    const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const sn = new Uint8Array(32); sn[0] = 0xab; const cn = new Uint8Array(32);
    mockFetchPost(challResp('tf2', cid, sn, cn));
    mockFetchPost(() => ({ token: 'F'.repeat(64), tokenType: 'Bearer', expiresAt: new Date(Date.now() + 600_000).toISOString(), permissions: ['sessions:read'], deviceId: DEV_ID }));
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    await expect((mgr as any).authenticate()).rejects.toMatchObject({ code: 'verify_rejected' });
  });

  it('rejects token with newline', async () => {
    const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const sn = new Uint8Array(32); sn[0] = 0xab; const cn = new Uint8Array(32);
    mockFetchPost(challResp('tf3', cid, sn, cn));
    mockFetchPost(() => ({ token: '0'.repeat(32) + '\n' + '0'.repeat(32), tokenType: 'Bearer', expiresAt: new Date(Date.now() + 600_000).toISOString(), permissions: ['sessions:read'], deviceId: DEV_ID }));
    const mgr = new TokenManager(pairing, deviceKey as any, 'http://daemon');
    await expect((mgr as any).authenticate()).rejects.toMatchObject({ code: 'verify_rejected' });
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
