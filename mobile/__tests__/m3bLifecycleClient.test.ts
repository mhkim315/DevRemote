// M3b: the mobile Stop / Force Kill / Delete-History lifecycle actions must ride
// the SAME host-bound paired-device transport as the accepted M3a create/reads,
// hit the EXACT production routes, and never use the forbidden legacy query
// DELETE. These proofs exercise the real production client functions
// (stopSession / killSession / deleteSessionHistory) — not copied helpers.
//
//   - stop/kill use POST /api/sessions/{id}/stop|kill; delete uses the canonical
//     PATH DELETE /api/sessions/{id} — never DELETE /api/sessions?id=...;
//   - the device bearer is sent ONLY to the paired origin (Authorization header),
//     never the Supabase/legacy token;
//   - a non-paired origin makes ZERO network requests (fail closed);
//   - an invalid-bearer 401 and a permission-403 are distinguishable via
//     PokitError.statusCode;
//   - a 409 (delete on running/stopping) is surfaced with statusCode 409;
//   - a network failure is NetworkUnreachable and no action is replayed;
//   - the bearer / credentials never appear in a thrown error message.

import {
  setBaseURL, setDeviceAuth,
  stopSession, killSession, deleteSessionHistory,
  ConnectivityFailure, PokitError,
} from '../src/lib/client';
import { LifecycleResultError } from '../src/lib/lifecycle';
import type { TokenManager } from '../src/lib/authClient';

const HOST_A = 'https://host-a.example.com';
const HOST_B = 'https://host-b.example.com';
const SID = 'controlled_pty:shell-1';

function fakeMgr(token = 'DEVICE_BEARER_A'): TokenManager {
  return {
    getValidToken: async () => token,
    refreshAfter401: async () => token + '-refreshed',
  } as unknown as TokenManager;
}

// Records every fetch call and returns canned responses in order.
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

