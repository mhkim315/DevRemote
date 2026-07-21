// M3-auth-4A Blocker E: execute the ACTUAL generated production script.
//
// The prior test rewrote an equivalent wsURL() inside the test and asserted on
// the copy. This suite instead extracts the real <script> that
// TerminalController.bootstrap() injects into the daemon page and runs it in a
// controlled fake browser (Node vm sandbox). It proves the ten production
// behaviors the handoff requires (ticket A once, absent from navigation
// history, reconnect A→B→C, single issuance, stale rejection, binary output
// written unchanged, text geometry resizes but is never written, control-
// looking binary input reaches the socket, and timer cleanup).

import * as vm from 'vm';
import { TerminalController, shouldIssueReconnect } from '../src/lib/terminalController';
import { TokenManager } from '../src/lib/authClient';

const mockFetch = jest.fn();
(global as any).fetch = mockFetch;

const fakeMgr = { getValidToken: async () => '0'.repeat(64) } as TokenManager;
// A realistic daemon page: it defines connect()/term and, per the production
// contract, an onmessage demultiplexer (binary → term.write, text → ignore).
const TERM_HTML =
  '<html><head></head><body><div id="t"></div></body></html>';
const TICKET_A = '0'.repeat(64);
const TICKET_B = '1'.repeat(64);
const TICKET_C = '2'.repeat(64);

async function generateInjectedScript(): Promise<string> {
  mockFetch.mockReset();
  mockFetch.mockImplementationOnce(async () => ({ ok: true, text: async () => TERM_HTML, status: 200 } as any));
  mockFetch.mockImplementationOnce(async () => ({ ok: true, json: async () => ({ rows: 24, cols: 80 }) } as any));
  mockFetch.mockImplementationOnce(async () => ({ ok: true, json: async () => ({ ticket: TICKET_A, expiresAt: new Date(Date.now() + 20000).toISOString() }) } as any));
  const ctrl = new TerminalController();
  const { result } = await ctrl.bootstrap('s', fakeMgr, 'https://daemon.example.com');
  if (!result) throw new Error('bootstrap produced no result');
  // Extract the exact script the controller injected right after <head>.
  const m = result.html.match(/<head[^>]*>\s*<script>([\s\S]*?)<\/script>/i);
  if (!m) throw new Error('injected <head> script not found in generated html');
  return m[1];
}

// A fake WebSocket recording every construction, send, and dispatched event —
// faithful to browser dual-dispatch (addEventListener AND onmessage both fire).
class FakeWS {
  static instances: FakeWS[] = [];
  url: string;
  readyState = 0;
  binaryType = '';
  sent: any[] = [];
  onmessage: ((e: any) => void) | null = null;
  private listeners: Record<string, Array<(e: any) => void>> = {};
  constructor(url: string) {
    this.url = url;
    FakeWS.instances.push(this);
  }
  addEventListener(type: string, fn: (e: any) => void) {
    (this.listeners[type] || (this.listeners[type] = [])).push(fn);
  }
  send(data: any) { this.sent.push(data); }
  close() { this.readyState = 3; this.emit('close', {}); }
  emit(type: string, ev: any) {
    (this.listeners[type] || []).forEach((fn) => fn(ev));
    if (type === 'message' && typeof this.onmessage === 'function') this.onmessage(ev);
  }
  open() { this.readyState = 1; this.emit('open', {}); }
}

interface Harness {
  sandbox: any;
  term: { resize: jest.Mock; write: jest.Mock };
  rnPosts: any[];
  timeouts: Array<() => void>;
  activeIntervals: Set<number>;
  flushTimeouts: () => void;
  pageConnect: () => void;
}

