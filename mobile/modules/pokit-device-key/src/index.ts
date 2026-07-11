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
import { p256 } from '@noble/curves/nist.js';
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

export type DeviceKeySecurityLevel = 'strongbox' | 'tee' | 'os_keystore' | 'unknown';

const SECURITY_LEVELS: ReadonlySet<string> = new Set([
  'strongbox', 'tee', 'os_keystore', 'unknown',
]);

export interface DeviceKeyInfo {
  provider: DeviceKeyProvider;
  keyVersion: 1;
  deviceId: string;
  publicKeySpki: Uint8Array;
  hardwareBacked: true;
  nonExportable: true;
  securityLevel: DeviceKeySecurityLevel;
}

export interface PokitDeviceKey {
  getSupport(): Promise<DeviceKeySupport>;
  hasKey(): Promise<boolean>;
  ensureKey(): Promise<DeviceKeyInfo>;
  getKeyInfo(): Promise<DeviceKeyInfo>;
  getPublicKeySpki(): Promise<Uint8Array>;
  sign(message: Uint8Array): Promise<Uint8Array>;
  deleteKey(): Promise<void>;
}

const SPKI_EXPECTED_LEN = 91;
const P256_SPKI_ALG_OID = '06082a8648ce3d030107';

// ── Native surface ──

export interface NativePokitDeviceKey {
  getSupport(): Promise<string>;
  hasKey(): Promise<boolean>;
  ensureKey(): Promise<Record<string, unknown>>;
  getKeyInfo(): Promise<Record<string, unknown>>;
  getPublicKeySpki(): Promise<string>;
  sign(messageHex: string): Promise<string>;
  deleteKey(): Promise<void>;
}

// ── Factory (test seam: inject a fake native via _createWithNative) ──

export function createPokitDeviceKey(): PokitDeviceKey {
  const native = requireOptionalNativeModule<NativePokitDeviceKey>('PokitDeviceKey');
  return _createWithNative(native);
}

// Exported for jest to inject configurable fakes through the public wrapper path.
export function _createWithNative(native: NativePokitDeviceKey | null): PokitDeviceKey {
  if (!native) return missingModuleProvider();
  return validatedProvider(native);
}

// ── fail-closed: native module not found ──

function missingModuleProvider(): PokitDeviceKey {
  const op = async () => { throw new Error('PokitDeviceKey: native module not found'); };
  return {
    getSupport: async () => 'not_implemented' as DeviceKeySupport,
    hasKey: async () => { throw new Error('PokitDeviceKey: native module not found'); },
    ensureKey: op,
    getKeyInfo: op,
    getPublicKeySpki: op,
    sign: op,
    deleteKey: op,
  };
}

// ── platform helpers ──

function expectedProvider(): DeviceKeyProvider | null {
  if (Platform.OS === 'android') return 'android_keystore';
  if (Platform.OS === 'ios') return 'ios_secure_enclave';
  return null; // Web / unknown → unsupported
}

// ── validated provider ──

function validatedProvider(native: NativePokitDeviceKey): PokitDeviceKey {
  return {
    getSupport: () => validateSupport(native),
    hasKey: async () => {
      // BLOCKER 4 fix: do not silence native hasKey errors.
      // Only boolean false means key absent. Every other result fails closed.
      return native.hasKey();
    },
    ensureKey: async () => validateKeyInfo(await native.ensureKey()),
    getKeyInfo: async () => validateKeyInfo(await native.getKeyInfo()),
    getPublicKeySpki: async () => {
      const hex = await native.getPublicKeySpki();
      return validateAndDecodeSPKI(hex);
    },
    sign: async (message) => {
      const sigHex = await native.sign(toHex(message));
      return fromHex(sigHex);
    },
    deleteKey: () => native.deleteKey(),
  };
}

// ── response validators (BLOCKER 4: one shared parser for DeviceKeyInfo) ──

async function validateSupport(native: NativePokitDeviceKey): Promise<DeviceKeySupport> {
  let raw: string;
  try { raw = await native.getSupport(); } catch { return 'not_implemented'; }
  if (typeof raw !== 'string' || !SUPPORT_VALUES.has(raw)) {
    return 'not_implemented';
  }
  return raw as DeviceKeySupport;
}

