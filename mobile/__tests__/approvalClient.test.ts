// A1 remediation (B7) — the mobile approval action rides ONLY the host-bound
// paired-device authenticated transport. Device auth is MANDATORY: no legacy bearer
// / checkedFetch fallback, missing credentials fail closed, an idempotency key is
// sent, and only an accepted/already_accepted receipt is a success.

import {
  setBaseURL, setDeviceAuth,
  resolveApproval, ConnectivityFailure, PokitError,
} from '../src/lib/client';
import { actionableApprovals } from '../src/lib/approvalRequest';
import type { TokenManager } from '../src/lib/authClient';

const HOST_A = 'https://host-a.example.com';
const HOST_B = 'https://host-b.example.com';
const SID = 'codex:shell-1';
const AID = 'AP-123';
const KEY = 'AP-123:reject:1';

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

const okBody = (action: string, outcome = 'accepted') => ({ status: 'ok', outcome, action });

describe('resolveApproval — host-bound device transport ONLY (B7)', () => {
  afterEach(() => setDeviceAuth(null));

  it('POSTs with the device bearer + idempotency key and decodes accepted', async () => {
    setDeviceAuth({ tokenManager: fakeMgr('DEVICE_BEARER_A'), origin: HOST_A });
    setBaseURL(HOST_A);
    const calls = mockFetch({ status: 200, body: okBody('reject', 'accepted') });

    const res = await resolveApproval(SID, AID, 'reject', undefined, KEY);

    expect(calls).toHaveLength(1);
    expect(calls[0].url).toBe(`${HOST_A}/api/sessions/${encodeURIComponent(SID)}/approvals/${encodeURIComponent(AID)}`);
    expect(calls[0].init.headers.Authorization).toBe('Bearer DEVICE_BEARER_A');
    expect(JSON.parse(calls[0].init.body)).toEqual({ action: 'reject', idempotencyKey: KEY });
    expect(res).toEqual({ outcome: 'accepted', action: 'reject' });
  });

  it('accepts an already_accepted idempotent receipt', async () => {
    setDeviceAuth({ tokenManager: fakeMgr(), origin: HOST_A });
    setBaseURL(HOST_A);
    mockFetch({ status: 200, body: okBody('reject', 'already_accepted') });
    const res = await resolveApproval(SID, AID, 'reject', undefined, KEY);
    expect(res.outcome).toBe('already_accepted');
  });

  it('FAILS CLOSED with NO request when device auth is absent (no legacy fallback)', async () => {
    setDeviceAuth(null);
    setBaseURL(HOST_A);
    const calls = mockFetch({ status: 200, body: okBody('reject') });
    await expect(resolveApproval(SID, AID, 'reject', undefined, KEY)).rejects.toMatchObject({
      failure: ConnectivityFailure.AuthError,
    });
    expect(calls).toHaveLength(0); // no legacy transport attempt whatsoever
  });

  it('fails closed on a non-paired origin — ZERO requests', async () => {
    setDeviceAuth({ tokenManager: fakeMgr('DEVICE_BEARER_A'), origin: HOST_A });
    setBaseURL(HOST_B);
    const calls = mockFetch({ status: 200, body: okBody('reject') });
    await expect(resolveApproval(SID, AID, 'reject', undefined, KEY)).rejects.toMatchObject({ failure: ConnectivityFailure.AuthError });
    expect(calls).toHaveLength(0);
  });

  it('surfaces the daemon receipt outcome without replaying the action', async () => {
    setDeviceAuth({ tokenManager: fakeMgr(), origin: HOST_A });
    setBaseURL(HOST_A);
    for (const [status, count] of [[502, 1], [409, 1], [410, 1], [400, 1]] as const) {
      const calls = mockFetch({ status, body: 'x' });
      await expect(resolveApproval(SID, AID, 'approve', undefined, KEY)).rejects.toMatchObject({ statusCode: status });
      expect(calls).toHaveLength(count);
    }
  });

  it('distinguishes 401 from 403', async () => {
    setDeviceAuth({ tokenManager: fakeMgr(), origin: HOST_A });
    setBaseURL(HOST_A);
    mockFetch({ status: 401, body: {} });
    await expect(resolveApproval(SID, AID, 'reject', undefined, KEY)).rejects.toMatchObject({ statusCode: 401 });
    mockFetch({ status: 403, body: {} });
    await expect(resolveApproval(SID, AID, 'reject', undefined, KEY)).rejects.toMatchObject({ statusCode: 403 });
  });

  it('rejects a malformed / non-success 2xx body (no false success)', async () => {
    setDeviceAuth({ tokenManager: fakeMgr(), origin: HOST_A });
    setBaseURL(HOST_A);
    // outcome not accepted/already_accepted
    mockFetch({ status: 200, body: { status: 'ok', outcome: 'unavailable', action: 'reject' } });
    await expect(resolveApproval(SID, AID, 'reject', undefined, KEY)).rejects.toBeInstanceOf(PokitError);
    // wrong action echoed
    mockFetch({ status: 200, body: okBody('approve') });
    await expect(resolveApproval(SID, AID, 'reject', undefined, KEY)).rejects.toBeInstanceOf(PokitError);
    // null body
    mockFetch({ status: 200, body: null });
    await expect(resolveApproval(SID, AID, 'reject', undefined, KEY)).rejects.toBeInstanceOf(PokitError);
  });

  it('classifies a transport failure as NetworkUnreachable, one attempt', async () => {
    setDeviceAuth({ tokenManager: fakeMgr(), origin: HOST_A });
    setBaseURL(HOST_A);
    let n = 0;
    (global as any).fetch = jest.fn(async () => { n++; throw new Error('refused'); });
    await expect(resolveApproval(SID, AID, 'reject', undefined, KEY)).rejects.toMatchObject({ failure: ConnectivityFailure.NetworkUnreachable });
    expect(n).toBe(1);
  });

  it('never leaks the bearer in a thrown error', async () => {
    setDeviceAuth({ tokenManager: fakeMgr('SECRET_XYZ'), origin: HOST_A });
    setBaseURL(HOST_B);
    mockFetch({ status: 200, body: okBody('reject') });
    try {
      await resolveApproval(SID, AID, 'reject', undefined, KEY);
      throw new Error('should have rejected');
    } catch (e: any) {
      expect(String(e.message)).not.toContain('SECRET_XYZ');
    }
  });
});

describe('A1 production path (mobile side)', () => {
  afterEach(() => setDeviceAuth(null));

  it('non-actionable safe DTO → no CTA, and the daemon has no delivery channel', async () => {
    // The daemon projects non-actionable intervention info for Codex (no proven
    // delivery channel), so the mobile surfaces zero actionable approvals.
    const serverApprovals = [{
      id: AID, sessionId: SID, summary: 'Agent requested an approval',
      state: 'pending', actionable: false, options: [],
      createdAt: '2026-07-14T08:00:00Z', expiresAt: '2026-07-14T08:05:00Z',
    }];
    expect(actionableApprovals(serverApprovals, SID)).toHaveLength(0);
  });

  it('status-only: no approvals array → no CTA', () => {
    expect(actionableApprovals(undefined, SID)).toEqual([]);
    expect(actionableApprovals([], SID)).toEqual([]);
  });
});
