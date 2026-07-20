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
  operationalBaseURL: string;                     // operational (tunnel/HTTPS) origin
  pairAndSave: (qrRaw: unknown, dk: PokitDeviceKey, base: string) => Promise<PairingResult>;
  connect: (base: string) => Promise<void>;
  onPaired?: () => Promise<boolean>;              // atomic trusted-state install (Blocker C)
  onReject: () => void;                           // re-enable the scanner exactly once
  onError: (msg: string) => void;                 // surface a recoverable error
}

// pairFromScannedQR runs the production pairing transition. It NEVER opens a
// connection before trusted auth state is installed: on approval it defers to
// pairThenConnect, which calls onPaired first and connects only if that
// succeeded. Any failure (no base URL, pairing rejected, thrown error) calls
// onReject exactly once so the scanner never sticks and no partial paired state
// is entered — no legacy remote fallback.
export async function pairFromScannedQR(deps: PairFromScanDeps): Promise<void> {
  const base = deps.operationalBaseURL;

  // Reject empty operational URL before any device key creation.
  if (!base || !base.trim()) {
    deps.onError('Set a valid HTTPS daemon URL before pairing.');
    deps.onReject();
    return;
  }

  // A remote pairing MUST install trusted auth state (onPaired) before any
  // connection. Without an installer we fail closed rather than connect on top
  // of a stale/previous TokenManager — this closes the re-pair-from-device_connect
  // hole where pairThenConnect would otherwise treat a missing callback as success.
  if (!deps.onPaired) {
    deps.onError('Pairing unavailable: no trusted-state installer');
    deps.onReject();
    return;
  }

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

// ── ConnectScreen scan boundary (BLOCKER 3 recovery) ──
//
// The FULL scan handling — including the lazy module load — must live inside one
// recovery boundary so that ANY failure (module import, dependency setup, or the
// pairing transition) resets the scanner exactly once and surfaces an error.
// Extracted so the real handler path is unit-tested without a React render.

export interface ScanModules {
  pairFromScannedQR: (deps: PairFromScanDeps) => Promise<void>;
  pairAndSave: PairFromScanDeps['pairAndSave'];
  createDeviceKey: () => PokitDeviceKey;
  operationalBaseURL: string;
}

export interface RunScanDeps {
  data: string;
  isPairing: (data: string) => boolean;    // = isPairingQR
  loadModules: () => Promise<ScanModules>;  // the lazy import bundle
  connect: (url: string) => Promise<void>;
  onPaired?: () => Promise<boolean>;
  setScanned: (v: boolean) => void;         // scanner lock
  notifyError: (msg: string) => void;       // e.g. alert
}

// runScan is the production QR handler body. A pairing QR sets the scanner lock
// and runs the lazy-load + transition inside a single try/catch: on ANY failure
// the scanner is reset exactly once and an error is shown, so a failed dynamic
// import can never leave the scanner stuck. A plain HTTPS URL connects directly.
export async function runScan(deps: RunScanDeps): Promise<void> {
  if (deps.isPairing(deps.data)) {
    deps.setScanned(true);
    try {
      const m = await deps.loadModules();
      await m.pairFromScannedQR({
        data: deps.data,
        createDeviceKey: m.createDeviceKey,
        operationalBaseURL: m.operationalBaseURL,
        pairAndSave: m.pairAndSave,
        connect: deps.connect,
        onPaired: deps.onPaired,
        onReject: () => deps.setScanned(false),
        onError: (msg) => deps.notifyError(msg),
      });
    } catch (e: any) {
      deps.notifyError('Pairing error: ' + (e?.message || String(e)));
      deps.setScanned(false);
    }
    return;
  }
  // Non-pairing QR: notify and reset scanner.
  deps.notifyError('Not a pairing QR. Scan the QR code from your terminal.');
  deps.setScanned(false);
}
