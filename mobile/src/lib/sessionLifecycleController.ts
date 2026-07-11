// M3b (BLOCKER 6): the session lifecycle action FLOW extracted from FeedScreen
// into a plain, testable production unit. FeedScreen instantiates and drives this
// controller — there is NO test-only copy of the policy or a parallel component.
//
// The controller owns exactly the non-visual behavior that must be proven:
//   - one in-flight action at a time (duplicate-tap / concurrent guard);
//   - stale-response guard across a session switch / unmount;
//   - Stop→stop, Force Kill→kill, Delete History→delete action mapping;
//   - authoritative-state application from the validated response;
//   - refresh (not retry) on 409 and on a failed Stop so `stopping` re-surfaces
//     and Force Kill becomes reachable;
//   - Delete success → onDeleted (navigate back);
//   - dispose() on Back/unmount issues NO lifecycle request.
//
// The daemon remains the authoritative boundary; on failure the controller never
// changes the lifecycle state (it only reports a recoverable, credential-free
// error and refreshes).

import { PokitError, ConnectivityFailure } from './client';
import {
  LifecycleResultError,
  stateFromActionResult,
  type ManagedLifecycleState,
  type PendingAction,
  type LifecycleActionKind,
} from './lifecycle';

export interface LifecycleActionFns {
  stop: (id: string, token?: string) => Promise<{ state: string }>;
  kill: (id: string, token?: string) => Promise<{ state: string }>;
  del: (id: string, token?: string) => Promise<{ state: string }>;
}

export interface LifecycleCallbacks {
  // onState receives the daemon-derived next state; the caller reconciles it
  // monotonically against the current state (server-authoritative, terminal-sticky).
  onState: (next: ManagedLifecycleState) => void;
  onPending: (pending: PendingAction) => void;
  onError: (message: string) => void;
  onRefresh: () => void;   // re-fetch the authoritative session list
  onDeleted: () => void;   // Delete succeeded → navigate back
}

// classifyLifecycleError turns any thrown error into a recoverable, credential-free
// message. Never includes bearer/session content. Exported for direct testing.
export function classifyLifecycleError(e: unknown, action: LifecycleActionKind): string {
  if (e instanceof LifecycleResultError) {
    return 'Unexpected daemon response — refreshed to the authoritative state.';
  }
  if (e instanceof PokitError) {
    if (e.statusCode === 409) return 'Stop the session first, then delete its history.';
    if (e.statusCode === 403) return 'You do not have permission for this action.';
    if (e.statusCode === 404) return 'Session not found — it may already be gone.';
    if (e.failure === ConnectivityFailure.NetworkUnreachable || e.failure === ConnectivityFailure.Timeout) {
      return 'Network error — the session was not changed. Check your connection.';
    }
    if (e.failure === ConnectivityFailure.AuthError) return 'Authentication failed. Re-scan the QR code.';
  }
  if (action === 'stop') return 'Stop did not complete — you can Force Kill or retry.';
  return 'Action failed — the session state is unchanged.';
}

export class SessionLifecycleController {
  private pending: PendingAction = null;
  private sessionId: string;
  private token?: string;

  constructor(
    sessionId: string,
    token: string | undefined,
    private readonly fns: LifecycleActionFns,
    private readonly cbs: LifecycleCallbacks,
  ) {
    this.sessionId = sessionId;
    this.token = token;
  }

  // setSession rebinds the controller when the viewed session changes. Any
  // in-flight action for the previous session is dropped by the stale guard.
  setSession(sessionId: string, token?: string): void {
    this.sessionId = sessionId;
    this.token = token;
    this.pending = null;
  }

  get pendingAction(): PendingAction {
    return this.pending;
  }

  stop(): Promise<void> {
    return this.run('stop', this.fns.stop, false);
  }

  forceKill(): Promise<void> {
    return this.run('kill', this.fns.kill, false);
  }

  deleteHistory(): Promise<void> {
    return this.run('delete', this.fns.del, true);
  }

  // dispose is invoked on Back / unmount. It clears local state and issues NO
  // lifecycle request — a viewer detach never Stops/Kills/Deletes the process.
  dispose(): void {
    this.pending = null;
  }

  private async run(
    action: LifecycleActionKind,
    fn: (id: string, token?: string) => Promise<{ state: string }>,
    isDelete: boolean,
  ): Promise<void> {
    if (this.pending) return; // duplicate-tap / concurrent action guard
    const forId = this.sessionId;
    this.pending = action;
    this.cbs.onPending(action);
    this.cbs.onError('');
    try {
      const result = await fn(forId, this.token);
      if (this.sessionId !== forId) return; // stale: session switched mid-flight
      this.cbs.onState(stateFromActionResult(result));
      this.cbs.onRefresh();
      if (isDelete) this.cbs.onDeleted();
    } catch (e) {
      if (this.sessionId !== forId) return; // stale
      this.cbs.onError(classifyLifecycleError(e, action));
      // Re-sync from the authoritative list (409, failed Stop, etc.). Never a
      // blind retry, and never a client-invented state change on failure.
      this.cbs.onRefresh();
    } finally {
      if (this.sessionId === forId) {
        this.pending = null;
        this.cbs.onPending(null);
      } else {
        this.pending = null;
      }
    }
  }
}
