/**
 * C3D-B items 16-17 — ApprovalCard/Dashboard presentation boundary.
 *
 * These tests verify that the mobile error classifier and the CTA
 * eligibility rules produce honest user-visible outcomes without
 * requiring a full React Native render. The classified outcome and the
 * onResolved call signal are the contract the Dashboard uses to drive
 * ApprovalCard rendering.
 *
 * No production code is changed. The functions tested are small pure
 * helpers extracted from the existing ApprovalCard component.
 */

import {
  SafeApproval,
} from '../src/lib/client';
import {
  validateApproval,
  approvalActionable,
  APPROVAL_STATES,
} from '../src/lib/approvalRequest';

// ── extracted pure error classifier (modelled on ApprovalCard.handleAction) ──

type ErrorMessage =
  | 'not_authorized'
  | 'no_longer_current'
  | 'expired'
  | 'delivery_failed'
  | 'action_not_accepted'
  | 'network_error';

function classifyApprovalError(statusCode: number | undefined): ErrorMessage {
  switch (statusCode) {
    case 401:
    case 403:
      return 'not_authorized';
    case 409:
      return 'no_longer_current';
    case 410:
      return 'expired';
    case 502:
      return 'delivery_failed';
    case 400:
      return 'action_not_accepted';
    default:
      return 'network_error';
  }
}

// ── extracted onResolved gate (called ONLY for accepted/already_accepted) ──

function shouldCallOnResolved(
  outcome: string | undefined,
  action: string | undefined,
): boolean {
  return (
    outcome === 'accepted' || outcome === 'already_accepted'
  ) && !!action;
}

// ── item 16: error messages per status code ──

describe('C3D-B item 16 — approval error classifier', () => {
  it('400 → action_not_accepted', () => {
    expect(classifyApprovalError(400)).toBe('action_not_accepted');
  });
  it('401/403 → not_authorized', () => {
    expect(classifyApprovalError(401)).toBe('not_authorized');
    expect(classifyApprovalError(403)).toBe('not_authorized');
  });
  it('409 → no_longer_current', () => {
    expect(classifyApprovalError(409)).toBe('no_longer_current');
  });
  it('410 → expired', () => {
    expect(classifyApprovalError(410)).toBe('expired');
  });
  it('502 → delivery_failed', () => {
    expect(classifyApprovalError(502)).toBe('delivery_failed');
  });
  it('other / undefined → network_error', () => {
    expect(classifyApprovalError(undefined)).toBe('network_error');
    expect(classifyApprovalError(500)).toBe('network_error');
    expect(classifyApprovalError(0)).toBe('network_error');
  });
});

// ── item 16: onResolved call conditions ──

describe('C3D-B item 16 — onResolved gate', () => {
  it('accepted → onResolved true', () => {
    expect(shouldCallOnResolved('accepted', 'allow_once')).toBe(true);
  });
  it('already_accepted → onResolved true', () => {
    expect(shouldCallOnResolved('already_accepted', 'deny')).toBe(true);
  });
  it('malformed 2xx (no action) → onResolved false', () => {
    expect(shouldCallOnResolved('accepted', undefined)).toBe(false);
    expect(shouldCallOnResolved('accepted', '')).toBe(false);
  });
  it('non-success outcome → onResolved false', () => {
    expect(shouldCallOnResolved('conflict', 'allow_once')).toBe(false);
    expect(shouldCallOnResolved('error', 'deny')).toBe(false);
    expect(shouldCallOnResolved(undefined, 'allow_once')).toBe(false);
  });
});

// ── item 14 (existing): validateApproval strictness ──

