// M3-auth-2B: iOS end-to-end production-path proof.
//
// The pairing → bearer → WS-ticket → Terminal chain is cross-platform
// TypeScript; the only native surface is the DeviceKey (iOS Secure Enclave,
// M3-auth-1B). This suite drives the REAL production functions (not helper
// copies) with a WRAPPED iOS Secure Enclave DeviceKey — validated through the
// shared parser as provider=ios_secure_enclave / securityLevel=secure_enclave —
// under Platform.OS='ios', against a host-signed fake daemon (real host + device
// P-256 keypairs, same technique as authClient/pairingClient tests). It proves,
// on the iOS platform:
//
//   - the wrapped iOS DeviceKey is accepted as ios_secure_enclave and a missing
//     native module fails closed;
//   - conductPairing (real protocol, SE-signed) reaches `approved`;
//   - the production ConnectScreen transition (pairFromScannedQR) drives
//     pair→approve→onPaired-BEFORE-connect and fails closed with no partial
//     paired state / no legacy fallback;
//   - completePairing enters paired_device only on a matching canonical origin,
//     and rejects a mismatched / missing pairing (cold-start restore guard);
//   - TokenManager challenge/verify produces a bearer SIGNED BY the SE key;
//   - getWSTicket yields a 64-hex ticket; the Terminal bootstrap injects ONLY the
//     ticket into the WebView HTML — never the bearer, key, nonce, or signature;
//   - deriveTerminalAuth routes paired_device to the ticket path, never ?token=;
//   - the host-bound transport refuses the iOS bearer to a non-paired origin.

jest.mock('react-native', () => ({ Platform: { OS: 'ios' }, NativeModules: {} }), { virtual: true });

// conductPairing provisions the key (ensureLegacyKeyRemoved) and the full-chain
// test persists via pairingStore — mock the native storages.
jest.mock('expo-secure-store', () => ({
  getItemAsync: async () => null, setItemAsync: async () => {}, deleteItemAsync: async () => {},
  WHEN_UNLOCKED_THIS_DEVICE_ONLY: 'when_unlocked_this_device_only',
}), { virtual: true });
jest.mock('@react-native-async-storage/async-storage', () => {
  const store = new Map<string, string>();
  return { __esModule: true, default: {
    getItem: async (k: string) => (store.has(k) ? store.get(k)! : null),
    setItem: async (k: string, v: string) => { store.set(k, v); },
    removeItem: async (k: string) => { store.delete(k); },
  } };
}, { virtual: true });

import { p256 } from '@noble/curves/nist.js';
import { sha256 } from '@noble/hashes/sha2.js';
import { toHex, toBase64, fromBase64 } from '../src/lib/crypto';
import { buildAuthTranscript } from '../src/lib/authTranscript';
import { TokenManager } from '../src/lib/authClient';
import { getWSTicket, WS_TICKET_MAX_TTL_MS } from '../src/lib/wsTicket';
import { completePairing, deriveTerminalAuth } from '../src/lib/authMode';
import { conductPairing, buildPairingTranscript } from '../src/lib/pairingClient';
import { pairAndSave } from '../src/lib/pairAndSave';
import { loadPairing } from '../src/lib/pairingStore';
import { pairFromScannedQR, isPairingQR, runScan } from '../src/lib/connectPairing';
import { TerminalController } from '../src/lib/terminalController';
import { _createWithNative } from '../modules/pokit-device-key/src';
import type { NativePokitDeviceKey } from '../modules/pokit-device-key/src';
import {
  setBaseURL, setDeviceAuth, listSessionProfiles, createSession, ConnectivityFailure,
} from '../src/lib/client';

const mockFetch = jest.fn();
(global as any).fetch = mockFetch;

// ── host + device keypairs (P-256) ──
const P256_PREFIX = new Uint8Array([0x30, 0x59, 0x30, 0x13, 0x06, 0x07, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x02, 0x01, 0x06, 0x08, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x03, 0x01, 0x07, 0x03, 0x42, 0x00]);
function spkiOf(pub: Uint8Array): Uint8Array { const s = new Uint8Array(91); s.set(P256_PREFIX, 0); s.set(pub, 26); return s; }