describe('M3b lifecycle actions — exact route + host-bound device transport', () => {
  afterEach(() => setDeviceAuth(null));

  it('stopSession POSTs /api/sessions/{id}/stop with the device bearer only, no body', async () => {
    setDeviceAuth({ tokenManager: fakeMgr('DEVICE_BEARER_A'), origin: HOST_A });
    setBaseURL(HOST_A);
    const calls = mockFetch({ status: 200, body: { sessionId: SID, action: 'stop', state: 'exited' } });

    const res = await stopSession(SID, 'SUPABASE_JWT');

    expect(calls).toHaveLength(1);
    expect(calls[0].url).toBe(`${HOST_A}/api/sessions/${encodeURIComponent(SID)}/stop`);
    expect(calls[0].init.method).toBe('POST');
    expect(calls[0].init.body).toBeUndefined();
    expect(calls[0].init.headers.Authorization).toBe('Bearer DEVICE_BEARER_A');
    expect(JSON.stringify(calls[0].init.headers)).not.toContain('SUPABASE_JWT');
    expect(res.state).toBe('exited');
  });

  it('killSession POSTs /api/sessions/{id}/kill with the device bearer', async () => {
    setDeviceAuth({ tokenManager: fakeMgr('DEVICE_BEARER_A'), origin: HOST_A });
    setBaseURL(HOST_A);
    const calls = mockFetch({ status: 200, body: { sessionId: SID, action: 'kill', state: 'killed' } });

    const res = await killSession(SID);

    expect(calls).toHaveLength(1);
    expect(calls[0].url).toBe(`${HOST_A}/api/sessions/${encodeURIComponent(SID)}/kill`);
    expect(calls[0].init.method).toBe('POST');
    expect(calls[0].init.headers.Authorization).toBe('Bearer DEVICE_BEARER_A');
    expect(res.state).toBe('killed');
  });

  it('deleteSessionHistory uses the canonical PATH DELETE, never the query form', async () => {
    setDeviceAuth({ tokenManager: fakeMgr('DEVICE_BEARER_A'), origin: HOST_A });
    setBaseURL(HOST_A);
    const calls = mockFetch({ status: 200, body: { sessionId: SID, action: 'delete', state: 'exited' } });

    await deleteSessionHistory(SID);

    expect(calls).toHaveLength(1);
    expect(calls[0].url).toBe(`${HOST_A}/api/sessions/${encodeURIComponent(SID)}`);
    expect(calls[0].url).not.toContain('?id=');
    expect(calls[0].init.method).toBe('DELETE');
    expect(calls[0].init.headers.Authorization).toBe('Bearer DEVICE_BEARER_A');
  });

  it('fails closed on a non-paired origin — ZERO network requests, bearer never sent', async () => {
    setDeviceAuth({ tokenManager: fakeMgr('DEVICE_BEARER_A'), origin: HOST_A });
    setBaseURL(HOST_B);
    const stopCalls = mockFetch({ status: 200, body: {} });
    await expect(stopSession(SID)).rejects.toMatchObject({ failure: ConnectivityFailure.AuthError });
    expect(stopCalls).toHaveLength(0);

    const killCalls = mockFetch({ status: 200, body: {} });
    await expect(killSession(SID)).rejects.toMatchObject({ failure: ConnectivityFailure.AuthError });
    expect(killCalls).toHaveLength(0);

    const delCalls = mockFetch({ status: 200, body: {} });
    await expect(deleteSessionHistory(SID)).rejects.toMatchObject({ failure: ConnectivityFailure.AuthError });
    expect(delCalls).toHaveLength(0);
  });

  it('distinguishes invalid-bearer 401 from permission 403 via statusCode', async () => {
    setDeviceAuth({ tokenManager: fakeMgr(), origin: HOST_A });
    setBaseURL(HOST_A);

    mockFetch({ status: 401, body: {} });
    await expect(stopSession(SID)).rejects.toMatchObject({
      name: 'PokitError', failure: ConnectivityFailure.AuthError, statusCode: 401,
    });

    // A member without sessions:stop → 403. Non-idempotent POST is NOT retried.
    const calls403 = mockFetch({ status: 403, body: { error: 'insufficient permissions' } });
    await expect(stopSession(SID)).rejects.toMatchObject({
      name: 'PokitError', failure: ConnectivityFailure.AuthError, statusCode: 403,
    });
    expect(calls403).toHaveLength(1);
  });

  it('surfaces a 409 delete-on-non-terminal with statusCode 409 (UI can say "Stop first")', async () => {
    setDeviceAuth({ tokenManager: fakeMgr(), origin: HOST_A });
    setBaseURL(HOST_A);
    const calls = mockFetch({ status: 409, body: { error: 'must be stopped first' } });

    await expect(deleteSessionHistory(SID)).rejects.toMatchObject({
      name: 'PokitError', failure: ConnectivityFailure.APIError, statusCode: 409,
    });
    expect(calls).toHaveLength(1); // no retry loop
  });

  it('surfaces a 422 unmanaged action with statusCode 422', async () => {
    setDeviceAuth({ tokenManager: fakeMgr(), origin: HOST_A });
    setBaseURL(HOST_A);
    mockFetch({ status: 422, body: { error: 'does not support managed lifecycle' } });
    await expect(stopSession(SID)).rejects.toMatchObject({
      failure: ConnectivityFailure.APIError, statusCode: 422,
    });
  });

  it('classifies a transport failure as NetworkUnreachable (no action replayed)', async () => {
    setDeviceAuth({ tokenManager: fakeMgr(), origin: HOST_A });
    setBaseURL(HOST_A);
    let n = 0;
    (global as any).fetch = jest.fn(async () => { n++; throw new Error('connection refused'); });

    await expect(stopSession(SID)).rejects.toMatchObject({
      name: 'PokitError', failure: ConnectivityFailure.NetworkUnreachable,
    });
    expect(n).toBe(1); // exactly one attempt, no blind retry
  });

  it('never leaks the bearer in a thrown error message', async () => {
    setDeviceAuth({ tokenManager: fakeMgr('SECRET_BEARER_XYZ'), origin: HOST_A });
    setBaseURL(HOST_B); // force the fail-closed rejection path
    mockFetch({ status: 200, body: {} });
    try {
      await stopSession(SID);
      throw new Error('should have rejected');
    } catch (e: any) {
      expect(e).toBeInstanceOf(PokitError);
      expect(String(e.message)).not.toContain('SECRET_BEARER_XYZ');
    }
  });

  it('rejects a malformed / wrong-session / wrong-action 2xx (BLOCKER 5: no false success)', async () => {
    setDeviceAuth({ tokenManager: fakeMgr(), origin: HOST_A });
    setBaseURL(HOST_A);

    // wrong sessionId
    mockFetch({ status: 200, body: { sessionId: 'controlled_pty:OTHER', action: 'stop', state: 'exited' } });
    await expect(stopSession(SID)).rejects.toBeInstanceOf(LifecycleResultError);

    // wrong action
    mockFetch({ status: 200, body: { sessionId: SID, action: 'kill', state: 'killed' } });
    await expect(stopSession(SID)).rejects.toBeInstanceOf(LifecycleResultError);

    // out-of-vocabulary state
    mockFetch({ status: 200, body: { sessionId: SID, action: 'delete', state: 'idle' } });
    await expect(deleteSessionHistory(SID)).rejects.toBeInstanceOf(LifecycleResultError);

    // malformed body
    mockFetch({ status: 200, body: null });
    await expect(deleteSessionHistory(SID)).rejects.toBeInstanceOf(LifecycleResultError);
  });
});

describe('M3b lifecycle actions — legacy (explicit_local_dev) transport', () => {
  afterEach(() => setDeviceAuth(null));

  it('uses the path route + legacy bearer when no device auth is configured', async () => {
    setDeviceAuth(null);
    setBaseURL('http://127.0.0.1:9171');
    const calls = mockFetch({ status: 200, body: { sessionId: SID, action: 'stop', state: 'exited' } });

    await stopSession(SID, 'dev-token');

    expect(calls).toHaveLength(1);
    expect(calls[0].url).toBe(`http://127.0.0.1:9171/api/sessions/${encodeURIComponent(SID)}/stop`);
    expect(calls[0].init.method).toBe('POST');
    expect(calls[0].init.headers.Authorization).toBe('Bearer dev-token');
  });
});
