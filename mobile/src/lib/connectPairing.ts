// M3-auth-2B: the production QR-pairing transition, extracted from ConnectScreen
// so the real UI→pairing path is testable end-to-end (Secure Enclave sign →
// persist canonical origin → install trusted auth BEFORE connect). ConnectScreen
// is a thin caller; behavior is unchanged.

import { pairThenConnect } from './authMode';
import type { PairingResult } from './pairingClient';
import type { PokitDeviceKey } from '../../modules/pokit-device-key';

// isPairingQR detects the pairing payload shape ConnectScreen keys on (a JSON
// object carrying sessionId + hostPubKey). A plain HTTPS URL is not a pairing QR.
export function isPairingQR(data: string): boolean {
  let p: any = null;
  try { p = JSON.parse(data); } catch { return false; }
  return !!p && typeof p === 'object' && !!p.sessionId && !!p.hostPubKey;
}

export interface PairFromScanDeps {
  data: string;                                   // raw scanned QR string
  createDeviceKey: () => PokitDeviceKey;          // production Secure Enclave/Keystore key
  getBaseURL: () => string;                       // operational (tunnel/HTTPS) origin
  pairAndSave: (qrRaw: unknown, dk: PokitDeviceKey, base: string) => Promise<PairingResult>;
  connect: (base: string) => Promise<void>;
  onPaired?: () => Promise<boolean>;              // atomic trusted-state install (Blocker C)
  onReject: () => void;                           // re-enable the scanner exactly once
  onError: (msg: string) => void;                 // surface a recoverable error
  onNeedBaseURL: () => void;                       // no operational URL set yet
}

// pairFromScannedQR runs the production pairing transition. It NEVER opens a
// connection before trusted auth state is installed: on approval it defers to
// pairThenConnect, which calls onPaired first and connects only if that
// succeeded. Any failure (no base URL, pairing rejected, thrown error) calls
// onReject exactly once so the scanner never sticks and no partial paired state
// is entered — no legacy remote fallback.
export async function pairFromScannedQR(deps: PairFromScanDeps): Promise<void> {
  const base = deps.getBaseURL();
  if (!base) { deps.onNeedBaseURL(); deps.onReject(); return; }

  let result: PairingResult;
  try {
    const dk = deps.createDeviceKey();
    result = await deps.pairAndSave(deps.data, dk, base);
  } catch (e: any) {
    deps.onError('Pairing error: ' + (e?.message || String(e)));
    deps.onReject();
    return;
  }

  if (result.status === 'approved') {
    await pairThenConnect({
      onPaired: deps.onPaired,
      connect: () => deps.connect(base),
      onReject: deps.onReject,
    });
  } else {
    deps.onError('Pairing failed: ' + (result.errorDetail || result.status));
    deps.onReject();
  }
}
