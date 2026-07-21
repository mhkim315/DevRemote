import { canCreateProfile, isRunnable, validateName, validateCwd, readCapabilities, computeActionPolicy } from '../src/lib/lifecycle';
import type { ActionPolicyInput } from '../src/lib/lifecycle';
import type { SessionLifecycle } from '../src/lib/client';

describe('canCreateProfile', () => {
  it('allows an available profile', () => {
    expect(canCreateProfile({ id: 'shell', label: 'Shell', available: true })).toBe(true);
  });
  it('blocks an unavailable profile', () => {
    expect(canCreateProfile({ id: 'codex', label: 'Codex', available: false })).toBe(false);
  });
});

describe('isRunnable (response validation gate)', () => {
  const ok: SessionLifecycle = { id: 'controlled_pty:shell-1', adapter: 'controlled_pty', state: 'running' };
  it('accepts a running controlled_pty with a canonical id', () => {
    expect(isRunnable(ok)).toBe(true);
  });
  it('rejects a missing/empty id', () => {
    expect(isRunnable({ ...ok, id: '' })).toBe(false);
    expect(isRunnable({ adapter: 'controlled_pty', state: 'running' } as any)).toBe(false);
  });
  it('rejects a non-controlled_pty adapter', () => {
    expect(isRunnable({ ...ok, adapter: 'observe' })).toBe(false);
  });
  it('rejects a non-running state', () => {
    for (const state of ['starting', 'stopping', 'exited', 'killed', 'failed', 'idle']) {
      expect(isRunnable({ ...ok, state })).toBe(false);
    }
  });
  it('rejects null/undefined', () => {
    expect(isRunnable(null)).toBe(false);
    expect(isRunnable(undefined)).toBe(false);
  });
});

describe('friendly field validation', () => {
  it('treats empty name/cwd as acceptable (daemon defaults)', () => {
    expect(validateName('')).toBeNull();
    expect(validateCwd('')).toBeNull();
  });
  it('rejects a relative cwd and control chars', () => {
    expect(validateCwd('relative/dir')).not.toBeNull();
    expect(validateCwd('/ok/path')).toBeNull();
    expect(validateName('bad\nname')).not.toBeNull();
  });
});

// ── PB.7 Input-A: effective permission and read-only ──

describe('Input-A: readCapabilities fails closed', () => {
  it('null/undefined → no managed, no input', () => {
    expect(readCapabilities(null)).toEqual({ managed: false, inputCapable: false });
    expect(readCapabilities(undefined)).toEqual({ managed: false, inputCapable: false });
  });

  it('missing adapterCapabilities → no input', () => {
    expect(readCapabilities({})).toEqual({ managed: false, inputCapable: false });
  });

  it('empty adapterCapabilities → no input', () => {
    expect(readCapabilities({ adapterCapabilities: [] })).toEqual({ managed: false, inputCapable: false });
  });

  it('managedLifecycle without input → managed but no input', () => {
    expect(readCapabilities({ adapterCapabilities: ['managedLifecycle'] }))
      .toEqual({ managed: true, inputCapable: false });
  });

  it('input present → inputCapable true', () => {
    expect(readCapabilities({ adapterCapabilities: ['managedLifecycle', 'input'] }))
      .toEqual({ managed: true, inputCapable: true });
  });

  it('input without managed → inputCapable true, not managed', () => {
    expect(readCapabilities({ adapterCapabilities: ['input'] }))
      .toEqual({ managed: false, inputCapable: true });
  });
});

describe('Input-A: read-only when inputCapable is false', () => {
  const runningManaged: ActionPolicyInput = {
    managed: true, inputCapable: false, state: 'running', pending: null,
  };

  it('inputEnabled false when inputCapable is false', () => {
    const p = computeActionPolicy(runningManaged);
    expect(p.inputEnabled).toBe(false);
  });

  it('viewOnly false for managed (lifecycle controls shown)', () => {
    const p = computeActionPolicy(runningManaged);
    expect(p.viewOnly).toBe(false);
  });

  it('inputEnabled false for all states when inputCapable is false', () => {
    const states = ['starting', 'running', 'stopping', 'exited', 'killed', 'failed', 'unknown'] as const;
    for (const s of states) {
      const p = computeActionPolicy({ ...runningManaged, state: s });
      expect(p.inputEnabled).toBe(false);
    }
  });
});

describe('Input-A: input enabled only when running + inputCapable', () => {
  const ok: ActionPolicyInput = {
    managed: true, inputCapable: true, state: 'running', pending: null,
  };

  it('input enabled when managed + inputCapable + running', () => {
    expect(computeActionPolicy(ok).inputEnabled).toBe(true);
  });

  it('input disabled when stopping', () => {
    expect(computeActionPolicy({ ...ok, state: 'stopping' }).inputEnabled).toBe(false);
  });

  it('input disabled when terminal', () => {
    for (const s of ['exited', 'killed', 'failed'] as const) {
      expect(computeActionPolicy({ ...ok, state: s }).inputEnabled).toBe(false);
    }
  });

  it('input remains enabled during pending action (keyboard not blocked)', () => {
    // Pending stop/kill/delete blocks lifecycle buttons, not keyboard input.
    expect(computeActionPolicy({ ...ok, pending: 'stop' }).inputEnabled).toBe(true);
  });
});

describe('Input-A: two-layer input gate', () => {
  // Layer 1 — sessionCanInput: adapterCapabilities includes 'input'.
  //   Tested via readCapabilities + computeActionPolicy (pure, tested).
  // Layer 2 — deviceCanInput is the server capability snapshot. The
  // production FeedScreen render test covers the actual UI guard.
  //
  // Layer 1 tests:
  it('sessionCanInput: inputCapable false → inputEnabled false', () => {
    const caps = readCapabilities({ adapterCapabilities: ['liveTerminal'] });
    expect(caps.inputCapable).toBe(false);
    const p = computeActionPolicy({ managed: false, inputCapable: false, state: 'unknown', pending: null });
    expect(p.inputEnabled).toBe(false);
  });

  it('sessionCanInput: inputCapable true → actionPolicy allows', () => {
    const caps = readCapabilities({ adapterCapabilities: ['managedLifecycle', 'input'] });
    expect(caps.inputCapable).toBe(true);
    const p = computeActionPolicy({ managed: true, inputCapable: true, state: 'running', pending: null });
    expect(p.inputEnabled).toBe(true); // session layer permits
  });

  it('sessionCanInput false remains disabled even with an authorized device', () => {
    const caps = readCapabilities({ adapterCapabilities: [] });
    expect(caps.inputCapable).toBe(false);
    const deviceCanInput = true;
    expect(caps.inputCapable && deviceCanInput).toBe(false);
  });
});