const HOST_PRIV = p256.utils.randomSecretKey();
const HOST_SPKI = spkiOf(p256.getPublicKey(HOST_PRIV, false));
const HOST_B64 = toBase64(HOST_SPKI);
const HOST_FP = toHex(sha256(HOST_SPKI));

const DEV_PRIV = p256.utils.randomSecretKey();
const DEV_PUB = p256.getPublicKey(DEV_PRIV, false);
const DEV_SPKI = spkiOf(DEV_PUB);
const DEV_SPKI_HEX = toHex(DEV_SPKI);
const DEV_ID = toHex(sha256(DEV_SPKI));

const BASE = 'https://host-a.example.com';
const OTHER = 'https://host-b.example.com';

const pairing = {
  hostId: 'host-001', hostPubKeyB64: HOST_B64, deviceId: DEV_ID,
  baseURL: BASE, origin: BASE, pairedAt: '2026', role: 'owner',
};

// A fake iOS Secure Enclave native module backed by the real DEV keypair, so its
// signatures actually verify. Wrapped via _createWithNative so the shared parser
// validates provider=ios_secure_enclave / securityLevel=secure_enclave / the
// recomputed deviceId — i.e. the exact iOS contract.
function iosNative(): NativePokitDeviceKey {
  const info = () => ({
    provider: 'ios_secure_enclave', keyVersion: 1, deviceId: DEV_ID,
    publicKeySpkiHex: DEV_SPKI_HEX, hardwareBacked: true, nonExportable: true,
    securityLevel: 'secure_enclave',
  });
  return {
    getSupport: async () => 'supported',
    hasKey: async () => true,
    ensureKey: async () => info(),
    getKeyInfo: async () => info(),
    getPublicKeySpki: async () => DEV_SPKI_HEX,
    sign: async (messageHex: string) => {
      const msg = new Uint8Array(messageHex.match(/.{2}/g)!.map(h => parseInt(h, 16)));
      return toHex(p256.sign(msg, DEV_PRIV, { format: 'der', prehash: true }));
    },
    deleteKey: async () => {},
  };
}
function iosDeviceKey() { return _createWithNative(iosNative()); }

// A wrapped iOS DeviceKey whose native getKeyInfo/ensureKey fail with a coded
// error — used to exercise cold-start identity binding (key deleted / inaccessible).
function iosThrowingKey(code: string) {
  const boom = async () => { throw Object.assign(new Error('native'), { code }); };
  return _createWithNative({
    getSupport: async () => 'supported', hasKey: async () => true,
    ensureKey: boom, getKeyInfo: boom, getPublicKeySpki: boom,
    sign: boom, deleteKey: async () => {},
  } as NativePokitDeviceKey);
}

