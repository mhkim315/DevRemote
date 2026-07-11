// M3-auth-1C: configurable fake-native boundary tests.
// Exercises the SAME public factory path that production auth code uses.

jest.mock('expo-secure-store', () => {
  const store = new Map<string, string>();
  const control = { failRead: false, failDelete: false };
  return {
    __control: control,
    getItemAsync: async (k: string) => {
      if (control.failRead) throw new Error('secure store read error');
      return store.get(k) ?? null;
    },
    setItemAsync: async (k: string, v: string) => { store.set(k, v); },
    deleteItemAsync: async (k: string) => {
      if (control.failDelete) throw new Error('secure store delete error');
      store.delete(k);
    },
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

const VALID_KEY_INFO = {
  provider: 'android_keystore',
  keyVersion: 1,
  deviceId: FINGERPRINT,
  publicKeySpkiHex: P256_SPKI_HEX,
  hardwareBacked: true,
  nonExportable: true,
  securityLevel: 'tee',
};

function fakeNative(overrides: Partial<NativePokitDeviceKey> = {}): NativePokitDeviceKey {
  return {
    getSupport: async () => 'not_implemented',
    hasKey: async () => { throw new Error('not implemented'); },
    ensureKey: async () => { throw new Error('not implemented'); },
    getKeyInfo: async () => { throw new Error('not implemented'); },
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
    ensureKey: async () => ({ ...VALID_KEY_INFO }),
    getKeyInfo: async () => ({ ...VALID_KEY_INFO }),
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
    await expect(k.ensureKey()).rejects.toMatchObject({ code: 'native_operation_failed' });
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
      ensureKey: async () => ({ provider: 'android_keystore', keyVersion: kv, deviceId: FINGERPRINT, publicKeySpkiHex: P256_SPKI_HEX, hardwareBacked: true, nonExportable: true, securityLevel: 'tee' }),
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
  it('rejects an off-curve point (valid prefix/length, wrong Y)', async () => {
    const bytes = fromHexStr(P256_SPKI_HEX);
    bytes[90] ^= 0x01; // flip last byte of Y — point no longer on curve
    const offCurve = toHexStr(bytes);
    const k = _createWithNative(fakeNative({
      getSupport: async () => 'supported',
      ensureKey: async () => ({ provider: 'android_keystore', keyVersion: 1, deviceId: FINGERPRINT, publicKeySpkiHex: offCurve, hardwareBacked: true, nonExportable: true }),
    }));
    await expect(k.ensureKey()).rejects.toThrow('valid P-256 curve point');
  });
  it('rejects coordinates outside the field (X = all 0xFF)', async () => {
    const bytes = fromHexStr(P256_SPKI_HEX);
    for (let i = 27; i < 59; i++) bytes[i] = 0xff; // X > field prime
    const badField = toHexStr(bytes);
    const k = _createWithNative(fakeNative({ getPublicKeySpki: async () => badField }));
    await expect(k.getPublicKeySpki()).rejects.toThrow('valid P-256 curve point');
  });
  it('off-curve rejected through getPublicKeySpki too (same validator)', async () => {
    const bytes = fromHexStr(P256_SPKI_HEX);
    bytes[58] ^= 0x02; // flip an X byte
    const k = _createWithNative(fakeNative({ getPublicKeySpki: async () => toHexStr(bytes) }));
    await expect(k.getPublicKeySpki()).rejects.toThrow('valid P-256 curve point');
  });
  it('accepts the valid protocol-vector SPKI', async () => {
    const k = _createWithNative(fakeNative({ getPublicKeySpki: async () => P256_SPKI_HEX }));
    const spki = await k.getPublicKeySpki();
    expect(spki.length).toBe(91);
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

// ── Native operation failure propagation + typed coded errors ──

// A coded native rejection, as delivered over the Expo bridge (error carries a
// stable `code`).
function codedErr(code: string): Error {
  return Object.assign(new Error('native detail (not for logic)'), { code });
}

describe('native operation failure propagation', () => {
  it('hasKey native exception → throws typed (never false)', async () => {
    const k = _createWithNative(fakeNative({ hasKey: async () => { throw codedErr('key_invalidated'); } }));
    await expect(k.hasKey()).rejects.toMatchObject({ name: 'DeviceKeyError', code: 'key_invalidated' });
  });
  it('hasKey false means key absent (successful native response)', async () => {
    const k = _createWithNative(fakeNative({ getSupport: async () => 'key_missing', hasKey: async () => false }));
    expect(await k.hasKey()).toBe(false);
  });
  it('ensureKey/getPublicKeySpki/sign/deleteKey coded errors propagate the code', async () => {
    const boom = async () => { throw codedErr('key_inaccessible'); };
    const k = _createWithNative(fakeNative({ ensureKey: boom, getPublicKeySpki: boom, sign: boom, deleteKey: boom }));
    for (const p of [k.ensureKey(), k.getPublicKeySpki(), k.sign(new Uint8Array(1)), k.deleteKey()]) {
      await expect(p).rejects.toMatchObject({ code: 'key_inaccessible' });
    }
  });
});

// ── typed error model (Expo bridge code → DeviceKeyError) ──

describe('typed coded-error mapping', () => {
  const codes = ['key_missing', 'key_invalidated', 'key_inaccessible', 'hardware_unavailable', 'key_exportable', 'key_incompatible', 'native_operation_failed', 'delete_failed'];
  it('preserves each recognized native code', async () => {
    for (const code of codes) {
      const k = _createWithNative(fakeNative({ getKeyInfo: async () => { throw codedErr(code); } }));
      await expect(k.getKeyInfo()).rejects.toMatchObject({ name: 'DeviceKeyError', code });
    }
  });
  it('unknown native code → native_operation_failed (fail closed)', async () => {
    const k = _createWithNative(fakeNative({ ensureKey: async () => { throw codedErr('totally_made_up'); } }));
    await expect(k.ensureKey()).rejects.toMatchObject({ code: 'native_operation_failed' });
  });
  it('native error with no code → native_operation_failed', async () => {
    const k = _createWithNative(fakeNative({ sign: async () => { throw new Error('raw android detail'); } }));
    await expect(k.sign(new Uint8Array(1))).rejects.toMatchObject({ code: 'native_operation_failed' });
  });
  it('a bad native key-info response fails closed as key_incompatible', async () => {
    const k = _createWithNative(fakeNative({
      getSupport: async () => 'supported',
      ensureKey: async () => ({ ...VALID_KEY_INFO, deviceId: 'wrongwrongwrong' }),
    }));
    await expect(k.ensureKey()).rejects.toMatchObject({ name: 'DeviceKeyError', code: 'key_incompatible' });
  });
  it('missing module → unsupported code', async () => {
    await expect(_createWithNative(null).ensureKey()).rejects.toMatchObject({ code: 'unsupported' });
  });
});

// ── Legacy migration (fail-closed) ──

describe('legacy migration', () => {
  const LEGACY = 'pokit.device.privkey';
  const control = (SecureStore as any).__control as { failRead: boolean; failDelete: boolean };

  beforeEach(async () => {
    control.failRead = false;
    control.failDelete = false;
    try { await SecureStore.deleteItemAsync(LEGACY); } catch {}
  });

  it('no legacy value → success (no-op)', async () => {
    await expect(ensureLegacyKeyRemoved()).resolves.toBeUndefined();
  });
  it('legacy value + successful deletion → success', async () => {
    await SecureStore.setItemAsync(LEGACY, 'deadbeef', {} as any);
    await expect(ensureLegacyKeyRemoved()).resolves.toBeUndefined();
    expect(await SecureStore.getItemAsync(LEGACY)).toBeNull();
  });
  it('read failure → typed error, provisioning stops (fail closed)', async () => {
    control.failRead = true;
    await expect(ensureLegacyKeyRemoved()).rejects.toThrow('could not determine whether a legacy software key exists');
  });
  it('delete failure → typed error, provisioning stops (fail closed)', async () => {
    await SecureStore.setItemAsync(LEGACY, 'deadbeef', {} as any);
    control.failDelete = true;
    await expect(ensureLegacyKeyRemoved()).rejects.toThrow('failed to delete the legacy software private key');
  });
  it('malformed legacy value is deleted without decoding', async () => {
    await SecureStore.setItemAsync(LEGACY, 'not-hex-@@@', {} as any);
    await expect(ensureLegacyKeyRemoved()).resolves.toBeUndefined();
    expect(await SecureStore.getItemAsync(LEGACY)).toBeNull();
  });
  it('repeated migration is idempotent', async () => {
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

// ── getKeyInfo + securityLevel ──

describe('getKeyInfo + securityLevel', () => {
  it('getKeyInfo returns a valid DeviceKeyInfo through the shared parser', async () => {
    const k = _createWithNative(okNative());
    const info = await k.getKeyInfo();
    expect(info.provider).toBe('android_keystore');
    expect(info.deviceId).toBe(FINGERPRINT);
    expect(info.publicKeySpki.length).toBe(91);
    expect(info.hardwareBacked).toBe(true);
    expect(info.nonExportable).toBe(true);
    expect(info.securityLevel).toBe('tee');
  });
  it('accepts strongbox / os_keystore levels', async () => {
    for (const level of ['strongbox', 'os_keystore', 'unknown']) {
      const k = _createWithNative(fakeNative({
        getSupport: async () => 'supported',
        getKeyInfo: async () => ({ ...VALID_KEY_INFO, securityLevel: level }),
      }));
      expect((await k.getKeyInfo()).securityLevel).toBe(level);
    }
  });
  it('rejects a missing securityLevel', async () => {
    const bad = { ...VALID_KEY_INFO } as any; delete bad.securityLevel;
    const k = _createWithNative(fakeNative({ getSupport: async () => 'supported', getKeyInfo: async () => bad }));
    await expect(k.getKeyInfo()).rejects.toThrow('securityLevel');
  });
  it('rejects an unknown securityLevel string', async () => {
    const k = _createWithNative(fakeNative({
      getSupport: async () => 'supported',
      getKeyInfo: async () => ({ ...VALID_KEY_INFO, securityLevel: 'magic_chip' }),
    }));
    await expect(k.getKeyInfo()).rejects.toThrow('securityLevel');
  });
  it('getKeyInfo native coded exception propagates the code', async () => {
    const k = _createWithNative(fakeNative({ getKeyInfo: async () => { throw Object.assign(new Error('x'), { code: 'key_invalidated' }); } }));
    await expect(k.getKeyInfo()).rejects.toMatchObject({ code: 'key_invalidated' });
  });
  it('missing module → getKeyInfo throws', async () => {
    await expect(_createWithNative(null).getKeyInfo()).rejects.toThrow('native module not found');
  });
});

// ── iOS Secure Enclave provider (M3-auth-1B) ──
//
// The shared JS parser is platform-bound via expectedProvider(): on iOS a valid
// key must report provider `ios_secure_enclave` and (this phase) securityLevel
// `secure_enclave`. Platform.OS is set for the whole block so the awaited
// validations see iOS (withPlatform() only holds across a sync body).

describe('iOS Secure Enclave provider', () => {
  let origOS: string;
  beforeAll(() => {
    origOS = (require('react-native') as any).Platform.OS;
    (require('react-native') as any).Platform.OS = 'ios';
  });
  afterAll(() => { (require('react-native') as any).Platform.OS = origOS; });

  const IOS_KEY_INFO = {
    provider: 'ios_secure_enclave',
    keyVersion: 1,
    deviceId: FINGERPRINT,
    publicKeySpkiHex: P256_SPKI_HEX,
    hardwareBacked: true,
    nonExportable: true,
    securityLevel: 'secure_enclave',
  };

  function iosNative(overrides: Partial<NativePokitDeviceKey> = {}): NativePokitDeviceKey {
    return fakeNative({
      getSupport: async () => 'supported',
      hasKey: async () => true,
      ensureKey: async () => ({ ...IOS_KEY_INFO }),
      getKeyInfo: async () => ({ ...IOS_KEY_INFO }),
      getPublicKeySpki: async () => P256_SPKI_HEX,
      deleteKey: async () => {},
      ...overrides,
    });
  }

  it('validates a Secure Enclave key: provider + secure_enclave level + recomputed deviceId', async () => {
    const k = _createWithNative(iosNative());
    const info = await k.getKeyInfo();
    expect(info.provider).toBe('ios_secure_enclave');
    expect(info.securityLevel).toBe('secure_enclave');
    expect(info.deviceId).toBe(FINGERPRINT);
    expect(info.publicKeySpki.length).toBe(91);
    expect(info.hardwareBacked).toBe(true);
    expect(info.nonExportable).toBe(true);
    // ensureKey runs through the same shared parser.
    expect((await k.ensureKey()).provider).toBe('ios_secure_enclave');
  });

  it('rejects the android_keystore provider on iOS (platform-bound)', async () => {
    const k = _createWithNative(iosNative({
      ensureKey: async () => ({ ...IOS_KEY_INFO, provider: 'android_keystore' }),
    }));
    await expect(k.ensureKey()).rejects.toThrow('provider must be ios_secure_enclave');
  });

  it('accepts secure_enclave and unknown levels, rejects an unknown string', async () => {
    for (const level of ['secure_enclave', 'unknown']) {
      const k = _createWithNative(iosNative({ getKeyInfo: async () => ({ ...IOS_KEY_INFO, securityLevel: level }) }));
      expect((await k.getKeyInfo()).securityLevel).toBe(level);
    }
    const bad = _createWithNative(iosNative({ getKeyInfo: async () => ({ ...IOS_KEY_INFO, securityLevel: 't2_chip' }) }));
    await expect(bad.getKeyInfo()).rejects.toThrow('securityLevel');
  });

  it('fails closed on a deviceId mismatch (key_incompatible)', async () => {
    const k = _createWithNative(iosNative({ ensureKey: async () => ({ ...IOS_KEY_INFO, deviceId: 'deadbeef'.repeat(8) }) }));
    await expect(k.ensureKey()).rejects.toMatchObject({ name: 'DeviceKeyError', code: 'key_incompatible' });
  });

  it('propagates coded native errors (hardware_unavailable) from the enclave', async () => {
    const k = _createWithNative(iosNative({ ensureKey: async () => { throw Object.assign(new Error('x'), { code: 'hardware_unavailable' }); } }));
    await expect(k.ensureKey()).rejects.toMatchObject({ code: 'hardware_unavailable' });
  });

  it('sign passes the enclave DER signature through the wrapper', async () => {
    const sigHex = '3044022001a7c80dc4afea65966c4e138bf1038a9bf21ee38ffdc09604533a12f6ecaf78022035097449d58db7da4d8c95a8e86424273820699df463920dd60e9a7361c74bdd';
    const k = _createWithNative(iosNative({ sign: async () => sigHex }));
    const sig = await k.sign(new Uint8Array([1, 2, 3]));
    expect(sig).toBeInstanceOf(Uint8Array);
    expect(sig.length).toBeGreaterThan(64);
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
