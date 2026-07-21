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
    expect(html).toContain('ticketConsumed=true');
    expect(html).toContain('window._nextTicket=null');
  });

  // ── Script execution tests ──
  // The generated production script is executed end-to-end in a controlled fake
  // browser in terminalGeneratedScript.test.ts (Blocker E). That suite proves
  // the real wsURL output, ticket A single-use, reconnect A→B→C, framing
  // demux, and timer cleanup — replacing the earlier copy-of-wsURL assertion.

  // ── TERM-G1 geometry validation tests ──

  function expectPTYSizeInjected(html: string, rows: number, cols: number) {
    // The assignment `window.__pokitPTYSize={rows:N,cols:M};` must appear
    // before the ticket script.
    expect(html).toContain(`window.__pokitPTYSize={rows:${rows},cols:${cols}};`);
  }

  function expectPTYSizeAbsent(html: string) {
    // The assignment must not be present. The consumer script
    // `if(window.__pokitPTYSize){...}` is always injected and is harmless
    // when the global is undefined.
    expect(html).not.toContain('window.__pokitPTYSize={rows:');
  }

  async function bootstrapWithSize(size: { rows: number; cols: number } | null): Promise<string> {
    mockFetch.mockImplementationOnce(async () => ({ ok: true, text: async () => TERM_HTML, status: 200 } as any));
    if (size) {
      mockFetch.mockImplementationOnce(async () => ({ ok: true, json: async () => size } as any));
    } else {
      mockFetch.mockImplementationOnce(async () => ({ ok: false, status: 404 } as any));
    }
    mockTicketResp();
    const ctrl = new TerminalController();
    const { result } = await ctrl.bootstrap('s', fakeMgr, 'http://d');
    return result!.html;
  }

  it('TERM-G1: valid PTY size is injected as __pokitPTYSize', async () => {
    const html = await bootstrapWithSize({ rows: 30, cols: 100 });
    expectPTYSizeInjected(html, 30, 100);
  });

  it('TERM-C1: bootstrap geometry is applied only through the control bridge', async () => {
    const html = await bootstrapWithSize({ rows: 30, cols: 100 });
    expect(html).toContain('window.__pokitControlBridge.bootstrapGeometry(__pokitPTYSize.rows,__pokitPTYSize.cols)');
    expect(html).not.toContain('term.resize(__pokitPTYSize.cols,__pokitPTYSize.rows)');
  });

  it('TERM-G1: rejects non-integer rows', async () => {
    const html = await bootstrapWithSize({ rows: 30.5, cols: 100 });
    expectPTYSizeAbsent(html);
  });

  it('TERM-G1: rejects non-integer cols', async () => {
    const html = await bootstrapWithSize({ rows: 30, cols: 100.1 });
    expectPTYSizeAbsent(html);
  });

  it('TERM-G1: rejects zero rows', async () => {
    const html = await bootstrapWithSize({ rows: 0, cols: 100 });
    expectPTYSizeAbsent(html);
  });

  it('TERM-G1: rejects zero cols', async () => {
    const html = await bootstrapWithSize({ rows: 30, cols: 0 });
    expectPTYSizeAbsent(html);
  });

  it('TERM-G1: rejects negative rows', async () => {
    const html = await bootstrapWithSize({ rows: -1, cols: 100 });
    expectPTYSizeAbsent(html);
  });

  it('TERM-G1: rejects negative cols', async () => {
    const html = await bootstrapWithSize({ rows: 30, cols: -1 });
    expectPTYSizeAbsent(html);
  });

  it('TERM-G1: rejects implausibly large rows (>1000)', async () => {
    const html = await bootstrapWithSize({ rows: 1001, cols: 100 });
    expectPTYSizeAbsent(html);
  });

  it('TERM-G1: rejects implausibly large cols (>2000)', async () => {
    const html = await bootstrapWithSize({ rows: 30, cols: 2001 });
    expectPTYSizeAbsent(html);
  });

  it('TERM-G1: accepts boundary-max rows=1000 cols=2000', async () => {
    const html = await bootstrapWithSize({ rows: 1000, cols: 2000 });
    expectPTYSizeInjected(html, 1000, 2000);
  });

  it('TERM-G1: accepts boundary-min rows=1 cols=1', async () => {
    const html = await bootstrapWithSize({ rows: 1, cols: 1 });
    expectPTYSizeInjected(html, 1, 1);
  });

  it('TERM-G1: absent /term/size response leaves __pokitPTYSize unset', async () => {
    const html = await bootstrapWithSize(null);
    expectPTYSizeAbsent(html);
  });

  it('TERM-G1: string rows/cols are coerced to numbers (accepted if valid)', async () => {
    // Number("30") → 30, which is a valid integer — strings are coerced.
    const html = await bootstrapWithSize({ rows: '30' as any, cols: '100' as any });
    expectPTYSizeInjected(html, 30, 100);
  });

  it('TERM-G1: NaN rows/cols are rejected', async () => {
    const html = await bootstrapWithSize({ rows: NaN, cols: NaN });
    expectPTYSizeAbsent(html);
  });

  it('TERM-G1: non-numeric strings are rejected', async () => {
    const html = await bootstrapWithSize({ rows: 'abc' as any, cols: 'xyz' as any });
    expectPTYSizeAbsent(html);
  });
});
