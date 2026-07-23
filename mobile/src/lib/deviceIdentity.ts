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

// ── 9.4-C §4.4: corruption handling ──

// VerifyIdentityResult encodes the three contract-mandated outcomes from
// verifying the device key against a stored pairing.
export type VerifyIdentityResult =
  | { ok: true; deviceId: string }
  | { ok: false; reason: 'key_missing' | 'key_invalidated' | 'key_inaccessible' | 'key_incompatible' | 'identity_mismatch' | 'keystore_unavailable' };

// verifyDeviceIdentityAgainstPairing checks that the ACTUAL hardware key on
// THIS device matches the stored pairing's deviceId. Returns the 9.4-C §4.4
// outcomes:
//   ok=true                    → key present, identity matches → safe to install bearer
//   ok=false, key_missing      → no key in Keystore → clear bearer, re-pair
//   ok=false, key_invalidated  → key was destroyed (lock screen change etc.) → clear bearer, re-pair
//   ok=false, key_inaccessible → Keystore unreachable → terminal trust failure
//   ok=false, identity_mismatch→ wrong key → clear bearer, re-pair
//   ok=false, keystore_unavailable → no hardware Keystore → terminal trust failure
export async function verifyDeviceIdentityAgainstPairing(
  expectedDeviceId: string,
  dk?: ReturnType<typeof createPokitDeviceKey>,
): Promise<VerifyIdentityResult> {
  const key = dk ?? deviceKey();
  let info;
  try {
    info = await key.getKeyInfo();
  } catch (e: any) {
    const code = e?.code || '';
    if (code === 'key_missing') return { ok: false, reason: 'key_missing' };
    if (code === 'key_invalidated') return { ok: false, reason: 'key_invalidated' };
    if (code === 'key_inaccessible') return { ok: false, reason: 'key_inaccessible' };
    if (code === 'key_incompatible') return { ok: false, reason: 'key_incompatible' };
    if (code === 'hardware_unavailable' || code === 'unsupported') {
      return { ok: false, reason: 'keystore_unavailable' };
    }
    return { ok: false, reason: 'key_inaccessible' };
  }
  if (!info || info.deviceId !== expectedDeviceId) {
    return { ok: false, reason: 'identity_mismatch' };
  }
  return { ok: true, deviceId: info.deviceId };
}

// clearLocalBearerState removes the active device bearer AND the stored
// pairing so a new pairing can be established. Called on corruption outcomes.
// The Keystore key itself is NOT deleted — only the authority chain is broken.
// Ordering: setDeviceAuth(null) FIRST (synchronous, cannot fail),
// then clearPairing (best-effort; failure is logged but not thrown).
export async function clearLocalBearerState(): Promise<void> {
  try {
    const { setDeviceAuth } = await import('./client');
    setDeviceAuth(null);
  } catch { /* best-effort — in-memory bearer already unset on new process */ }
  try {
    const { clearPairing } = await import('./pairingStore');
    await clearPairing();
  } catch { /* best-effort — pairing is stale on disk but authority chain broken */ }
}

// ── legacy migration (BLOCKER 5: fail-closed) ──

// ensureLegacyKeyRemoved deletes the old JavaScript software private key.
// - read succeeds, no legacy key → success (provisioning continues)
// - read succeeds, legacy key present + deleted → success (re-pair may be needed)
// - read FAILS → typed error, provisioning stops (we cannot confirm the software
//   key is gone, so we must not proceed to create a native identity)
// - delete FAILS → typed error, provisioning stops
// Raw storage errors and stored values are never logged.
export async function ensureLegacyKeyRemoved(): Promise<void> {
  let legacy: string | null;
  try {
    legacy = await SecureStore.getItemAsync(LEGACY_PRIVKEY_KEY);
  } catch {
    throw new Error(
      'PokitDeviceKey: could not determine whether a legacy software key exists; ' +
      'provisioning stopped (fail closed).'
    );
  }
  if (legacy === null) return; // no legacy key — safe to continue

  try {
    await SecureStore.deleteItemAsync(LEGACY_PRIVKEY_KEY);
  } catch {
    throw new Error(
      'PokitDeviceKey: failed to delete the legacy software private key; ' +
      're-pairing is required but the old key could not be removed (fail closed).'
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
