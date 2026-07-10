// M3-auth-1: persistent device identity (software P-256 key in expo-secure-store).
//
// The private scalar is generated once and stored (hex) in the platform secure
// store (Android EncryptedSharedPreferences / iOS Keychain). The public
// identity (SPKI DER + deviceId fingerprint) is derived deterministically and
// must match the Go daemon exactly (see crypto.ts).

import * as SecureStore from 'expo-secure-store';
import { generatePrivateKey, deriveIdentity, toHex, fromHex, DeviceIdentity } from './crypto';

const PRIV_KEY_STORE = 'pokit.device.privkey';

export type { DeviceIdentity } from './crypto';
export { deriveIdentity } from './crypto';

// loadOrCreateDeviceIdentity returns the persisted device identity, creating and
// storing a new key on first run. The key never leaves the secure store except
// as an in-memory scalar for signing.
export async function loadOrCreateDeviceIdentity(): Promise<DeviceIdentity> {
  const stored = await SecureStore.getItemAsync(PRIV_KEY_STORE);
  if (stored) {
    return deriveIdentity(fromHex(stored));
  }
  const priv = generatePrivateKey();
  await SecureStore.setItemAsync(PRIV_KEY_STORE, toHex(priv), {
    keychainAccessible: SecureStore.WHEN_UNLOCKED_THIS_DEVICE_ONLY,
  });
  return deriveIdentity(priv);
}

// hasDeviceIdentity reports whether a device key has been provisioned.
export async function hasDeviceIdentity(): Promise<boolean> {
  return (await SecureStore.getItemAsync(PRIV_KEY_STORE)) !== null;
}
