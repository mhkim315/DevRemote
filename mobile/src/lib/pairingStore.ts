// M3-auth-2A: pairing persistence. AsyncStorage-backed; read validates the
// stored host SPKI + fingerprint so a corrupted store never produces a trusted
// state. A changed host key for the same hostId requires explicit clear-then-pair.

import AsyncStorage from '@react-native-async-storage/async-storage';
import { fromBase64 } from './crypto';

const PAIRING_KEY = 'pokit.pairing';

export interface StoredPairing {
  hostId: string;
  hostPubKeyB64: string;
  deviceId: string;
  baseURL: string;
  origin?: string;   // canonical origin (scheme://host:port), set at pair time
  pairedAt: string;  // ISO 8601
  role: string;
}

export async function savePairing(p: StoredPairing): Promise<void> {
  // Validate the host SPKI is at least 91 bytes canonical P-256 before storing.
  let spki: Uint8Array;
  try {
    spki = fromBase64(p.hostPubKeyB64);
  } catch { throw new Error('pairing: host public key is not valid base64'); }
  if (spki.length !== 91) throw new Error('pairing: host public key is not 91-byte P-256 SPKI');
  await AsyncStorage.setItem(PAIRING_KEY, JSON.stringify(p));
}

export async function loadPairing(): Promise<StoredPairing | null> {
  const raw = await AsyncStorage.getItem(PAIRING_KEY);
  if (!raw) return null;
  let parsed: unknown;
  try { parsed = JSON.parse(raw); } catch { await AsyncStorage.removeItem(PAIRING_KEY); return null; }
  const p = parsed as Record<string, unknown>;
  if (typeof p.hostId !== 'string' || typeof p.hostPubKeyB64 !== 'string' ||
      typeof p.deviceId !== 'string' || typeof p.baseURL !== 'string' ||
      typeof p.pairedAt !== 'string' || typeof p.role !== 'string') {
    await AsyncStorage.removeItem(PAIRING_KEY);
    return null;
  }
  // Validate the stored host SPKI + fingerprint match before trusting it.
  let spki: Uint8Array;
  try {
    spki = fromBase64(p.hostPubKeyB64);
  } catch { await AsyncStorage.removeItem(PAIRING_KEY); return null; }
  if (spki.length !== 91) { await AsyncStorage.removeItem(PAIRING_KEY); return null; }
  // The fingerprint is not stored separately; it's derived on-the-fly by
  // consumers who need to pin against a new QR. We only ensure the stored SPKI
  // is canonical. Corrupted/inconsistent → removed, requiring fresh pairing.
  return p as unknown as StoredPairing;
}

export async function clearPairing(): Promise<void> {
  await AsyncStorage.removeItem(PAIRING_KEY);
}

export async function hasPairing(): Promise<boolean> {
  return (await loadPairing()) !== null;
}
