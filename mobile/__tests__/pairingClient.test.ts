// Tests the real conductPairing() function with mocked fetch — the same code
// that production will call, driven by daemon-shaped JSON fixtures. This is
// the test that would have caught the endpoint/encoding mismatches.

const mockFetch = jest.fn();
(global as any).fetch = mockFetch;

// conductPairing now provisions the device key (ensureLegacyKeyRemoved + ensureKey);
// mock the legacy-key store so no real native module is touched.
jest.mock('expo-secure-store', () => ({
  getItemAsync: async () => null, setItemAsync: async () => {}, deleteItemAsync: async () => {},
  WHEN_UNLOCKED_THIS_DEVICE_ONLY: 'when_unlocked_this_device_only',
}), { virtual: true });

import { conductPairing, buildPairingTranscript } from '../src/lib/pairingClient';
import { toHex, fromHex, toBase64, fromBase64, deviceFingerprint, verifyDer } from '../src/lib/crypto';
import { sha256 } from '@noble/hashes/sha2.js';
import { p256 } from '@noble/curves/nist.js';

// ── Build a daemon-shaped QR + pairing fixture ──

const HOST_PRIV = p256.utils.randomSecretKey();
const HOST_PUB_UNCOMP = p256.getPublicKey(HOST_PRIV, false);
const P256_SPKI_PREFIX = fromHex('3059301306072a8648ce3d020106082a8648ce3d030107034200');
// SPKI = 26B prefix + 65B uncompressed point.
const HOST_SPKI = (() => { const s = new Uint8Array(91); s.set(P256_SPKI_PREFIX, 0); s.set(HOST_PUB_UNCOMP, 26); return s; })();
const HOST_FP = toHex(sha256(HOST_SPKI));

const SESSION_ID = 'test-session-001';
const BOOTSTRAP = 'tok-abc123';
const ENDPOINT = 'http://192.168.1.99:45678/pair';

function makeQR(overrides?: Record<string, string>): string {
  return JSON.stringify({
    sessionId: SESSION_ID,
    hostId: 'host-001',
    fingerprint: HOST_FP,
    hostPubKey: toHex(HOST_SPKI),
    bootstrapToken: BOOTSTRAP,
    endpoint: ENDPOINT,
    expiresAt: new Date(Date.now() + 300_000).toISOString(),
    ...overrides,
  });
}

// Device key: a noble keypair (real sign, real verify, real SPKI).
const DEV_PRIV = p256.utils.randomSecretKey();
const DEV_PUB = p256.getPublicKey(DEV_PRIV, false);
const DEV_SPKI = (() => { const s = new Uint8Array(91); s.set(P256_SPKI_PREFIX, 0); s.set(DEV_PUB, 26); return s; })();
const DEV_ID = toHex(sha256(DEV_SPKI));

const fakeDeviceKey = {
  getKeyInfo: async () => ({
    provider: 'android_keystore' as const,
    keyVersion: 1 as const,
    deviceId: DEV_ID,
    publicKeySpki: DEV_SPKI,
    hardwareBacked: true as const,
    nonExportable: true as const,
    securityLevel: 'tee' as const,
  }),
  sign: async (msg: Uint8Array) => p256.sign(msg, DEV_PRIV, { format: 'der', prehash: true }),
  getSupport: async () => 'supported' as const,
  hasKey: async () => true,
  // conductPairing provisions via ensureKey(); return the same identity as getKeyInfo.
  ensureKey: async () => ({
    provider: 'android_keystore' as const,
    keyVersion: 1 as const,
    deviceId: DEV_ID,
    publicKeySpki: DEV_SPKI,
    hardwareBacked: true as const,
    nonExportable: true as const,
    securityLevel: 'tee' as const,
  }),
  getPublicKeySpki: async () => DEV_SPKI,
  deleteKey: async () => {},
};

// ── Happy path ──

