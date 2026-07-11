// M3-auth-3A: centralized, authenticated REST transport.
//
// Uses TokenManager directly: every request pulls a valid token; on 401 the
// manager is invalidated and the request retried once (idempotent only).

import { AuthError, TokenManager } from './authClient';

export async function authenticatedFetch(
  url: string, init: RequestInit | undefined, tokenMgr: TokenManager,
): Promise<Response> {
  const isIdempotent = !init?.method || init.method === 'GET';
  return doFetch(url, init, tokenMgr, isIdempotent);
}

async function doFetch(
  url: string, init: RequestInit | undefined, tokenMgr: TokenManager,
  mayRetry: boolean,
): Promise<Response> {
  const token = await tokenMgr.getValidToken();
  let res: Response;
  try {
    res = await fetch(url, {
      ...init,
      headers: { ...(init?.headers as Record<string, string> || {}), Authorization: `Bearer ${token}` },
    });
  } catch {
    throw new AuthError('network_error', 'request failed');
  }
  if (res.status === 401 && mayRetry) {
    // Force a fresh challenge — the current bearer may have been revoked
    // or replaced. Then retry exactly once with the new token.
    tokenMgr.invalidate();
    const fresh = await tokenMgr.getValidToken(true);
    try {
      return await fetch(url, {
        ...init,
        headers: { ...(init?.headers as Record<string, string> || {}), Authorization: `Bearer ${fresh}` },
      });
    } catch {
      throw new AuthError('network_error', 'retry request failed');
    }
  }
  if (res.status === 401 || res.status === 403) {
    throw new AuthError('host_pin_fail', `auth rejected: ${res.status}`, res.status);
  }
  return res;
}
