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

// localTestOriginKeys is the closed set of hostnames permitted by
// canonicalLocalTestOrigin. Emulator aliases (10.0.2.2), LAN IPs, and
// arbitrary domains are never added to this set.
const localTestOriginKeys = new Set(['localhost', '127.0.0.1']);

// canonicalLocalTestOrigin is the explicit local-E2E origin validator for
// the Android Emulator development path. It permits HTTP ONLY when ALL of
// the following are true:
//   - localTestExplicit is true (caller attests to debug + NO_LOGIN flag)
//   - originMode is 'explicit_local_dev'
//   - the scheme is 'http:'
//   - the hostname is exactly 'localhost' or '127.0.0.1'
// Every other input — LAN IPs, 10.0.2.2, arbitrary domains, missing flags,
// wrong mode, missing port, credentials, query, fragment, path — is
// rejected. Production HTTPS origins are never routed through this function.
export function canonicalLocalTestOrigin(
  baseURL: string,
  localTestExplicit: boolean,
  originMode: string,
): { origin?: string; error?: string } {
  if (!localTestExplicit) return { error: 'local test requires explicit opt-in' };
  if (originMode !== 'explicit_local_dev') return { error: 'local test requires explicit_local_dev mode' };

  let u: URL;
  try { u = new URL(baseURL); } catch { return { error: 'invalid base URL' }; }
  if (u.protocol !== 'http:') return { error: 'local test only supports HTTP' };
  if (!localTestOriginKeys.has(u.hostname)) return { error: 'local test only permits localhost and 127.0.0.1' };
  if (u.username || u.password || u.search || u.hash) return { error: 'base URL must not contain credentials, query, or fragment' };
  if (u.pathname !== '/' && u.pathname !== '') return { error: 'base URL must not contain a path' };
  if (!u.port) return { error: 'local test requires an explicit port' };
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
  // M3-auth-2B: bind the stored pairing to the ACTUAL hardware key present on
  // THIS device before trusting it. A key that is missing or a different
  // identity (deleted/wiped/backup-restored/other key) is re-pairable
  // (pairing_required); an inaccessible/invalidated/incompatible key is an
  // explicit security failure (failed). Never enter paired_device on a key that
  // does not match the persisted deviceId.
  let info;
  try {
    info = await dk.getKeyInfo();
  } catch (e: any) {
    if (e && e.code === 'key_missing') return { mode: 'pairing_required' };
    return { mode: 'failed' };
  }
  if (!info || info.deviceId !== p.deviceId) return { mode: 'pairing_required' };
  let tokenMgr: TokenManager;
  try { tokenMgr = deps.makeTokenManager(p, dk, base); } catch { return { mode: 'failed' }; }
  return { mode: 'paired_device', tokenMgr, baseURL: base };
}

// pairThenConnect enforces the Blocker C ordering at the scan site: the trusted
// state install (onPaired) MUST resolve before any connection is opened, and a
// connection is opened ONLY when onPaired reports success. On failure — a false
// result OR a thrown/rejected onPaired/connect — onReject runs exactly once so
// the scanner never sticks in a scanned state and no partial paired state is
// entered.
export async function pairThenConnect(args: {
  onPaired?: () => Promise<boolean>;
  connect: () => Promise<void>;
  onReject: () => void;
}): Promise<void> {
  let ok: boolean;
  try {
    ok = args.onPaired ? await args.onPaired() : true;
  } catch {
    // BUG-004 fix: onPaired threw — leave scanner locked. The error was
    // surfaced via onError/notifyError above. Do NOT call onReject which
    // would immediately re-enable the scanner and cause a re-scan loop.
    return;
  }
  if (!ok) {
    // BUG-004 fix: onPaired returned false — same as above.
    return;
  }
  try {
    await args.connect();
  } catch {
    // connect() threw after trusted auth was installed — leave scanner
    // locked and show the error rather than risking a re-scan loop.
  }
}

// ── M3-auth-4A: app-entry routing ──
//
// Device pairing is the remote authentication root. A valid paired_device must
// reach the product WITHOUT a Supabase session; Supabase is only the gate for
// the legacy/explicit_local_dev path.
export type AppRoute =
  | 'loading'
  | 'pairing_required'
  | 'failed'
  | 'device_connect' // paired but the authenticated probe hasn't connected yet
  | 'supabase_auth'  // legacy path: no Supabase session
  | 'legacy_connect' // legacy path: has session, not connected
  | 'product';

export function selectAppRoute(
  authCtx: AuthContext | undefined,
  opts: { loading: boolean; session: boolean; isConnected: boolean },
): AppRoute {
  if (opts.loading || !authCtx || authCtx.mode === 'initializing') return 'loading';
  if (authCtx.mode === 'pairing_required') return 'pairing_required';
  if (authCtx.mode === 'failed') return 'failed';
  if (authCtx.mode === 'paired_device') {
    // No Supabase session required — the device bearer is the auth root.
    return opts.isConnected ? 'product' : 'device_connect';
  }
  // explicit_local_dev / legacy: existing Supabase-gated flow.
  if (!opts.session) return 'supabase_auth';
  if (!opts.isConnected) return 'legacy_connect';
  return 'product';
}
