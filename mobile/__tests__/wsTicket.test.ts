const mockFetch = jest.fn();
(global as any).fetch = mockFetch;

import { getWSTicket, wsTicketURL } from '../src/lib/wsTicket';
import { TokenManager } from '../src/lib/authClient';

// A minimal TokenManager that always returns a fixed bearer.
const fixedToken = '0'.repeat(64);
const fakeTokenMgr = { getValidToken: async () => fixedToken } as TokenManager;

const VALID_TICKET = 'a1b2c3d4e5f6';
const FUTURE = new Date(Date.now() + 20_000).toISOString(); // within 30s TTL

describe('getWSTicket', () => {
  beforeEach(() => mockFetch.mockReset());

  it('happy path: returns ticket + expiresAt', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ ticket: VALID_TICKET, expiresAt: FUTURE }),
    } as any);
    const t = await getWSTicket('controlled_pty:shell-1', fakeTokenMgr, 'http://daemon');
    expect(t.ticket).toBe(VALID_TICKET);
    expect(t.expiresAt.toISOString()).toBe(FUTURE);
  });

  it('includes the session ID in the URL query', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ ticket: VALID_TICKET, expiresAt: FUTURE }),
    } as any);
    await getWSTicket('controlled_pty:shell-1', fakeTokenMgr, 'http://daemon');
    const [url] = mockFetch.mock.calls[0];
    expect(url).toContain('session=controlled_pty%3Ashell-1');
  });

  it('sends Authorization: Bearer header', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ ticket: VALID_TICKET, expiresAt: FUTURE }),
    } as any);
    await getWSTicket('s', fakeTokenMgr, 'http://daemon');
    const [, init] = mockFetch.mock.calls[0];
    expect(init.headers['Authorization']).toBe('Bearer ' + fixedToken);
  });

  it('rejects a non-hex ticket', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ ticket: '!!!not-hex!!!', expiresAt: FUTURE }),
    } as any);
    await expect(getWSTicket('s', fakeTokenMgr, 'http://daemon')).rejects.toMatchObject({ code: 'network_error' });
  });

  it('rejects an empty ticket', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ ticket: '', expiresAt: FUTURE }),
    } as any);
    await expect(getWSTicket('s', fakeTokenMgr, 'http://daemon')).rejects.toMatchObject({ code: 'network_error' });
  });

  it('rejects an expired ticket', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ ticket: VALID_TICKET, expiresAt: '2020-01-01T00:00:00Z' }),
    } as any);
    await expect(getWSTicket('s', fakeTokenMgr, 'http://daemon')).rejects.toMatchObject({ code: 'network_error' });
  });

  it('rejects ticket with excessive TTL (40s > 30s max)', async () => {
    const farFuture = new Date(Date.now() + 40_000).toISOString();
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ ticket: VALID_TICKET, expiresAt: farFuture }),
    } as any);
    await expect(getWSTicket('s', fakeTokenMgr, 'http://daemon')).rejects.toMatchObject({ code: 'network_error' });
  });

  it('rejects response with unknown field', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ ticket: VALID_TICKET, expiresAt: FUTURE, secret: 'x' }),
    } as any);
    await expect(getWSTicket('s', fakeTokenMgr, 'http://daemon')).rejects.toMatchObject({ code: 'network_error' });
  });

  it('rejects 401 → host_pin_fail', async () => {
    mockFetch.mockResolvedValueOnce({ ok: false, status: 401 } as any);
    await expect(getWSTicket('s', fakeTokenMgr, 'http://daemon')).rejects.toMatchObject({ code: 'host_pin_fail' });
  });

  it('rejects non-JSON response', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => { throw new Error('parse error'); },
    } as any);
    await expect(getWSTicket('s', fakeTokenMgr, 'http://daemon')).rejects.toMatchObject({ code: 'network_error' });
  });
});

describe('wsTicketURL', () => {
  it('produces a safe ws:// URL from an http base', () => {
    const url = wsTicketURL('abc123', 'controlled_pty:x', 'http://192.168.1.10:9171');
    expect(url).toBe('ws://192.168.1.10:9171/term/ws?session=controlled_pty%3Ax&ticket=abc123');
  });

  it('upgrades https → wss', () => {
    const url = wsTicketURL('abc123', 's', 'https://daemon.example.com');
    expect(url.startsWith('wss://')).toBe(true);
  });
});
