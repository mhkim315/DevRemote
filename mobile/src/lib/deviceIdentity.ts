// M3-auth-1C: cross-platform device identity backed by the local Expo module
// pokit-device-key. The private scalar lives inside the platform provider
// and NEVER enters JavaScript memory. Unsupported platforms fail closed.

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
  await ensureLegacyKeyRemoved();
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

// ── legacy migration (BLOCKER 5: fail-closed) ──

// ensureLegacyKeyRemoved deletes the old JavaScript software private key.
// - no legacy key present → success
// - legacy key present + successfully deleted → success (re-pair may be needed)
// - legacy key present + deletion fails → throw (provisioning stops)
export async function ensureLegacyKeyRemoved(): Promise<void> {
  let legacy: string | null;
  try {
    legacy = await SecureStore.getItemAsync(LEGACY_PRIVKEY_KEY);
  } catch {
    // SecureStore unavailable (Web, certain emulators) — not a blocker.
    return;
  }
  if (legacy === null) return; // no legacy key

  try {
    await SecureStore.deleteItemAsync(LEGACY_PRIVKEY_KEY);
  } catch (e: any) {
    throw new Error(
      'PokitDeviceKey: failed to delete legacy software private key. ' +
      'Re-pairing is required but the old key could not be removed.'
    );
  }
}

async function markKeyAlias(): Promise<void> {
  try {
    await SecureStore.setItemAsync(KEY_ALIAS_STORE, KEY_ALIAS, {
      keychainAccessible: SecureStore.WHEN_UNLOCKED_THIS_DEVICE_ONLY,
    });
  } catch {}
}
