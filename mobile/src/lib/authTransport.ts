// M3-auth-3A: centralized, authenticated REST transport.
//
// On 401, atomically refreshes the bearer via TokenManager.refreshAfter401,
// which only forces a new challenge if the 401 token is STILL current.
// Retries exactly once for idempotent methods. Non-idempotent 401s fail
// immediately. Retry responses are classified through the same policy.

import { AuthError, TokenManager } from './authClient';

export async function authenticatedFetch(
  url: string, init: RequestInit | undefined, tokenMgr: TokenManager,
): Promise<Response> {
  const isIdempotent = !init?.method || init.method === 'GET';
  const token = await tokenMgr.getValidToken();
  const res = await doRequest(url, init, token);
  if (res.status === 401 && isIdempotent) {
    const fresh = await tokenMgr.refreshAfter401(token);
    const retry = await doRequest(url, init, fresh);
    if (retry.status === 401 || retry.status === 403) {
      throw new AuthError('host_pin_fail', `auth rejected on retry: ${retry.status}`, retry.status);
    }
    return retry;
  }
  if (res.status === 401 || res.status === 403) {
    throw new AuthError('host_pin_fail', `auth rejected: ${res.status}`, res.status);
  }
  return res;
}

async function doRequest(url: string, init: RequestInit | undefined, token: string): Promise<Response> {
  try {
    return await fetch(url, {
      ...init,
      headers: { ...(init?.headers as Record<string, string> || {}), Authorization: `Bearer ${token}` },
    });
  } catch {
    throw new AuthError('network_error', 'request failed');
  }
}
