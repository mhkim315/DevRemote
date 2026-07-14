// S1-D: the mobile agent-activity dimension is strictly-validated, display-only,
// and never a peer of the legacy heuristic. These proofs exercise the ACTUAL
// functions AgentCard imports (deriveCardActivity → validateAgentActivity +
// activityDisplay), so they cover the production render decision.

import {
  validateAgentActivity, activityDisplay, deriveCardActivity, sessionNeedsApproval,
  isConnectionStale, ACTIVITY_CONTRACT_VERSION,
} from '../src/lib/agentActivity';
import { computeActionPolicy } from '../src/lib/lifecycle';

const VALID = {
  contractVersion: ACTIVITY_CONTRACT_VERSION,
  status: 'working', provenance: 'native_log', confidence: 0.9,
  degraded: false, observedAt: '2026-07-13T00:00:00Z', stale: false,
};

describe('validateAgentActivity — fail closed', () => {
  it('accepts a complete well-formed DTO', () => {
    expect(validateAgentActivity(VALID)).not.toBeNull();
  });

  it('uses the frozen T0 contract version and rejects any other', () => {
    expect(ACTIVITY_CONTRACT_VERSION).toBe('t0.1');
    expect(validateAgentActivity({ ...VALID, contractVersion: 's1.1' })).toBeNull();
  });

  it('rejects a DTO missing ANY mandatory field (no defaults)', () => {
    for (const k of ['contractVersion', 'status', 'provenance', 'confidence', 'degraded', 'observedAt', 'stale']) {
      const clone: Record<string, unknown> = { ...VALID };
      delete clone[k];
      expect(validateAgentActivity(clone)).toBeNull();
    }
  });

  it('rejects non-objects and unknown fields', () => {
    for (const bad of [null, undefined, 'x', 3, true, []]) {
      expect(validateAgentActivity(bad as unknown)).toBeNull();
    }
    expect(validateAgentActivity({ ...VALID, evil: 'x' })).toBeNull();
  });

  it('enforces closed status and provenance vocabularies (empty provenance rejected)', () => {
    expect(validateAgentActivity({ ...VALID, status: 'orchestrator_thought' })).toBeNull();
    expect(validateAgentActivity({ ...VALID, provenance: 'made_up' })).toBeNull();
    expect(validateAgentActivity({ ...VALID, provenance: '' })).toBeNull(); // '' not in closed vocab
    for (const s of ['unknown', 'idle', 'thinking', 'working', 'waiting_approval',
      'waiting_input', 'completed', 'failed', 'interrupted', 'degraded']) {
      expect(validateAgentActivity({ ...VALID, status: s })).not.toBeNull();
    }
    for (const p of ['runtime', 'provider_protocol', 'provider_hook', 'native_log',
      'pty_structural', 'heuristic', 'prompt_hint', 'unknown']) {
      expect(validateAgentActivity({ ...VALID, provenance: p })).not.toBeNull();
    }
  });

  it('requires finite confidence in [0,1]', () => {
    for (const c of [-0.1, 1.1, NaN, Infinity, '0.5']) {
      expect(validateAgentActivity({ ...VALID, confidence: c })).toBeNull();
    }
  });

  it('requires bounded, calendar-valid, strict RFC3339 timestamps', () => {
    // date-only / no timezone / junk
    expect(validateAgentActivity({ ...VALID, observedAt: '2026-07-13' })).toBeNull();
    expect(validateAgentActivity({ ...VALID, observedAt: '2026-07-13T00:00:00' })).toBeNull();
    expect(validateAgentActivity({ ...VALID, observedAt: 'not-a-date' })).toBeNull();
    // oversized fractional seconds (pathological payload)
    expect(validateAgentActivity({ ...VALID, observedAt: '2026-07-13T00:00:00.' + '9'.repeat(100) + 'Z' })).toBeNull();
    // calendar-impossible dates that Date.parse would silently normalize
    expect(validateAgentActivity({ ...VALID, observedAt: '2026-02-30T00:00:00Z' })).toBeNull();
    expect(validateAgentActivity({ ...VALID, observedAt: '2026-02-29T00:00:00Z' })).toBeNull(); // 2026 not leap
    expect(validateAgentActivity({ ...VALID, observedAt: '2026-13-01T00:00:00Z' })).toBeNull();
    expect(validateAgentActivity({ ...VALID, observedAt: '2026-07-13T25:00:00Z' })).toBeNull();
    // valid UTC and offset forms, incl. a real leap day
    expect(validateAgentActivity({ ...VALID, observedAt: '2026-07-13T00:00:00Z' })).not.toBeNull();
    expect(validateAgentActivity({ ...VALID, observedAt: '2026-07-13T00:00:00+09:00' })).not.toBeNull();
    expect(validateAgentActivity({ ...VALID, observedAt: '2026-07-13T00:00:00.123Z' })).not.toBeNull();
    expect(validateAgentActivity({ ...VALID, observedAt: '2024-02-29T12:34:56Z' })).not.toBeNull(); // leap year
  });

  it('rejects non-boolean degraded/stale', () => {
    expect(validateAgentActivity({ ...VALID, degraded: 'yes' })).toBeNull();
    expect(validateAgentActivity({ ...VALID, stale: 1 })).toBeNull();
  });
});

