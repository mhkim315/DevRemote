// M3-auth-4A Blocker C: authoritative, atomic pairing completion.
//
// These tests exercise the RN-free helpers that App and ConnectScreen use so
// the ordering contract is proven without a React render:
//   - completePairing installs trusted state (origin-bound) or fails closed;
//   - pairThenConnect opens a connection ONLY after, and ONLY if, the trusted
//     state install succeeds.

import { completePairing, pairThenConnect, selectAppRoute } from '../src/lib/authMode';
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
// M3-auth-2B: completePairing now binds the stored pairing to the actual device
// key identity (getKeyInfo().deviceId === stored deviceId) before paired_device.
const fakeDeviceKey = { getKeyInfo: async () => ({ deviceId: 'd' }) } as unknown as PokitDeviceKey;
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

  // M3-auth-2B: cold-start / restore binds the stored identity to the actual key.
  it('key deleted on this device (key_missing) → pairing_required (re-pairable)', async () => {
    const ctx = await completePairing(deps({
      createDeviceKey: () => ({ getKeyInfo: async () => { throw Object.assign(new Error('x'), { code: 'key_missing' }); } } as any),
    }));
    expect(ctx.mode).toBe('pairing_required');
    expect(ctx.tokenMgr).toBeUndefined();
  });

  it('stored deviceId ≠ actual key identity → pairing_required (never paired_device)', async () => {
    const ctx = await completePairing(deps({
      createDeviceKey: () => ({ getKeyInfo: async () => ({ deviceId: 'someone-elses-key' }) } as any),
    }));
    expect(ctx.mode).toBe('pairing_required');
  });

  // 9.4-C §4.4: inaccessible/invalidated/incompatible → clear-and-repair.
  // Only keystore_unavailable is terminal failed.
  it('inaccessible/invalidated key → pairing_required (clear-and-repair)', async () => {
    const ctx = await completePairing(deps({
      createDeviceKey: () => ({ getKeyInfo: async () => { throw Object.assign(new Error('x'), { code: 'key_inaccessible' }); } } as any),
    }));
    expect(ctx.mode).toBe('pairing_required');
    expect(ctx.tokenMgr).toBeUndefined();
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

  // BUG-004: scanner is one-shot. onPaired failure does NOT call onReject
  // (which would immediately re-enable the scanner and cause a re-scan loop).
  // The error is surfaced via onError/notifyError; the user taps "Scan Again".
  it('does NOT connect when trusted-state install fails; keeps scanner LOCKED', async () => {
    const order: string[] = [];
    await pairThenConnect({
      onPaired: async () => { order.push('onPaired'); return false; },
      connect: async () => { order.push('connect'); },
      onReject: () => order.push('reject'),
    });
    expect(order).toEqual(['onPaired']);
    expect(order).not.toContain('connect');
    expect(order).not.toContain('reject');
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

  // BUG-004: onPaired throwing does NOT call onReject — scanner stays locked.
  it('onPaired throwing → no onReject, no connect (scanner stays locked)', async () => {
    const order: string[] = [];
    await pairThenConnect({
      onPaired: async () => { throw new Error('install failed'); },
      connect: async () => { order.push('connect'); },
      onReject: () => { order.push('reject'); },
    });
    expect(order).toEqual([]);
    expect(order).not.toContain('connect');
    expect(order).not.toContain('reject');
  });

  // BUG-004: connect rejecting does NOT call onReject — scanner stays locked.
  it('connect rejecting after a successful install → no onReject (scanner stays locked)', async () => {
    const order: string[] = [];
    await pairThenConnect({
      onPaired: async () => { order.push('onPaired'); return true; },
      connect: async () => { order.push('connect'); throw new Error('probe failed'); },
      onReject: () => { order.push('reject'); },
    });
    expect(order).toEqual(['onPaired', 'connect']);
    expect(order).not.toContain('reject');
  });
});

describe('selectAppRoute — B2: paired device needs no Supabase session', () => {
  const paired = { mode: 'paired_device', tokenMgr: {} as any, baseURL: 'https://h' } as const;

  it('paired_device + connected → product WITHOUT a Supabase session', () => {
    expect(selectAppRoute(paired, { loading: false, session: false, isConnected: true })).toBe('product');
  });

  it('paired_device + not yet connected → device_connect (never supabase_auth)', () => {
    const r = selectAppRoute(paired, { loading: false, session: false, isConnected: false });
    expect(r).toBe('device_connect');
    expect(r).not.toBe('supabase_auth');
  });

  it('initializing / loading → loading', () => {
    expect(selectAppRoute({ mode: 'initializing' }, { loading: false, session: false, isConnected: false })).toBe('loading');
    expect(selectAppRoute(paired, { loading: true, session: true, isConnected: true })).toBe('loading');
  });

  it('pairing_required and failed pass through', () => {
    expect(selectAppRoute({ mode: 'pairing_required' }, { loading: false, session: false, isConnected: false })).toBe('pairing_required');
    expect(selectAppRoute({ mode: 'failed' }, { loading: false, session: true, isConnected: true })).toBe('failed');
  });

  it('legacy/explicit_local_dev still requires a Supabase session', () => {
    const local = { mode: 'explicit_local_dev', legacyToken: 'dev-token' } as const;
    expect(selectAppRoute(local, { loading: false, session: false, isConnected: false })).toBe('supabase_auth');
    expect(selectAppRoute(local, { loading: false, session: true, isConnected: false })).toBe('legacy_connect');
    expect(selectAppRoute(local, { loading: false, session: true, isConnected: true })).toBe('product');
  });
});
