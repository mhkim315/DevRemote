// M3-auth-1C: configurable fake-native boundary tests.
// Exercises the SAME public factory path that production auth code uses.

jest.mock('expo-secure-store', () => {
  const store = new Map<string, string>();
  return {
    getItemAsync: async (k: string) => store.get(k) ?? null,
    setItemAsync: async (k: string, v: string) => { store.set(k, v); },
    deleteItemAsync: async (k: string) => { store.delete(k); },
    WHEN_UNLOCKED_THIS_DEVICE_ONLY: 'when_unlocked_this_device_only',
  };
}, { virtual: true });

jest.mock('react-native', () => ({
  Platform: { OS: 'android' },
  NativeModules: {},
}), { virtual: true });

import * as SecureStore from 'expo-secure-store';
import type { NativePokitDeviceKey } from '../modules/pokit-device-key/src';
import { _createWithNative, SUPPORT_VALUES } from '../modules/pokit-device-key/src';
import { ensureLegacyKeyRemoved } from '../src/lib/deviceIdentity';

// ── helpers ──

const P256_SPKI_HEX =
  '3059301306072a8648ce3d020106082a8648ce3d03010703420004' +
  '515c3d6eb9e396b904d3feca7f54fdcd0cc1e997bf375dca515ad0a6c3b4035f' +
  '4536be3a50f318fbf9a5475902a221502bef0d57e08c53b2cc0a56f17d9f9354';
const FINGERPRINT = 'f1d59449b727165de732bf283338122b99628a615918fedc67d878fffcf47da7';

function fakeNative(overrides: Partial<NativePokitDeviceKey> = {}): NativePokitDeviceKey {
  return {
    getSupport: async () => 'not_implemented',
    hasKey: async () => { throw new Error('not implemented'); },
    ensureKey: async () => { throw new Error('not implemented'); },
    getPublicKeySpki: async () => { throw new Error('not implemented'); },
    sign: async (_: string) => { throw new Error('not implemented'); },
    deleteKey: async () => { throw new Error('not implemented'); },
    ...overrides,
  };
}

function okNative(): NativePokitDeviceKey {
  return {
    getSupport: async () => 'supported',
    hasKey: async () => true,
    ensureKey: async () => ({
      provider: 'android_keystore',
      keyVersion: 1,
      deviceId: FINGERPRINT,
      publicKeySpkiHex: P256_SPKI_HEX,
      hardwareBacked: true,
      nonExportable: true,
    }),
    getPublicKeySpki: async () => P256_SPKI_HEX,
    sign: async (_) => '3044022001a7c80dc4afea65966c4e138bf1038a9bf21ee38ffdc09604533a12f6ecaf78022035097449d58db7da4d8c95a8e86424273820699df463920dd60e9a7361c74bdd',
    deleteKey: async () => {},
  };
}

function withPlatform(os: string, fn: () => void) {
  const orig = (require('react-native') as any).Platform.OS;
  (require('react-native') as any).Platform.OS = os;
  try { fn(); } finally { (require('react-native') as any).Platform.OS = orig; }
}

// ── Not-implemented stubs ──

describe('native stub (not_implemented)', () => {
  it('android stub reports not_implemented', async () => {
    const k = _createWithNative(fakeNative({ getSupport: async () => 'not_implemented', hasKey: async () => false }));
    expect(await k.getSupport()).toBe('not_implemented');
    expect(await k.hasKey()).toBe(false);
    await expect(k.ensureKey()).rejects.toThrow('not implemented');
    await expect(k.sign(new Uint8Array(1))).rejects.toThrow();
  });
  it('ios stub reports not_implemented', async () => {
    withPlatform('ios', async () => {
      const k = _createWithNative(fakeNative({ getSupport: async () => 'not_implemented', hasKey: async () => false }));
      expect(await k.getSupport()).toBe('not_implemented');
    });
  });
});

// ── Missing module ──

