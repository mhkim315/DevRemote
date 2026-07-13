// S1-D (D7): a request guard binds each async response to a monotonically
// increasing generation and to the mounted/active state. A response is committed
// only when it belongs to the LATEST request AND the guard has not been cancelled
// (unmounted / view switched). This prevents a previous request from rendering
// after a session switch or after the component unmounts.

export interface RequestGuard {
  /** begin a new request; returns its generation token. */
  begin(): number;
  /** true only for the latest token while the guard is active (mounted). */
  isCurrent(token: number): boolean;
  /** cancel the guard (on unmount); no token is current afterwards. */
  cancel(): void;
}

export function createRequestGuard(): RequestGuard {
  let gen = 0;
  let active = true;
  return {
    begin() {
      return ++gen;
    },
    isCurrent(token) {
      return active && token === gen;
    },
    cancel() {
      active = false;
    },
  };
}