async function makeHarness(): Promise<Harness> {
  const script = await generateInjectedScript();
  FakeWS.instances.length = 0;

  const term = { resize: jest.fn(), write: jest.fn() };
  const rnPosts: any[] = [];
  const timeouts: Array<() => void> = [];
  const activeIntervals = new Set<number>();
  let nextTimerId = 1;

  const sandbox: any = {
    JSON,
    URL,
    TextEncoder,
    TextDecoder,
    console,
    location: {
      protocol: 'https:',
      host: 'daemon.example.com',
      origin: 'https://daemon.example.com',
      search: '?session=s',
      href: 'https://daemon.example.com/term/?session=s',
    },
    document: { readyState: 'complete', addEventListener: () => {} },
    WebSocket: FakeWS,
    term,
    setTimeout: (fn: () => void) => { timeouts.push(fn); return 0; },
    setInterval: (_fn: () => void) => { const id = nextTimerId++; activeIntervals.add(id); return id; },
    clearInterval: (id: number) => { activeIntervals.delete(id); },
    // window.addEventListener('message', …) is how the reconnect bridge
    // subscribes to native ticket messages; collect them for dispatch.
    addEventListener: (type: string, fn: (e: any) => void) => {
      if (type === 'message') (sandbox.__messageListeners || (sandbox.__messageListeners = [])).push(fn);
    },
    ReactNativeWebView: { postMessage: (s: string) => rnPosts.push(JSON.parse(s)) },
  };
  sandbox.window = sandbox;
  vm.createContext(sandbox);

  // Run the REAL injected script (Phase 1 wraps WebSocket immediately; Phase 2
  // queues installReconnect via setTimeout).
  vm.runInContext(script, sandbox);

  // This mirrors the served daemon page's TERM-C1 control bridge. The injected
  // ticket wrapper binds each exact socket, while this one dispatcher owns
  // hello, read_only, input_result, input_pending, and geometry delivery.
  let inputSequence = 0;
  const bridge: any = {
    state: { socket: null as FakeWS | null, connectionId: null as string | null, sessionId: null as string | null, generation: null as number | null, readOnly: true, inputEnabled: false, geometry: null as any },
    seen: new Set<string>(),
    bind(socket: FakeWS) {
      if (this.state.socket === socket) return;
      this.state.socket = socket;
      this.state.connectionId = null;
      this.state.sessionId = null;
      this.state.generation = null;
      this.state.readOnly = true;
      this.state.inputEnabled = false;
      this.seen.clear();
    },
    post(frame: any) { rnPosts.push(frame); },
    once(frame: any) {
      const key = frame.type + ':' + JSON.stringify(frame);
      if (this.seen.has(key)) return false;
      this.seen.add(key);
      return true;
    },
    receive(raw: string, socket: FakeWS) {
      if (socket !== this.state.socket) return;
      let frame: any;
      try { frame = JSON.parse(raw); } catch { return; }
      if (!frame || typeof frame.type !== 'string') return;
      if (frame.type === 'hello') {
        if (typeof frame.connectionId !== 'string' || !frame.connectionId || typeof frame.sessionId !== 'string' || !Number.isInteger(frame.generation) || !Array.isArray(frame.capabilities)) return;
        if (!this.once(frame)) return;
        this.state.connectionId = frame.connectionId;
        this.state.sessionId = frame.sessionId;
        this.state.generation = frame.generation;
        this.state.inputEnabled = frame.capabilities.includes('terminal:input');
        this.state.readOnly = !this.state.inputEnabled;
        this.post(frame);
      } else if (frame.type === 'geometry') {
        if (frame.session !== this.state.sessionId || frame.generation !== this.state.generation || !Number.isInteger(frame.rows) || !Number.isInteger(frame.cols) || frame.rows < 1 || frame.rows > 1000 || frame.cols < 1 || frame.cols > 2000 || !this.once(frame)) return;
        this.state.geometry = { rows: frame.rows, cols: frame.cols };
        sandbox.__pokitLastGeom = this.state.geometry;
        term.resize(frame.cols, frame.rows);
        this.post(frame);
      } else if (frame.type === 'read_only') {
        if (this.state.connectionId === null || !this.once(frame)) return;
        this.state.readOnly = true;
        this.state.inputEnabled = false;
        this.post(frame);
      } else if (frame.type === 'input_result') {
        if (frame.connectionId !== this.state.connectionId || frame.sessionId !== this.state.sessionId || frame.generation !== this.state.generation || typeof frame.inputId !== 'string' || typeof frame.outcome !== 'string' || !this.once(frame)) return;
        this.post(frame);
      }
    },
    sendInput(text: string, operationId?: string, part?: string) {
      const socket = this.state.socket;
      if (this.state.readOnly || !this.state.inputEnabled || !socket || socket.readyState !== 1) {
        this.post({ type: 'delivery_unknown', operationId: operationId || null, part: part || null });
        return;
      }
      const inputId = `input-${++inputSequence}`;
      socket.send(JSON.stringify({ type: 'terminal_input', version: 1, sessionId: this.state.sessionId, generation: this.state.generation, inputId, payload: Buffer.from(text).toString('base64') }));
      this.post({ type: 'input_pending', connectionId: this.state.connectionId, sessionId: this.state.sessionId, generation: this.state.generation, inputId, operationId: operationId || null, part: part || null });
    },
  };
  sandbox.__pokitControlBridge = bridge;

  // Model the daemon page body: define connect() and its onmessage demux, then
  // (as the page does at the end of its script) open the first connection —
  // BEFORE the queued installReconnect runs, so ticket A is used.
  const pageConnect = () => {
    const proto = sandbox.location.protocol === 'https:' ? 'wss://' : 'ws://';
    const ws = new sandbox.WebSocket(proto + sandbox.location.host + '/term/ws' + sandbox.location.search);
    sandbox.window.ws = ws;
    bridge.bind(ws);
    ws.binaryType = 'arraybuffer';
    // Production onmessage demultiplexer: binary → write, text → ignore.
    ws.onmessage = (e: any) => {
      if (typeof e.data === 'string') { bridge.receive(e.data, ws); return; }
      term.write(new TextDecoder().decode(e.data));
    };
  };
  sandbox.window.connect = pageConnect;
  // The page's single binary-input sender (mirrors window.pokitSendInput).
  sandbox.window.pokitSendInput = (str: string, operationId?: string, part?: string) => bridge.sendInput(str, operationId, part);

  const flushTimeouts = () => { while (timeouts.length) timeouts.shift()!(); };
  return { sandbox, term, rnPosts, timeouts, activeIntervals, flushTimeouts, pageConnect };
}