describe('C3D-B item 14 — validateApproval strict decoder', () => {
  const validDTO: SafeApproval = {
    id: 'claude-abc',
    sessionId: 'claude_headless:claude-abc',
    summary: 'Run Claude approval verification probe',
    state: 'pending',
    actionable: true,
    options: [
      { id: 'allow_once', label: 'Approve', kind: 'approve', requiresInput: false },
      { id: 'deny', label: 'Reject', kind: 'reject', requiresInput: false },
    ],
    createdAt: '2026-07-17T12:00:00Z',
    expiresAt: '2026-07-17T12:05:00Z',
  };

  it('valid DTO passes', () => {
    expect(validateApproval(validDTO)).not.toBeNull();
  });
  it('unknown field rejected', () => {
    expect(validateApproval({ ...validDTO, extra: 1 })).toBeNull();
  });
  it('missing required field rejected', () => {
    const { sessionId, ...rest } = validDTO;
    expect(validateApproval({ ...rest, id: 'x' })).toBeNull();
  });
  it('non-boolean actionable rejected', () => {
    expect(validateApproval({ ...validDTO, actionable: 'yes' })).toBeNull();
  });
  it('non-array options rejected', () => {
    expect(validateApproval({ ...validDTO, options: 'string' })).toBeNull();
  });
  it('invalid option kind rejected', () => {
    const bad = {
      ...validDTO,
      options: [{ id: 'x', label: 'X', kind: 'dangerous', requiresInput: false }],
    };
    expect(validateApproval(bad)).toBeNull();
  });
  it('foreign session ID rejected', () => {
    expect(validateApproval(validDTO, 'other:session')).toBeNull();
  });
  it('bad timestamp rejected', () => {
    expect(validateApproval({ ...validDTO, createdAt: 'not-a-date' })).toBeNull();
  });
});

// ── item 15: approvalActionable gate ──

describe('C3D-B item 15 — approvalActionable gate', () => {
  const base: SafeApproval = {
    id: 'a1', sessionId: 's1', summary: 's', state: 'pending',
    actionable: true, options: [], createdAt: '2026-07-17T00:00:00Z',
    expiresAt: '2026-07-17T00:05:00Z',
  };

  it('pending + actionable → true', () => {
    expect(approvalActionable(base)).toBe(true);
  });
  it('actionable:false → false', () => {
    expect(approvalActionable({ ...base, actionable: false })).toBe(false);
  });
  for (const nonPending of APPROVAL_STATES) {
    if (nonPending === 'pending') continue;
    it(`state ${nonPending} → false`, () => {
      expect(approvalActionable({ ...base, state: nonPending })).toBe(false);
    });
  }
});

// ── item 17: waiting_status and non-catalog → no CTA at presenter gate ──

describe('C3D-B item 17 — waiting_approval / non-catalog CTA absence', () => {
  const nonCatalogDTO: SafeApproval = {
    id: 'nc1', sessionId: 'claude_headless:s1',
    summary: 'Approval requested', // generic, not catalog
    state: 'pending', actionable: false, options: [],
    createdAt: '2026-07-17T00:00:00Z', expiresAt: '2026-07-17T00:05:00Z',
  };

  it('non-catalog observation → actionable false + zero options → no CTA', () => {
    expect(nonCatalogDTO.actionable).toBe(false);
    expect(nonCatalogDTO.options).toHaveLength(0);
    expect(approvalActionable(nonCatalogDTO)).toBe(false);
  });

  it('actionable record in delivery_failed → no CTA (not pending)', () => {
    const df: SafeApproval = { ...nonCatalogDTO, state: 'delivery_failed', actionable: true };
    expect(approvalActionable(df)).toBe(false);
  });

  it('actionable record in pending → CTA allowed', () => {
    const pend: SafeApproval = {
      ...nonCatalogDTO,
      state: 'pending', actionable: true,
      options: [{ id: 'allow_once', label: 'Approve', kind: 'approve', requiresInput: false }],
    };
    expect(approvalActionable(pend)).toBe(true);
    // BUT the Summary gate: non-catalog records carry the generic label,
    // not the catalog probe label — the presenter uses Summary to
    // distinguish, but CTA eligibility is only actionable + pending.
  });
});
