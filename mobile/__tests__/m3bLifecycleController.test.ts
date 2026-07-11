// M3b remediation (BLOCKER 6): the production lifecycle FLOW is a testable unit
// (SessionLifecycleController) that FeedScreen drives directly — no test-only
// parallel component, no copied policy. These proofs exercise the real controller
// with mocked action fns + spy callbacks and pin every behavior the verifier
// required: confirmations→action mapping, duplicate taps, stale response/session
// switch, unmount/back with no request, 409 refresh, Stop failure → Force Kill
// reachability, Delete → onDeleted, and no client-invented state on failure.

import { SessionLifecycleController, classifyLifecycleError } from '../src/lib/sessionLifecycleController';
import { PokitError, ConnectivityFailure } from '../src/lib/client';
import { LifecycleResultError } from '../src/lib/lifecycle';

const SID = 'controlled_pty:shell-1';

function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: any) => void;
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

function makeController(overrides: Partial<{
  stop: jest.Mock; kill: jest.Mock; del: jest.Mock;
}> = {}) {
  const fns = {
    stop: overrides.stop || jest.fn(async () => ({ sessionId: SID, action: 'stop', state: 'exited' })),
    kill: overrides.kill || jest.fn(async () => ({ sessionId: SID, action: 'kill', state: 'killed' })),
    del: overrides.del || jest.fn(async () => ({ sessionId: SID, action: 'delete', state: 'exited' })),
  };
  const cbs = {
    onState: jest.fn(),
    onPending: jest.fn(),
    onError: jest.fn(),
    onRefresh: jest.fn(),
    onDeleted: jest.fn(),
  };
  const ctrl = new SessionLifecycleController(SID, 'TOK', fns as any, cbs);
  return { ctrl, fns, cbs };
}

describe('SessionLifecycleController — action mapping', () => {
  it('stop() → stop fn with (id, token); applies returned state; refreshes; no delete-nav', async () => {
    const { ctrl, fns, cbs } = makeController();
    await ctrl.stop();
    expect(fns.stop).toHaveBeenCalledWith(SID, 'TOK');
    expect(fns.kill).not.toHaveBeenCalled();
    expect(cbs.onState).toHaveBeenCalledWith('exited');
    expect(cbs.onRefresh).toHaveBeenCalledTimes(1);
    expect(cbs.onDeleted).not.toHaveBeenCalled();
    expect(cbs.onPending).toHaveBeenNthCalledWith(1, 'stop');
    expect(cbs.onPending).toHaveBeenLastCalledWith(null);
  });

  it('forceKill() → kill fn only', async () => {
    const { ctrl, fns, cbs } = makeController();
    await ctrl.forceKill();
    expect(fns.kill).toHaveBeenCalledWith(SID, 'TOK');
    expect(fns.stop).not.toHaveBeenCalled();
    expect(cbs.onState).toHaveBeenCalledWith('killed');
  });

  it('deleteHistory() success → del fn, then onDeleted (navigate back)', async () => {
    const { ctrl, fns, cbs } = makeController();
    await ctrl.deleteHistory();
    expect(fns.del).toHaveBeenCalledWith(SID, 'TOK');
    expect(cbs.onDeleted).toHaveBeenCalledTimes(1);
    expect(cbs.onRefresh).toHaveBeenCalledTimes(1);
  });
});

describe('SessionLifecycleController — guards', () => {
  it('duplicate tap: a second action while one is in flight is ignored', async () => {
    const d = deferred<any>();
    const stop = jest.fn(() => d.promise);
    const { ctrl, cbs } = makeController({ stop });
    const p1 = ctrl.stop();
    const p2 = ctrl.stop(); // ignored — one in flight
    d.resolve({ sessionId: SID, action: 'stop', state: 'exited' });
    await Promise.all([p1, p2]);
    expect(stop).toHaveBeenCalledTimes(1);
  });

  it('stale: a response after a session switch does not mutate the new session UI', async () => {
    const d = deferred<any>();
    const stop = jest.fn(() => d.promise);
    const { ctrl, cbs } = makeController({ stop });
    const p = ctrl.stop();
    ctrl.setSession('controlled_pty:other', 'TOK2'); // switch mid-flight
    d.resolve({ sessionId: SID, action: 'stop', state: 'exited' });
    await p;
    expect(cbs.onState).not.toHaveBeenCalled();
    expect(cbs.onRefresh).not.toHaveBeenCalled();
    expect(cbs.onDeleted).not.toHaveBeenCalled();
  });

  it('dispose() (Back/unmount) issues NO lifecycle request', () => {
    const { ctrl, fns } = makeController();
    ctrl.dispose();
    expect(fns.stop).not.toHaveBeenCalled();
    expect(fns.kill).not.toHaveBeenCalled();
    expect(fns.del).not.toHaveBeenCalled();
  });
});

