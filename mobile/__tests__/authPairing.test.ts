// M3-auth-4A Blocker C: authoritative, atomic pairing completion.
//
// These tests exercise the RN-free helpers that App and ConnectScreen use so
// the ordering contract is proven without a React render:
//   - completePairing installs trusted state (origin-bound) or fails closed;
//   - pairThenConnect opens a connection ONLY after, and ONLY if, the trusted
//     state install succeeds.

import { completePairing, pairThenConnect } from '../src/lib/authMode';
import type { StoredPairing } from '../src/lib/pairingStore';
import type { TokenManager } from '../src/lib/authClient';
import type { PokitDeviceKey } from '../modules/pokit-device-key';

const BASE = 'https://term.example.com';
function pairing(origin?: string): StoredPairing {
  return {
    hostId: 'h', hostPubKeyB64: 'k', deviceId: 'd', baseURL: BASE,
    origin, pairedAt: '2026-07-11T00:00:00Z', role: 'owner',
  };
}
const fakeDeviceKey = {} as PokitDeviceKey;
const fakeTokenMgr = { __brand: 'tm' } as unknown as TokenManager;

function deps(over: Partial<Parameters<typeof completePairing>[0]> = {}) {
  return {
    loadPairing: async () => pairing('https://term.example.com'),
    getBaseURL: () => BASE,
    createDeviceKey: () => fakeDeviceKey,
    makeTokenManager: () => fakeTokenMgr,
    ...over,
  };
}

describe('completePairing', () => {
  it('origin-bound success → paired_device with a TokenManager', async () => {
    const ctx = await completePairing(deps());
    expect(ctx.mode).toBe('paired_device');
    expect(ctx.tokenMgr).toBe(fakeTokenMgr);
    expect(ctx.baseURL).toBe(BASE);
  });

  it('no saved pairing → pairing_required (scanner stays)', async () => {
    const ctx = await completePairing(deps({ loadPairing: async () => null }));
    expect(ctx.mode).toBe('pairing_required');
  });

  it('no base URL → pairing_required', async () => {
    const ctx = await completePairing(deps({ getBaseURL: () => '' }));
    expect(ctx.mode).toBe('pairing_required');
  });

  it('origin mismatch → failed (no partial paired state)', async () => {
    const ctx = await completePairing(deps({ loadPairing: async () => pairing('https://evil.example.com') }));
    expect(ctx.mode).toBe('failed');
    expect(ctx.tokenMgr).toBeUndefined();
  });

  it('pairing missing an origin → fails closed', async () => {
    const ctx = await completePairing(deps({ loadPairing: async () => pairing(undefined) }));
    expect(ctx.mode).toBe('failed');
  });

  it('non-HTTPS base → failed', async () => {
    const ctx = await completePairing(deps({ getBaseURL: () => 'http://term.example.com' }));
    expect(ctx.mode).toBe('failed');
  });

  it('DeviceKey creation failure → failed, never connects', async () => {
    const ctx = await completePairing(deps({ createDeviceKey: () => { throw new Error('no keystore'); } }));
    expect(ctx.mode).toBe('failed');
    expect(ctx.tokenMgr).toBeUndefined();
  });

  it('TokenManager creation failure → failed', async () => {
    const ctx = await completePairing(deps({ makeTokenManager: () => { throw new Error('bad'); } }));
    expect(ctx.mode).toBe('failed');
  });

  it('loadPairing throwing → failed', async () => {
    const ctx = await completePairing(deps({ loadPairing: async () => { throw new Error('storage'); } }));
    expect(ctx.mode).toBe('failed');
  });
});

describe('pairThenConnect ordering', () => {
  it('connects strictly AFTER trusted-state install resolves', async () => {
    const order: string[] = [];
    let resolvePaired!: (v: boolean) => void;
    const paired = new Promise<boolean>((r) => { resolvePaired = r; });

    const p = pairThenConnect({
      onPaired: () => { order.push('onPaired:start'); return paired; },
      connect: async () => { order.push('connect'); },
      onReject: () => order.push('reject'),
    });

    // connect must not have run while onPaired is still pending.
    await Promise.resolve();
    expect(order).toEqual(['onPaired:start']);

    resolvePaired(true);
    await p;
    expect(order).toEqual(['onPaired:start', 'connect']);
  });

  it('does NOT connect when trusted-state install fails; keeps scanner', async () => {
    const order: string[] = [];
    await pairThenConnect({
      onPaired: async () => { order.push('onPaired'); return false; },
      connect: async () => { order.push('connect'); },
      onReject: () => order.push('reject'),
    });
    expect(order).toEqual(['onPaired', 'reject']);
    expect(order).not.toContain('connect');
  });

  it('connects when no onPaired callback is supplied (legacy default)', async () => {
    const order: string[] = [];
    await pairThenConnect({
      onPaired: undefined,
      connect: async () => { order.push('connect'); },
      onReject: () => order.push('reject'),
    });
    expect(order).toEqual(['connect']);
  });
});