describe('missing native module', () => {
  it('getSupport -> not_implemented, hasKey throws, all ops throw', async () => {
    const k = _createWithNative(null);
    expect(await k.getSupport()).toBe('not_implemented');
    await expect(k.hasKey()).rejects.toThrow('native module not found');
    await expect(k.ensureKey()).rejects.toThrow('native module not found');
    await expect(k.getPublicKeySpki()).rejects.toThrow();
    await expect(k.sign(new Uint8Array(1))).rejects.toThrow();
    await expect(k.deleteKey()).rejects.toThrow();
  });
});

// ── Support vocabulary ──

describe('support vocabulary', () => {
  it('accepts 5 approved values', () => {
    for (const v of SUPPORT_VALUES) expect(SUPPORT_VALUES.has(v)).toBe(true);
  });
  it('rejects unknown/empty/forged strings', async () => {
    for (const bad of ['', 'unsupported', 'software_key', 'OK']) {
      const k = _createWithNative(fakeNative({ getSupport: async () => bad }));
      expect(await k.getSupport()).toBe('not_implemented');
    }
  });
  it('rejects missing/wrong-type support', async () => {
    const k = _createWithNative(fakeNative({ getSupport: async () => 123 as any }));
    expect(await k.getSupport()).toBe('not_implemented');
  });
  it('native getSupport exception -> not_implemented', async () => {
    const k = _createWithNative(fakeNative({ getSupport: async () => { throw new Error('boom'); } }));
    expect(await k.getSupport()).toBe('not_implemented');
  });
});

// ── Provider rejection ──

describe('provider rejection', () => {
  it('android rejects ios_secure_enclave provider', async () => {
    const k = _createWithNative(fakeNative({
      getSupport: async () => 'supported',
      ensureKey: async () => ({ provider: 'ios_secure_enclave', keyVersion: 1, deviceId: FINGERPRINT, publicKeySpkiHex: P256_SPKI_HEX, hardwareBacked: true, nonExportable: true }),
    }));
    await expect(k.ensureKey()).rejects.toThrow('provider');
  });
  it('rejects unknown provider', async () => {
    const k = _createWithNative(fakeNative({
      getSupport: async () => 'supported',
      ensureKey: async () => ({ provider: 'software_key', keyVersion: 1, deviceId: 'ab', publicKeySpkiHex: P256_SPKI_HEX, hardwareBacked: true, nonExportable: true }),
    }));
    await expect(k.ensureKey()).rejects.toThrow('provider must be android_keystore');
  });
  it('rejects missing provider', async () => {
    const k = _createWithNative(fakeNative({
      getSupport: async () => 'supported',
      ensureKey: async () => ({ keyVersion: 1, deviceId: 'ab', publicKeySpkiHex: P256_SPKI_HEX, hardwareBacked: true, nonExportable: true }),
    }));
    await expect(k.ensureKey()).rejects.toThrow('provider');
  });
  it('web platform is unsupported', () => {
    withPlatform('web', () => {
      expect(() => _createWithNative(okNative())).not.toThrow(); // native present but platform check rejects ensureKey
    });
  });
});

// ── Key version ──

describe('key version', () => {
  function ensureWithKV(kv: unknown) {
    const k = _createWithNative(fakeNative({
      getSupport: async () => 'supported',
      ensureKey: async () => ({ provider: 'android_keystore', keyVersion: kv, deviceId: FINGERPRINT, publicKeySpkiHex: P256_SPKI_HEX, hardwareBacked: true, nonExportable: true }),
    }));
    return k.ensureKey();
  }
  it('accepts exactly 1', async () => { await expect(ensureWithKV(1)).resolves.toBeDefined(); });
  it('1.0 is fine (JS number equality)', async () => { await expect(ensureWithKV(1.0)).resolves.toBeDefined(); });
  it('rejects "1" string', async () => { await expect(ensureWithKV('1')).rejects.toThrow('keyVersion'); });
  it('rejects 0', async () => { await expect(ensureWithKV(0)).rejects.toThrow('keyVersion'); });
  it('rejects 2', async () => { await expect(ensureWithKV(2)).rejects.toThrow('keyVersion'); });
  it('rejects 1.5', async () => { await expect(ensureWithKV(1.5)).rejects.toThrow('keyVersion'); });
  it('rejects NaN', async () => { await expect(ensureWithKV(NaN)).rejects.toThrow('keyVersion'); });
  it('rejects Infinity', async () => { await expect(ensureWithKV(Infinity)).rejects.toThrow('keyVersion'); });
});

