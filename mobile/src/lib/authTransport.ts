// M3-auth-3A: centralized, authenticated REST transport.
//
// Wraps the existing client.fetch pattern: every request adds
// Authorization: Bearer <token> sourced from authClient.getValidToken().
// On 401, attempts a single refresh-and-retry; only idempotent requests
// (GET) are retried. Non-idempotent 401s fail immediately. Callers
// provide a typed device token provider instead of raw token strings.

import { AuthError, type BearerState } from './authClient';

export type TokenProvider = () => Promise<string>;

// authenticatedFetch sends a request with a device bearer. On 401 it
// refreshes and retries exactly once for idempotent methods; non-idempotent
// 401s fail immediately. The caller provides a typed tokenSource function
// (typically authClient.getValidToken).

export async function authenticatedFetch(
  url: string, init: RequestInit | undefined, tokenSource: TokenProvider,
): Promise<Response> {
  const isIdempotent = !init?.method || init.method === 'GET';
  return doFetch(url, init, tokenSource, isIdempotent);
}

async function doFetch(
  url: string, init: RequestInit | undefined, tokenSource: TokenProvider,
  mayRetry: boolean,
): Promise<Response> {
  const token = await tokenSource();
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
    // Force a fresh auth, then retry once.
    const fresh = await tokenSource();
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