// ── host-signed fake daemon: challenge/verify (bearer), mirroring authClient.test.ts ──
interface Session { cn: Uint8Array; createdAtMS: number; expiresAtMS: number; bootId: string; cid: Uint8Array; sn: Uint8Array; }
let tokenCounter = 0;
function makeSession(bootId: string): Session {
  const now = Date.now();
  const cid = new Uint8Array(32); for (let i = 0; i < 32; i++) cid[i] = i + 1;
  const sn = new Uint8Array(32); for (let i = 0; i < 32; i++) sn[i] = 200 - i;
  return { cn: new Uint8Array(32), createdAtMS: now, expiresAtMS: now + 300_000, bootId, cid, sn };
}
function mockChallenge(ses: Session) {
  mockFetch.mockImplementationOnce(async (_url: string, init: RequestInit) => {
    const body = JSON.parse(init!.body as string);
    const cn = new Uint8Array(32);
    for (let i = 0; i < 64; i += 2) cn[i >> 1] = parseInt(body.clientNonce.substr(i, 2), 16);
    ses.cn.set(cn);
    const t = buildAuthTranscript({ role: 'host', hostId: 'host-001', deviceId: DEV_ID, daemonBootId: ses.bootId, challengeId: ses.cid, clientNonce: ses.cn, serverNonce: ses.sn, createdAtMS: ses.createdAtMS, expiresAtMS: ses.expiresAtMS });
    const sig = p256.sign(t, HOST_PRIV, { format: 'der', prehash: true });
    return { ok: true, status: 200, json: async () => ({ version: 1, challengeId: toHex(ses.cid), hostId: 'host-001', hostKeyFingerprint: HOST_FP, daemonBootId: ses.bootId, serverNonce: toHex(ses.sn), hostSignature: toHex(sig), issuedAt: new Date(ses.createdAtMS).toISOString(), expiresAt: new Date(ses.expiresAtMS).toISOString() }) } as any;
  });
}
function mockVerify(ses: Session): string {
  const token = (++tokenCounter).toString(16).padStart(64, '0');
  mockFetch.mockImplementationOnce(async (_url: string, init: RequestInit) => {
    const body = JSON.parse(init!.body as string);
    const sig = new Uint8Array(body.signature.match(/.{2}/g)!.map((h: string) => parseInt(h, 16)));
    const t = buildAuthTranscript({ role: 'device', hostId: 'host-001', deviceId: DEV_ID, daemonBootId: ses.bootId, challengeId: ses.cid, clientNonce: ses.cn, serverNonce: ses.sn, createdAtMS: ses.createdAtMS, expiresAtMS: ses.expiresAtMS });
    if (!p256.verify(sig, t, DEV_PUB, { format: 'der', prehash: true })) throw new Error('device sig mismatch');
    return { ok: true, status: 200, json: async () => ({ token, tokenType: 'Bearer', expiresAt: new Date(Date.now() + 600_000).toISOString(), permissions: ['sessions:read', 'sessions:create'], deviceId: DEV_ID }) } as any;
  });
  return token;
}

// ── host-signed fake daemon: pairing protocol, mirroring pairingClient.test.ts ──
const PAIR_ENDPOINT = 'http://192.168.1.99:45678/pair';
function makePairingQR(): string {
  return JSON.stringify({
    sessionId: 'pair-session-001', hostId: 'host-001', fingerprint: HOST_FP,
    hostPubKey: toHex(HOST_SPKI), bootstrapToken: 'boot-abc', endpoint: PAIR_ENDPOINT,
    expiresAt: new Date(Date.now() + 300_000).toISOString(),
  });
}
function mockPairApprove() {
  const hostNonce = new Uint8Array(32); hostNonce[0] = 0xab;
  mockFetch.mockResolvedValueOnce({ ok: true, json: async () => ({ hostNonce: toBase64(hostNonce), hostPublicKey: toBase64(HOST_SPKI), hostFingerprint: HOST_FP }) } as any);
  mockFetch.mockImplementationOnce(async (_url: string, init: RequestInit) => {
    const p1Body = JSON.parse((mockFetch.mock.calls[0][1] as any).body);
    const phoneNonce = fromBase64(p1Body.phoneNonce);
    const t = buildPairingTranscript(phoneNonce, hostNonce, HOST_SPKI, 'pair-session-001');
    const hostProof = p256.sign(t, HOST_PRIV, { format: 'der', prehash: true });
    return { ok: true, json: async () => ({ status: 'proof_verified', hostProof: toHex(hostProof) }) } as any;
  });
  mockFetch.mockResolvedValueOnce({ ok: true, json: async () => ({ status: 'approved', deviceId: DEV_ID, fingerprint: DEV_ID, role: 'owner' }) } as any);
}

