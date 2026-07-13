// A1-E — the mobile approval action must ride the SAME host-bound paired-device
// transport as the accepted lifecycle writes (never the legacy checkedFetch +
// Supabase token), hit the exact production route, decode the result strictly, and
// fail closed. Plus an end-to-end production-path proof from an accepted-Codex
// approval DTO through the strict decoder to a delivered action.

import {
  setBaseURL, setDeviceAuth,
  resolveApproval, ConnectivityFailure, PokitError,
} from '../src/lib/client';
import { pendingApprovals } from '../src/lib/approvalRequest';
import type { TokenManager } from '../src/lib/authClient';

const HOST_A = 'https://host-a.example.com';
const HOST_B = 'https://host-b.example.com';
const SID = 'codex:shell-1';
const AID = 'AP-123';

function fakeMgr(token = 'DEVICE_BEARER_A'): TokenManager {
  return {
    getValidToken: async () => token,
    refreshAfter401: async () => token + '-refreshed',
  } as unknown as TokenManager;
}

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

const okBody = (action: string) => ({ status: 'ok', outcome: 'ok', action });

describe('resolveApproval — host-bound device transport', () => {
  afterEach(() => setDeviceAuth(null));

  it('POSTs the exact route with the device bearer only, never the legacy token', async () => {
    setDeviceAuth({ tokenManager: fakeMgr('DEVICE_BEARER_A'), origin: HOST_A });
    setBaseURL(HOST_A);
    const calls = mockFetch({ status: 200, body: okBody('reject') });

    const res = await resolveApproval(SID, AID, 'reject', undefined, 'SUPABASE_JWT');

    expect(calls).toHaveLength(1);
    expect(calls[0].url).toBe(`${HOST_A}/api/sessions/${encodeURIComponent(SID)}/approvals/${encodeURIComponent(AID)}`);
    expect(calls[0].init.method).toBe('POST');
    expect(calls[0].init.headers.Authorization).toBe('Bearer DEVICE_BEARER_A');
    expect(JSON.stringify(calls[0].init.headers)).not.toContain('SUPABASE_JWT');
    expect(JSON.parse(calls[0].init.body)).toEqual({ action: 'reject' });
    expect(res).toEqual({ outcome: 'ok', action: 'reject' });
  });

  it('sends input in the body when provided', async () => {
    setDeviceAuth({ tokenManager: fakeMgr(), origin: HOST_A });
    setBaseURL(HOST_A);
    const calls = mockFetch({ status: 200, body: okBody('send') });
    await resolveApproval(SID, AID, 'send', 'ls -la');
    expect(JSON.parse(calls[0].init.body)).toEqual({ action: 'send', input: 'ls -la' });
  });

  it('fails closed on a non-paired origin — ZERO requests, bearer never sent', async () => {
    setDeviceAuth({ tokenManager: fakeMgr('DEVICE_BEARER_A'), origin: HOST_A });
    setBaseURL(HOST_B);
    const calls = mockFetch({ status: 200, body: okBody('reject') });
    await expect(resolveApproval(SID, AID, 'reject')).rejects.toMatchObject({ failure: ConnectivityFailure.AuthError });
    expect(calls).toHaveLength(0);
  });

  it('surfaces the daemon outcome status without replaying the action', async () => {
    setDeviceAuth({ tokenManager: fakeMgr(), origin: HOST_A });
    setBaseURL(HOST_A);
    // 502 delivery_failed
    let calls = mockFetch({ status: 502, body: 'delivery_failed' });
    await expect(resolveApproval(SID, AID, 'approve')).rejects.toMatchObject({ statusCode: 502 });
    expect(calls).toHaveLength(1);
    // 409 already_terminal / stale_generation
    mockFetch({ status: 409, body: 'already_terminal' });
    await expect(resolveApproval(SID, AID, 'reject')).rejects.toMatchObject({ statusCode: 409 });
    // 410 expired
    mockFetch({ status: 410, body: 'expired' });
    await expect(resolveApproval(SID, AID, 'reject')).rejects.toMatchObject({ statusCode: 410 });
    // 400 input_rejected
    mockFetch({ status: 400, body: 'input_rejected' });
    await expect(resolveApproval(SID, AID, 'send', 'x')).rejects.toMatchObject({ statusCode: 400 });
  });

  it('distinguishes invalid-bearer 401 from permission 403', async () => {
    setDeviceAuth({ tokenManager: fakeMgr(), origin: HOST_A });
    setBaseURL(HOST_A);
    mockFetch({ status: 401, body: {} });
    await expect(resolveApproval(SID, AID, 'reject')).rejects.toMatchObject({
      failure: ConnectivityFailure.AuthError, statusCode: 401,
    });
    const calls403 = mockFetch({ status: 403, body: {} });
    await expect(resolveApproval(SID, AID, 'reject')).rejects.toMatchObject({
      failure: ConnectivityFailure.AuthError, statusCode: 403,
    });
    expect(calls403).toHaveLength(1); // no retry of the non-idempotent POST
  });

  it('rejects a malformed 2xx body (no false success)', async () => {
    setDeviceAuth({ tokenManager: fakeMgr(), origin: HOST_A });
    setBaseURL(HOST_A);
    // outcome not ok
    mockFetch({ status: 200, body: { status: 'ok', outcome: 'delivery_failed', action: 'reject' } });
    await expect(resolveApproval(SID, AID, 'reject')).rejects.toBeInstanceOf(PokitError);
    // wrong action echoed back
    mockFetch({ status: 200, body: okBody('approve') });
    await expect(resolveApproval(SID, AID, 'reject')).rejects.toBeInstanceOf(PokitError);
    // null body
    mockFetch({ status: 200, body: null });
    await expect(resolveApproval(SID, AID, 'reject')).rejects.toBeInstanceOf(PokitError);
  });

  it('classifies a transport failure as NetworkUnreachable, one attempt', async () => {
    setDeviceAuth({ tokenManager: fakeMgr(), origin: HOST_A });
    setBaseURL(HOST_A);
    let n = 0;
    (global as any).fetch = jest.fn(async () => { n++; throw new Error('connection refused'); });
    await expect(resolveApproval(SID, AID, 'reject')).rejects.toMatchObject({
      failure: ConnectivityFailure.NetworkUnreachable,
    });
    expect(n).toBe(1);
  });

  it('never leaks the bearer in a thrown error message', async () => {
    setDeviceAuth({ tokenManager: fakeMgr('SECRET_BEARER_XYZ'), origin: HOST_A });
    setBaseURL(HOST_B); // force fail-closed
    mockFetch({ status: 200, body: okBody('reject') });
    try {
      await resolveApproval(SID, AID, 'reject');
      throw new Error('should have rejected');
    } catch (e: any) {
      expect(e).toBeInstanceOf(PokitError);
      expect(String(e.message)).not.toContain('SECRET_BEARER_XYZ');
    }
  });
});