describe('SessionLifecycleController — failures keep server authoritative', () => {
  it('409 delete → "Stop first" message + refresh, NO state change, NO nav', async () => {
    const del = jest.fn(async () => { throw new PokitError('conflict', ConnectivityFailure.APIError, 409); });
    const { ctrl, cbs } = makeController({ del });
    await ctrl.deleteHistory();
    expect(cbs.onError).toHaveBeenLastCalledWith(expect.stringContaining('Stop the session first'));
    expect(cbs.onRefresh).toHaveBeenCalledTimes(1); // refresh, not retry
    expect(del).toHaveBeenCalledTimes(1);
    expect(cbs.onState).not.toHaveBeenCalled();
    expect(cbs.onDeleted).not.toHaveBeenCalled();
  });

  it('Stop failure (500) → refresh so stopping/Force Kill re-surfaces; state unchanged', async () => {
    const stop = jest.fn(async () => { throw new PokitError('terminate failed', ConnectivityFailure.APIError, 500); });
    const { ctrl, cbs } = makeController({ stop });
    await ctrl.stop();
    expect(cbs.onRefresh).toHaveBeenCalledTimes(1); // authoritative re-sync → Force Kill reachable
    expect(cbs.onState).not.toHaveBeenCalled();     // never invents a terminal state
    expect(cbs.onError).toHaveBeenLastCalledWith(expect.stringContaining('Force Kill'));
  });

  it('full chain: Stop fails → (refresh) → user taps Force Kill → real kill request', async () => {
    const stop = jest.fn(async () => { throw new PokitError('terminate failed', ConnectivityFailure.APIError, 500); });
    const kill = jest.fn(async () => ({ sessionId: SID, action: 'kill', state: 'killed' }));
    const { ctrl, fns, cbs } = makeController({ stop, kill });
    // 1. graceful Stop does not confirm termination
    await ctrl.stop();
    expect(cbs.onRefresh).toHaveBeenCalledTimes(1);
    // 2. the pending guard is released, so a subsequent Force Kill is admitted
    expect(ctrl.pendingAction).toBeNull();
    // 3. user taps Force Kill → an actual /kill request is issued and applied
    await ctrl.forceKill();
    expect(fns.kill).toHaveBeenCalledWith(SID, 'TOK');
    expect(cbs.onState).toHaveBeenCalledWith('killed');
  });

  it('malformed/wrong-session response is not committed as success (no nav, no state)', async () => {
    const del = jest.fn(async () => { throw new LifecycleResultError('session mismatch'); });
    const { ctrl, cbs } = makeController({ del });
    await ctrl.deleteHistory();
    expect(cbs.onDeleted).not.toHaveBeenCalled();
    expect(cbs.onState).not.toHaveBeenCalled();
    expect(cbs.onError).toHaveBeenCalled();
  });

  it('403 is a distinct permission message (not re-auth)', async () => {
    const stop = jest.fn(async () => { throw new PokitError('forbidden', ConnectivityFailure.AuthError, 403); });
    const { ctrl, cbs } = makeController({ stop });
    await ctrl.stop();
    expect(cbs.onError).toHaveBeenLastCalledWith(expect.stringContaining('permission'));
    expect(cbs.onState).not.toHaveBeenCalled();
  });
});

describe('classifyLifecycleError — credential-free, actionable', () => {
  it('never includes a bearer or raw content', () => {
    const msg = classifyLifecycleError(new PokitError('SECRET_BEARER inside', ConnectivityFailure.AuthError, 401), 'stop');
    expect(msg).not.toContain('SECRET_BEARER');
  });
  it('maps 409/403/404/network distinctly', () => {
    expect(classifyLifecycleError(new PokitError('', ConnectivityFailure.APIError, 409), 'delete')).toContain('Stop the session first');
    expect(classifyLifecycleError(new PokitError('', ConnectivityFailure.AuthError, 403), 'kill')).toContain('permission');
    expect(classifyLifecycleError(new PokitError('', ConnectivityFailure.APIError, 404), 'delete')).toContain('not found');
    expect(classifyLifecycleError(new PokitError('', ConnectivityFailure.NetworkUnreachable, 0), 'stop')).toContain('Network error');
  });
});
