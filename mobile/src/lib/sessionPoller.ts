// S1-D (D7 + starvation fix) — the session-list poll controller used by
// DashboardScreen. It separates two concerns that a single request-generation
// counter conflated:
//
//   1. singleflight: never start a poll while one is in flight, so a slow response
//      is not superseded by the next tick. On a slow network every response still
//      commits — there is no permanent starvation.
//   2. mounted guard: a response that resolves after unmount is dropped, never
//      committed.
//
// This is the actual controller DashboardScreen imports; testing it exercises the
// production polling path with delayed/failed responses.

export interface PollController<T> {
  /** run fn unless a poll is already in flight or the controller is stopped. */
  poll(fn: () => Promise<T>, onData: (data: T) => void, onError: (err: unknown) => void): void;
  /** stop the controller on unmount; no further response commits. */
  stop(): void;
  /** introspection for tests. */
  inFlight(): boolean;
  active(): boolean;
}

export function createPollController<T = unknown>(): PollController<T> {
  let busy = false;
  let alive = true;
  return {
    poll(fn, onData, onError) {
      if (!alive || busy) return; // singleflight + stopped
      busy = true;
      fn().then(
        (data) => {
          busy = false;
          if (alive) onData(data); // drop post-unmount
        },
        (err) => {
          busy = false;
          if (alive) onError(err);
        },
      );
    },
    stop() {
      alive = false;
    },
    inFlight() {
      return busy;
    },
    active() {
      return alive;
    },
  };
}
