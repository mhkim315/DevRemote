// SP0.5-R3 — a fetcher that HANGS in a pre-HTTP stage and IGNORES the abort
// signal (the hung bearer challenge/verify refresh inside authenticatedFetch)
// must not bypass the poller/controller deadline: the tick completes
// logically, freshness expiry marks the status non-current, close() works,
// the shared token refresh stays single, and a late auth completion commits
// nothing.
import {
  ManagedSessionPoller,
  ManagedSessionController,
  ManagedPollerState,
  MANAGED_CONTRACT_VERSION,
} from '../src/lib/managedSession';

const SESSION = 'codex_app_server:codex-app-1';

function statusDTO() {
  return {
    id: SESSION, provider: 'codex', version: 'codex-cli 0.144.1', nativeStatus: 'working',
    launchGen: 1, createdAt: '2026-07-15T00:00:00Z', statusChangedAt: '2026-07-15T00:00:05Z', exited: false,
  };
}

function workingResponse() {
  return {
    contractVersion: MANAGED_CONTRACT_VERSION,
    session: statusDTO(),
    events: [{
      contractVersion: MANAGED_CONTRACT_VERSION, sessionId: SESSION, epoch: 1,
      seq: 1, kind: 'working', observedAt: '2026-07-15T00:00:05Z',
    }],
    nextCursor: 1,
  };
}

// hungAuthFetcher models the production failure: getValidToken() (shared
// singleflight refresh) blocks BEFORE any abortable HTTP request. The signal
// is deliberately IGNORED. All callers share ONE refresh promise; its late
// resolution releases every waiter with a valid-looking response.
function hungAuthHarness() {
  const updates: ManagedPollerState[] = [];
  let refreshCount = 0;
  let releaseRefresh: (() => void) | null = null;
  let sharedRefresh: Promise<void> | null = null;
  let fetchCalls = 0;

  const getSharedRefresh = () => {
    if (!sharedRefresh) {
      refreshCount++;
      sharedRefresh = new Promise<void>((resolve) => { releaseRefresh = resolve; });
    }
    return sharedRefresh;
  };

  const fetchEvents = async (_cursor: number, _signal: AbortSignal) => {
    fetchCalls++;
    await getSharedRefresh(); // hangs here; signal ignored (pre-HTTP stage)
    return workingResponse();
  };

  const poller = new ManagedSessionPoller(SESSION, fetchEvents, (s) => updates.push(s), {
    deadlineMs: 1000,
    freshnessMs: 300,
    now: () => Date.now(),
  });

  return {
    poller, updates,
    fetchCalls: () => fetchCalls,
    refreshCount: () => refreshCount,
    releaseRefresh: () => releaseRefresh && releaseRefresh(),
  };
}

async function flush() {
  for (let i = 0; i < 8; i++) await Promise.resolve();
}

describe('hung bearer refresh cannot bypass the deadline (SP0.5-R3)', () => {
  beforeEach(() => jest.useFakeTimers());
  afterEach(() => jest.useRealTimers());

  it('deadline completes the tick and marks working non-current', async () => {
    const h = hungAuthHarness();
    // Fresh success is impossible here — go straight to the hang: the tick's
    // await must END at the deadline even though the fetcher ignores abort.
    const t = h.poller.tick();
    jest.advanceTimersByTime(1100);
    await t; // resolves ONLY because raceWithAbort completes logically
    const last = h.updates[h.updates.length - 1];
    expect(last.status).toBe('unavailable');
    expect(last.current).toBe(false);
  });

  it('repeated deadline ticks share ONE auth refresh and stay bounded', async () => {
    const h = hungAuthHarness();
    for (let i = 0; i < 4; i++) {
      const t = h.poller.tick();
      jest.advanceTimersByTime(1100);
      await t;
    }
    expect(h.fetchCalls()).toBe(4);   // ticks were never pinned
    expect(h.refreshCount()).toBe(1); // singleflight refresh: exactly one real auth attempt
  });

  it('poller closes cleanly while the refresh is still hung', async () => {
    const h = hungAuthHarness();
    const t = h.poller.tick();
    h.poller.close(); // must not block on the hung fetcher
    await t;
    expect(h.updates).toHaveLength(0); // closed: nothing committed
  });

  it('late auth completion after deadline/close commits nothing', async () => {
    const h = hungAuthHarness();
    const t = h.poller.tick();
    jest.advanceTimersByTime(1100);
    await t;
    expect(h.updates[h.updates.length - 1].current).toBe(false);
    const before = h.updates.length;

    h.poller.close();
    h.releaseRefresh(); // the hung refresh finally resolves with a VALID working response
    await flush();
    expect(h.updates.length).toBe(before); // stale completion never committed
  });

  it('a working status stays current only until freshness expires under the hang', async () => {
    // First a FRESH working response, then the auth hang: the previously
    // positive status must be invalidated at freshness expiry.
    const updates: ManagedPollerState[] = [];
    let hang = false;
    let release: (() => void) | null = null;
    const fetchEvents = async (_c: number, _s: AbortSignal) => {
      if (hang) {
        await new Promise<void>((r) => { release = r; }); // ignores signal
      }
      return workingResponse();
    };
    const poller = new ManagedSessionPoller(SESSION, fetchEvents, (s) => updates.push(s), {
      deadlineMs: 1000, freshnessMs: 300, now: () => Date.now(),
    });

    const t1 = poller.tick();
    await flush();
    await t1;
    expect(updates[updates.length - 1]).toMatchObject({ status: 'working', current: true });

    hang = true;
    jest.advanceTimersByTime(500); // past freshnessMs since the fresh response
    const t2 = poller.tick();
    jest.advanceTimersByTime(1100); // past the deadline
    await t2;
    const last = updates[updates.length - 1];
    expect(last.status).toBe('unavailable');
    expect(last.current).toBe(false);
    poller.close();
    if (release !== null) (release as () => void)();
  });

  it('controller bootstrap deadline also completes against a hung status fetcher', async () => {
    const errors: string[] = [];
    let scheduled = 0;
    const ctrl = new ManagedSessionController(SESSION, {
      fetchStatus: () => new Promise(() => {}), // hangs forever, ignores signal
      fetchEvents: async () => workingResponse(),
      onUpdate: () => {},
      onError: (m) => errors.push(m),
      schedule: () => { scheduled++; return {}; },
      cancelSchedule: () => {},
    }, { deadlineMs: 1000, pollMs: 100 });
    const start = ctrl.start();
    jest.advanceTimersByTime(1100);
    await start;
    expect(errors).toEqual(['Managed session unavailable.']);
    expect(scheduled).toBe(0);
    ctrl.close();
  });
});
