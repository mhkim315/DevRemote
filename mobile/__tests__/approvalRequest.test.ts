// A1-E — strict bounded approval-DTO decoder. The daemon's approvals array is
// UNTRUSTED; a malformed, foreign-session, unknown-status, or oversized record
// must never render an actionable CTA. These prove the decoder fails closed and
// mirrors the frozen closed vocabularies.

import {
  validateApproval, decodeApprovals, pendingApprovals, approvalActionable,
  APPROVAL_STATUSES,
} from '../src/lib/approvalRequest';

function validRaw(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: 'AP-1', sessionId: 'codex:s1', agentKind: 'codex', kind: 'approval',
    status: 'pending', prompt: 'approve this?',
    options: [
      { id: 'approve', label: 'Approve', kind: 'approve' },
      { id: 'reject', label: 'Reject', kind: 'reject' },
    ],
    default: 'reject', source: 'jsonl', confidence: 0.9,
    createdAt: '2026-07-14T08:00:00Z',
    ...over,
  };
}

describe('validateApproval — fail-closed strict decode', () => {
  it('accepts a well-formed approval', () => {
    const a = validateApproval(validRaw());
    expect(a).not.toBeNull();
    expect(a!.id).toBe('AP-1');
    expect(a!.options).toHaveLength(2);
  });

  it('rejects unknown envelope fields', () => {
    expect(validateApproval(validRaw({ evil: 'x' }))).toBeNull();
  });

  it('rejects a missing mandatory field', () => {
    const r = validRaw();
    delete (r as any).prompt;
    expect(validateApproval(r)).toBeNull();
  });

  it('accepts every status in the closed set and rejects others', () => {
    for (const s of APPROVAL_STATUSES) {
      expect(validateApproval(validRaw({ status: s }))).not.toBeNull();
    }
    for (const bad of ['executing', 'thinking', 'waiting_approval', 'bogus', '']) {
      expect(validateApproval(validRaw({ status: bad }))).toBeNull();
    }
  });

  it('enforces exact session binding when expected', () => {
    expect(validateApproval(validRaw(), 'codex:s1')).not.toBeNull();
    expect(validateApproval(validRaw(), 'codex:OTHER')).toBeNull();
    expect(validateApproval(validRaw({ sessionId: '' }))).toBeNull();
  });

  it('rejects an option with an out-of-vocabulary kind', () => {
    expect(validateApproval(validRaw({ options: [{ id: 'x', label: 'X', kind: 'destroy' }] }))).toBeNull();
  });

  it('rejects an option with an unknown field or empty id', () => {
    expect(validateApproval(validRaw({ options: [{ id: 'a', label: 'A', kind: 'approve', evil: 1 }] }))).toBeNull();
    expect(validateApproval(validRaw({ options: [{ id: '', label: 'A', kind: 'approve' }] }))).toBeNull();
  });

  it('validates the optional input schema strictly', () => {
    expect(validateApproval(validRaw({
      options: [{ id: 'send', label: 'Send', kind: 'neutral', input: { required: true, placeholder: 'x', multiline: false } }],
    }))).not.toBeNull();
    // unknown input field
    expect(validateApproval(validRaw({
      options: [{ id: 'send', label: 'Send', kind: 'neutral', input: { required: true, evil: 1 } }],
    }))).toBeNull();
    // required not a boolean
    expect(validateApproval(validRaw({
      options: [{ id: 'send', label: 'Send', kind: 'neutral', input: { required: 'yes' } }],
    }))).toBeNull();
  });

  it('rejects out-of-bounds strings and oversized option arrays', () => {
    expect(validateApproval(validRaw({ id: 'A'.repeat(257) }))).toBeNull();
    expect(validateApproval(validRaw({ prompt: 'A'.repeat(4097) }))).toBeNull();
    const many = Array.from({ length: 33 }, (_, i) => ({ id: `o${i}`, label: 'x', kind: 'neutral' }));
    expect(validateApproval(validRaw({ options: many }))).toBeNull();
  });

  it('rejects a non-finite / out-of-range confidence', () => {
    expect(validateApproval(validRaw({ confidence: 1.5 }))).toBeNull();
    expect(validateApproval(validRaw({ confidence: -0.1 }))).toBeNull();
    expect(validateApproval(validRaw({ confidence: NaN }))).toBeNull();
    expect(validateApproval(validRaw({ confidence: '0.9' }))).toBeNull();
  });

  it('requires a strict RFC3339 createdAt and validates optional resolvedAt', () => {
    expect(validateApproval(validRaw({ createdAt: '2026-07-14' }))).toBeNull(); // date-only
    expect(validateApproval(validRaw({ createdAt: '2026-02-30T00:00:00Z' }))).toBeNull(); // impossible date
    expect(validateApproval(validRaw({ resolvedAt: 'not-a-time' }))).toBeNull();
    expect(validateApproval(validRaw({ resolvedAt: '2026-07-14T09:00:00Z' }))).not.toBeNull();
  });

  it('rejects non-object input', () => {
    expect(validateApproval(null)).toBeNull();
    expect(validateApproval('x')).toBeNull();
    expect(validateApproval(42)).toBeNull();
  });
});

describe('decodeApprovals / pendingApprovals / approvalActionable', () => {
  it('drops invalid and foreign-session records, keeps valid ones', () => {
    const raw = [
      validRaw({ id: 'a1' }),
      validRaw({ id: 'a2', sessionId: 'codex:OTHER' }), // foreign session
      { garbage: true },
      validRaw({ id: 'a3', status: 'bogus' }), // bad status
    ];
    const out = decodeApprovals(raw, 'codex:s1');
    expect(out.map(a => a.id)).toEqual(['a1']);
  });

  it('non-array input yields empty', () => {
    expect(decodeApprovals(undefined, 'codex:s1')).toEqual([]);
    expect(decodeApprovals({}, 'codex:s1')).toEqual([]);
  });

  it('only pending is actionable', () => {
    expect(approvalActionable('pending')).toBe(true);
    for (const s of ['approved', 'rejected', 'resolved', 'delivery_failed', 'expired', 'invalidated']) {
      expect(approvalActionable(s)).toBe(false);
    }
  });

  it('pendingApprovals returns only pending validated records', () => {
    const raw = [
      validRaw({ id: 'p', status: 'pending' }),
      validRaw({ id: 'd', status: 'delivery_failed' }),
      validRaw({ id: 'e', status: 'expired' }),
    ];
    expect(pendingApprovals(raw, 'codex:s1').map(a => a.id)).toEqual(['p']);
  });
});