function validateKeyInfo(raw: Record<string, unknown>): DeviceKeyInfo {
  // provider
  const provider = raw.provider;
  if (typeof provider !== 'string') throw keyInfoErr('provider must be a string');
  const exp = expectedProvider();
  if (!exp) throw keyInfoErr('unsupported platform — no device key provider');
  if (provider !== exp) throw keyInfoErr(`provider must be ${exp}`);

  // keyVersion — must be a finite integer exactly 1 (1.0 is fine in JS)
  const kv = raw.keyVersion;
  if (typeof kv !== 'number' || !Number.isFinite(kv) || !Number.isInteger(kv) || kv !== 1) {
    throw keyInfoErr('keyVersion must be exactly 1');
  }

  // hardwareBacked + nonExportable — native must claim BOTH explicitly.
  if (raw.hardwareBacked !== true) throw keyInfoErr('hardwareBacked must be true');
  if (raw.nonExportable !== true) throw keyInfoErr('nonExportable must be true');

  // SPKI
  const spkiHex = raw.publicKeySpkiHex;
  if (typeof spkiHex !== 'string') throw keyInfoErr('publicKeySpkiHex must be a string');
  const spki = validateAndDecodeSPKI(spkiHex);

  // deviceId — trust nothing, recompute.
  const nativeDeviceId = raw.deviceId;
  if (typeof nativeDeviceId !== 'string') throw keyInfoErr('deviceId must be a string');
  const computed = deviceFingerprint(spki);
  if (computed !== nativeDeviceId) throw keyInfoErr('deviceId mismatch');

  // securityLevel — allowlisted; must be hardware-backed to reach here.
  const level = raw.securityLevel;
  if (typeof level !== 'string' || !SECURITY_LEVELS.has(level)) {
    throw keyInfoErr('securityLevel must be strongbox/tee/os_keystore/unknown');
  }

  return {
    provider: provider as DeviceKeyProvider,
    keyVersion: 1,
    deviceId: computed,
    publicKeySpki: spki,
    hardwareBacked: true,
    nonExportable: true,
    securityLevel: level as DeviceKeySecurityLevel,
  };
}

// validateAndDecodeSPKI checks the canonical P-256 SPKI structure. Shared by
// getPublicKeySpki and the DeviceKeyInfo parser.
function validateAndDecodeSPKI(hex: string): Uint8Array {
  if (typeof hex !== 'string' || hex.length !== SPKI_EXPECTED_LEN * 2) {
    throw keyInfoErr('SPKI must be a ' + (SPKI_EXPECTED_LEN * 2) + '-char hex string');
  }
  // Must start with canonical P-256 SPKI prefix (SEQUENCE + OID + BIT STRING header).
  if (!hex.toLowerCase().startsWith('3059301306072a8648ce3d020106082a8648ce3d030107034200')) {
    throw keyInfoErr('SPKI missing canonical P-256 structure');
  }
  // Verify the named-curve OID is present within the AlgorithmIdentifier.
  if (!hex.toLowerCase().includes(P256_SPKI_ALG_OID)) {
    throw keyInfoErr('SPKI missing P-256 curve OID');
  }
  const spki = fromHex(hex, SPKI_EXPECTED_LEN);
  // Uncompressed point starts at byte 26 (SPKI prefix ends). Must be 0x04.
  if (spki[26] !== 0x04) {
    throw keyInfoErr('SPKI point must be uncompressed (0x04)');
  }
  // Validate the 65-byte uncompressed point is actually ON the P-256 curve
  // (X,Y in field, satisfies the curve equation, not infinity). @noble's
  // Point.fromBytes throws for off-curve, out-of-field, and malformed points.
  const point = spki.subarray(26, 91); // 0x04 || X(32) || Y(32)
  try {
    p256.Point.fromBytes(point);
  } catch {
    throw keyInfoErr('SPKI point is not a valid P-256 curve point');
  }
  return spki;
}

function keyInfoErr(msg: string): Error {
  return new Error(`PokitDeviceKey: invalid native key info: ${msg}`);
}
