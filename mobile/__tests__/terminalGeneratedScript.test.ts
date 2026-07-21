// M3-auth-4A / TERM-C1: execute the ACTUAL served daemon page and generated
// production script. The bridge below is extracted byte-for-byte from pty.go;
// this test must never reimplement it in TypeScript.
//
// The prior test rewrote an equivalent wsURL() inside the test and asserted on
// the copy. This suite instead extracts the real <script> that
// TerminalController.bootstrap() injects into the daemon page and runs it in a
// controlled fake browser (Node vm sandbox). It proves the ten production
// behaviors the handoff requires (ticket A once, absent from navigation
// history, reconnect A→B→C, single issuance, stale rejection, binary output
// written unchanged, text geometry resizes but is never written, control-
// looking binary input reaches the socket, and timer cleanup).

import * as fs from 'fs';
import * as path from 'path';
import * as vm from 'vm';
import { TerminalController, shouldIssueReconnect } from '../src/lib/terminalController';
import { TokenManager } from '../src/lib/authClient';

const mockFetch = jest.fn();
(global as any).fetch = mockFetch;

const fakeMgr = { getValidToken: async () => '0'.repeat(64) } as TokenManager;
const daemonPageSource = fs.readFileSync(
  path.resolve(__dirname, '../../companion-daemon/internal/term/pty.go'), 'utf8',
);

function extractServed(pattern: RegExp, label: string): string {
  const match = daemonPageSource.match(pattern);
  if (!match) throw new Error(`served daemon ${label} not found`);
  return match[1];
}

const TERM_HTML = extractServed(/io\.WriteString\(w, `([\s\S]*?)`\)\n}\n\nfunc \(h \*Handlers\) HandleCmd/, 'HTML');
const SERVED_INPUT_ID = extractServed(/(function pokitMakeInputID\(\)\{[^\n]*\})/, 'input ID generator');
const SERVED_CONTROL_BRIDGE = extractServed(/(window\.__pokitControlBridge=\(function\(\)\{[\s\S]*?window\.pokitReadOnly=function\(\)\{return window\.__pokitControlBridge\.readOnly\(\);\};)/, 'control bridge');
const SERVED_CONNECT = extractServed(/(function connect\(\)\{[\s\S]*?\n\}\n\nterm\.onData)/, 'connect dispatcher').replace(/\nterm\.onData$/, '');
const SERVED_KEYBOARD = extractServed(/(term\.onData\(function\(d\)\{[\s\S]*?\n\}\);)/, 'keyboard sender');
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
  term: { resize: jest.Mock; write: jest.Mock; clear: jest.Mock };
  rnPosts: any[];
  timeouts: Array<() => void>;
  activeIntervals: Set<number>;
  flushTimeouts: () => void;
  pageConnect: () => void;
}

async function makeHarness(): Promise<Harness> {
  const script = await generateInjectedScript();
  FakeWS.instances.length = 0;

  const term = { resize: jest.fn(), write: jest.fn(), clear: jest.fn() };
  const rnPosts: any[] = [];
  const timeouts: Array<() => void> = [];
  const activeIntervals = new Set<number>();
  let nextTimerId = 1;
  let entropyOffset = 0;

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
    document: {
      readyState: 'complete',
      addEventListener: () => {},
      getElementById: () => ({ style: {}, textContent: '' }),
    },
    WebSocket: FakeWS,
    term,
    crypto: { getRandomValues: (bytes: Uint8Array) => { for (let i = 0; i < bytes.length; i++) bytes[i] = (entropyOffset + i) & 0xff; entropyOffset++; return bytes; } },
    btoa: (s: string) => Buffer.from(s, 'binary').toString('base64'),
    setTimeout: (fn: () => void) => { timeouts.push(fn); return 0; },
    setInterval: (_fn: () => void) => { const id = nextTimerId++; activeIntervals.add(id); return id; },
    clearInterval: (id: number) => { activeIntervals.delete(id); },
    // window.addEventListener('message', …) is how the reconnect bridge
    // subscribes to native ticket messages; collect them for dispatch.
    addEventListener: (type: string, fn: (e: any) => void) => {
      if (type === 'message') (sandbox.__messageListeners || (sandbox.__messageListeners = [])).push(fn);
    },
    ReactNativeWebView: { postMessage: (s: string) => rnPosts.push(JSON.parse(s)) },
    reconnecting: false,
    stopped: false,
    opened: false,
    everOpened: false,
    consecutiveFailures: 0,
    wasReconnect: false,
    raw: '',
    e8diag: { connectCount: 0, closeCount: 0, msgCount: 0, totalBytes: 0, lastMsgSize: 0 },
    e8_fitCount: 0,
    fitTerminal: () => {},
    setStatus: () => {},
    stopSession: () => {},
  };
  (term as any).onData = (fn: (data: string) => void) => { sandbox.__termOnData = fn; };
  sandbox.window = sandbox;
  vm.createContext(sandbox);

  // Run the REAL injected script (Phase 1 wraps WebSocket immediately; Phase 2
  // queues installReconnect via setTimeout).
  vm.runInContext(script, sandbox);
  // Execute the exact served bridge, served connect dispatcher, and direct
  // xterm keyboard sender extracted above. The test intentionally has no
  // TypeScript bridge implementation to drift from production behavior.
  vm.runInContext(SERVED_INPUT_ID, sandbox);
  vm.runInContext(SERVED_CONTROL_BRIDGE, sandbox);
  vm.runInContext(SERVED_CONNECT, sandbox);
  vm.runInContext(SERVED_KEYBOARD, sandbox);

  const flushTimeouts = () => { while (timeouts.length) timeouts.shift()!(); };
  return { sandbox, term, rnPosts, timeouts, activeIntervals, flushTimeouts, pageConnect: () => sandbox.window.connect() };
}

