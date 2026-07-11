// M3-auth-4A: production pairing lifecycle — conduct + persist.
// The caller (ConnectScreen or the pairing UX) calls pairAndSave, which
// runs the full protocol and saves the result to AsyncStorage on success.

import type { PokitDeviceKey } from '../../modules/pokit-device-key';
import { conductPairing, type PairingResult } from './pairingClient';
import { savePairing, type StoredPairing } from './pairingStore';

// pairAndSave runs the full pairing protocol and persists the result on
// success. The operational origin comes from setBaseURL (the user-configured
// tunnel URL), NOT from the LAN pairing endpoint.
export async function pairAndSave(
  qrRaw: unknown, deviceKey: PokitDeviceKey, operationalBaseURL: string,
): Promise<PairingResult> {
  const result = await conductPairing(qrRaw, deviceKey);
  if (result.status !== 'approved') return result;

  // Canonicalise the operational origin from the base URL (not the LAN endpoint).
  let origin = '';
  try {
    const u = new URL(operationalBaseURL);
    if (u.protocol !== 'https:') return { status: 'network_error', errorDetail: 'production base URL must be HTTPS' };
    origin = u.origin;
  } catch {
    return { status: 'network_error', errorDetail: 'invalid operational base URL' };
  }

  try {
    await savePairing({
      hostId: result.hostId!,
      hostPubKeyB64: result.hostPubKeyB64!,
      deviceId: result.deviceId!,
      baseURL: operationalBaseURL,
      origin,
      pairedAt: new Date().toISOString(),
      role: result.role || 'owner',
    });
  } catch {
    return { status: 'network_error', errorDetail: 'failed to persist pairing' };
  }
  return result;
}
