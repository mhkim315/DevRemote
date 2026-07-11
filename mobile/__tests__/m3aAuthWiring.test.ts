// M3-auth-R1: the M3a New Session flow (launch-profile list + session create)
// must ride the SAME host-bound device transport as the accepted reads, not the
// legacy Supabase/dev token. These proofs exercise the real production client
// functions (listSessionProfiles / createSession) — not copied helpers — and
// pin the profile+create authentication boundary from handoff §7:
//
//   - paired-device profile list sends the device bearer, not a Supabase token;
//   - owner create sends exactly {profileId,name,cwd} with the device bearer;
//   - the device bearer is NEVER sent to a non-paired origin (fail closed);
//   - a 401/403 create surfaces a recoverable auth error and is NOT replayed
//     (non-idempotent → exactly one POST → no duplicate session);
//   - a network failure adds no session;
//   - a malformed 2xx create is not runnable (the modal stays open, no phantom).

import {
  setBaseURL, setDeviceAuth,
  listSessionProfiles, createSession,
  ConnectivityFailure, PokitError,
} from '../src/lib/client';
import { isRunnable } from '../src/lib/lifecycle';
import type { TokenManager } from '../src/lib/authClient';

const HOST_A = 'https://host-a.example.com';
const HOST_B = 'https://host-b.example.com';

function fakeMgr(token = 'DEVICE_BEARER_A'): TokenManager {
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
    const r = responses[Math.min(i, responses.length - 1)] || { status: 200, body: {} };
    i++;
    return { status: r.status, ok: r.status >= 200 && r.status < 300, json: async () => r.body } as unknown as Response;
  });
  return calls;
}

describe('M3a profile list — host-bound device transport', () => {
  afterEach(() => setDeviceAuth(null));

  it('sends the device bearer to the paired origin, never the Supabase token', async () => {
    setDeviceAuth({ tokenManager: fakeMgr('DEVICE_BEARER_A'), origin: HOST_A });
    setBaseURL(HOST_A);
    const calls = mockFetch({ status: 200, body: [{ id: 'shell', label: 'Shell', available: true }] });

    const profiles = await listSessionProfiles('SUPABASE_JWT');

    expect(profiles).toHaveLength(1);
    expect(calls).toHaveLength(1);
    expect(calls[0].url).toBe(`${HOST_A}/api/session-profiles`);
    expect(calls[0].init.headers.Authorization).toBe('Bearer DEVICE_BEARER_A');
    expect(JSON.stringify(calls[0].init.headers)).not.toContain('SUPABASE_JWT');
  });

  it('fails closed on a non-paired origin — zero network requests', async () => {
    setDeviceAuth({ tokenManager: fakeMgr('DEVICE_BEARER_A'), origin: HOST_A });
    setBaseURL(HOST_B);
    const calls = mockFetch({ status: 200, body: [] });

    await expect(listSessionProfiles()).rejects.toMatchObject({ failure: ConnectivityFailure.AuthError });
    expect(calls).toHaveLength(0);
  });
});

describe('M3a session create — host-bound device transport', () => {
  afterEach(() => setDeviceAuth(null));

  it('owner create sends exactly {profileId,name,cwd} + device bearer, returns running controlled_pty', async () => {
    setDeviceAuth({ tokenManager: fakeMgr('DEVICE_BEARER_A'), origin: HOST_A });
    setBaseURL(HOST_A);
    const calls = mockFetch({
      status: 200,
      body: { id: 'controlled_pty:shell-1', adapter: 'controlled_pty', profileId: 'shell', name: 'work', state: 'running' },
    });

    const created = await createSession({ profileId: 'shell', name: 'work', cwd: '/tmp' }, 'SUPABASE_JWT');

    expect(calls).toHaveLength(1);
    expect(calls[0].url).toBe(`${HOST_A}/api/sessions`);
    expect(calls[0].init.method).toBe('POST');
    expect(calls[0].init.headers.Authorization).toBe('Bearer DEVICE_BEARER_A');
    expect(JSON.stringify(calls[0].init.headers)).not.toContain('SUPABASE_JWT');

    const body = JSON.parse(calls[0].init.body);
    expect(body).toEqual({ profileId: 'shell', name: 'work', cwd: '/tmp' });
    for (const forbidden of ['id', 'runner', 'command', 'executable', 'adapter', 'workspaceId', 'runnerColor']) {
      expect(body).not.toHaveProperty(forbidden);
    }

    // The accepted success contract: canonical id + controlled_pty + running.
    expect(isRunnable(created)).toBe(true);
    expect(created.id).toBe('controlled_pty:shell-1');
    expect(created.adapter).toBe('controlled_pty');
    expect(created.state).toBe('running');
  });

  it('member/read-only 403 → recoverable AuthError, exactly one POST (no duplicate)', async () => {
    setDeviceAuth({ tokenManager: fakeMgr(), origin: HOST_A });
    setBaseURL(HOST_A);
    const calls = mockFetch({ status: 403, body: { error: 'insufficient permission' } });

    await expect(createSession({ profileId: 'shell' })).rejects.toMatchObject({
      name: 'PokitError',
      failure: ConnectivityFailure.AuthError,
    });
    expect(calls).toHaveLength(1); // 403 is not retried
  });

  it('401 create is NOT auto-replayed (non-idempotent) → exactly one POST', async () => {
    setDeviceAuth({ tokenManager: fakeMgr(), origin: HOST_A });
    setBaseURL(HOST_A);
    // A blind retry would consume the second (200) response and create a
    // duplicate session. The transport must send exactly one POST.
    const calls = mockFetch(
      { status: 401, body: {} },
      { status: 200, body: { id: 'controlled_pty:dup', adapter: 'controlled_pty', state: 'running' } },
    );

    await expect(createSession({ profileId: 'shell' })).rejects.toMatchObject({
      failure: ConnectivityFailure.AuthError,
    });
    expect(calls).toHaveLength(1);
  });

  it('non-paired origin → device bearer never sent, ZERO network requests', async () => {
    setDeviceAuth({ tokenManager: fakeMgr('DEVICE_BEARER_A'), origin: HOST_A });
    setBaseURL(HOST_B);
    const calls = mockFetch({ status: 200, body: { id: 'x', adapter: 'controlled_pty', state: 'running' } });

    await expect(createSession({ profileId: 'shell' })).rejects.toMatchObject({
      failure: ConnectivityFailure.AuthError,
    });
    expect(calls).toHaveLength(0);
  });

  it('network failure → NetworkUnreachable (no session created)', async () => {
    setDeviceAuth({ tokenManager: fakeMgr(), origin: HOST_A });
    setBaseURL(HOST_A);
    (global as any).fetch = jest.fn(async () => { throw new Error('connection refused'); });

    await expect(createSession({ profileId: 'shell' })).rejects.toMatchObject({
      name: 'PokitError',
      failure: ConnectivityFailure.NetworkUnreachable,
    });
  });

  it('malformed 2xx (missing running/id) is not runnable → no navigation, no phantom card', async () => {
    setDeviceAuth({ tokenManager: fakeMgr(), origin: HOST_A });
    setBaseURL(HOST_A);
    mockFetch({ status: 200, body: { adapter: 'controlled_pty', state: 'starting' } }); // no id, not running

    const created = await createSession({ profileId: 'shell' });
    // createSession resolves (it is a 2xx), but the navigation gate rejects it.
    expect(isRunnable(created)).toBe(false);
  });
});
