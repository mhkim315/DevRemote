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
