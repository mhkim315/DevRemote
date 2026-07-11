// Auth client tests with mocked fetch + daemon-shaped challenge/verify fixtures.

const mockFetch = jest.fn();
(global as any).fetch = mockFetch;

import { p256 } from '@noble/curves/nist.js';
import { sha256 } from '@noble/hashes/sha2.js';
import { authenticate, AuthError, type BearerState } from '../src/lib/authClient';
import { toHex, toBase64 } from '../src/lib/crypto';
import { buildAuthTranscript } from '../src/lib/authTranscript';

const P256_PREFIX = new Uint8Array([0x30, 0x59, 0x30, 0x13, 0x06, 0x07, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x02, 0x01, 0x06, 0x08, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x03, 0x01, 0x07, 0x03, 0x42, 0x00]);

const HOST_PRIV = p256.utils.randomSecretKey();
const HOST_PUB = p256.getPublicKey(HOST_PRIV, false);
const PREFIX = (() => { const s = new Uint8Array(91); s.set(P256_PREFIX, 0); s.set(HOST_PUB, 26); return s; })();
const HOST_B64 = toBase64(PREFIX);

const DEV_PRIV = p256.utils.randomSecretKey();
const DEV_PUB = p256.getPublicKey(DEV_PRIV, false);
const DEV_SPKI = (() => { const s = new Uint8Array(91); s.set(P256_PREFIX, 0); s.set(DEV_PUB, 26); return s; })();
const DEV_ID = toHex(sha256(DEV_SPKI));

const pairing = { hostId: 'host-001', hostPubKeyB64: HOST_B64, deviceId: DEV_ID, baseURL: 'http://daemon', pairedAt: '2026', role: 'owner' };
const deviceKey = {
  getKeyInfo: async () => ({ provider: 'android_keystore' as const, keyVersion: 1 as const, deviceId: DEV_ID, publicKeySpki: DEV_SPKI, hardwareBacked: true as const, nonExportable: true as const, securityLevel: 'tee' as const }),
  sign: async (msg: Uint8Array) => p256.sign(msg, DEV_PRIV, { format: 'der', prehash: true }),
  getSupport: async () => 'supported' as const, hasKey: async () => true, ensureKey: async () => { throw new Error('nope'); }, getPublicKeySpki: async () => DEV_SPKI, deleteKey: async () => {},
};

describe('authenticate — full challenge/verify', () => {
  beforeEach(() => mockFetch.mockReset());

  it('happy path: challenge → verify host → sign → verify → bearer', async () => {
    // 1. Challenge
    const bootId = 'boot-123';
    const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
    const sn = new Uint8Array(32); sn[0] = 0xab;
    const cn = new Uint8Array(32); cn[0] = 0x10;
    const expiresAt = new Date(Date.now() + 300_000);
    const createdAtMS = expiresAt.getTime() - 300_000;

    // Capture clientNonce from the challenge request body.
    let capturedNonceBytes = new Uint8Array(32);
    mockFetch.mockImplementationOnce(async (url: string, init: RequestInit) => {
      const body = JSON.parse(init!.body as string);
      capturedNonceBytes = (() => { const b = new Uint8Array(32); for (let i = 0; i < 64; i += 2) b[i >> 1] = parseInt(body.clientNonce.substr(i, 2), 16); return b; })();
      // Build host signature over the ACTUAL captured client nonce bytes.
      const t = buildAuthTranscript({
        role: 'host', hostId: 'host-001', deviceId: DEV_ID, daemonBootId: bootId,
        challengeId: cid, clientNonce: capturedNonceBytes, serverNonce: sn,
        createdAtMS, expiresAtMS: expiresAt.getTime(),
      });
      const sig = p256.sign(t, HOST_PRIV, { format: 'der', prehash: true });
      return { ok: true, json: async () => ({ challengeId: toHex(cid), serverNonce: toHex(sn), hostSignature: toHex(sig), daemonBootId: bootId, hostId: 'host-001', expiresAt: expiresAt.toISOString() }) } as any;
    });

    // 2. Verify — must be called with the correct device signature.
    mockFetch.mockImplementationOnce(async (url: string, init: RequestInit) => {
      const body = JSON.parse(init!.body as string);
      const devSig = body.signature;
      // Rebuild the device transcript and verify the device signature.
      const t = buildAuthTranscript({
        role: 'device', hostId: 'host-001', deviceId: DEV_ID, daemonBootId: bootId,
        challengeId: cid, clientNonce: capturedNonceBytes, serverNonce: sn,
        createdAtMS, expiresAtMS: expiresAt.getTime(),
      });
      const sigBytes = new Uint8Array(devSig.match(/.{2}/g).map((h: string) => parseInt(h, 16)));
      const ok = p256.verify(sigBytes, t, DEV_PUB, { format: 'der', prehash: true });
      if (!ok) return { ok: false, status: 401 } as any;
      const bearerExp = new Date(Date.now() + 600_000);
      return { ok: true, json: async () => ({ token: 'bearer-abc', tokenType: 'Bearer', expiresAt: bearerExp.toISOString(), permissions: ['sessions:read', 'sessions:create'], deviceId: DEV_ID }) } as any;
    });

    const result = await authenticate(pairing, deviceKey as any, 'http://daemon');
    expect(result.token).toBe('bearer-abc');
    expect(result.permissions).toContain('sessions:read');
  });
});