describe('activityDisplay — stale/degraded NEVER shown as an active status', () => {
  it('fresh → the active status name, current', () => {
    const d = activityDisplay(validateAgentActivity(VALID)!);
    expect(d.current).toBe(true);
    expect(d.label).toBe('Working');
  });

  it('stale → "Stale activity", not current, never "Working"', () => {
    const d = activityDisplay(validateAgentActivity({ ...VALID, stale: true })!);
    expect(d.current).toBe(false);
    expect(d.label).toBe('Stale activity');
    expect(d.label).not.toContain('Working');
  });

  it('degraded → "Degraded activity", not current, never "Working"', () => {
    const d = activityDisplay(validateAgentActivity({ ...VALID, degraded: true })!);
    expect(d.current).toBe(false);
    expect(d.label).toBe('Degraded activity');
  });
});

describe('deriveCardActivity — accepted DTO governs; no legacy peer/fallback', () => {
  it('valid DTO present → activity shown, legacy suppressed', () => {
    const c = deriveCardActivity({ agentActivity: VALID, agentStatus: 'working', agentKind: 'codex' });
    expect(c.activity).not.toBeNull();
    expect(c.showLegacyStatus).toBe(false);
    expect(c.malformed).toBe(false);
    expect(c.unavailable).toBe(false);
  });

  it('malformed DTO present → NO activity and NO legacy fallback', () => {
    const c = deriveCardActivity({ agentActivity: { ...VALID, contractVersion: 'bad' }, agentStatus: 'working', agentKind: 'codex' });
    expect(c.activity).toBeNull();
    expect(c.malformed).toBe(true);
    expect(c.showLegacyStatus).toBe(false);
  });

  it('no DTO + non-accepted session → legacy heuristic may show', () => {
    const c = deriveCardActivity({ agentStatus: 'working', agentKind: 'gemini' });
    expect(c.activity).toBeNull();
    expect(c.showLegacyStatus).toBe(true);
    expect(c.unavailable).toBe(false);
  });

  it('no DTO + accepted session (e.g. after restart) → Unavailable, NO legacy fallback', () => {
    for (const kind of ['codex', 'claude']) {
      const c = deriveCardActivity({ agentStatus: 'working', agentKind: kind });
      expect(c.activity).toBeNull();
      expect(c.unavailable).toBe(true);
      expect(c.showLegacyStatus).toBe(false); // heuristic never becomes accepted authority
    }
  });

  it('stale DTO → activity is non-current', () => {
    const c = deriveCardActivity({ agentActivity: { ...VALID, stale: true } });
    expect(c.activity!.current).toBe(false);
    expect(c.activity!.label).toBe('Stale activity');
  });

  it('connection stale → a fresh DTO is forced non-current (never live "Working")', () => {
    const fresh = deriveCardActivity({ agentActivity: VALID }, { connectionStale: false });
    expect(fresh.activity!.current).toBe(true);
    const stale = deriveCardActivity({ agentActivity: VALID }, { connectionStale: true });
    expect(stale.activity!.current).toBe(false);
    expect(stale.activity!.label).toBe('Stale activity');
  });
});

describe('isConnectionStale — poll failure or freshness expiry', () => {
  it('a failed last poll is stale', () => {
    expect(isConnectionStale('Cannot reach daemon.', 1000, 1500, 6000)).toBe(true);
  });
  it('a recent success with no error is fresh', () => {
    expect(isConnectionStale('', 1000, 2000, 6000)).toBe(false);
  });
  it('an expired last-success is stale even without an error', () => {
    expect(isConnectionStale('', 1000, 1000 + 7000, 6000)).toBe(true);
  });
  it('no success yet and no error is not stale', () => {
    expect(isConnectionStale('', null, 5000, 6000)).toBe(false);
  });
});

describe('agent activity NEVER drives approval or lifecycle actions', () => {
  it('approval CTA reads pending approvals only', () => {
    expect(sessionNeedsApproval([])).toBe(false);
    expect(sessionNeedsApproval([{ state: 'approved' }])).toBe(false);
    expect(sessionNeedsApproval([{ state: 'pending' }])).toBe(true);
  });

  it('lifecycle controls derive from lifecycle state only', () => {
    const exited = computeActionPolicy({ managed: true, inputCapable: true, state: 'exited', pending: null });
    expect(exited.canStop).toBe(false);
    expect(exited.inputEnabled).toBe(false);
    const running = computeActionPolicy({ managed: true, inputCapable: true, state: 'running', pending: null });
    expect(running.canStop).toBe(true);
  });
});
