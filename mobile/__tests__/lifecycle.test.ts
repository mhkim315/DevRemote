import { canCreateProfile, isRunnable, validateName, validateCwd } from '../src/lib/lifecycle';
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
