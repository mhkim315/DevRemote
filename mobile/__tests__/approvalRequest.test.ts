// A1 remediation (B6) — strict bounded decoder for the safe approval DTO. A
// malformed, foreign-session, unknown-state, or oversized record must never render
// a CTA; only a pending AND actionable record is actionable.

import {
  validateApproval, decodeApprovals, actionableApprovals, pendingDisplayApprovals,
  approvalActionable, APPROVAL_STATES,
} from '../src/lib/approvalRequest';
import { SafeApproval } from '../src/lib/client';

function validRaw(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: 'AP-1', sessionId: 'codex:s1', summary: 'Agent requested an approval',
    state: 'pending', actionable: true,
    options: [
      { id: 'approve', label: 'Approve', kind: 'approve', requiresInput: false },
      { id: 'reject', label: 'Reject', kind: 'reject', requiresInput: false },
    ],
    createdAt: '2026-07-14T08:00:00Z', expiresAt: '2026-07-14T08:05:00Z',
    ...over,
  };
}

describe('validateApproval — safe DTO fail-closed decode', () => {
  it('accepts a well-formed safe approval', () => {
    const a = validateApproval(validRaw()) as SafeApproval;
    expect(a).not.toBeNull();
    expect(a.id).toBe('AP-1');
    expect(a.options).toHaveLength(2);
    expect(a.actionable).toBe(true);
  });

  it('rejects unknown envelope fields (e.g. leaked raw prompt/payload)', () => {
    expect(validateApproval(validRaw({ prompt: 'raw provider prompt' }))).toBeNull();
    expect(validateApproval(validRaw({ payload: 'y\n' }))).toBeNull();
    expect(validateApproval(validRaw({ evil: 1 }))).toBeNull();
  });

  it('rejects a missing mandatory field', () => {
    const r = validRaw();
    delete (r as any).summary;
    expect(validateApproval(r)).toBeNull();
    const r2 = validRaw();
    delete (r2 as any).actionable;
    expect(validateApproval(r2)).toBeNull();
  });

  it('accepts every closed state and rejects others', () => {
    for (const s of APPROVAL_STATES) {
      expect(validateApproval(validRaw({ state: s }))).not.toBeNull();
    }
    for (const bad of ['executing', 'waiting_approval', 'bogus', '']) {
      expect(validateApproval(validRaw({ state: bad }))).toBeNull();
    }
  });

  it('enforces exact session binding', () => {
    expect(validateApproval(validRaw(), 'codex:s1')).not.toBeNull();
    expect(validateApproval(validRaw(), 'codex:OTHER')).toBeNull();
  });

  it('rejects an option with an out-of-vocabulary kind or unknown field', () => {
    expect(validateApproval(validRaw({ options: [{ id: 'x', label: 'X', kind: 'destroy', requiresInput: false }] }))).toBeNull();
    expect(validateApproval(validRaw({ options: [{ id: 'x', label: 'X', kind: 'approve', requiresInput: false, payload: 'y' }] }))).toBeNull();
    // missing requiresInput
    expect(validateApproval(validRaw({ options: [{ id: 'x', label: 'X', kind: 'approve' }] }))).toBeNull();
  });

  it('rejects out-of-bounds strings and oversized option arrays', () => {
    expect(validateApproval(validRaw({ summary: 'Z'.repeat(201) }))).toBeNull();
    const many = Array.from({ length: 9 }, (_, i) => ({ id: `o${i}`, label: 'x', kind: 'neutral', requiresInput: false }));
    expect(validateApproval(validRaw({ options: many }))).toBeNull();
  });

  it('requires strict RFC3339 timestamps', () => {
    expect(validateApproval(validRaw({ createdAt: '2026-07-14' }))).toBeNull();
    expect(validateApproval(validRaw({ expiresAt: 'not-a-time' }))).toBeNull();
    expect(validateApproval(validRaw({ createdAt: '2026-02-30T00:00:00Z' }))).toBeNull();
  });

  it('rejects a non-boolean actionable', () => {
    expect(validateApproval(validRaw({ actionable: 'yes' }))).toBeNull();
  });
});

describe('actionability filters', () => {
  it('approvalActionable requires pending AND actionable', () => {
    expect(approvalActionable(validateApproval(validRaw({ state: 'pending', actionable: true })) as SafeApproval)).toBe(true);
    expect(approvalActionable(validateApproval(validRaw({ state: 'pending', actionable: false })) as SafeApproval)).toBe(false);
    expect(approvalActionable(validateApproval(validRaw({ state: 'approved', actionable: true })) as SafeApproval)).toBe(false);
  });

  it('actionableApprovals drops non-actionable and foreign-session records', () => {
    const raw = [
      validRaw({ id: 'a1', actionable: true }),
      validRaw({ id: 'a2', actionable: false }), // intervention info, no CTA
      validRaw({ id: 'a3', sessionId: 'codex:OTHER' }), // foreign
      { garbage: true },
    ];
    expect(actionableApprovals(raw, 'codex:s1').map(a => a.id)).toEqual(['a1']);
  });

  it('pendingDisplayApprovals keeps non-actionable pending for display', () => {
    const raw = [
      validRaw({ id: 'a1', actionable: true }),
      validRaw({ id: 'a2', actionable: false }),
      validRaw({ id: 'a3', state: 'approved' }),
    ];
    expect(pendingDisplayApprovals(raw, 'codex:s1').map(a => a.id).sort()).toEqual(['a1', 'a2']);
  });

  it('non-array input yields empty', () => {
    expect(decodeApprovals(undefined, 'codex:s1')).toEqual([]);
    expect(actionableApprovals({}, 'codex:s1')).toEqual([]);
  });
});
