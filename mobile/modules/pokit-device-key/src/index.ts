// PokitDeviceKey — cross-platform non-exportable device identity.
//
// Resolves the native module through expo-modules-core (requireOptionalNativeModule).
// When the native module is absent or returns not_implemented, all signing operations
// fail closed — no JavaScript software-key fallback ever executes.
//
// Every native response is validated before constructing DeviceKeyInfo.
// Malformed or untrusted fields cause the operation to fail closed.

import { requireOptionalNativeModule } from 'expo-modules-core';
import { Platform } from 'react-native';
import { fromHex, toHex, deviceFingerprint } from '../../../src/lib/crypto';

// ── Common contract ──

export type DeviceKeyProvider = 'android_keystore' | 'ios_secure_enclave';

export const SUPPORT_VALUES: ReadonlySet<string> = new Set([
  'supported', 'not_implemented', 'hardware_unavailable', 'key_missing', 'key_invalidated',
]);

export type DeviceKeySupport =
  | 'supported'
  | 'not_implemented'
  | 'hardware_unavailable'
  | 'key_missing'
  | 'key_invalidated';

export interface DeviceKeyInfo {
  provider: DeviceKeyProvider;
  keyVersion: 1;
  deviceId: string; // hex(sha256(spki))
  publicKeySpki: Uint8Array; // 91-byte P-256 SPKI DER
  hardwareBacked: true;
  nonExportable: true;
}

export interface PokitDeviceKey {
  getSupport(): Promise<DeviceKeySupport>;
  hasKey(): Promise<boolean>;
  ensureKey(): Promise<DeviceKeyInfo>;
  getPublicKeySpki(): Promise<Uint8Array>;
  sign(message: Uint8Array): Promise<Uint8Array>;
  deleteKey(): Promise<void>;
}

const SPKI_EXPECTED_LEN = 91;
const P256_SPKI_PREFIX_HEX = '3059301306072a8648ce3d020106082a8648ce3d030107034200';

// ── Native surface (keys must match the Kotlin/Swift ModuleDefinition Name) ──

interface NativePokitDeviceKey {
  getSupport(): Promise<string>;
  hasKey(): Promise<boolean>;
  ensureKey(): Promise<Record<string, unknown>>;
  getKeyInfo(): Promise<Record<string, unknown>>;
  getPublicKeySpki(): Promise<string>;
  sign(messageHex: string): Promise<string>;
  deleteKey(): Promise<void>;
}

// ── Provider factory ──

export function createPokitDeviceKey(): PokitDeviceKey {
  const native = requireOptionalNativeModule<NativePokitDeviceKey>('PokitDeviceKey');
  if (!native) return missingModuleProvider();
  return validatedProvider(native);
}

// ── fail-closed: native module not found ──

function missingModuleProvider(): PokitDeviceKey {
  const op = async () => { throw new Error('PokitDeviceKey: native module not found'); };
  return {
    getSupport: async () => 'not_implemented',
    hasKey: async () => false,
    ensureKey: op,
    getPublicKeySpki: op,
    sign: op,
    deleteKey: op,
  };
}

// ── validated provider ──

function validatedProvider(native: NativePokitDeviceKey): PokitDeviceKey {
  return {
    getSupport: () => validateSupport(native),
    hasKey: async () => {
      try { return await native.hasKey(); } catch { return false; }
    },
    ensureKey: async () => validateKeyInfo(native, await native.ensureKey()),
    getPublicKeySpki: async () => {
      const hex = await native.getPublicKeySpki();
      return fromHex(hex, SPKI_EXPECTED_LEN);
    },
    sign: async (message) => {
      const sigHex = await native.sign(toHex(message));
      return fromHex(sigHex);
    },
    deleteKey: () => native.deleteKey(),
  };
}

// ── response validators ──

async function validateSupport(native: NativePokitDeviceKey): Promise<DeviceKeySupport> {
  let raw: string;
  try { raw = await native.getSupport(); } catch { return 'not_implemented'; }
  if (!SUPPORT_VALUES.has(raw)) {
    // Unknown support string — treat as not_implemented but don't log the raw value.
    return 'not_implemented';
  }
  return raw as DeviceKeySupport;
}

function expectedProvider(): DeviceKeyProvider {
  if (Platform.OS === 'android') return 'android_keystore';
  return 'ios_secure_enclave';
}

function validateKeyInfo(native: NativePokitDeviceKey, raw: Record<string, unknown>): DeviceKeyInfo {
  // provider
  const provider = raw.provider;
  if (typeof provider !== 'string') throw keyInfoErr('provider must be a string');
  const expected = expectedProvider();
  if (provider !== expected) throw keyInfoErr(`provider must be ${expected}`);

  // keyVersion
  const kv = raw.keyVersion;
  if (kv !== 1) throw keyInfoErr('keyVersion must be 1');

  // hardwareBacked + nonExportable — native must claim BOTH exactly.
  const hw = raw.hardwareBacked;
  if (hw !== true) throw keyInfoErr('hardwareBacked must be true');
  const ne = raw.nonExportable;
  if (ne !== true) throw keyInfoErr('nonExportable must be true');

  // SPKI
  const spkiHex = raw.publicKeySpkiHex;
  if (typeof spkiHex !== 'string' || spkiHex.length !== SPKI_EXPECTED_LEN * 2) {
    throw keyInfoErr('publicKeySpkiHex must be a ' + (SPKI_EXPECTED_LEN * 2) + '-char hex string');
  }
  if (!spkiHex.toLowerCase().startsWith(P256_SPKI_PREFIX_HEX)) {
    throw keyInfoErr('publicKeySpkiHex must have canonical P-256 SPKI prefix');
  }
  const spki = fromHex(spkiHex, SPKI_EXPECTED_LEN);

  // deviceId — trust nothing, recompute.
  const nativeDeviceId = raw.deviceId;
  if (typeof nativeDeviceId !== 'string') throw keyInfoErr('deviceId must be a string');
  const computed = deviceFingerprint(spki);
  if (computed !== nativeDeviceId) throw keyInfoErr('deviceId mismatch');

  return {
    provider: provider as DeviceKeyProvider,
    keyVersion: 1,
    deviceId: computed,
    publicKeySpki: spki,
    hardwareBacked: true,
    nonExportable: true,
  };
}

function keyInfoErr(msg: string): Error {
  return new Error(`PokitDeviceKey: invalid native key info: ${msg}`);
}
