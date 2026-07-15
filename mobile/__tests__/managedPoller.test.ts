// SP0.5-R1 — polling staleness (blocker 1) and byte/capacity bounds
// (blocker 2) for the production managed polling controller and feed.
import {
  ManagedSessionPoller,
  ManagedPollerState,
  ManagedSessionFeed,
  decodeManagedEventsResponse,
  utf8ByteLength,
  MANAGED_FEED_CAP,
  MANAGED_CONTRACT_VERSION,
} from '../src/lib/managedSession';

const SESSION = 'codex_app_server:codex-app-1';

function response(status: string, events: Array<{ seq: number; kind: string; text?: string }>, nextCursor: number) {
  return {
    contractVersion: MANAGED_CONTRACT_VERSION,
    session: {
      id: SESSION, provider: 'codex', version: 'codex-cli 0.144.1',
      nativeStatus: status, launchGen: 1,
      createdAt: '2026-07-15T00:00:00Z', statusChangedAt: '2026-07-15T00:00:05Z', exited: false,
    },
    events: events.map((e) => ({
      contractVersion: MANAGED_CONTRACT_VERSION, sessionId: SESSION, epoch: 1,
      seq: e.seq, kind: e.kind, ...(e.text !== undefined ? { text: e.text } : {}),
      observedAt: '2026-07-15T00:00:05Z',
    })),
    nextCursor,
  };
}

describe('utf8ByteLength (blocker 2 counterexample)', () => {
  it('counts UTF-8 bytes, not UTF-16 units', () => {
    expect(utf8ByteLength('abcd')).toBe(4);
    expect(utf8ByteLength('😀')).toBe(4);
    // The reviewer counterexample: 2048 emoji = 4096 UTF-16 units but 8192 bytes.
    expect('😀'.repeat(2048).length).toBe(4096);
    expect(utf8ByteLength('😀'.repeat(2048))).toBe(8192);
  });

  it('decoder enforces the BYTE bound', () => {
    // ASCII exact bound passes; one over fails.
    expect(decodeManagedEventsResponse(response('working', [{ seq: 1, kind: 'assistant', text: 'x'.repeat(4096) }], 1))).not.toBeNull();
    expect(decodeManagedEventsResponse(response('working', [{ seq: 1, kind: 'assistant', text: 'x'.repeat(4097) }], 1))).toBeNull();
    // Multi-byte exact bound passes (1024 emoji = 4096 bytes); the UTF-16-passing
    // over-bound case is rejected.
    expect(decodeManagedEventsResponse(response('working', [{ seq: 1, kind: 'assistant', text: '😀'.repeat(1024) }], 1))).not.toBeNull();
    expect(decodeManagedEventsResponse(response('working', [{ seq: 1, kind: 'assistant', text: '😀'.repeat(2048) }], 1))).toBeNull();
  });
});

describe('ManagedSessionFeed capacity (blocker 2)', () => {
  it('caps accumulation and preserves an explicit gap', () => {
    const feed = new ManagedSessionFeed(SESSION);
    let seq = 0;
    // Long-lived polling: far more events than the cap, across many batches.
    for (let batch = 0; batch < 10; batch++) {
      const events = Array.from({ length: 50 }, () => ({ seq: ++seq, kind: 'working' }));
      const r = decodeManagedEventsResponse(response('working', events, seq));
      expect(r).not.toBeNull();
      expect(feed.apply(r!)).toBe(true);
    }
    const got = feed.getEvents();
    expect(got.length).toBe(MANAGED_FEED_CAP + 1); // capped + leading gap marker
    expect(got[0].kind).toBe('gap');
    expect(got[1].seq).toBe(seq - MANAGED_FEED_CAP + 1);
    expect(got[got.length - 1].seq).toBe(seq);
  });
});

