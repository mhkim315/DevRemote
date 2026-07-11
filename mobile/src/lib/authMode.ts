// M3-auth-4A: explicit terminal authentication mode.
// Do not infer the auth path from optional props — use an explicit enum so
// the legacy token path is unreachable in production remote mode.

import type { StoredPairing } from './pairingStore';
import type { TokenManager } from './authClient';
import type { PokitDeviceKey } from '../../modules/pokit-device-key';

export type AuthMode =
  | 'initializing'      // loading pairing, creating TokenManager
  | 'paired_device'     // remote mode: device bearer + WS ticket
  | 'explicit_local_dev' // local dev only: NO_LOGIN flag, dev-token
  | 'pairing_required'  // authenticated, no pairing → show pairing UX
  | 'failed';           // init failure → visible error

export interface AuthContext {
  mode: AuthMode;
  tokenMgr?: import('./authClient').TokenManager;
  baseURL?: string;
  legacyToken?: string; // only used in explicit_local_dev
}

// canonicalOrigin validates a base URL for production use: HTTPS, no userinfo,
// query, fragment, or non-root path. Returns the canonical origin (scheme://host)
// or an error string. Shared by pairAndSave (save-time) and App (load-time).
export function canonicalOrigin(baseURL: string): { origin?: string; error?: string } {
  let u: URL;
  try { u = new URL(baseURL); } catch { return { error: 'invalid base URL' }; }
  if (u.protocol !== 'https:') return { error: 'production base URL must be HTTPS' };
  if (u.username || u.password || u.search || u.hash) return { error: 'base URL must not contain credentials, query, or fragment' };
  if (u.pathname !== '/' && u.pathname !== '') return { error: 'base URL must not contain a path' };
  return { origin: u.origin };
}

// deriveTerminalAuth is the ONE authoritative production decision for which
// terminal auth path to use. Exported from here (no RN deps) so jest imports
// it directly without needing React Native mocks.
export function deriveTerminalAuth(
  authCtx: AuthContext | undefined,
  session: string,
): { tokenMgr?: import('./authClient').TokenManager; baseURL?: string; termURI: string | null } {
  if (authCtx?.mode === 'paired_device' && authCtx.tokenMgr && authCtx.baseURL) {
    return { tokenMgr: authCtx.tokenMgr, baseURL: authCtx.baseURL, termURI: null };
  }
  if (authCtx?.mode === 'explicit_local_dev' && authCtx.legacyToken) {
    return { tokenMgr: undefined, baseURL: undefined, termURI: '/term/?session=' + encodeURIComponent(session) + '&token=' + encodeURIComponent(authCtx.legacyToken) };
  }
  return { tokenMgr: undefined, baseURL: undefined, termURI: null };
}

// ── M3-auth-4A Blocker C: authoritative, atomic pairing completion ──
//
// After a QR pair is saved, the app must install trusted auth state BEFORE it
// opens any connection. These two helpers make that ordering testable and
// keep the RN-free contract (jest imports them directly).

export interface PairingCompletionDeps {
  loadPairing: () => Promise<StoredPairing | null>;
  getBaseURL: () => string;
  createDeviceKey: () => PokitDeviceKey;
  makeTokenManager: (p: StoredPairing, dk: PokitDeviceKey, base: string) => TokenManager;
}

// completePairing performs the post-pair trusted-state install in one
// authoritative step: reload the saved pairing, validate the canonical origin
// binding (scheme+host+port), create the DeviceKey + TokenManager, and return
// the AuthContext to enter. It NEVER opens a connection. A non-'paired_device'
// result means the caller must not connect and must not enter a partially
// paired state. Origin binding is required (a pairing without a stored origin
// fails closed) to match the load-time check in App and the security invariant.
export async function completePairing(deps: PairingCompletionDeps): Promise<AuthContext> {
  let p: StoredPairing | null;
  try { p = await deps.loadPairing(); } catch { return { mode: 'failed' }; }
  const base = deps.getBaseURL();
  if (!p || !base) return { mode: 'pairing_required' };
  const { origin, error } = canonicalOrigin(base);
  if (error || !origin) return { mode: 'failed' };
  if (!p.origin || origin !== p.origin) return { mode: 'failed' };
  let dk: PokitDeviceKey;
  try { dk = deps.createDeviceKey(); } catch { return { mode: 'failed' }; }
  let tokenMgr: TokenManager;
  try { tokenMgr = deps.makeTokenManager(p, dk, base); } catch { return { mode: 'failed' }; }
  return { mode: 'paired_device', tokenMgr, baseURL: base };
}

// pairThenConnect enforces the Blocker C ordering at the scan site: the trusted
// state install (onPaired) MUST resolve before any connection is opened, and a
// connection is opened ONLY when onPaired reports success. On failure onReject
// keeps the scanner visible without entering a partially paired state.
export async function pairThenConnect(args: {
  onPaired?: () => Promise<boolean>;
  connect: () => Promise<void>;
  onReject: () => void;
}): Promise<void> {
  const ok = args.onPaired ? await args.onPaired() : true;
  if (ok) {
    await args.connect();
  } else {
    args.onReject();
  }
}
