// M3-auth-1C: cross-platform device identity backed by the local Expo module
// pokit-device-key. The private scalar lives inside the platform provider
// (Android Keystore / iOS Secure Enclave) and NEVER enters JavaScript memory.
// Unsupported platforms fail closed — no software-key fallback.

import * as SecureStore from 'expo-secure-store';
import { createPokitDeviceKey } from '../../modules/pokit-device-key';
import type { DeviceKeyInfo } from '../../modules/pokit-device-key';
import type { DeviceIdentity } from './crypto';

export type { DeviceIdentity } from './crypto';

const KEY_ALIAS_STORE = 'pokit.device.keyAlias';
const KEY_ALIAS = 'pokit.device.identity.v1';
const LEGACY_PRIVKEY_KEY = 'pokit.device.privkey';

let _deviceKey: ReturnType<typeof createPokitDeviceKey> | null = null;

function deviceKey() {
  if (!_deviceKey) _deviceKey = createPokitDeviceKey();
  return _deviceKey;
}

// ── public API ──

export async function getDeviceKeySupport() {
  return deviceKey().getSupport();
}

export async function loadOrCreateDeviceIdentity(): Promise<DeviceIdentity> {
  await migrateLegacySoftwareIdentity();
  const info = await deviceKey().ensureKey();
  await markKeyAlias();
  return { spkiDer: info.publicKeySpki, deviceId: info.deviceId };
}

export async function hasDeviceIdentity(): Promise<boolean> {
  return deviceKey().hasKey();
}

export async function signWithDeviceKey(message: Uint8Array): Promise<Uint8Array> {
  return deviceKey().sign(message);
}

// ── legacy migration ──

// migrateLegacySoftwareIdentity removes the old JS software private key from
// SecureStore. It never imports a software key into a native provider. Safe to
// call multiple times (idempotent — does not fail if the key is absent).
export async function migrateLegacySoftwareIdentity(): Promise<void> {
  try {
    const legacy = await SecureStore.getItemAsync(LEGACY_PRIVKEY_KEY);
    if (legacy !== null) {
      await SecureStore.deleteItemAsync(LEGACY_PRIVKEY_KEY);
    }
  } catch {
    // SecureStore may be unavailable (Web, certain emulators) — not a blocker.
  }
}

async function markKeyAlias(): Promise<void> {
  try {
    await SecureStore.setItemAsync(KEY_ALIAS_STORE, KEY_ALIAS, {
      keychainAccessible: SecureStore.WHEN_UNLOCKED_THIS_DEVICE_ONLY,
    });
  } catch {}
}
