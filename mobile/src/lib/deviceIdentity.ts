// M3-auth-1: Android Keystore non-exportable ECDSA device identity.
//
// The private scalar lives inside the AndroidKeyStore and NEVER enters
// JavaScript memory. expo-secure-store holds only the key alias reference +
// host pairing metadata (not the key material). Software-key fallback is
// deliberately absent — the M2.5 contract requires a Keystore key.

import * as SecureStore from 'expo-secure-store';
import { NativeModules, Platform } from 'react-native';
import { deviceFingerprint, toHex, fromHex } from './crypto';
import type { DeviceIdentity } from './crypto';

export type { DeviceIdentity } from './crypto';

const KEY_ALIAS_STORE = 'pokit.device.keyAlias';
const KEY_ALIAS = 'pokit_device_identity'; // matches Kotlin KEY_ALIAS

function native(): { hasKey(): Promise<boolean>; generateKey(): Promise<string>; getPublicKey(): Promise<string | null>; sign(messageHex: string): Promise<string>; deleteKey(): Promise<void> } {
  if (Platform.OS !== 'android' || !NativeModules.PokitDeviceKey) {
    throw new Error('PokitDeviceKey native module is only available on Android');
  }
  return NativeModules.PokitDeviceKey;
}

// loadOrCreateDeviceIdentity returns the persistent Keystore-backed identity,
// creating a new key on first run. The private key is never exposed to JS; the
// native module returns the SPKI DER hex as the identity root.
export async function loadOrCreateDeviceIdentity(): Promise<DeviceIdentity> {
  if (await native().hasKey()) {
    return identityFromNative();
  }
  const spkiHex = await native().generateKey();
  // Record the alias so we know a key was provisioned.
  await SecureStore.setItemAsync(KEY_ALIAS_STORE, KEY_ALIAS, {
    keychainAccessible: SecureStore.WHEN_UNLOCKED_THIS_DEVICE_ONLY,
  });
  return identityFromSPKI(spkiHex);
}

// hasDeviceIdentity reports whether a Keystore key has been provisioned.
export async function hasDeviceIdentity(): Promise<boolean> {
  return native().hasKey();
}

// signWithDeviceKey signs a raw message using the Keystore key (SHA256withECDSA,
// matching Go's single-hash semantics). The JS side only hex-encodes the
// message and hex-decodes the returned DER signature — no crypto.
export async function signWithDeviceKey(message: Uint8Array): Promise<Uint8Array> {
  const sigHex = await native().sign(toHex(message));
  return fromHex(sigHex);
}

// identityFromSPKI computes the DeviceIdentity from the Keystore public SPKI DER hex.
export function identityFromSPKI(spkiHex: string): DeviceIdentity {
  const spkiDer = fromHex(spkiHex);
  return {
    publicKeyUncompressed: new Uint8Array(0), // not available from Keystore (only SPKI)
    spkiDer,
    deviceId: deviceFingerprint(spkiDer),
  } as DeviceIdentity;
}

async function identityFromNative(): Promise<DeviceIdentity> {
  const spkiHex = await native().getPublicKey();
  if (!spkiHex) throw new Error('Keystore key not found');
  return identityFromSPKI(spkiHex);
}
