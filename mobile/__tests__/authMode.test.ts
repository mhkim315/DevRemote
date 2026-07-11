// AuthMode enforcement — tests the REAL production deriveTerminalAuth from
// FeedScreen.tsx, not a copied helper. Proves the switch is authoritative.

import { deriveTerminalAuth } from '../src/lib/authMode';

describe('deriveTerminalAuth (production function)', () => {
  it('paired_device → tokenMgr+baseURL, termURI=null (async ticket path)', () => {
    const r = deriveTerminalAuth({ mode: 'paired_device', tokenMgr: {} as any, baseURL: 'https://h' }, 's');
    expect(r.tokenMgr).toBeDefined();
    expect(r.baseURL).toBeDefined();
    expect(r.termURI).toBeNull();
  });

  it('explicit_local_dev → termURI non-null (legacy path)', () => {
    const r = deriveTerminalAuth({ mode: 'explicit_local_dev', legacyToken: 'dev-token' }, 's');
    expect(r.tokenMgr).toBeUndefined();
    expect(r.baseURL).toBeUndefined();
    expect(r.termURI).toContain('/term/?session=s&token=dev-token');
  });

  it('explicit_local_dev with legacyToken→termURI, without→null', () => {
    const r = deriveTerminalAuth({ mode: 'explicit_local_dev', legacyToken: 'dev-token' }, 's');
    expect(r.termURI).toContain('/term/?session=s&token=dev-token');
    // No legacyToken → fail closed (the session token param is gone).
    const r2 = deriveTerminalAuth({ mode: 'explicit_local_dev' }, 's');
    expect(r2.termURI).toBeNull();
  });

  it('initializing → null termURI, no credentials (fail-closed)', () => {
    const r = deriveTerminalAuth({ mode: 'initializing' }, 's');
    expect(r.termURI).toBeNull();
    expect(r.tokenMgr).toBeUndefined();
    expect(r.baseURL).toBeUndefined();
  });

  it('pairing_required → null termURI (no fallback to legacy)', () => {
    const r = deriveTerminalAuth({ mode: 'pairing_required' }, 's');
    expect(r.termURI).toBeNull();
  });

  it('failed → null termURI', () => {
    const r = deriveTerminalAuth({ mode: 'failed' }, 's');
    expect(r.termURI).toBeNull();
  });

  it('undefined authCtx → null termURI', () => {
    const r = deriveTerminalAuth(undefined, 's');
    expect(r.termURI).toBeNull();
  });

  it('paired_device with missing tokenMgr → null termURI (fail-closed)', () => {
    const r = deriveTerminalAuth({ mode: 'paired_device' } as any, 's');
    expect(r.termURI).toBeNull();
  });

  // Origin binding: paired_device with a non-HTTPS baseURL is still
  // structurally accepted by deriveTerminalAuth (the TM constructor in App
  // validates the URL). This is tested at the App boot level.
});