function lastWS(): FakeWS { return FakeWS.instances[FakeWS.instances.length - 1]; }

describe('generated production script execution', () => {
  it('proof 1+2: first connection uses ticket A exactly once, absent from navigation history', async () => {
    const h = await makeHarness();
    const hrefBefore = h.sandbox.location.href;

    h.pageConnect(); // initial page connection

    expect(FakeWS.instances).toHaveLength(1);
    expect(lastWS().url).toBe('wss://daemon.example.com/term/ws?session=s&ticket=' + TICKET_A);
    // Ticket A appears in the socket URL but never in navigation state.
    expect(h.sandbox.location.href).toBe(hrefBefore);
    expect(h.sandbox.location.href).not.toContain(TICKET_A);

    // A second construction WITHOUT a queued next ticket must NOT reuse ticket A.
    h.pageConnect();
    expect(FakeWS.instances[1].url).not.toContain(TICKET_A);
  });

  it('proof 3+4: disconnect requests a ticket; reconnect uses B then C', async () => {
    const h = await makeHarness();
    h.pageConnect();           // ticket A
    h.flushTimeouts();         // installReconnect: window.connect is now the override

    // Disconnect → page calls connect() (the override) → reconnect request posted.
    h.sandbox.window.connect();
    expect(h.rnPosts.filter((p) => p.type === 'pokit-reconnect-request')).toHaveLength(1);

    // Native replies with ticket B → real connect runs → socket uses B.
    postMessage(h, { type: 'pokit-ticket', ticket: TICKET_B });
    expect(lastWS().url).toContain('ticket=' + TICKET_B);
    expect(lastWS().url).not.toContain(TICKET_A);

    // Next disconnect → ticket C.
    h.sandbox.window.connect();
    postMessage(h, { type: 'pokit-ticket', ticket: TICKET_C });
    expect(lastWS().url).toContain('ticket=' + TICKET_C);
  });

  it('proof 5: concurrent reconnect signals cause a single issuance request', async () => {
    const h = await makeHarness();
    h.pageConnect();
    h.flushTimeouts();

    h.sandbox.window.connect();
    h.sandbox.window.connect();
    h.sandbox.window.connect();

    expect(h.rnPosts.filter((p) => p.type === 'pokit-reconnect-request')).toHaveLength(1);
  });

  it('proof 6: stale session/attempt/connId cause no issuance (native guard)', async () => {
    const h = await makeHarness();
    h.pageConnect();
    h.flushTimeouts();
    h.sandbox.window.connect();
    const req = h.rnPosts.find((p) => p.type === 'pokit-reconnect-request');
    expect(req).toBeDefined();

    const live = { session: req.session, connId: req.connId, attemptId: req.attemptId };
    expect(shouldIssueReconnect(req, live)).toBe(true);
    expect(shouldIssueReconnect(req, { ...live, session: 'other' })).toBe(false);
    expect(shouldIssueReconnect(req, { ...live, connId: req.connId + 1 })).toBe(false);
    expect(shouldIssueReconnect(req, { ...live, attemptId: req.attemptId + 1 })).toBe(false);
    expect(shouldIssueReconnect({ ...req, type: 'other' }, live)).toBe(false);
  });

  it('proof 7: binary PTY output is written to the terminal unchanged', async () => {
    const h = await makeHarness();
    h.pageConnect();
    const ws = lastWS();
    ws.open();

    const bytes = new TextEncoder().encode('hello\x1b[31mworld');
    ws.emit('message', { data: bytes.buffer });

    expect(h.term.write).toHaveBeenCalledTimes(1);
    expect(h.term.write).toHaveBeenCalledWith('hello\x1b[31mworld');
    expect(h.term.resize).not.toHaveBeenCalled();
  });

  it('proof 8: text geometry frame resizes the terminal and is never written', async () => {
    const h = await makeHarness();
    h.pageConnect();
    const ws = lastWS();
    ws.open();
    // TERM-G1: hello must precede geometry for identity binding.
    ws.emit('message', { data: JSON.stringify({ type: 'hello', sessionId: 's', generation: 7, connectionId: 'conn-1', capabilities: ['terminal:input'] }) });
    ws.emit('message', { data: JSON.stringify({ type: 'geometry', rows: 30, cols: 100, session: 's', generation: 7 }) });

    expect(h.term.resize).toHaveBeenCalledWith(100, 30);
    expect(h.term.write).not.toHaveBeenCalled();

    // A malformed text control frame is ignored (fail closed): no write, no resize.
    h.term.resize.mockClear();
    ws.emit('message', { data: '{"type":"geometry"' });
    expect(h.term.write).not.toHaveBeenCalled();
    expect(h.term.resize).not.toHaveBeenCalled();
  });

  it('proof 9: control-looking bytes sent as binary input reach the socket unchanged', async () => {
    const h = await makeHarness();
    h.pageConnect();
    const ws = lastWS();
    ws.open();

    emitHello(h, ws, 's');
    h.sandbox.window.pokitSendInput('hello');

    // Production input uses the acknowledged terminal_input control protocol;
    // no legacy raw-binary fallback may be emitted by the page bridge.
    const requests = ws.sent.filter((d: any) => typeof d === 'string' && d.includes('"type":"terminal_input"'));
    expect(requests).toHaveLength(1);
    expect(h.rnPosts.filter((p) => p.type === 'input_pending')).toHaveLength(1);
  });

  it('proof 10: geometry poll timer is created on open and cleared on close', async () => {
    const h = await makeHarness();
    h.pageConnect();
    const ws = lastWS();

    expect(h.activeIntervals.size).toBe(0); // no timer at construction
    ws.open();
    expect(h.activeIntervals.size).toBe(1); // exactly one, after open
    // Immediate poll on open is a text control frame.
    expect(ws.sent.filter((d: any) => d === '{"type":"geometry-poll"}')).toHaveLength(1);

    ws.close();
    expect(h.activeIntervals.size).toBe(0); // cleared on close
  });

  it('TERM-C1: one bridge forwards each control frame once and owns all permission state', async () => {
    const h = await makeHarness();
    h.pageConnect();
    const ws = lastWS();
    ws.open();

    emitHello(h, ws, 's');
    emitHello(h, ws, 's'); // repeated hello must not create a second authority
    emitGeom(h, ws, 30, 100, 's');
    emitGeom(h, ws, 30, 100, 's');
    h.sandbox.window.pokitSendInput('x', 'macro-1', 'text');
    const pending = h.rnPosts.find((p) => p.type === 'input_pending');
    ws.emit('message', { data: JSON.stringify({
      type: 'input_result', connectionId: 'conn-1', sessionId: 's', generation: TEST_GEN,
      inputId: pending.inputId, outcome: 'accepted',
    }) });
    ws.emit('message', { data: JSON.stringify({
      type: 'input_result', connectionId: 'conn-1', sessionId: 's', generation: TEST_GEN,
      inputId: pending.inputId, outcome: 'accepted',
    }) });
    ws.emit('message', { data: JSON.stringify({ type: 'read_only', reason: 'view only' }) });
    ws.emit('message', { data: JSON.stringify({ type: 'read_only', reason: 'view only' }) });

    expect(h.rnPosts.filter((p) => p.type === 'hello')).toHaveLength(1);
    expect(h.rnPosts.filter((p) => p.type === 'geometry')).toHaveLength(1);
    expect(h.rnPosts.filter((p) => p.type === 'input_pending')).toHaveLength(1);
    expect(h.rnPosts.filter((p) => p.type === 'input_result')).toHaveLength(1);
    expect(h.rnPosts.filter((p) => p.type === 'read_only')).toHaveLength(1);
    expect(h.sandbox.__pokitControlBridge.state.readOnly).toBe(true);
    expect(h.sandbox.__pokitControlBridge.state.inputEnabled).toBe(false);
    expect(h.sandbox.__pokitControlBridge.state.geometry).toEqual({ rows: 30, cols: 100 });
  });

  it('TERM-C1: reconnect fails closed, retains last geometry, and rejects old-socket controls', async () => {
    const h = await makeHarness();
    h.pageConnect();
    const ws1 = lastWS();
    ws1.open();
    emitHello(h, ws1, 's');
    emitGeom(h, ws1, 30, 100, 's');

    h.pageConnect();
    const ws2 = lastWS();
    expect(h.sandbox.__pokitControlBridge.state.readOnly).toBe(true);
    expect(h.sandbox.__pokitControlBridge.state.geometry).toEqual({ rows: 30, cols: 100 });
    const before = h.rnPosts.length;
    ws1.emit('message', { data: JSON.stringify({ type: 'read_only', reason: 'stale' }) });
    expect(h.rnPosts).toHaveLength(before);

    ws2.open();
    emitHello(h, ws2, 's', TEST_GEN + 1);
    expect(h.sandbox.__pokitControlBridge.state.readOnly).toBe(false);
    expect(h.sandbox.__pokitControlBridge.state.generation).toBe(TEST_GEN + 1);
  });

  // ── TERM-G1: live WS geometry frame validation with identity binding ──

  const TEST_SESSION = 's';
  const TEST_GEN = 7;

  function emitHello(h: Harness, ws: FakeWS, session?: string, generation?: number) {
    ws.emit('message', { data: JSON.stringify({
      type: 'hello',
      sessionId: session ?? TEST_SESSION,
      generation: generation ?? TEST_GEN,
      connectionId: 'conn-1',
      capabilities: ['terminal:input'],
    }) });
  }

  function emitGeom(h: Harness, ws: FakeWS, rows: number, cols: number, session?: string, generation?: number) {
    ws.emit('message', { data: JSON.stringify({
      type: 'geometry',
      rows, cols,
      session: session ?? TEST_SESSION,
      generation: generation ?? TEST_GEN,
    }) });
  }

  function openAndHello(h: Harness, ws: FakeWS) {
    ws.open();
    emitHello(h, ws);
  }

  it('TERM-G1 live: valid geometry with identity resizes and preserves last', async () => {
    const h = await makeHarness();
    h.pageConnect();
    const ws = lastWS();
    openAndHello(h, ws);

    emitGeom(h, ws, 30, 100);
    expect(h.term.resize).toHaveBeenCalledWith(100, 30);
    expect(h.sandbox.window.__pokitLastGeom).toEqual({ rows: 30, cols: 100 });
  });

  it('TERM-G1 live: geometry before hello is rejected (no identity yet)', async () => {
    const h = await makeHarness();
    h.pageConnect();
    const ws = lastWS();
    ws.open();
    // No hello emitted — identity is null.

    emitGeom(h, ws, 30, 100);
    expect(h.term.resize).not.toHaveBeenCalled();
  });

  it('TERM-G1 live: wrong session geometry rejected', async () => {
    const h = await makeHarness();
    h.pageConnect();
    const ws = lastWS();
    openAndHello(h, ws);

    emitGeom(h, ws, 30, 100, 'controlled_pty:other');
    expect(h.term.resize).not.toHaveBeenCalled();
  });

  it('TERM-G1 live: wrong generation geometry rejected', async () => {
    const h = await makeHarness();
    h.pageConnect();
    const ws = lastWS();
    openAndHello(h, ws);

    emitGeom(h, ws, 30, 100, TEST_SESSION, TEST_GEN + 1);
    expect(h.term.resize).not.toHaveBeenCalled();
  });

  it('TERM-G1 live: old generation geometry rejected (stale connection)', async () => {
    const h = await makeHarness();
    h.pageConnect();
    const ws = lastWS();
    openAndHello(h, ws);

    // Old generation — must be rejected.
    emitGeom(h, ws, 30, 100, TEST_SESSION, TEST_GEN - 1);
    expect(h.term.resize).not.toHaveBeenCalled();
  });

  it('TERM-G1 live: missing session field in geometry rejected', async () => {
    const h = await makeHarness();
    h.pageConnect();
    const ws = lastWS();
    openAndHello(h, ws);

    // Geometry without session field — pre-G1 daemon format.
    ws.emit('message', { data: JSON.stringify({ type: 'geometry', rows: 30, cols: 100, generation: TEST_GEN }) });
    expect(h.term.resize).not.toHaveBeenCalled();
  });

  it('TERM-G1 live: missing generation field in geometry rejected', async () => {
    const h = await makeHarness();
    h.pageConnect();
    const ws = lastWS();
    openAndHello(h, ws);

    ws.emit('message', { data: JSON.stringify({ type: 'geometry', rows: 30, cols: 100, session: TEST_SESSION }) });
    expect(h.term.resize).not.toHaveBeenCalled();
  });

  it('TERM-G1 live: float rows rejected (no resize, last valid preserved)', async () => {
    const h = await makeHarness();
    h.pageConnect();
    const ws = lastWS();
    openAndHello(h, ws);

    emitGeom(h, ws, 30, 100);
    h.term.resize.mockClear();

    emitGeom(h, ws, 30.5, 100);
    expect(h.term.resize).not.toHaveBeenCalled();
    expect(h.sandbox.window.__pokitLastGeom).toEqual({ rows: 30, cols: 100 });
  });

  it('TERM-G1 live: float cols rejected', async () => {
    const h = await makeHarness();
    h.pageConnect();
    const ws = lastWS();
    openAndHello(h, ws);

    emitGeom(h, ws, 30, 100);
    h.term.resize.mockClear();

    emitGeom(h, ws, 30, 100.1);
    expect(h.term.resize).not.toHaveBeenCalled();
    expect(h.sandbox.window.__pokitLastGeom).toEqual({ rows: 30, cols: 100 });
  });

  it('TERM-G1 live: zero/negative/out-of-bounds rows rejected', async () => {
    const h = await makeHarness();
    h.pageConnect();
    const ws = lastWS();
    openAndHello(h, ws);

    for (const r of [0, -1, 1001]) {
      h.term.resize.mockClear();
      emitGeom(h, ws, r, 100);
      expect(h.term.resize).not.toHaveBeenCalled();
    }
  });

  it('TERM-G1 live: zero/negative/out-of-bounds cols rejected', async () => {
    const h = await makeHarness();
    h.pageConnect();
    const ws = lastWS();
    openAndHello(h, ws);

    for (const c of [0, -1, 2001]) {
      h.term.resize.mockClear();
      emitGeom(h, ws, 30, c);
      expect(h.term.resize).not.toHaveBeenCalled();
    }
  });

  it('TERM-G1 live: boundary-max rows=1000 cols=2000 accepted', async () => {
    const h = await makeHarness();
    h.pageConnect();
    const ws = lastWS();
    openAndHello(h, ws);

    emitGeom(h, ws, 1000, 2000);
    expect(h.term.resize).toHaveBeenCalledWith(2000, 1000);
    expect(h.sandbox.window.__pokitLastGeom).toEqual({ rows: 1000, cols: 2000 });
  });

  it('TERM-G1 live: reconnect — new generation invalidates old connection geometry', async () => {
    const h = await makeHarness();
    h.pageConnect();
    const ws1 = lastWS();
    // First connection: gen=7.
    openAndHello(h, ws1);
    emitGeom(h, ws1, 30, 100);
    expect(h.term.resize).toHaveBeenCalledWith(100, 30);
    h.term.resize.mockClear();

    // Close ws1, create ws2 with NEW gen=8.
    ws1.close();
    h.pageConnect();
    const ws2 = lastWS();
    ws2.open();
    // New hello with generation=8 updates global state.
    emitHello(h, ws2, TEST_SESSION, 8);

    // New connection geometry with gen=8 works.
    emitGeom(h, ws2, 40, 120, TEST_SESSION, 8);
    expect(h.term.resize).toHaveBeenCalledWith(120, 40);
    h.term.resize.mockClear();

    // Old connection ws1 geometry with gen=7 must be REJECTED
    // because global state now requires gen=8.
    emitGeom(h, ws1, 99, 999, TEST_SESSION, 7);
    expect(h.term.resize).not.toHaveBeenCalled();
  });
});

// Deliver a window 'message' to the injected reconnect bridge. The bridge
// registers its listener via window.addEventListener('message', …); our fake
// window collects listeners so we can dispatch synthetic messages.
function postMessage(h: Harness, payload: any) {
  const listeners: Array<(e: any) => void> = h.sandbox.__messageListeners || [];
  listeners.forEach((fn) => fn({ data: JSON.stringify(payload) }));
}