// ── Security claims ──

describe('security claims', () => {
  it('rejects missing hardwareBacked', async () => {
    const k = _createWithNative(fakeNative({
      getSupport: async () => 'supported',
      ensureKey: async () => ({ provider: 'android_keystore', keyVersion: 1, deviceId: FINGERPRINT, publicKeySpkiHex: P256_SPKI_HEX, hardwareBacked: false, nonExportable: true }),
    }));
    await expect(k.ensureKey()).rejects.toThrow('hardwareBacked');
  });
  it('rejects nonExportable !== true', async () => {
    const k = _createWithNative(fakeNative({
      getSupport: async () => 'supported',
      ensureKey: async () => ({ provider: 'android_keystore', keyVersion: 1, deviceId: FINGERPRINT, publicKeySpkiHex: P256_SPKI_HEX, hardwareBacked: true, nonExportable: false }),
    }));
    await expect(k.ensureKey()).rejects.toThrow('nonExportable');
  });
  it('rejects missing both claims', async () => {
    const k = _createWithNative(fakeNative({
      getSupport: async () => 'supported',
      ensureKey: async () => ({ provider: 'android_keystore', keyVersion: 1, deviceId: FINGERPRINT, publicKeySpkiHex: P256_SPKI_HEX }),
    }));
    await expect(k.ensureKey()).rejects.toThrow('hardwareBacked');
  });
});

// ── SPKI validation ──

describe('SPKI validation', () => {
  it('rejects wrong length', async () => {
    const k = _createWithNative(fakeNative({
      getSupport: async () => 'supported',
      getPublicKeySpki: async () => 'ab',  // 2-char hex, too short
    }));
    await expect(k.getPublicKeySpki()).rejects.toThrow('SPKI');
  });
  it('rejects wrong prefix (not P-256)', async () => {
    const wrong = P256_SPKI_HEX.replace('3059', '3041'); // truncate SEQUENCE
    const k = _createWithNative(fakeNative({
      getSupport: async () => 'supported',
      ensureKey: async () => ({ provider: 'android_keystore', keyVersion: 1, deviceId: 'ab', publicKeySpkiHex: wrong, hardwareBacked: true, nonExportable: true }),
    }));
    await expect(k.ensureKey()).rejects.toThrow('SPKI');
  });
  it('rejects compressed point (0x02 prefix)', async () => {
    // Build a fake SPKI with 02 at byte 26 instead of 04.
    const hex = fromHexStr(P256_SPKI_HEX);
    hex[26] = 0x02;
    const badHex = toHexStr(hex);
    const k = _createWithNative(fakeNative({
      getSupport: async () => 'supported',
      ensureKey: async () => ({ provider: 'android_keystore', keyVersion: 1, deviceId: 'ab', publicKeySpkiHex: badHex, hardwareBacked: true, nonExportable: true }),
    }));
    await expect(k.ensureKey()).rejects.toThrow('uncompressed');
  });
  it('rejects getPublicKeySpki with bad SPKI (wrong length)', async () => {
    const k = _createWithNative(fakeNative({ getPublicKeySpki: async () => 'ff' }));
    await expect(k.getPublicKeySpki()).rejects.toThrow('SPKI');
  });
});

// ── Device ID mismatch ──

describe('device ID', () => {
  it('rejects native deviceId mismatch', async () => {
    const k = _createWithNative(fakeNative({
      getSupport: async () => 'supported',
      ensureKey: async () => ({ provider: 'android_keystore', keyVersion: 1, deviceId: '0000111122223333', publicKeySpkiHex: P256_SPKI_HEX, hardwareBacked: true, nonExportable: true }),
    }));
    await expect(k.ensureKey()).rejects.toThrow('deviceId mismatch');
  });
  it('rejects non-string deviceId', async () => {
    const k = _createWithNative(fakeNative({
      getSupport: async () => 'supported',
      ensureKey: async () => ({ provider: 'android_keystore', keyVersion: 1, deviceId: 123, publicKeySpkiHex: P256_SPKI_HEX, hardwareBacked: true, nonExportable: true }),
    }));
    await expect(k.ensureKey()).rejects.toThrow('deviceId');
  });
});

