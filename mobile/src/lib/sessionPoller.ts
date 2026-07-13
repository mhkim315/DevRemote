// S1-D/E — the session-list poll controller. It bounds three failure modes that a
// naive polling loop mishandles:
//
//   1. singleflight: never start a poll while one is in flight, so a slow response
//      is not superseded — no permanent starvation.
//   2. bounded deadline (E1): a request that does not settle within `timeoutMs` is
//      failed (onError) and the controller is freed, so a hung request cannot block
//      polling forever and the UI is driven to a non-current state.
//   3. connection epoch (E2): `invalidate()` bumps a generation so any in-flight
//      response that began before a disconnect is dropped after reconnect — only a
//      post-reconnect response commits. `stop()` (unmount) drops everything.
//
// This is the actual controller DashboardScreen imports; its tests drive delayed,
// timed-out, invalidated, and post-unmount responses through the production path.

export interface PollController<T> {
  /** run fn unless in flight or stopped; fail via onError after timeoutMs (0 = none). */
  poll(fn: () => Promise<T>, onData: (data: T) => void, onError: (err: unknown) => void, timeoutMs?: number): void;
  /** drop any in-flight response (disconnect) and allow a fresh poll (reconnect). */
  invalidate(): void;
  /** stop on unmount; no further response commits. */
  stop(): void;
  inFlight(): boolean;
  active(): boolean;
  epoch(): number;
}

export function createPollController<T = unknown>(): PollController<T> {
  let busy = false;
  let alive = true;
  let epoch = 0;
  return {
    poll(fn, onData, onError, timeoutMs) {
      if (!alive || busy) return; // singleflight + stopped
      busy = true;
      const myEpoch = epoch;
      let settled = false;
      let timer: ReturnType<typeof setTimeout> | null = null;
      const finish = (commit: () => void) => {
        if (settled) return;
        settled = true;
        if (timer !== null) {
          clearTimeout(timer);
          timer = null;
        }
        if (myEpoch !== epoch) return; // invalidated: drop; a newer poll owns `busy`
        busy = false;
        if (alive) commit();
      };
      if (timeoutMs && timeoutMs > 0) {
        timer = setTimeout(() => finish(() => onError(new Error('poll deadline exceeded'))), timeoutMs);
      }
      fn().then(
        (data) => finish(() => onData(data)),
        (err) => finish(() => onError(err)),
      );
    },
    invalidate() {
      epoch++;
      busy = false;
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
    epoch() {
      return epoch;
    },
  };
}
