// M3-auth-1C: cross-platform device identity backed by the local Expo module
// pokit-device-key. The private scalar lives inside the platform provider
// (Android Keystore / iOS Secure Enclave) and NEVER enters JavaScript memory.
// Unsupported platforms fail closed — no software-key fallback.

import * as SecureStore from 'expo-secure-store';
import { createPokitDeviceKey } from '../../modules/pokit-device-key/src';
import type { DeviceKeyInfo } from '../../modules/pokit-device-key/src';
import { deviceFingerprint } from './crypto';
import type { DeviceIdentity } from './crypto';

export type { DeviceIdentity } from './crypto';

const KEY_ALIAS_STORE = 'pokit.device.keyAlias';
const KEY_ALIAS = 'pokit.device.identity.v1'; // matches native alias

let _deviceKey: ReturnType<typeof createPokitDeviceKey> | null = null;

function deviceKey() {
  if (!_deviceKey) _deviceKey = createPokitDeviceKey();
  return _deviceKey;
}

// ── public API ──

// getDeviceKeySupport returns the platform support status.
export async function getDeviceKeySupport() {
  return deviceKey().getSupport();
}

// loadOrCreateDeviceIdentity returns the persistent native identity, creating a
// new key on first run. The private key is never exposed to JS.
// Throws on unsupported platforms or provisioning failures.
export async function loadOrCreateDeviceIdentity(): Promise<DeviceIdentity> {
  const info = await deviceKey().ensureKey();
  return identityFromKeyInfo(info);
}

// hasDeviceIdentity reports whether a native key has been provisioned.
export async function hasDeviceIdentity(): Promise<boolean> {
  return deviceKey().hasKey();
}

// signWithDeviceKey signs a raw message using the native key.
export async function signWithDeviceKey(message: Uint8Array): Promise<Uint8Array> {
  return deviceKey().sign(message);
}

// ── helpers ──

function identityFromKeyInfo(info: DeviceKeyInfo): DeviceIdentity {
  // SPKI bytes are validated by the native wrapper (91 bytes).
  // Record the alias so we know a key was provisioned.
  SecureStore.setItemAsync(KEY_ALIAS_STORE, KEY_ALIAS, {
    keychainAccessible: SecureStore.WHEN_UNLOCKED_THIS_DEVICE_ONLY,
  }).catch(() => {}); // best-effort — the native key is the real identity anchor
  return { spkiDer: info.publicKeySpki, deviceId: info.deviceId };
}
