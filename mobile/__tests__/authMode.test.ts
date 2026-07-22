// AuthMode enforcement — tests the REAL production deriveTerminalAuth from
// FeedScreen.tsx, not a copied helper. Proves the switch is authoritative.

import { deriveTerminalAuth, selectAppRoute, canonicalLocalTestOrigin, canonicalOrigin } from '../src/lib/authMode';

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

// ── selectAppRoute enforcement ──

describe('selectAppRoute', () => {
  it('pairing_required + isConnected=true → pairing_required (fail-closed)', () => {
    const route = selectAppRoute(
      { mode: 'pairing_required' },
      { loading: false, session: false, isConnected: true },
    );
    expect(route).toBe('pairing_required');
  });

  it('pairing_required + isConnected=false → pairing_required', () => {
    const route = selectAppRoute(
      { mode: 'pairing_required' },
      { loading: false, session: false, isConnected: false },
    );
    expect(route).toBe('pairing_required');
  });

  it('paired_device + isConnected=true → product', () => {
    const route = selectAppRoute(
      { mode: 'paired_device', tokenMgr: {} as any, baseURL: 'https://x' },
      { loading: false, session: false, isConnected: true },
    );
    expect(route).toBe('product');
  });

  it('paired_device + isConnected=false → device_connect', () => {
    const route = selectAppRoute(
      { mode: 'paired_device', tokenMgr: {} as any, baseURL: 'https://x' },
      { loading: false, session: false, isConnected: false },
    );
    expect(route).toBe('device_connect');
  });
});

// ── Local E2E test origin validation ──

describe('canonicalLocalTestOrigin', () => {
  const mode = 'explicit_local_dev';

  // Positive: allowed
  it('allows http://localhost:9172 with explicit flag + correct mode', () => {
    const r = canonicalLocalTestOrigin('http://localhost:9172', true, mode);
    expect(r.error).toBeUndefined();
    expect(r.origin).toBe('http://localhost:9172');
  });

  it('allows http://127.0.0.1:9172 with explicit flag + correct mode', () => {
    const r = canonicalLocalTestOrigin('http://127.0.0.1:9172', true, mode);
    expect(r.error).toBeUndefined();
    expect(r.origin).toBe('http://127.0.0.1:9172');
  });

  it('allows http://localhost:8081 (Metro port)', () => {
    const r = canonicalLocalTestOrigin('http://localhost:8081', true, mode);
    expect(r.error).toBeUndefined();
    expect(r.origin).toBe('http://localhost:8081');
  });

  // Negative: missing flags
  it('rejects when localTestExplicit is false', () => {
    const r = canonicalLocalTestOrigin('http://localhost:9172', false, mode);
    expect(r.error).toContain('explicit opt-in');
  });

  it('rejects when originMode is not explicit_local_dev', () => {
    const r = canonicalLocalTestOrigin('http://localhost:9172', true, 'paired_device');
    expect(r.error).toContain('explicit_local_dev');
  });

  // Negative: wrong hosts
  it('rejects LAN IP (192.168.x.x)', () => {
    const r = canonicalLocalTestOrigin('http://192.168.219.100:9172', true, mode);
    expect(r.error).toContain('only permits localhost');
  });

  it('rejects emulator alias 10.0.2.2', () => {
    const r = canonicalLocalTestOrigin('http://10.0.2.2:9172', true, mode);
    expect(r.error).toContain('only permits localhost');
  });

  it('rejects arbitrary domain', () => {
    const r = canonicalLocalTestOrigin('http://example.com:9172', true, mode);
    expect(r.error).toContain('only permits localhost');
  });

  it('rejects term.fullcount.kr (production domain over HTTP)', () => {
    const r = canonicalLocalTestOrigin('http://term.fullcount.kr', true, mode);
    expect(r.error).toContain('only permits localhost');
  });

  // Negative: wrong scheme
  it('rejects HTTPS (production path — use canonicalOrigin instead)', () => {
    const r = canonicalLocalTestOrigin('https://localhost:9172', true, mode);
    expect(r.error).toContain('only supports HTTP');
  });

  // Negative: malformed
  it('rejects missing port', () => {
    const r = canonicalLocalTestOrigin('http://localhost', true, mode);
    expect(r.error).toContain('explicit port');
  });

  it('rejects credentials in URL', () => {
    const r = canonicalLocalTestOrigin('http://user:pass@localhost:9172', true, mode);
    expect(r.error).toContain('credentials');
  });

  it('rejects query string', () => {
    const r = canonicalLocalTestOrigin('http://localhost:9172?foo=bar', true, mode);
    expect(r.error).toContain('credentials');
  });

  it('rejects fragment', () => {
    const r = canonicalLocalTestOrigin('http://localhost:9172#hash', true, mode);
    expect(r.error).toContain('credentials');
  });

  it('rejects path', () => {
    const r = canonicalLocalTestOrigin('http://localhost:9172/api', true, mode);
    expect(r.error).toContain('path');
  });

  it('rejects malformed URL', () => {
    const r = canonicalLocalTestOrigin('not-a-url', true, mode);
    expect(r.error).toContain('invalid');
  });

  // Production https unchanged
  it('production canonicalOrigin still rejects HTTP', () => {
    const r = canonicalOrigin('http://localhost:9172');
    expect(r.error).toContain('HTTPS');
  });

  it('production canonicalOrigin still accepts HTTPS', () => {
    const r = canonicalOrigin('https://term.fullcount.kr');
    expect(r.error).toBeUndefined();
    expect(r.origin).toBe('https://term.fullcount.kr');
  });
});
