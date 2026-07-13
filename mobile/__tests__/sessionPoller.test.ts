// S1-D: the session-list poll controller — singleflight + mounted guard. Proves a
// slow response still commits (no starvation), overlapping polls are skipped, and
// a response resolving after unmount is dropped. This is the ACTUAL controller
// DashboardScreen.fetchSessions drives.

import { createPollController } from '../src/lib/sessionPoller';

// A manually-resolved promise so tests control response timing (delayed polling).
function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

describe('createPollController', () => {
  it('commits a delayed response (no starvation) and skips overlapping polls', async () => {
    const c = createPollController<number[]>();
    const d1 = deferred<number[]>();
    let committed: number[] | null = null;
    let calls = 0;

    // First poll starts a slow request.
    c.poll(() => { calls++; return d1.promise; }, data => { committed = data; }, () => {});
    expect(c.inFlight()).toBe(true);

    // While in flight, further ticks are SKIPPED (singleflight) — no new request.
    c.poll(() => { calls++; return deferred<number[]>().promise; }, () => {}, () => {});
    c.poll(() => { calls++; return deferred<number[]>().promise; }, () => {}, () => {});
    expect(calls).toBe(1);

    // The slow response finally arrives → it commits (never starved out).
    d1.resolve([1, 2, 3]);
    await d1.promise;
    expect(committed).toEqual([1, 2, 3]);
    expect(c.inFlight()).toBe(false);

    // After it completes, a new poll may run.
    const d2 = deferred<number[]>();
    c.poll(() => { calls++; return d2.promise; }, () => {}, () => {});
    expect(calls).toBe(2);
  });

  it('drops a response that resolves after unmount (stop)', async () => {
    const c = createPollController<number[]>();
    const d = deferred<number[]>();
    let committed = false;
    c.poll(() => d.promise, () => { committed = true; }, () => { committed = true; });

    c.stop(); // component unmounts while the request is in flight
    d.resolve([9]);
    await d.promise;
    expect(committed).toBe(false);
    expect(c.active()).toBe(false);
  });

  it('routes rejections to onError while active', async () => {
    const c = createPollController<number[]>();
    const d = deferred<number[]>();
    let erred: unknown = null;
    c.poll(() => d.promise, () => {}, e => { erred = e; });
    d.reject(new Error('boom'));
    await d.promise.catch(() => {});
    expect((erred as Error).message).toBe('boom');
  });

  it('does not start a poll after stop', () => {
    const c = createPollController<number[]>();
    c.stop();
    let called = false;
    c.poll(() => { called = true; return Promise.resolve([]); }, () => {}, () => {});
    expect(called).toBe(false);
  });
});