describe('ManagedSessionPoller (blocker 1)', () => {
  beforeEach(() => jest.useFakeTimers());
  afterEach(() => jest.useRealTimers());

  function harness(opts?: { deadlineMs?: number; freshnessMs?: number }) {
    const updates: ManagedPollerState[] = [];
    const pending: Array<{ resolve: (v: unknown) => void; reject: (e: unknown) => void }> = [];
    const signals: AbortSignal[] = [];
    let calls = 0;
    let outstanding = 0;
    let maxOutstanding = 0;
    // Models real fetch: the promise REJECTS when its signal aborts.
    const fetchEvents = (_cursor: number, signal: AbortSignal) => {
      calls++;
      outstanding++;
      maxOutstanding = Math.max(maxOutstanding, outstanding);
      signals.push(signal);
      return new Promise<unknown>((resolve, reject) => {
        const settle = <T,>(fn: (v: T) => void) => (v: T) => { outstanding--; fn(v); };
        pending.push({ resolve: settle(resolve), reject: settle(reject) });
        signal.addEventListener('abort', () => {
          const req = pending.shift();
          if (req) req.reject(new Error('aborted'));
        });
      });
    };
    const poller = new ManagedSessionPoller(SESSION, fetchEvents, (s) => updates.push(s), {
      deadlineMs: opts?.deadlineMs ?? 1000,
      freshnessMs: opts?.freshnessMs ?? 3000,
      now: () => Date.now(),
    });
    return {
      poller, updates, signals,
      callCount: () => calls,
      maxOutstanding: () => maxOutstanding,
      outstanding: () => outstanding,
      resolveNext: (v: unknown) => pending.shift()!.resolve(v),
      rejectNext: () => pending.shift()!.reject(new Error('network')),
    };
  }

  it('enforces one request in flight', async () => {
    const h = harness();
    void h.poller.tick();
    void h.poller.tick();
    void h.poller.tick();
    expect(h.callCount()).toBe(1); // overlapping ticks skipped
    h.resolveNext(response('working', [], 0));
    await Promise.resolve(); await Promise.resolve(); await Promise.resolve();
    void h.poller.tick();
    expect(h.callCount()).toBe(2);
  });

  it('marks status non-current after failures past the freshness window', async () => {
    const h = harness();
    // Fresh success first: working is current.
    const t1 = h.poller.tick();
    h.resolveNext(response('working', [{ seq: 1, kind: 'working' }], 1));
    await t1;
    expect(h.updates[h.updates.length - 1]).toMatchObject({ status: 'working', current: true });

    // Connection dies: failures inside the freshness window stay quiet, but
    // once freshness expires the positive status is invalidated.
    const t2 = h.poller.tick();
    h.rejectNext();
    await t2;
    jest.advanceTimersByTime(4000); // past freshnessMs
    const t3 = h.poller.tick();
    h.rejectNext();
    await t3;
    const last = h.updates[h.updates.length - 1];
    expect(last.status).toBe('unavailable');
    expect(last.current).toBe(false);
    expect(last.events.length).toBe(1); // history preserved, just non-current
  });

  it('deadline expiry ABORTS the underlying hung request', async () => {
    const h = harness({ deadlineMs: 500, freshnessMs: 300 });
    const t = h.poller.tick();
    expect(h.signals[0].aborted).toBe(false);
    jest.advanceTimersByTime(600); // request hangs past the deadline
    await t;
    // The REAL request was cancelled, not just a wrapper promise.
    expect(h.signals[0].aborted).toBe(true);
    expect(h.outstanding()).toBe(0);
    const last = h.updates[h.updates.length - 1];
    expect(last).toMatchObject({ status: 'unavailable', current: false });
    // The hung request no longer blocks new ticks (in-flight released).
    void h.poller.tick();
    expect(h.callCount()).toBe(2);
  });

  it('keeps at most ONE real outstanding request across repeated timeouts', async () => {
    const h = harness({ deadlineMs: 500, freshnessMs: 100 });
    for (let i = 0; i < 5; i++) {
      const t = h.poller.tick();
      jest.advanceTimersByTime(600); // every request times out and is aborted
      await t;
    }
    expect(h.callCount()).toBe(5);
    expect(h.maxOutstanding()).toBe(1); // aborted before the next tick starts
    expect(h.outstanding()).toBe(0);
  });

  it('close() aborts the in-flight request', async () => {
    const h = harness();
    const t = h.poller.tick();
    h.poller.close();
    await t;
    expect(h.signals[0].aborted).toBe(true);
    expect(h.outstanding()).toBe(0);
  });

  it('only a NEW fresh snapshot restores current after reconnect', async () => {
    const h = harness({ freshnessMs: 100 });
    const t1 = h.poller.tick();
    h.resolveNext(response('working', [], 0));
    await t1;
    jest.advanceTimersByTime(5000);
    const t2 = h.poller.tick();
    h.rejectNext();
    await t2;
    expect(h.updates[h.updates.length - 1].current).toBe(false);
    // Reconnect: a fresh response restores current with the daemon's status.
    const t3 = h.poller.tick();
    h.resolveNext(response('completed', [{ seq: 1, kind: 'completed' }], 1));
    await t3;
    expect(h.updates[h.updates.length - 1]).toMatchObject({ status: 'completed', current: true });
  });

  it('rejects every commit after close (unmount)', async () => {
    const h = harness();
    h.poller.close();
    const t = h.poller.tick();
    await t;
    expect(h.callCount()).toBe(0);
    expect(h.updates.length).toBe(0);
  });
});
