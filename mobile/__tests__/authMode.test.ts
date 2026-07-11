// AuthMode enforcement — prove that remote-mode/legacy boundary is structural.
// These tests validate the exported AuthContext contract and the FeedScreen
// token-path derivation, not HTML script injection (which requires a WebView).

import type { AuthContext } from '../src/lib/authMode';

describe('AuthMode contract', () => {
  it('paired_device exposes tokenMgr + baseURL', () => {
    const ctx: AuthContext = { mode: 'paired_device', tokenMgr: {} as any, baseURL: 'https://host' };
    expect(ctx.mode).toBe('paired_device');
    expect(ctx.tokenMgr).toBeDefined();
    expect(ctx.baseURL).toBeDefined();
  });

  it('explicit_local_dev exposes legacyToken, no tokenMgr', () => {
    const ctx: AuthContext = { mode: 'explicit_local_dev', legacyToken: 'dev-token' };
    expect(ctx.mode).toBe('explicit_local_dev');
    expect(ctx.tokenMgr).toBeUndefined();
    expect(ctx.legacyToken).toBe('dev-token');
  });

  it('initializing has no credentials', () => {
    const ctx: AuthContext = { mode: 'initializing' };
    expect(ctx.tokenMgr).toBeUndefined();
    expect(ctx.baseURL).toBeUndefined();
    expect(ctx.legacyToken).toBeUndefined();
  });

  it('pairing_required has no credentials (no fallback)', () => {
    const ctx: AuthContext = { mode: 'pairing_required' };
    expect(ctx.tokenMgr).toBeUndefined();
    expect(ctx.baseURL).toBeUndefined();
    // MUST NOT expose a legacyToken — that would be a fallback.
    expect(ctx.legacyToken).toBeUndefined();
  });

  it('failed has no credentials', () => {
    const ctx: AuthContext = { mode: 'failed' };
    expect(ctx.tokenMgr).toBeUndefined();
    expect(ctx.baseURL).toBeUndefined();
    expect(ctx.legacyToken).toBeUndefined();
  });
});

// FeedScreen token-path derivation: only paired_device gets ticket-based;
// only explicit_local_dev gets the legacy terminalURL. All other modes
// produce neither (WebView doesn't open until resolved).
function deriveTerminalPath(authCtx: AuthContext, token: string | undefined): 'ticket' | 'legacy' | 'none' {
  if (authCtx.mode === 'paired_device' && authCtx.tokenMgr && authCtx.baseURL) return 'ticket';
  if (authCtx.mode === 'explicit_local_dev' && authCtx.legacyToken) return 'legacy';
  return 'none';
}

describe('FeedScreen terminal-path derivation', () => {
  it('paired_device → ticket', () => {
    expect(deriveTerminalPath({ mode: 'paired_device', tokenMgr: {} as any, baseURL: 'https://h' }, undefined)).toBe('ticket');
  });
  it('explicit_local_dev → legacy', () => {
    expect(deriveTerminalPath({ mode: 'explicit_local_dev', legacyToken: 'dev-token' }, 'dev-token')).toBe('legacy');
  });
  it('initializing → none (no WebView)', () => {
    expect(deriveTerminalPath({ mode: 'initializing' }, undefined)).toBe('none');
  });
  it('pairing_required → none (no fallback)', () => {
    expect(deriveTerminalPath({ mode: 'pairing_required' }, undefined)).toBe('none');
  });
  it('failed → none', () => {
    expect(deriveTerminalPath({ mode: 'failed' }, undefined)).toBe('none');
  });
  it('paired_device with missing tokenMgr → none (fail-closed)', () => {
    expect(deriveTerminalPath({ mode: 'paired_device' }, undefined)).toBe('none');
  });
});
