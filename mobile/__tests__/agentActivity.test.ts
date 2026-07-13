// S1-D: the mobile agent-activity dimension is a strictly-validated, display-only
// projection. These proofs pin: strict DTO validation (contract version / closed
// vocabulary / bounds / unknown-field / fail-closed), stale/degraded never shown as
// current, and that the approval CTA and lifecycle controls never derive from agent
// activity. They exercise the ACTUAL functions AgentCard imports (validateAgentActivity
// + activityDisplay), so they cover the production renderer's decision logic.

import {
  validateAgentActivity, activityDisplay, sessionNeedsApproval,
  ACTIVITY_CONTRACT_VERSION, type AgentActivity,
} from '../src/lib/agentActivity';
import { computeActionPolicy } from '../src/lib/lifecycle';

const VALID = {
  contractVersion: ACTIVITY_CONTRACT_VERSION,
  status: 'working', provenance: 'native_log', confidence: 0.9,
  degraded: false, observedAt: '2026-07-13T00:00:00Z', stale: false,
};

describe('validateAgentActivity — strict version / vocab / bounds / fail-closed', () => {
  it('accepts a well-formed DTO', () => {
    const a = validateAgentActivity(VALID);
    expect(a).not.toBeNull();
    expect(a!.status).toBe('working');
    expect(a!.provenance).toBe('native_log');
    expect(a!.confidence).toBeCloseTo(0.9);
  });

  it('rejects a wrong/missing contract version', () => {
    expect(validateAgentActivity({ ...VALID, contractVersion: 's9.9' })).toBeNull();
    const { contractVersion, ...noVer } = VALID;
    expect(validateAgentActivity(noVer)).toBeNull();
  });

  it('rejects non-objects', () => {
    for (const bad of [null, undefined, 'x', 3, true, []]) {
      expect(validateAgentActivity(bad as unknown)).toBeNull();
    }
  });

  it('rejects unknown envelope fields', () => {
    expect(validateAgentActivity({ ...VALID, evil: 'x' })).toBeNull();
  });

  it('rejects a status outside the closed vocabulary', () => {
    expect(validateAgentActivity({ ...VALID, status: 'orchestrator_thought' })).toBeNull();
    expect(validateAgentActivity({ ...VALID, status: '' })).toBeNull();
    expect(validateAgentActivity({ ...VALID, status: 123 })).toBeNull();
    // every known status is accepted.
    for (const s of ['unknown', 'idle', 'thinking', 'working', 'waiting_approval',
      'waiting_input', 'completed', 'failed', 'interrupted', 'degraded']) {
      expect(validateAgentActivity({ ...VALID, status: s })).not.toBeNull();
    }
  });

  it('rejects a provenance outside the closed vocabulary', () => {
    expect(validateAgentActivity({ ...VALID, provenance: 'made_up' })).toBeNull();
    expect(validateAgentActivity({ ...VALID, provenance: '' })).not.toBeNull(); // empty allowed
  });

  it('rejects out-of-range / non-finite / non-number confidence', () => {
    for (const c of [-0.1, 1.1, NaN, Infinity, '0.5']) {
      expect(validateAgentActivity({ ...VALID, confidence: c })).toBeNull();
    }
  });

  it('rejects non-boolean degraded/stale', () => {
    expect(validateAgentActivity({ ...VALID, degraded: 'yes' })).toBeNull();
    expect(validateAgentActivity({ ...VALID, stale: 1 })).toBeNull();
  });

  it('rejects an unparseable or oversized observedAt', () => {
    expect(validateAgentActivity({ ...VALID, observedAt: 'not-a-date' })).toBeNull();
    expect(validateAgentActivity({ ...VALID, observedAt: 'x'.repeat(41) })).toBeNull();
  });
});

describe('activityDisplay — stale/degraded never shown as current (the AgentCard label)', () => {
  const base: AgentActivity = validateAgentActivity(VALID)!;

  it('a fresh record is current', () => {
    const d = activityDisplay(base);
    expect(d.current).toBe(true);
    expect(d.label).toBe('Working');
  });

  it('a stale record is flagged and NOT current', () => {
    const d = activityDisplay({ ...base, stale: true });
    expect(d.current).toBe(false);
    expect(d.stale).toBe(true);
    expect(d.label).toContain('stale');
  });

  it('a degraded record is flagged and NOT current', () => {
    const d = activityDisplay({ ...base, degraded: true });
    expect(d.current).toBe(false);
    expect(d.label).toContain('degraded');
  });

  it('AgentCard render decision: validate → display end-to-end', () => {
    // This composes exactly what AgentCard does: validate the raw DTO then display.
    const fresh = validateAgentActivity({ ...VALID, status: 'thinking' });
    expect(fresh).not.toBeNull();
    expect(activityDisplay(fresh!).current).toBe(true);

    const staleRaw = { ...VALID, status: 'working', stale: true };
    const view = activityDisplay(validateAgentActivity(staleRaw)!);
    expect(view.current).toBe(false); // muted, never rendered as live activity
    expect(view.label).toContain('stale');

    // A malformed DTO yields null → AgentCard renders no activity line at all.
    expect(validateAgentActivity({ ...VALID, contractVersion: 'bad' })).toBeNull();
  });
});

describe('agent activity NEVER drives approval or lifecycle actions', () => {
  it('the approval CTA reads pending approvals only — not agent activity', () => {
    expect(sessionNeedsApproval([])).toBe(false);
    expect(sessionNeedsApproval(undefined)).toBe(false);
    expect(sessionNeedsApproval([{ status: 'approved' }])).toBe(false);
    expect(sessionNeedsApproval([{ status: 'pending' }])).toBe(true);
  });

  it('lifecycle controls derive from lifecycle state only, for EVERY activity status', () => {
    // computeActionPolicy has no agentActivity input; action visibility is identical
    // regardless of activity. A terminal lifecycle yields no stop/input; a running one
    // enables stop — agent activity cannot change either.
    const exited = computeActionPolicy({ managed: true, inputCapable: true, state: 'exited', pending: null });
    expect(exited.canStop).toBe(false);
    expect(exited.inputEnabled).toBe(false);
    expect(exited.canDelete).toBe(true);

    const running = computeActionPolicy({ managed: true, inputCapable: true, state: 'running', pending: null });
    expect(running.canStop).toBe(true);
    expect(running.inputEnabled).toBe(true);
  });
});
