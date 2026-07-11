// M3-auth-4A remediation: the central device transport is HOST-BOUND. The
// device bearer is sent ONLY to the exact canonical paired origin and fails
// closed (no network request) on any mismatch or URL variant. These tests also
// prove the app reaches the accepted ticket Terminal transport.

import {
  setBaseURL, setDeviceAuth, hasDeviceAuth,
  probeDaemon, listSessions, ConnectivityFailure,
} from '../src/lib/client';
import { deriveTerminalAuth } from '../src/lib/authMode';
import type { TokenManager } from '../src/lib/authClient';

const HOST_A = 'https://host-a.example.com';
const HOST_B = 'https://host-b.example.com';

function fakeMgr(token = 'BEARER_A'): TokenManager {
  return {
    getValidToken: async () => token,
    refreshAfter401: async () => token + '-refreshed',
  } as unknown as TokenManager;
}

// Installs a jest fetch mock and returns the recorded calls array.
function mockFetch(...responses: Array<{ status: number; body?: any }>) {
  const calls: Array<{ url: string; init: any }> = [];
  let i = 0;
  (global as any).fetch = jest.fn(async (url: string, init: any) => {
    calls.push({ url, init });
    const r = responses[Math.min(i, responses.length - 1)] || { status: 200, body: [] };
    i++;
    return { status: r.status, ok: r.status >= 200 && r.status < 300, json: async () => r.body } as unknown as Response;
  });
  return calls;
}

describe('host-bound device transport', () => {
  afterEach(() => { setDeviceAuth(null); });

  it('Host A auth → Host A request succeeds with the device bearer', async () => {
    setDeviceAuth({ tokenManager: fakeMgr('BEARER_A'), origin: HOST_A });
    setBaseURL(HOST_A);
    const calls = mockFetch({ status: 200, body: [{ id: 'controlled_pty:s1' }] });

    const res = await probeDaemon('LEGACY_SUPABASE');

    expect(res.reachable).toBe(true);
    expect(res.sessionsLoaded).toBe(true);
    expect(res.failure).toBe(ConnectivityFailure.None);
    expect(calls).toHaveLength(1);
    expect(calls[0].url).toBe(`${HOST_A}/api/sessions`);
    expect(calls[0].init.headers.Authorization).toBe('Bearer BEARER_A');
    expect(JSON.stringify(calls[0].init.headers)).not.toContain('LEGACY_SUPABASE');
  });

  it('authenticated session list loads with the device bearer', async () => {
    setDeviceAuth({ tokenManager: fakeMgr('BEARER_A'), origin: HOST_A });
    setBaseURL(HOST_A);
    const calls = mockFetch({ status: 200, body: [{ id: 's1' }, { id: 's2' }] });

    const list = await listSessions('LEGACY_SUPABASE');

    expect(list).toHaveLength(2);
    expect(calls[0].init.headers.Authorization).toBe('Bearer BEARER_A');
  });

  it('Host A auth → Host B URL makes ZERO network requests (fail closed)', async () => {
    setDeviceAuth({ tokenManager: fakeMgr('BEARER_A'), origin: HOST_A });
    setBaseURL(HOST_B);
    const calls = mockFetch({ status: 200, body: [] });

    await expect(listSessions()).rejects.toMatchObject({ failure: ConnectivityFailure.AuthError });
    expect(calls).toHaveLength(0);

    // probeDaemon classifies the same fail-closed result and still sends nothing.
    const res = await probeDaemon();
    expect(res.failure).toBe(ConnectivityFailure.AuthError);
    expect(calls).toHaveLength(0);
  });

  it('modified host/port/path/query/fragment/userinfo makes ZERO requests', async () => {
    const variants = [
      'https://host-a.example.com:8443',        // different port
      'https://evil.example.com',                // different host
      'https://host-a.example.com/api',          // non-root path
      'https://host-a.example.com?x=1',          // query
      'https://host-a.example.com#z',            // fragment
      'https://user:pass@host-a.example.com',    // userinfo
      'http://host-a.example.com',               // non-HTTPS
    ];
    for (const v of variants) {
      setDeviceAuth({ tokenManager: fakeMgr('BEARER_A'), origin: HOST_A });
      setBaseURL(v);
      const calls = mockFetch({ status: 200, body: [] });
      await expect(listSessions()).rejects.toMatchObject({ failure: ConnectivityFailure.AuthError });
      expect(calls).toHaveLength(0);
    }
  });

  it('disconnect / re-pair disposes the previous transport', async () => {
    setDeviceAuth({ tokenManager: fakeMgr('BEARER_A'), origin: HOST_A });
    expect(hasDeviceAuth()).toBe(true);

    // Disconnect clears device auth → legacy path uses the passed token.
    setDeviceAuth(null);
    expect(hasDeviceAuth()).toBe(false);
    setBaseURL(HOST_A);
    const calls = mockFetch({ status: 200, body: [] });
    await probeDaemon('LEGACY_TOKEN');
    expect(calls[0].init.headers.Authorization).toBe('Bearer LEGACY_TOKEN');
  });

  it('Host B bearer is used ONLY after Host B pairing completes', async () => {
    // Still paired to A: a Host B request sends nothing.
    setDeviceAuth({ tokenManager: fakeMgr('BEARER_A'), origin: HOST_A });
    setBaseURL(HOST_B);
    let calls = mockFetch({ status: 200, body: [] });
    await expect(listSessions()).rejects.toMatchObject({ failure: ConnectivityFailure.AuthError });
    expect(calls).toHaveLength(0);

    // Host B pairing completes → new host-bound transport installed.
    setDeviceAuth({ tokenManager: fakeMgr('BEARER_B'), origin: HOST_B });
    calls = mockFetch({ status: 200, body: [{ id: 's1' }] });
    await listSessions();
    expect(calls).toHaveLength(1);
    expect(calls[0].url).toBe(`${HOST_B}/api/sessions`);
    expect(calls[0].init.headers.Authorization).toBe('Bearer BEARER_B');
  });

  it('device-mode 401 (after refresh) classifies as AuthError, still reachable', async () => {
    setDeviceAuth({ tokenManager: fakeMgr(), origin: HOST_A });
    setBaseURL(HOST_A);
    mockFetch({ status: 401 }, { status: 401 }); // idempotent GET retries once
    const res = await probeDaemon();
    expect(res.failure).toBe(ConnectivityFailure.AuthError);
    expect(res.sessionsLoaded).toBe(false);
    expect(res.reachable).toBe(true);
  });

  it('legacy mode (no device auth) still uses the passed token', async () => {
    setDeviceAuth(null);
    setBaseURL(HOST_A);
    const calls = mockFetch({ status: 200, body: [] });
    await probeDaemon('LEGACY_TOKEN');
    expect(calls[0].init.headers.Authorization).toBe('Bearer LEGACY_TOKEN');
  });

  it('a selected paired session routes to the ticket Terminal transport (not legacy URL)', () => {
    // deriveTerminalAuth is the authoritative Terminal auth decision. A
    // paired_device session yields the async ticket path (tokenMgr + baseURL,
    // termURI=null) which TerminalController.bootstrap turns into a one-time WS
    // ticket (proven in terminalGeneratedScript.test.ts). This closes the chain
    // session list → select → ticket Terminal transport.
    const r = deriveTerminalAuth({ mode: 'paired_device', tokenMgr: {} as any, baseURL: HOST_A }, 'controlled_pty:s1');
    expect(r.tokenMgr).toBeDefined();
    expect(r.baseURL).toBe(HOST_A);
    expect(r.termURI).toBeNull(); // never the legacy ?token= URL
  });
});
