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

  // BLOCKER 1: a hung request hits the bounded deadline, fails, and frees the
  // controller — so freshness can be surfaced (non-current) without a live render.
  it('fails a pending request at the bounded deadline and frees itself', () => {
    jest.useFakeTimers();
    try {
      const c = createPollController<number[]>();
      let erred = false;
      let committed = false;
      c.poll(() => new Promise<number[]>(() => {}), () => { committed = true; }, () => { erred = true; }, 5000);
      expect(c.inFlight()).toBe(true);
      jest.advanceTimersByTime(4999);
      expect(erred).toBe(false);
      jest.advanceTimersByTime(1);
      expect(erred).toBe(true);        // deadline fired → onError → UI shows non-current
      expect(committed).toBe(false);
      expect(c.inFlight()).toBe(false); // freed for the next poll
    } finally {
      jest.useRealTimers();
    }
  });

  // BLOCKER 2: a response that began before a disconnect is dropped after reconnect;
  // only the post-reconnect response commits.
  it('drops a pre-disconnect response after invalidate; commits only the reconnect one', async () => {
    const c = createPollController<number[]>();
    const committed: number[][] = [];
    const d1 = deferred<number[]>();
    c.poll(() => d1.promise, d => committed.push(d), () => {});
    expect(c.inFlight()).toBe(true);

    c.invalidate(); // disconnect: bump the connection epoch, free the controller
    expect(c.inFlight()).toBe(false);

    const d2 = deferred<number[]>();
    c.poll(() => d2.promise, d => committed.push(d), () => {}); // reconnect poll
    d2.resolve([2]);
    await d2.promise;

    d1.resolve([1]); // the stale pre-disconnect response arrives late
    await d1.promise;

    expect(committed).toEqual([[2]]); // only the post-reconnect response committed
    expect(c.inFlight()).toBe(false);
  });

  it('a late invalidated response does not clobber the current in-flight poll', async () => {
    const c = createPollController<number[]>();
    const d1 = deferred<number[]>();
    c.poll(() => d1.promise, () => {}, () => {});
    c.invalidate();
    const d2 = deferred<number[]>();
    c.poll(() => d2.promise, () => {}, () => {}); // now in flight (epoch 1)
    d1.resolve([1]); // stale epoch-0 response resolves
    await d1.promise;
    expect(c.inFlight()).toBe(true); // d2 still owns the controller
  });
});
