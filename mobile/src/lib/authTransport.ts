// M3-auth-3A: centralized, authenticated REST transport.
//
// On 401, conditionally invalidates (only if the 401 token is STILL the
// current bearer), forces a fresh challenge, and retries exactly once for
// idempotent methods. Non-idempotent 401s fail immediately.

import { AuthError, TokenManager } from './authClient';

export async function authenticatedFetch(
  url: string, init: RequestInit | undefined, tokenMgr: TokenManager,
): Promise<Response> {
  const isIdempotent = !init?.method || init.method === 'GET';
  const token = await tokenMgr.getValidToken();
  const res = await doRequest(url, init, token);
  if (res.status === 401 && isIdempotent) {
    tokenMgr.invalidateIfCurrent(token);
    const fresh = await tokenMgr.getValidToken(true);
    const retry = await doRequest(url, init, fresh);
    // Retry must also be checked — a second 401/403 is a typed failure.
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