describe('M3-auth-2B iOS integration', () => {
  beforeEach(() => { mockFetch.mockReset(); setDeviceAuth(null); });
  afterEach(() => setDeviceAuth(null));

  // ── DeviceKey selection ──

  it('the wrapped iOS DeviceKey is accepted as ios_secure_enclave', async () => {
    const info = await iosDeviceKey().getKeyInfo();
    expect(info.provider).toBe('ios_secure_enclave');
    expect(info.securityLevel).toBe('secure_enclave');
    expect(info.deviceId).toBe(DEV_ID);
    expect(info.publicKeySpki.length).toBe(91);
  });

  it('a missing native module fails closed (no JS/software key)', async () => {
    const k = _createWithNative(null);
    expect(await k.getSupport()).toBe('not_implemented');
    await expect(k.ensureKey()).rejects.toMatchObject({ code: 'unsupported' });
    await expect(k.sign(new Uint8Array(1))).rejects.toThrow();
  });

  // ── Pairing protocol (SE-signed) ──

  it('conductPairing reaches approved, driven by the Secure Enclave signature', async () => {
    mockPairApprove();
    const r = await conductPairing(makePairingQR(), iosDeviceKey() as any);
    expect(r.status).toBe('approved');
    expect(r.deviceId).toBe(DEV_ID);   // identity derived from the SE public key
    expect(r.role).toBe('owner');
    expect(r.hostPubKeyB64).toBe(HOST_B64);
  });

  // ── Production ConnectScreen transition (pairFromScannedQR) ──

  it('isPairingQR keys on the pairing payload, not a plain URL', () => {
    expect(isPairingQR(makePairingQR())).toBe(true);
    expect(isPairingQR('https://host-a.example.com')).toBe(false);
    expect(isPairingQR('not json')).toBe(false);
  });

  it('approved scan installs trusted auth (onPaired) BEFORE connect, with the iOS key', async () => {
    const order: string[] = [];
    let pairedKeyProvider = '';
    await pairFromScannedQR({
      data: makePairingQR(),
      createDeviceKey: () => iosDeviceKey() as any,
      getBaseURL: () => BASE,
      pairAndSave: async (_qr, dk) => { pairedKeyProvider = (await dk.getKeyInfo()).provider; return { status: 'approved' } as any; },
      connect: async () => { order.push('connect'); },
      onPaired: async () => { order.push('onPaired'); return true; },
      onReject: () => order.push('reject'),
      onError: () => order.push('error'),
      onNeedBaseURL: () => order.push('needBase'),
    });
    expect(pairedKeyProvider).toBe('ios_secure_enclave');
    expect(order).toEqual(['onPaired', 'connect']); // trusted state before connect; no reject/error
  });

  it('a rejected pairing fails closed: no connect, scanner re-enabled (no legacy fallback)', async () => {
    const order: string[] = [];
    await pairFromScannedQR({
      data: makePairingQR(),
      createDeviceKey: () => iosDeviceKey() as any,
      getBaseURL: () => BASE,
      pairAndSave: async () => ({ status: 'operator_rejected' } as any),
      connect: async () => { order.push('connect'); },
      onPaired: async () => { order.push('onPaired'); return true; },
      onReject: () => order.push('reject'),
      onError: () => order.push('error'),
      onNeedBaseURL: () => order.push('needBase'),
    });
    expect(order).toContain('error');
    expect(order).toContain('reject');
    expect(order).not.toContain('connect');
    expect(order).not.toContain('onPaired');
  });

  it('a thrown pairing error and a missing base URL both fail closed', async () => {
    const a: string[] = [];
    await pairFromScannedQR({
      data: makePairingQR(), createDeviceKey: () => iosDeviceKey() as any, getBaseURL: () => BASE,
      pairAndSave: async () => { throw new Error('boom'); }, onPaired: async () => true,
      connect: async () => { a.push('connect'); }, onReject: () => a.push('reject'),
      onError: () => a.push('error'), onNeedBaseURL: () => a.push('needBase'),
    });
    expect(a).toEqual(['error', 'reject']);

    const b: string[] = [];
    let pairCalled = false;
    await pairFromScannedQR({
      data: makePairingQR(), createDeviceKey: () => iosDeviceKey() as any, getBaseURL: () => '',
      pairAndSave: async () => { pairCalled = true; return { status: 'approved' } as any; }, onPaired: async () => true,
      connect: async () => { b.push('connect'); }, onReject: () => b.push('reject'),
      onError: () => b.push('error'), onNeedBaseURL: () => b.push('needBase'),
    });
    expect(b).toEqual(['needBase', 'reject']);
    expect(pairCalled).toBe(false); // never attempt pairing without an operational origin
  });

  it('a remote pairing without an onPaired installer fails closed (no connect, no pairing)', async () => {
    const a: string[] = [];
    let pairCalled = false;
    await pairFromScannedQR({
      data: makePairingQR(), createDeviceKey: () => iosDeviceKey() as any, getBaseURL: () => BASE,
      pairAndSave: async () => { pairCalled = true; return { status: 'approved' } as any; },
      connect: async () => { a.push('connect'); }, onReject: () => a.push('reject'),
      onError: () => a.push('error'), onNeedBaseURL: () => a.push('needBase'),
      // onPaired intentionally omitted — a re-pair with no trusted-state installer
      // must NOT connect on top of a stale TokenManager.
    });
    expect(a).toEqual(['error', 'reject']);
    expect(pairCalled).toBe(false);
  });

  // ── Cold-start restore / paired_device guard ──

  it('completePairing enters paired_device only on a matching canonical origin', async () => {
    const ok = await completePairing({
      loadPairing: async () => pairing as any, getBaseURL: () => BASE,
      createDeviceKey: () => iosDeviceKey() as any,
      makeTokenManager: (p, dk, base) => new TokenManager(p as any, dk as any, base),
    });
    expect(ok.mode).toBe('paired_device');
    expect(ok.baseURL).toBe(BASE);
  });

  it('cold-start rejects a mismatched origin and a missing pairing (fail closed)', async () => {
    const mismatch = await completePairing({
      loadPairing: async () => pairing as any, getBaseURL: () => OTHER, // origin ≠ stored
      createDeviceKey: () => iosDeviceKey() as any,
      makeTokenManager: (p, dk, base) => new TokenManager(p as any, dk as any, base),
    });
    expect(mismatch.mode).toBe('failed');

    const missing = await completePairing({
      loadPairing: async () => null, getBaseURL: () => BASE,
      createDeviceKey: () => iosDeviceKey() as any,
      makeTokenManager: (p, dk, base) => new TokenManager(p as any, dk as any, base),
    });
    expect(missing.mode).toBe('pairing_required');
  });

  it('cold-start binds to the actual SE key: missing/mismatch → pairing_required, inaccessible → failed', async () => {
    const run = (createDeviceKey: () => any, p: any = pairing) => completePairing({
      loadPairing: async () => p, getBaseURL: () => BASE, createDeviceKey,
      makeTokenManager: (pp, dk, base) => new TokenManager(pp as any, dk as any, base),
    });
    // key deleted/wiped on this device → re-pairable
    expect((await run(() => iosThrowingKey('key_missing'))).mode).toBe('pairing_required');
    // a DIFFERENT Secure Enclave key exists → stored deviceId ≠ actual → re-pairable
    expect((await run(() => iosDeviceKey(), { ...pairing, deviceId: 'ff'.repeat(32) })).mode).toBe('pairing_required');
    // key present but inaccessible/invalidated → explicit security failure
    expect((await run(() => iosThrowingKey('key_inaccessible'))).mode).toBe('failed');
    // valid, matching identity → paired_device
    expect((await run(() => iosDeviceKey())).mode).toBe('paired_device');
  });

  it('fresh-install / post-wipe: the pairing path PROVISIONS the SE key, then enters paired_device', async () => {
    // A stateful native with NO key until ensureKey() creates it (fresh install
    // or key wipe). getKeyInfo/sign fail key_missing until provisioned.
    let provisioned = false;
    const info = () => ({ provider: 'ios_secure_enclave', keyVersion: 1, deviceId: DEV_ID, publicKeySpkiHex: DEV_SPKI_HEX, hardwareBacked: true, nonExportable: true, securityLevel: 'secure_enclave' });
    const missing = (): never => { throw Object.assign(new Error('x'), { code: 'key_missing' }); };
    const sharedNative: NativePokitDeviceKey = {
      getSupport: async () => 'supported',
      hasKey: async () => provisioned,
      ensureKey: async () => { provisioned = true; return info(); },
      getKeyInfo: async () => (provisioned ? info() : missing()),
      getPublicKeySpki: async () => (provisioned ? DEV_SPKI_HEX : missing()),
      sign: async (messageHex: string) => {
        if (!provisioned) return missing();
        const msg = new Uint8Array(messageHex.match(/.{2}/g)!.map(h => parseInt(h, 16)));
        return toHex(p256.sign(msg, DEV_PRIV, { format: 'der', prehash: true }));
      },
      deleteKey: async () => { provisioned = false; },
    };
    const makeKey = () => _createWithNative(sharedNative);
    const complete = () => completePairing({
      loadPairing: async () => (await loadPairing()) as any, getBaseURL: () => BASE,
      createDeviceKey: () => makeKey() as any,
      makeTokenManager: (p, dk, base) => new TokenManager(p as any, dk as any, base),
    });

    // Before pairing (no stored pairing AND no key) → pairing_required, not stuck.
    expect((await complete()).mode).toBe('pairing_required');

    mockPairApprove(); // daemon fake for conductPairing

    // The REAL chain: runScan → pairFromScannedQR → pairAndSave → conductPairing
    // (ensureKey provisions the key) → savePairing → onPaired (completePairing) →
    // connect. paired_device is entered only after the approved deviceId matches
    // the freshly provisioned identity.
    const order: string[] = [];
    await runScan({
      data: makePairingQR(), isPairing: isPairingQR,
      loadModules: async () => ({ pairFromScannedQR, pairAndSave, createDeviceKey: () => makeKey() as any, getBaseURL: () => BASE }),
      connect: async () => { order.push('connect'); },
      onPaired: async () => { const ctx = await complete(); order.push('paired:' + ctx.mode); return ctx.mode === 'paired_device'; },
      setScanned: () => {}, notifyError: (m) => order.push('error:' + m),
    });

    expect(provisioned).toBe(true);                              // the SE key was CREATED during pairing
    expect(order).toEqual(['paired:paired_device', 'connect']);  // then paired_device, then connect
    expect((await loadPairing())?.deviceId).toBe(DEV_ID);        // persisted pairing binds to the new identity
  });

  // ── ConnectScreen scan boundary (runScan) ──

  it('runScan resets the scanner on a module-load failure — never sticks', async () => {
    const ev: string[] = [];
    await runScan({
      data: makePairingQR(), isPairing: isPairingQR,
      loadModules: async () => { throw new Error('import failed'); },
      connect: async () => { ev.push('connect'); },
      onPaired: async () => true,
      setScanned: (v) => ev.push('scanned=' + v),
      notifyError: () => ev.push('error'),
    });
    expect(ev).toEqual(['scanned=true', 'error', 'scanned=false']); // lock, then reset exactly once
    expect(ev).not.toContain('connect');
  });

  it('runScan runs the real transition on a pairing QR, and connects a plain HTTPS URL', async () => {
    const ev: string[] = [];
    await runScan({
      data: makePairingQR(), isPairing: isPairingQR,
      loadModules: async () => ({
        pairFromScannedQR,
        pairAndSave: async () => ({ status: 'approved' } as any),
        createDeviceKey: () => iosDeviceKey() as any,
        getBaseURL: () => BASE,
      }),
      connect: async () => { ev.push('connect'); },
      onPaired: async () => { ev.push('onPaired'); return true; },
      setScanned: () => {}, notifyError: () => ev.push('error'),
    });
    expect(ev).toEqual(['onPaired', 'connect']); // trusted state installed before connect

    const ev2: string[] = [];
    await runScan({
      data: 'https://host-a.example.com', isPairing: isPairingQR,
      loadModules: async () => { throw new Error('must not load for a plain URL'); },
      connect: async () => { ev2.push('connect'); },
      setScanned: () => {}, notifyError: () => ev2.push('error'),
    });
    expect(ev2).toEqual(['connect']);
  });

  // ── Bearer + ticket ──

  it('TokenManager challenge→verify→bearer, signed by the Secure Enclave key', async () => {
    const ses = makeSession('boot-ios-1');
    mockChallenge(ses);
    const tok = mockVerify(ses);
    const mgr = new TokenManager(pairing as any, iosDeviceKey() as any, BASE);
    expect(await mgr.getValidToken()).toBe(tok);
    expect(mockFetch).toHaveBeenCalledTimes(2);
  });

  it('getWSTicket obtains a single-use 64-hex ticket with the iOS-derived bearer', async () => {
    const ses = makeSession('boot-ios-2');
    mockChallenge(ses);
    const tok = mockVerify(ses);
    const mgr = new TokenManager(pairing as any, iosDeviceKey() as any, BASE);
    const ticketHex = 'ab'.repeat(32);
    mockFetch.mockImplementationOnce(async (url: string, init: RequestInit) => {
      expect(String(url)).toContain('/api/device-auth/ws-ticket?session=');
      expect((init!.headers as Record<string, string>).Authorization).toBe(`Bearer ${tok}`);
      return { ok: true, status: 200, json: async () => ({ ticket: ticketHex, expiresAt: new Date(Date.now() + WS_TICKET_MAX_TTL_MS - 1000).toISOString() }) } as any;
    });
    const t = await getWSTicket('controlled_pty:s1', mgr, BASE);
    expect(t.ticket).toMatch(/^[0-9a-f]{64}$/);
    expect(t.ticket).toBe(ticketHex);
  });

  // ── Terminal: no secret leak ──

  it('Terminal bootstrap injects ONLY the ticket into the WebView HTML — never the bearer', async () => {
    const ses = makeSession('boot-ios-3');
    mockChallenge(ses);
    const tok = mockVerify(ses); // the device bearer — must NOT reach the HTML
    const mgr = new TokenManager(pairing as any, iosDeviceKey() as any, BASE);
    const ticketHex = 'cd'.repeat(32);

    // bootstrap fetches: HTML GET, size GET, ws-ticket POST (bearer is in the
    // Authorization header of each, never in the response HTML).
    mockFetch.mockImplementationOnce(async (_url: string, init: RequestInit) => {
      expect((init!.headers as Record<string, string>).Authorization).toBe(`Bearer ${tok}`);
      return { ok: true, status: 200, text: async () => '<html><head></head><body></body></html>' } as any;
    });
    mockFetch.mockImplementationOnce(async () => ({ ok: true, status: 200, json: async () => ({ rows: 24, cols: 80 }) } as any));
    mockFetch.mockImplementationOnce(async () => ({ ok: true, status: 200, json: async () => ({ ticket: ticketHex, expiresAt: new Date(Date.now() + WS_TICKET_MAX_TTL_MS - 1000).toISOString() }) } as any));

    const ctrl = new TerminalController();
    const { result } = await ctrl.bootstrap('controlled_pty:s1', mgr, BASE);
    expect(result).toBeDefined();
    expect(result!.ticket).toBe(ticketHex);
    // The one-time ticket IS in the injected script; the bearer / device secrets are NOT.
    expect(result!.html).toContain(ticketHex);
    expect(result!.html).not.toContain(tok);
    expect(result!.html).not.toContain(DEV_SPKI_HEX);
    expect(result!.html).not.toContain(HOST_B64);
  });

  it('a paired_device session routes to the ticket Terminal path, not legacy ?token=', () => {
    const r = deriveTerminalAuth({ mode: 'paired_device', tokenMgr: {} as any, baseURL: BASE }, 'controlled_pty:s1');
    expect(r.tokenMgr).toBeDefined();
    expect(r.baseURL).toBe(BASE);
    expect(r.termURI).toBeNull();
  });

  // ── Host-bound transport ──

  it('the host-bound transport refuses the iOS bearer to a non-paired origin (zero requests)', async () => {
    const mgr = new TokenManager(pairing as any, iosDeviceKey() as any, BASE);
    setDeviceAuth({ tokenManager: mgr, origin: BASE });
    setBaseURL(OTHER); // mismatch
    await expect(listSessionProfiles()).rejects.toMatchObject({ failure: ConnectivityFailure.AuthError });
    await expect(createSession({ profileId: 'shell' })).rejects.toMatchObject({ failure: ConnectivityFailure.AuthError });
    expect(mockFetch).not.toHaveBeenCalled();
  });
});