describe('resolveApproval — legacy (explicit_local_dev) transport', () => {
  afterEach(() => setDeviceAuth(null));
  it('uses the legacy bearer when no device auth is configured', async () => {
    setDeviceAuth(null);
    setBaseURL('http://127.0.0.1:9171');
    const calls = mockFetch({ status: 200, body: okBody('reject') });
    await resolveApproval(SID, AID, 'reject', undefined, 'dev-token');
    expect(calls).toHaveLength(1);
    expect(calls[0].init.headers.Authorization).toBe('Bearer dev-token');
  });
});

describe('A1-E end-to-end production path', () => {
  afterEach(() => setDeviceAuth(null));

  it('accepted Codex approval DTO → strict decode → delivered action', async () => {
    // The daemon projects this from the generation-bound store (accepted Codex).
    const serverApprovals = [{
      id: AID, sessionId: SID, agentKind: 'codex', kind: 'approval',
      status: 'pending', prompt: 'run rm -rf?', default: 'reject',
      source: 'jsonl', confidence: 0.9, createdAt: '2026-07-14T08:00:00Z',
      options: [
        { id: 'approve', label: 'Approve', kind: 'approve' },
        { id: 'reject', label: 'Reject', kind: 'reject' },
      ],
    }];
    // Strict decode + actionable filter yields exactly the pending request.
    const decoded = pendingApprovals(serverApprovals, SID);
    expect(decoded).toHaveLength(1);
    expect(decoded[0].id).toBe(AID);

    // Deliver a reject over the host-bound device transport.
    setDeviceAuth({ tokenManager: fakeMgr('DEVICE_BEARER_A'), origin: HOST_A });
    setBaseURL(HOST_A);
    const calls = mockFetch({ status: 200, body: okBody('reject') });
    const res = await resolveApproval(SID, decoded[0].id, 'reject');
    expect(res.outcome).toBe('ok');
    expect(calls[0].init.headers.Authorization).toBe('Bearer DEVICE_BEARER_A');
  });

  it('status-only: waiting_approval activity with no approvals drives NO CTA', async () => {
    // A session with a waiting_approval activity but an empty/absent approvals array
    // must surface zero actionable approvals — activity is display-only.
    expect(pendingApprovals(undefined, SID)).toEqual([]);
    expect(pendingApprovals([], SID)).toEqual([]);
  });

  it('no-capability provider: a session that never produced an approval has no CTA', async () => {
    // Claude declares no approval capability, so its approvals array is always empty.
    expect(pendingApprovals([], 'claude:s1')).toEqual([]);
  });
});
