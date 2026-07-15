// SP0.5-R2 — bootstrap generation guard + real request cancellation for the
// production ManagedSessionController (the exact component lifecycle logic).
import {
  ManagedSessionController,
  ManagedPollerState,
  MANAGED_CONTRACT_VERSION,
} from '../src/lib/managedSession';

const SESSION_A = 'codex_app_server:codex-app-A';
const SESSION_B = 'codex_app_server:codex-app-B';

function statusDTO(id: string) {
  return {
    id, provider: 'codex', version: 'codex-cli 0.144.1', nativeStatus: 'idle',
    launchGen: 1, createdAt: '2026-07-15T00:00:00Z', statusChangedAt: '2026-07-15T00:00:00Z', exited: false,
  };
}

function eventsResponse(id: string) {
  return {
    contractVersion: MANAGED_CONTRACT_VERSION,
    session: statusDTO(id),
    events: [],
    nextCursor: 0,
  };
}

// controllable fetch harness modelling real fetch (rejects on signal abort).
function harness(sessionId: string) {
  const updates: ManagedPollerState[] = [];
  const errors: string[] = [];
  const statusReqs: Array<{ resolve: (v: unknown) => void; reject: (e: unknown) => void; signal: AbortSignal }> = [];
  const eventSignals: AbortSignal[] = [];
  let eventCalls = 0;
  let outstandingEvents = 0;
  let maxOutstandingEvents = 0;
  let scheduled = 0;
  let cancelled = 0;

  const ctrl = new ManagedSessionController(sessionId, {
    fetchStatus: (signal) =>
      new Promise<unknown>((resolve, reject) => {
        statusReqs.push({ resolve, reject, signal });
        signal.addEventListener('abort', () => reject(new Error('aborted')));
      }),
    fetchEvents: (_epoch, _cursor, signal) => {
      eventCalls++;
      outstandingEvents++;
      maxOutstandingEvents = Math.max(maxOutstandingEvents, outstandingEvents);
      eventSignals.push(signal);
      return new Promise<unknown>((resolve, reject) => {
        signal.addEventListener('abort', () => { outstandingEvents--; reject(new Error('aborted')); });
        // Resolve immediately with an empty bounded response.
        outstandingEvents--;
        resolve(eventsResponse(sessionId));
      });
    },
    onUpdate: (s) => updates.push(s),
    onError: (m) => errors.push(m),
    schedule: (fn, _ms) => { scheduled++; return { fn }; },
    cancelSchedule: () => { cancelled++; },
  }, { deadlineMs: 1000, pollMs: 100 });

  return {
    ctrl, updates, errors, statusReqs, eventSignals,
    eventCalls: () => eventCalls,
    maxOutstandingEvents: () => maxOutstandingEvents,
    scheduled: () => scheduled,
    cancelled: () => cancelled,
  };
}

describe('ManagedSessionController (SP0.5-R2)', () => {
  beforeEach(() => jest.useFakeTimers());
  afterEach(() => jest.useRealTimers());

  it('late bootstrap response after a session SWITCH installs nothing', async () => {
    const a = harness(SESSION_A);
    const b = harness(SESSION_B);

    const startA = a.ctrl.start(); // A bootstrap pending
    a.ctrl.close();                // switch away: cleanup runs
    const startB = b.ctrl.start(); // B bootstrap starts

    // A's real request was aborted by the switch.
    expect(a.statusReqs[0].signal.aborted).toBe(true);
    // Even if A's response ALSO resolves late, it is inert.
    a.statusReqs[0].resolve(statusDTO(SESSION_A));
    await startA;
    expect(a.scheduled()).toBe(0);   // no timer installed
    expect(a.eventCalls()).toBe(0);  // no poller created / no poll issued
    expect(a.updates).toHaveLength(0);
    expect(a.errors).toHaveLength(0); // closed: not even an error banner

    // B proceeds normally and is unaffected.
    b.statusReqs[0].resolve(statusDTO(SESSION_B));
    await startB;
    expect(b.scheduled()).toBe(1);
    expect(b.eventCalls()).toBeGreaterThanOrEqual(1);
  });

  it('late bootstrap response after UNMOUNT installs nothing', async () => {
    const a = harness(SESSION_A);
    const start = a.ctrl.start();
    a.ctrl.close(); // unmount
    a.statusReqs[0].resolve(statusDTO(SESSION_A));
    await start;
    expect(a.scheduled()).toBe(0);
    expect(a.eventCalls()).toBe(0);
    expect(a.updates).toHaveLength(0);
  });

  it('bootstrap deadline ABORTS the underlying status request', async () => {
    const a = harness(SESSION_A);
    const start = a.ctrl.start();
    expect(a.statusReqs[0].signal.aborted).toBe(false);
    jest.advanceTimersByTime(1100); // past deadlineMs
    await start;
    expect(a.statusReqs[0].signal.aborted).toBe(true);
    expect(a.errors).toEqual(['Managed session unavailable.']);
    expect(a.scheduled()).toBe(0);
  });

  it('close() cancels the poll timer and aborts poller requests', async () => {
    const a = harness(SESSION_A);
    const start = a.ctrl.start();
    a.statusReqs[0].resolve(statusDTO(SESSION_A));
    await start;
    expect(a.scheduled()).toBe(1);
    a.ctrl.close();
    expect(a.cancelled()).toBe(1);
    // At most one real events request ever existed.
    expect(a.maxOutstandingEvents()).toBeLessThanOrEqual(1);
  });

  it('invalid bootstrap DTO fails closed without installing anything', async () => {
    const a = harness(SESSION_A);
    const start = a.ctrl.start();
    a.statusReqs[0].resolve({ nope: true });
    await start;
    expect(a.errors).toEqual(['Managed session unavailable.']);
    expect(a.scheduled()).toBe(0);
    expect(a.eventCalls()).toBe(0);
  });
});