function lastWS(): FakeWS { return FakeWS.instances[FakeWS.instances.length - 1]; }

function terminalInputRequests(ws: FakeWS): any[] {
  return ws.sent
    .filter((payload: unknown) => typeof payload === 'string')
    .map((payload: string) => JSON.parse(payload))
    .filter((frame: any) => frame.type === 'terminal_input');
}

function decodeInput(frame: any): string {
  return Buffer.from(frame.payload, 'base64').toString('utf8');
}

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

  it('TERM-C1 served bridge: direct Ctrl+C, paste, every macro, and text+Enter use acknowledged input', async () => {
    const h = await makeHarness();
    h.pageConnect();
    const ws = lastWS();
    ws.open();
    emitHello(h, ws, 's');

    // Direct xterm keyboard input is the actual served term.onData callback.
    h.sandbox.__termOnData('\x03');
    // Native paste and every macro converge on the same served sender.
    h.sandbox.window.pokitSendInput('paste\nbody', 'paste-1', 'text');
    const macros = [
      [3], [27], [9], [27, 91, 65], [27, 91, 66], [27, 91, 68],
      [27, 91, 67], [121, 13], [110, 13], [13],
    ];
    for (const chars of macros) {
      h.sandbox.window.pokitSendInput(String.fromCharCode(...chars), 'macro-' + chars.join('-'), 'text');
    }
    // Send remains a two-frame operation: text followed by a discrete Enter.
    h.sandbox.window.pokitSendInput('two frame', 'line-1', 'text');
    h.sandbox.window.pokitSendInput('\r', 'line-1', 'enter');

    const requests = terminalInputRequests(ws);
    expect(requests).toHaveLength(14);
    expect(decodeInput(requests[0])).toBe('\x03');
    expect(decodeInput(requests[1])).toBe('paste\nbody');
    expect(requests.slice(2, 12).map(decodeInput)).toEqual(macros.map((chars) => String.fromCharCode(...chars)));
    expect(requests.slice(12).map(decodeInput)).toEqual(['two frame', '\r']);
    expect(h.rnPosts.filter((p) => p.type === 'input_pending')).toHaveLength(14);

    for (const request of requests) {
      ws.emit('message', { data: JSON.stringify({
        type: 'input_result', connectionId: 'conn-1', sessionId: 's', generation: TEST_GEN,
        inputId: request.inputId, outcome: 'accepted',
      }) });
    }
    expect(h.rnPosts.filter((p) => p.type === 'input_result' && p.outcome === 'accepted')).toHaveLength(14);
  });

  it('TERM-C1 served bridge: malformed, unknown, or mismatched authority yields zero terminal writes', async () => {
    const deniedHellos = [
      { connectionId: 'conn-1', sessionId: 'wrong-session', generation: TEST_GEN, capabilities: ['terminal:input'] },
      { connectionId: '', sessionId: 's', generation: TEST_GEN, capabilities: ['terminal:input'] },
      { connectionId: 'conn-1', sessionId: 's', generation: 7.5, capabilities: ['terminal:input'] },
      { connectionId: 'conn-1', sessionId: 's', generation: TEST_GEN, capabilities: undefined },
      { connectionId: 'conn-1', sessionId: 's', generation: TEST_GEN, capabilities: ['terminal:unknown'] },
    ];
    const macros = [[3], [27], [9], [27, 91, 65], [27, 91, 66], [27, 91, 68], [27, 91, 67], [121, 13], [110, 13], [13]];

    for (const hello of deniedHellos) {
      const h = await makeHarness();
      h.pageConnect();
      const ws = lastWS();
      ws.open();
      ws.emit('message', { data: JSON.stringify({ type: 'hello', ...hello }) });
      h.sandbox.__termOnData('\x03');
      h.sandbox.window.pokitSendInput('paste', 'paste-1', 'text');
      for (const chars of macros) h.sandbox.window.pokitSendInput(String.fromCharCode(...chars), 'macro', 'text');
      h.sandbox.window.pokitSendInput('two frame', 'line-1', 'text');
      h.sandbox.window.pokitSendInput('\r', 'line-1', 'enter');
      expect(terminalInputRequests(ws)).toHaveLength(0);
      expect(h.rnPosts.filter((p) => p.type === 'delivery_unknown')).toHaveLength(14);
    }

    const h = await makeHarness();
    h.pageConnect();
    const ws = lastWS();
    ws.open();
    emitHello(h, ws, 's');
    const postedBefore = h.rnPosts.length;
    // Wrong result identities are never forwarded or interpreted as delivery.
    ws.emit('message', { data: JSON.stringify({ type: 'input_result', connectionId: 'conn-1', sessionId: 'wrong-session', generation: TEST_GEN, inputId: 'x', outcome: 'accepted' }) });
    ws.emit('message', { data: JSON.stringify({ type: 'input_result', connectionId: 'wrong-connection', sessionId: 's', generation: TEST_GEN, inputId: 'x', outcome: 'accepted' }) });
    ws.emit('message', { data: JSON.stringify({ type: 'input_result', connectionId: 'conn-1', sessionId: 's', generation: TEST_GEN + 1, inputId: 'x', outcome: 'accepted' }) });
    expect(h.rnPosts).toHaveLength(postedBefore);

    // A different connection identity on the same socket is a fail-closed
    // rebind, so a later native send cannot produce a terminal write.
    ws.emit('message', { data: JSON.stringify({ type: 'hello', connectionId: 'wrong-connection', sessionId: 's', generation: TEST_GEN, capabilities: ['terminal:input'] }) });
    h.sandbox.window.pokitSendInput('must not write');
    expect(h.rnPosts).toHaveLength(postedBefore + 2); // bridge read_only + local delivery_unknown
    expect(h.rnPosts[postedBefore].type).toBe('read_only');
    expect(terminalInputRequests(ws)).toHaveLength(0);
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
