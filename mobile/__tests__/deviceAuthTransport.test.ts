// M3-auth-4A remediation: prove the app actually reaches the accepted ticket
// Terminal transport. These tests exercise the real client.ts REST transport
// with a paired-device TokenManager installed, plus the routing decision that
// lets a selected session reach the ticket bootstrap.

import {
  setBaseURL, setDeviceAuth, hasDeviceAuth,
  probeDaemon, listSessions, ConnectivityFailure,
} from '../src/lib/client';
import { deriveTerminalAuth } from '../src/lib/authMode';
import type { TokenManager } from '../src/lib/authClient';

function fakeMgr(token = 'DEVICE_BEARER'): TokenManager {
  return {
    getValidToken: async () => token,
    refreshAfter401: async () => token + '-refreshed',
  } as unknown as TokenManager;
}

function mockFetchSequence(...responses: Array<{ status: number; body?: any }>) {
  const calls: Array<{ url: string; init: any }> = [];
  let i = 0;
  (global as any).fetch = jest.fn(async (url: string, init: any) => {
    calls.push({ url, init });
    const r = responses[Math.min(i, responses.length - 1)];
    i++;
    return {
      status: r.status,
      ok: r.status >= 200 && r.status < 300,
      json: async () => r.body,
    } as unknown as Response;
  });
  return calls;
}

describe('paired-device authenticated REST transport', () => {
  beforeEach(() => { setBaseURL('https://daemon.test'); });
  afterEach(() => { setDeviceAuth(null); });

  it('proof: pairing → authenticated probe → connected (device bearer, not legacy)', async () => {
    setDeviceAuth(fakeMgr('DEVICE_BEARER'));
    const calls = mockFetchSequence({ status: 200, body: [{ id: 'controlled_pty:s1' }] });

    // Even if a legacy token is passed, the device bearer must be used.
    const res = await probeDaemon('LEGACY_SUPABASE');

    expect(res.reachable).toBe(true);
    expect(res.sessionsLoaded).toBe(true);
    expect(res.failure).toBe(ConnectivityFailure.None);
    expect(calls[0].url).toBe('https://daemon.test/api/sessions');
    expect(calls[0].init.headers.Authorization).toBe('Bearer DEVICE_BEARER');
    expect(JSON.stringify(calls[0].init.headers)).not.toContain('LEGACY_SUPABASE');
  });

  it('proof: authenticated session list loads with the device bearer', async () => {
    setDeviceAuth(fakeMgr('DEVICE_BEARER'));
    const calls = mockFetchSequence({ status: 200, body: [{ id: 'controlled_pty:s1' }, { id: 'controlled_pty:s2' }] });

    const list = await listSessions('LEGACY_SUPABASE');

    expect(list).toHaveLength(2);
    expect(calls[0].init.headers.Authorization).toBe('Bearer DEVICE_BEARER');
  });

  it('device-mode 401 (after refresh) classifies as AuthError, still reachable', async () => {
    setDeviceAuth(fakeMgr());
    // authenticatedFetch retries idempotent GET once after refresh → two 401s.
    mockFetchSequence({ status: 401 }, { status: 401 });
    const res = await probeDaemon();
    expect(res.failure).toBe(ConnectivityFailure.AuthError);
    expect(res.sessionsLoaded).toBe(false);
    expect(res.reachable).toBe(true);
  });

  it('legacy mode (no device auth) still uses the passed token', async () => {
    setDeviceAuth(null);
    expect(hasDeviceAuth()).toBe(false);
    const calls = mockFetchSequence({ status: 200, body: [] });
    await probeDaemon('LEGACY_TOKEN');
    expect(calls[0].init.headers.Authorization).toBe('Bearer LEGACY_TOKEN');
  });

  it('proof: a selected paired session routes to the ticket Terminal transport (not legacy URL)', () => {
    // deriveTerminalAuth is the authoritative Terminal auth decision. A
    // paired_device session yields the async ticket path (tokenMgr + baseURL,
    // termURI=null) which TerminalController.bootstrap turns into a one-time WS
    // ticket (proven in terminalGeneratedScript.test.ts). This closes the chain
    // session list → select → ticket Terminal transport.
    const r = deriveTerminalAuth({ mode: 'paired_device', tokenMgr: {} as any, baseURL: 'https://daemon.test' }, 'controlled_pty:s1');
    expect(r.tokenMgr).toBeDefined();
    expect(r.baseURL).toBe('https://daemon.test');
    expect(r.termURI).toBeNull(); // never the legacy ?token= URL
  });
});
