// M3-auth-4A: explicit terminal authentication mode.
// Do not infer the auth path from optional props — use an explicit enum so
// the legacy token path is unreachable in production remote mode.

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
