// M3-auth-1C: fail-closed provider + response validation + migration tests.

jest.mock('expo-secure-store', () => {
  const store = new Map<string, string>();
  return {
    getItemAsync: async (k: string) => store.get(k) ?? null,
    setItemAsync: async (k: string, v: string) => { store.set(k, v); },
    deleteItemAsync: async (k: string) => { store.delete(k); },
    WHEN_UNLOCKED_THIS_DEVICE_ONLY: 'when_unlocked_this_device_only',
  };
}, { virtual: true });

import * as SecureStore from 'expo-secure-store';
import { createPokitDeviceKey } from '../modules/pokit-device-key';
import { migrateLegacySoftwareIdentity } from '../src/lib/deviceIdentity';
import { SUPPORT_VALUES } from '../modules/pokit-device-key/src';

// ── Missing native module (jest mock returns null) ──

describe('PokitDeviceKey — missing native module', () => {
  it('getSupport returns not_implemented', async () => {
    expect(await createPokitDeviceKey().getSupport()).toBe('not_implemented');
  });
  it('hasKey returns false', async () => {
    expect(await createPokitDeviceKey().hasKey()).toBe(false);
  });
  it('ensureKey throws', async () => {
    await expect(createPokitDeviceKey().ensureKey()).rejects.toThrow('native module not found');
  });
  it('getPublicKeySpki throws', async () => {
    await expect(createPokitDeviceKey().getPublicKeySpki()).rejects.toThrow();
  });
  it('sign throws', async () => {
    await expect(createPokitDeviceKey().sign(new Uint8Array([1, 2, 3]))).rejects.toThrow();
  });
  it('deleteKey throws', async () => {
    await expect(createPokitDeviceKey().deleteKey()).rejects.toThrow();
  });
});

// ── Response vocabulary ──

describe('SUPPORT_VALUES', () => {
  it('contains the 5 approved values', () => {
    expect(SUPPORT_VALUES.has('supported')).toBe(true);
    expect(SUPPORT_VALUES.has('not_implemented')).toBe(true);
    expect(SUPPORT_VALUES.has('hardware_unavailable')).toBe(true);
    expect(SUPPORT_VALUES.has('key_missing')).toBe(true);
    expect(SUPPORT_VALUES.has('key_invalidated')).toBe(true);
  });
  it('rejects unknown / empty / software-key strings', () => {
    expect(SUPPORT_VALUES.has('')).toBe(false);
    expect(SUPPORT_VALUES.has('unsupported')).toBe(false);
    expect(SUPPORT_VALUES.has('software_key')).toBe(false);
  });
});

// ── Legacy software-key migration ──

describe('legacy software-key migration', () => {
  const LEGACY = 'pokit.device.privkey';

  beforeEach(async () => {
    try { await SecureStore.deleteItemAsync(LEGACY); } catch {}
  });

  it('removes a present legacy key', async () => {
    await SecureStore.setItemAsync(LEGACY, 'deadbeef', { keychainAccessible: 'test' } as any);
    await migrateLegacySoftwareIdentity();
    expect(await SecureStore.getItemAsync(LEGACY)).toBeNull();
  });

  it('is idempotent when no legacy key exists', async () => {
    await migrateLegacySoftwareIdentity();
    await migrateLegacySoftwareIdentity();
  });

  it('does not throw on repeated migration', async () => {
    await expect(migrateLegacySoftwareIdentity()).resolves.toBeUndefined();
  });
});

// ── No software-key path ──

describe('no software-key fallback', () => {
  it('does not expose JS private-key generation in deviceIdentity', () => {
    const mod = require('../src/lib/deviceIdentity');
    expect(typeof mod.loadOrCreateDeviceIdentity).toBe('function');
    expect(typeof mod.signWithDeviceKey).toBe('function');
    // These were removed in e55d835f5 and must not reappear:
    expect(mod.generatePrivateKey).toBeUndefined();
    expect(mod.publicKeyUncompressed).toBeUndefined();
    expect(mod.deriveIdentity).toBeUndefined();
  });
});
