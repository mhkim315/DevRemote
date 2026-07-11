// PokitDeviceKey — cross-platform non-exportable device identity.
//
// Each platform exposes a provider. No provider falls back to a JavaScript
// software key; an unsupported platform fails closed and blocks pairing.
// The TypeScript wrapper owns codec enforcement and byte-length validation;
// native implementations transport bytes in canonical hex, never as opaque
// objects.

import { NativeModules, Platform } from 'react-native';
import { fromHex, toHex } from '../../../src/lib/crypto';

// ── Common contract ──

export type DeviceKeyProvider = 'android_keystore' | 'ios_secure_enclave';

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

// ── Provider stubs per platform ──

function stubSupport(): DeviceKeySupport {
  if (Platform.OS === 'android') return 'not_implemented';
  if (Platform.OS === 'ios') return 'not_implemented';
  return 'not_implemented';
}

function unsupported(op: string): Error {
  return new Error(`PokitDeviceKey: ${op} is ${stubSupport()} on ${Platform.OS}`);
}

// ── Android Keystore provider ──

const nativeAndroid = NativeModules.PokitDeviceKey as {
  getSupport: () => Promise<string>;
  hasKey: () => Promise<boolean>;
  ensureKey: () => Promise<{ provider: string; keyVersion: 1; deviceId: string; publicKeySpkiHex: string; hardwareBacked: true }>;
  getPublicKeySpki: () => Promise<string>;
  sign: (messageHex: string) => Promise<string>;
  deleteKey: () => Promise<void>;
} | undefined;

// ── iOS Secure Enclave provider ──

const nativeIOS = NativeModules.PokitDeviceKey as {
  getSupport: () => Promise<string>;
  hasKey: () => Promise<boolean>;
  ensureKey: () => Promise<{ provider: string; keyVersion: 1; deviceId: string; publicKeySpkiHex: string; hardwareBacked: true }>;
  getPublicKeySpki: () => Promise<string>;
  sign: (messageHex: string) => Promise<string>;
  deleteKey: () => Promise<void>;
} | undefined;

// ── Web (standalone stub) ──

const webProvider: PokitDeviceKey = {
  async getSupport() { return 'not_implemented'; },
  async hasKey() { return false; },
  async ensureKey() { throw unsupported('ensureKey'); },
  async getPublicKeySpki() { throw unsupported('getPublicKeySpki'); },
  async sign(_msg: Uint8Array) { throw unsupported('sign'); },
  async deleteKey() { throw unsupported('deleteKey'); },
};

// ── Public factory ──

export function createPokitDeviceKey(): PokitDeviceKey {
  if (Platform.OS === 'android' && nativeAndroid) {
    return createAndroidProvider(nativeAndroid);
  }
  if (Platform.OS === 'ios' && nativeIOS) {
    return createIOSProvider(nativeIOS);
  }
  return webProvider;
}

function createAndroidProvider(native: NonNullable<typeof nativeAndroid>): PokitDeviceKey {
  return {
    async getSupport(): Promise<DeviceKeySupport> {
      const s = await native.getSupport();
      return s as DeviceKeySupport;
    },
    hasKey: () => native.hasKey(),
    async ensureKey(): Promise<DeviceKeyInfo> {
      const raw = await native.ensureKey();
      return makeKeyInfo(raw);
    },
    async getPublicKeySpki(): Promise<Uint8Array> {
      const hex = await native.getPublicKeySpki();
      return fromHex(hex, SPKI_EXPECTED_LEN);
    },
    async sign(message: Uint8Array): Promise<Uint8Array> {
      const sigHex = await native.sign(toHex(message));
      return fromHex(sigHex);
    },
    deleteKey: () => native.deleteKey(),
  };
}

function createIOSProvider(native: NonNullable<typeof nativeIOS>): PokitDeviceKey {
  return {
    async getSupport(): Promise<DeviceKeySupport> {
      const s = await native.getSupport();
      return s as DeviceKeySupport;
    },
    hasKey: () => native.hasKey(),
    async ensureKey(): Promise<DeviceKeyInfo> {
      const raw = await native.ensureKey();
      return makeKeyInfo(raw);
    },
    async getPublicKeySpki(): Promise<Uint8Array> {
      const hex = await native.getPublicKeySpki();
      return fromHex(hex, SPKI_EXPECTED_LEN);
    },
    async sign(message: Uint8Array): Promise<Uint8Array> {
      const sigHex = await native.sign(toHex(message));
      return fromHex(sigHex);
    },
    deleteKey: () => native.deleteKey(),
  };
}

function makeKeyInfo(raw: { provider: string; keyVersion: 1; deviceId: string; publicKeySpkiHex: string; hardwareBacked: true }): DeviceKeyInfo {
  return {
    provider: raw.provider as DeviceKeyProvider,
    keyVersion: 1,
    deviceId: raw.deviceId,
    publicKeySpki: fromHex(raw.publicKeySpkiHex, SPKI_EXPECTED_LEN),
    hardwareBacked: true,
  };
}