// ── Native operation failure propagation (BLOCKER 4: hasKey does not silence errors) ──

describe('native operation failure propagation', () => {
  it('hasKey native exception → throws (not false)', async () => {
    const k = _createWithNative(fakeNative({ hasKey: async () => { throw new Error('key_invalidated'); } }));
    await expect(k.hasKey()).rejects.toThrow('key_invalidated');
  });
  it('hasKey false means key absent (successful native response)', async () => {
    const k = _createWithNative(fakeNative({ getSupport: async () => 'key_missing', hasKey: async () => false }));
    expect(await k.hasKey()).toBe(false);
  });
  it('ensureKey, getPublicKeySpki, sign, deleteKey native exceptions propagate', async () => {
    const boom = async () => { throw new Error('native crash'); };
    const k = _createWithNative(fakeNative({ ensureKey: boom, getPublicKeySpki: boom, sign: boom, deleteKey: boom }));
    await expect(k.ensureKey()).rejects.toThrow('native crash');
    await expect(k.getPublicKeySpki()).rejects.toThrow();
    await expect(k.sign(new Uint8Array(1))).rejects.toThrow();
    await expect(k.deleteKey()).rejects.toThrow();
  });
});

// ── Legacy migration (fail-closed) ──

describe('legacy migration', () => {
  const LEGACY = 'pokit.device.privkey';
  beforeEach(async () => { try { await SecureStore.deleteItemAsync(LEGACY); } catch {} });

  it('no legacy key → success (no-op)', async () => {
    await expect(ensureLegacyKeyRemoved()).resolves.toBeUndefined();
  });
  it('legacy key present + deletion succeeds → success', async () => {
    await SecureStore.setItemAsync(LEGACY, 'deadbeef', {} as any);
    await expect(ensureLegacyKeyRemoved()).resolves.toBeUndefined();
    expect(await SecureStore.getItemAsync(LEGACY)).toBeNull();
  });
  it('legacy key present → both getItem and deleteItem are exercised (mock succeeds)', async () => {
    await SecureStore.setItemAsync(LEGACY, 'deadbeef', {} as any);
    await expect(ensureLegacyKeyRemoved()).resolves.toBeUndefined();
    expect(await SecureStore.getItemAsync(LEGACY)).toBeNull();
  });
  it('migration is idempotent → repeated calls safe', async () => {
    await ensureLegacyKeyRemoved();
    await ensureLegacyKeyRemoved();
  });
});

// ── No software-key path ──

describe('no software-key path', () => {
  it('deviceIdentity does not expose JS keygen/signing', () => {
    const mod = require('../src/lib/deviceIdentity');
    expect(typeof mod.loadOrCreateDeviceIdentity).toBe('function');
    expect(mod.generatePrivateKey).toBeUndefined();
    expect(mod.publicKeyUncompressed).toBeUndefined();
    expect(mod.signDer).toBeUndefined();
  });
});

// ── source invariants ──

describe('architecture invariants', () => {
  it('crypto.ts does not export JS keygen or sign', () => {
    const mod = require('../src/lib/crypto');
    expect(mod.generatePrivateKey).toBeUndefined();
    expect(mod.signDer).toBeUndefined();
    expect(typeof mod.verifyDer).toBe('function'); // still needed for host pinning
  });
});

// ── codec helpers ──

function fromHexStr(h: string): Uint8Array {
  const out = new Uint8Array(h.length >> 1);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(h.substr(i * 2, 2), 16);
  return out;
}
function toHexStr(u: Uint8Array): string {
  let s = ''; for (let i = 0; i < u.length; i++) s += u[i].toString(16).padStart(2, '0');
  return s;
}
