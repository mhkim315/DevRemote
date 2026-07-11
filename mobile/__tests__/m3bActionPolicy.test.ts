// M3b: the lifecycle action policy is a PURE reducer over declared capability +
// authoritative state + in-flight action. These proofs pin the plan's UI policy
// table exactly, prove fail-closed View-Only for unknown/missing capability, and
// prove the client NEVER invents a terminal state from a display hint.

import {
  readCapabilities, computeActionPolicy, reconcileState, stateFromActionResult,
  isTerminalState, type ManagedLifecycleState,
} from '../src/lib/lifecycle';

const MANAGED = { adapterCapabilities: ['observe', 'liveTerminal', 'reliableTranscript', 'input', 'control', 'managedLifecycle'] };
const EXTERNAL_TMUX = { adapterCapabilities: ['observe', 'liveTerminal', 'reliableTranscript', 'input', 'control'] };
const OBSERVE_ONLY_CMUX = { adapterCapabilities: ['observe', 'bestEffortTranscript'] };

describe('readCapabilities', () => {
  it('reads managedLifecycle + input independently for controlled_pty', () => {
    expect(readCapabilities(MANAGED)).toEqual({ managed: true, inputCapable: true });
  });
  it('external tmux is NOT managed but keeps input', () => {
    expect(readCapabilities(EXTERNAL_TMUX)).toEqual({ managed: false, inputCapable: true });
  });
  it('observe-only cmux is neither managed nor input-capable', () => {
    expect(readCapabilities(OBSERVE_ONLY_CMUX)).toEqual({ managed: false, inputCapable: false });
  });
  it('fails closed for missing/legacy/unknown capabilities', () => {
    expect(readCapabilities(undefined)).toEqual({ managed: false, inputCapable: false });
    expect(readCapabilities(null)).toEqual({ managed: false, inputCapable: false });
    expect(readCapabilities({})).toEqual({ managed: false, inputCapable: false });
    expect(readCapabilities({ adapterCapabilities: undefined })).toEqual({ managed: false, inputCapable: false });
  });
});

describe('computeActionPolicy — plan UI policy table (managed)', () => {
  const base = { managed: true, inputCapable: true, pending: null as any };

  it('starting: no Stop/Kill/Delete, input disabled', () => {
    const p = computeActionPolicy({ ...base, state: 'starting' });
    expect(p).toMatchObject({ canStop: false, canForceKill: false, canDelete: false, inputEnabled: false, viewOnly: false });
    expect(p.statusLabel).toBe('Starting…');
  });

  it('running: Stop shown, input enabled, Kill/Delete hidden', () => {
    const p = computeActionPolicy({ ...base, state: 'running' });
    expect(p).toMatchObject({ canStop: true, canForceKill: false, canDelete: false, inputEnabled: true });
    expect(p.statusLabel).toBe('Running');
  });

  it('stopping: Force Kill available, Stop hidden, input disabled', () => {
    const p = computeActionPolicy({ ...base, state: 'stopping' });
    expect(p).toMatchObject({ canStop: false, canForceKill: true, canDelete: false, inputEnabled: false });
    expect(p.statusLabel).toBe('Stopping…');
  });

  it.each<ManagedLifecycleState>(['exited', 'killed', 'failed'])('terminal %s: only Delete, input/Stop/Kill hidden', (state) => {
    const p = computeActionPolicy({ ...base, state });
    expect(p).toMatchObject({ canStop: false, canForceKill: false, canDelete: true, inputEnabled: false, terminal: true });
  });

  it('managed running WITHOUT input capability disables input but keeps Stop', () => {
    const p = computeActionPolicy({ managed: true, inputCapable: false, state: 'running', pending: null });
    expect(p).toMatchObject({ canStop: true, inputEnabled: false });
  });

  it('a pending action disables ALL destructive controls (duplicate-tap guard)', () => {
    expect(computeActionPolicy({ ...base, state: 'running', pending: 'stop' }).canStop).toBe(false);
    expect(computeActionPolicy({ ...base, state: 'stopping', pending: 'kill' }).canForceKill).toBe(false);
    expect(computeActionPolicy({ ...base, state: 'exited', pending: 'delete' }).canDelete).toBe(false);
  });
});

describe('computeActionPolicy — non-managed is View Only', () => {
  it('external tmux: no destructive controls, input retained', () => {
    const p = computeActionPolicy({ managed: false, inputCapable: true, state: 'unknown', pending: null });
    expect(p).toMatchObject({ canStop: false, canForceKill: false, canDelete: false, viewOnly: true, inputEnabled: true });
  });
  it('observe-only / unknown: View Only, no input, no destructive controls', () => {
    const p = computeActionPolicy({ managed: false, inputCapable: false, state: 'unknown', pending: null });
    expect(p).toMatchObject({ canStop: false, canForceKill: false, canDelete: false, viewOnly: true, inputEnabled: false });
    expect(p.statusLabel).toBe('View only');
  });
});

describe('reconcileState — monotonic, terminal-sticky, no invented terminal', () => {
  it('advances starting → running → stopping', () => {
    expect(reconcileState('starting', 'running')).toBe('running');
    expect(reconcileState('running', 'stopping')).toBe('stopping');
  });
  it('never regresses a stopping session back to running (stale live snapshot)', () => {
    expect(reconcileState('stopping', 'running')).toBe('stopping');
  });
  it('terminal is final — a later "running" snapshot cannot revive it', () => {
    expect(reconcileState('exited', 'running')).toBe('exited');
    expect(reconcileState('killed', 'stopping')).toBe('killed');
    expect(reconcileState('failed', 'running')).toBe('failed');
  });
  it('accepts an authoritative terminal transition from the action response', () => {
    expect(reconcileState('running', 'exited')).toBe('exited');
    expect(reconcileState('stopping', 'exited')).toBe('exited');
  });
});

describe('stateFromActionResult — never fabricates a terminal state', () => {
  it('maps real daemon states through', () => {
    expect(stateFromActionResult({ state: 'stopping' })).toBe('stopping');
    expect(stateFromActionResult({ state: 'exited' })).toBe('exited');
    expect(stateFromActionResult({ state: 'killed' })).toBe('killed');
  });
  it('an unrecognised/empty/missing state is unknown (fail closed), not terminal', () => {
    expect(stateFromActionResult({ state: 'bogus' })).toBe('unknown');
    expect(stateFromActionResult({})).toBe('unknown');
    expect(stateFromActionResult(null)).toBe('unknown');
    expect(isTerminalState(stateFromActionResult(null))).toBe(false);
  });
});
