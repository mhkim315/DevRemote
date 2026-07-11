// TerminalController — singleflight, stale cancellation with real deferreds.

const mockFetch = jest.fn();
(global as any).fetch = mockFetch;

import { TerminalController } from '../src/lib/terminalController';
import { TokenManager } from '../src/lib/authClient';

const fakeMgr = { getValidToken: async () => '0'.repeat(64) } as TokenManager;
const TERM_HTML = '<html><head></head><body><pre>terminal</pre></body></html>';
const TICKET = '0'.repeat(64);

// A controllable Deferred lets us resolve/reject on demand.
class Deferred<T> {
  resolve!: (v: T) => void; reject!: (e: any) => void;
  promise: Promise<T>;
  constructor() { this.promise = new Promise((res, rej) => { this.resolve = res; this.reject = rej; }); }
}

function mockTicketResp() {
  mockFetch.mockImplementationOnce(async () => ({ ok: true, json: async () => ({ ticket: TICKET, expiresAt: new Date(Date.now() + 20_000).toISOString() }) } as any));
}

describe('TerminalController', () => {
  beforeEach(() => mockFetch.mockReset());

  function mockPTYSize() {
    mockFetch.mockImplementationOnce(async () => ({ ok: true, json: async () => ({ rows: 24, cols: 80 }) } as any));
  }

  it('bootstrap returns {result} with injected ticket', async () => {
    mockFetch.mockImplementationOnce(async () => ({ ok: true, text: async () => TERM_HTML, status: 200 } as any));
    mockPTYSize();
    mockTicketResp();
    const ctrl = new TerminalController();
    const { result } = await ctrl.bootstrap('s', fakeMgr, 'http://d');
    expect(result).toBeDefined();
    expect(result!.html).toContain('pokit-ticket');
    expect(result!.ticket).toBe(TICKET);
  });

  it('cancel before resolve → bootstrap returns no result', async () => {
    const d = new Deferred<any>();
    mockFetch.mockImplementationOnce(() => d.promise);
    const ctrl = new TerminalController();
    const p = ctrl.bootstrap('s', fakeMgr, 'http://d');
    ctrl.cancel();
    d.resolve({ ok: true, text: async () => TERM_HTML, status: 200 } as any);
    const { result } = await p;
    expect(result).toBeUndefined();
  });

  it('reconnectTicket returns {ticket}', async () => {
    mockTicketResp();
    const ctrl = new TerminalController();
    const { ticket } = await ctrl.reconnectTicket('s', fakeMgr, 'http://d');
    expect(ticket).toBe(TICKET);
  });

  it('reconnect singleflight: two callers share one request', async () => {
    mockTicketResp(); // exactly ONE ticket request
    const ctrl = new TerminalController();
    const [a, b] = await Promise.all([ctrl.reconnectTicket('s', fakeMgr, 'http://d'), ctrl.reconnectTicket('s', fakeMgr, 'http://d')]);
    expect(a.ticket).toBe(TICKET);
    expect(b.ticket).toBe(TICKET);
    expect(a.attemptId).toBe(b.attemptId);
    expect(mockFetch).toHaveBeenCalledTimes(1);
  });

  it('reconnect after completion starts a new generation', async () => {
    mockTicketResp();
    const ctrl = new TerminalController();
    const a = await ctrl.reconnectTicket('s', fakeMgr, 'http://d');
    expect(a.ticket).toBe(TICKET);
    mockTicketResp();
    const b = await ctrl.reconnectTicket('s', fakeMgr, 'http://d');
    expect(b.ticket).toBe(TICKET);
    expect(b.attemptId).toBeGreaterThan(a.attemptId);
    expect(mockFetch).toHaveBeenCalledTimes(2);
  });

  // ── Injected script tests ──
  it('injected script intercepts WebSocket with ticket A on first call', async () => {
    mockFetch.mockImplementationOnce(async () => ({ ok: true, text: async () => TERM_HTML, status: 200 } as any));
    mockPTYSize();
    mockTicketResp();
    const ctrl = new TerminalController();
    const { result } = await ctrl.bootstrap('s', fakeMgr, 'http://d');
    expect(result).toBeDefined();
    const html = result!.html;
    // Verify: immediate WebSocket constructor interception (Phase 1)
    expect(html).toContain('window.WebSocket=function(url,protocols)');
    expect(html).toContain('ticketConsumed');
    // Verify: the first WS call gets ticket A via wsURL(ticketA)
    expect(html).toContain('wsURL(ticketA)');
    // Verify: reconnect wrapper deferred (Phase 2, DOMContentLoaded)
    expect(html).toContain('installReconnect');
    expect(html).toContain('DOMContentLoaded');
    // Verify: ticket variable is serialized with JSON.stringify
    expect(html).toContain(JSON.stringify(TICKET));
  });

  it('injected script clears ticket A after first use', async () => {
    mockFetch.mockImplementationOnce(async () => ({ ok: true, text: async () => TERM_HTML, status: 200 } as any));
    mockPTYSize();
    mockTicketResp();
    const ctrl = new TerminalController();
    const { result } = await ctrl.bootstrap('s', fakeMgr, 'http://d');
    const html = result!.html;
    // ticketConsumed is set to true after first WS call; _nextTicket is
    // cleared (set to null) after use.
    expect(html).toContain('ticketConsumed=true');
    expect(html).toContain('window._nextTicket=null');
  });
});