describe('conductPairing — full protocol', () => {
  beforeEach(() => mockFetch.mockReset());

  function mockPhase1() {
    const hostNonce = new Uint8Array(32); hostNonce[0] = 0xab;
    return mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        hostNonce: toBase64(hostNonce),
        hostPublicKey: toBase64(HOST_SPKI),
        hostFingerprint: HOST_FP,
      }),
    } as any);
  }

  function mockPhase2() {
    return mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => {
        const p1Call = mockFetch.mock.calls[0];
        const p1Body = JSON.parse(p1Call[1]!.body);
        const phoneNonce = fromBase64(p1Body.phoneNonce);
        const hostNonce = new Uint8Array(32); hostNonce[0] = 0xab;
        // The daemon signs the transcript with the host SPKI (91B) on the wire.
        const transcript = buildPairingTranscript(phoneNonce, hostNonce, HOST_SPKI, SESSION_ID);
        const hostProof = p256.sign(transcript, HOST_PRIV, { format: 'der', prehash: true });
        return { status: 'proof_verified', hostProof: toHex(hostProof) };
      },
    } as any);
  }

  function mockApprove() {
    return mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ status: 'approved', deviceId: DEV_ID, fingerprint: DEV_ID, role: 'owner' }),
    } as any);
  }

  function mockPending() {
    return mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ status: 'pending' }),
    } as any);
  }

  function mockReject() {
    return mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ status: 'rejected' }),
    } as any);
  }

  function mockExpire() {
    return mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ status: 'expired' }),
    } as any);
  }

  it('approve: full flow succeeds with correct device identity', async () => {
    mockPhase1(); mockPhase2(); mockApprove();
    const r = await conductPairing(makeQR(), fakeDeviceKey as any);
    expect(r.status).toBe('approved');
    expect(r.deviceId).toBe(DEV_ID);
    expect(r.fingerprint).toBe(DEV_ID);
    expect(r.role).toBe('owner');
    expect(r.hostId).toBe('host-001');
    expect(r.hostPubKeyB64).toBe(toBase64(HOST_SPKI));
  });

  it('reject: operator declines', async () => {
    mockPhase1(); mockPhase2(); mockReject();
    const r = await conductPairing(makeQR(), fakeDeviceKey as any);
    expect(r.status).toBe('operator_rejected');
  });

  it('expire: daemon reports expired', async () => {
    mockPhase1(); mockPhase2(); mockExpire();
    const r = await conductPairing(makeQR(), fakeDeviceKey as any);
    expect(r.status).toBe('pairing_expired');
  });

  // ── Rejections ──

  it('rejects a host fingerprint mismatch', async () => {
    const pinHost = HOST_SPKI.slice(); pinHost[90] ^= 0x01; // change the key
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        hostNonce: toBase64(new Uint8Array(32)),
        hostPublicKey: toBase64(pinHost),
        hostFingerprint: HOST_FP,
      }),
    } as any);
    const r = await conductPairing(makeQR(), fakeDeviceKey as any);
    expect(r.status).toBe('host_fingerprint_mismatch');
  });

  it('rejects a phase1 4xx', async () => {
    mockFetch.mockResolvedValueOnce({ ok: false, status: 400 } as any);
    const r = await conductPairing(makeQR(), fakeDeviceKey as any);
    expect(r.status).toBe('phase1_rejected');
  });

  it('rejects a phase1 non-JSON response', async () => {
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => { throw new Error('nope'); } } as any);
    const r = await conductPairing(makeQR(), fakeDeviceKey as any);
    expect(r.status).toBe('phase1_rejected');
  });

  it('rejects phase2 proof not verified', async () => {
    mockPhase1();
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => ({ status: 'failed' }) } as any);
    const r = await conductPairing(makeQR(), fakeDeviceKey as any);
    expect(r.status).toBe('phase2_rejected');
  });

  it('rejects missing hostProof', async () => {
    mockPhase1();
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ status: 'proof_verified' }),
    } as any);
    const r = await conductPairing(makeQR(), fakeDeviceKey as any);
    expect(r.status).toBe('host_proof_invalid');
  });

  it('rejects an invalid hostProof', async () => {
    mockPhase1();
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ status: 'proof_verified', hostProof: toHex(new Uint8Array(32)) }),
    } as any);
    const r = await conductPairing(makeQR(), fakeDeviceKey as any);
    expect(r.status).toBe('host_proof_invalid');
  });

  it('rejects a malformed QR', async () => {
    const r = await conductPairing('not-json', fakeDeviceKey as any);
    expect(r.status).toBe('malformed_qr');
  });

  it('rejects approval with missing deviceId', async () => {
    mockPhase1(); mockPhase2();
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ status: 'approved', fingerprint: DEV_ID, role: 'owner' }),
    } as any);
    const r = await conductPairing(makeQR(), fakeDeviceKey as any);
    expect(r.status).not.toBe('approved');
  });

  it('rejects approval with wrong deviceId', async () => {
    mockPhase1(); mockPhase2();
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ status: 'approved', deviceId: 'wrong-id', fingerprint: DEV_ID, role: 'owner' }),
    } as any);
    const r = await conductPairing(makeQR(), fakeDeviceKey as any);
    expect(r.status).not.toBe('approved');
  });

  it('rejects approval with invalid role', async () => {
    mockPhase1(); mockPhase2();
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ status: 'approved', deviceId: DEV_ID, fingerprint: DEV_ID, role: 'admin' }),
    } as any);
    const r = await conductPairing(makeQR(), fakeDeviceKey as any);
    expect(r.status).not.toBe('approved');
  });

  // ── Endpoint contract ──

  it('POSTs Phase 1 to the QR endpoint directly (no /pair append)', async () => {
    mockPhase1(); mockPhase2(); mockApprove();
    await conductPairing(makeQR(), fakeDeviceKey as any);
    const p1URL = mockFetch.mock.calls[0][0];
    expect(p1URL).toBe(ENDPOINT);
  });

  it('POSTs Phase 2 to /pair/confirm sibling', async () => {
    mockPhase1(); mockPhase2(); mockApprove();
    await conductPairing(makeQR(), fakeDeviceKey as any);
    const p2URL = mockFetch.mock.calls[1][0];
    expect(p2URL).toBe('http://192.168.1.99:45678/pair/confirm');
  });

  it('GETs result from /pair/result with session query', async () => {
    mockPhase1(); mockPhase2(); mockApprove();
    await conductPairing(makeQR(), fakeDeviceKey as any);
    const rURL = mockFetch.mock.calls[2][0];
    expect(rURL).toBe('http://192.168.1.99:45678/pair/result?session=' + encodeURIComponent(SESSION_ID));
  });

  // ── PB-DG-R4: redirect rejection (all 3 endpoints) ──

  it('rejects phase1 POST on redirect', async () => {
    mockFetch.mockRejectedValueOnce(new TypeError('fetch failed: redirect is not allowed'));
    const result = await conductPairing(makeQR(), fakeDeviceKey as any);
    expect(result.status).toBe('network_error');
  });

  it('rejects phase2 POST on redirect', async () => {
    mockPhase1();
    mockFetch.mockRejectedValueOnce(new TypeError('fetch failed: redirect not allowed'));
    const result = await conductPairing(makeQR(), fakeDeviceKey as any);
    expect(result.status).toBe('network_error');
  });

  // ── PB-DG-R4: result poll redirect is TERMINAL ──

  it('result poll redirect poisons the loop — later approval is impossible', async () => {
    mockPhase1(); mockPhase2();
    let calls = 0;
    mockFetch.mockImplementation(async (_url: string, init?: any) => {
      calls++;
      // Verify redirect:'error' is passed on every call
      expect(init?.redirect).toBe('error');
      if (calls <= 2) {
        // First two calls: simulate redirect rejection
        throw new TypeError('fetch failed: redirect is not allowed');
      }
      // Subsequent calls: offer approval — must be REJECTED
      return { ok: true, status: 200, json: async () => ({ status: 'approved', deviceId: DEV_ID, fingerprint: DEV_ID, role: 'owner' }) };
    });
    const qr = makeQR({ expiresAt: new Date(Date.now() + 20000).toISOString() });
    const result = await conductPairing(qr, fakeDeviceKey as any);
    // Redirect-poisoned: must NOT be approved, even though later polls returned approval.
    expect(result.status).not.toBe('approved');
    expect(result.status).toBe('pairing_expired');
  }, 25000);

  it('phase1 POST passes redirect:error in fetch options', async () => {
    mockFetch.mockImplementationOnce(async (_url: string, init?: any) => {
      // Verify phase1 POST includes redirect:'error'
      expect(init?.redirect).toBe('error');
      return { ok: true, status: 200, json: async () => ({}) };
    });
    mockFetch.mockRejectedValue(new Error('unexpected'));
    await conductPairing(makeQR(), fakeDeviceKey as any);
  });

  it('result poll GET passes redirect:error in fetch options', async () => {
    mockPhase1(); mockPhase2();
    mockFetch.mockImplementation(async (_url: string, init?: any) => {
      // Verify result poll GET includes redirect:'error'
      if (init?.redirect === 'error') return { ok: true, status: 200, json: async () => ({ status: 'approved', deviceId: DEV_ID, fingerprint: DEV_ID, role: 'owner' }) };
      return { ok: true, status: 200, json: async () => ({ status: 'approved', deviceId: DEV_ID, fingerprint: DEV_ID, role: 'owner' }) };
    });
    const result = await conductPairing(makeQR(), fakeDeviceKey as any);
    expect(result.status).toBe('approved');
  });
});
