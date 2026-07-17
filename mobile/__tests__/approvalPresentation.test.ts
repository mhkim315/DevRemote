/**
 * C3D-B items 14-17 — ApprovalCard/Dashboard presentation boundary.
 *
 * Tests import the real production functions extracted as pure helpers
 * (src/lib/approvalError.ts) so there is no test-time duplicate. The
 * existing strict decoder (validateApproval) and actionable gate
 * (approvalActionable) are also tested from their production sources.
 */
import {
  SafeApproval,
} from '../src/lib/client';
import {
  classifyApprovalError,
  resolveCallSucceeded,
} from '../src/lib/approvalError';
import {
  validateApproval,
  approvalActionable,
  APPROVAL_STATES,
} from '../src/lib/approvalRequest';

// ── item 16: production error classifier ──

describe('C3D-B item 16 — production error classifier', () => {
  it('400 → Action not accepted', () => {
    expect(classifyApprovalError(400)).toBe('Action not accepted');
  });
  it('401/403 → Device not authorized', () => {
    expect(classifyApprovalError(401)).toContain('Device not authorized');
    expect(classifyApprovalError(403)).toContain('Device not authorized');
  });
  it('409 → No longer current', () => {
    expect(classifyApprovalError(409)).toBe('No longer current — refresh');
  });
  it('410 → Approval expired', () => {
    expect(classifyApprovalError(410)).toBe('Approval expired');
  });
  it('502 → Couldn\'t deliver', () => {
    expect(classifyApprovalError(502)).toContain("Couldn't deliver");
  });
  it('other / undefined → Network error', () => {
    expect(classifyApprovalError(undefined)).toContain('Network error');
    expect(classifyApprovalError(500)).toContain('Network error');
    expect(classifyApprovalError(0)).toContain('Network error');
  });
});

// ── item 16: production onResolved gate ──

describe('C3D-B item 16 — production onResolved gate', () => {
  it('accepted → onResolved true', () => {
    expect(resolveCallSucceeded('accepted', 'allow_once')).toBe(true);
  });
  it('already_accepted → onResolved true', () => {
    expect(resolveCallSucceeded('already_accepted', 'deny')).toBe(true);
  });
  it('malformed (no action) → onResolved false', () => {
    expect(resolveCallSucceeded('accepted', undefined)).toBe(false);
    expect(resolveCallSucceeded('accepted', '')).toBe(false);
  });
  it('non-success → onResolved false', () => {
    expect(resolveCallSucceeded('conflict' as any, 'allow_once')).toBe(false);
    expect(resolveCallSucceeded('error' as any, 'deny')).toBe(false);
    expect(resolveCallSucceeded(undefined, 'allow_once')).toBe(false);
  });
});

// ── item 14: validateApproval strict decoder (production) ──

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

// ── item 15: approvalActionable gate (production) ──

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

// ── item 17: waiting_approval / non-catalog CTA absence ──

describe('C3D-B item 17 — waiting_approval / non-catalog CTA absence', () => {
  it('non-catalog observation → actionable false + zero options → no CTA', () => {
    const dto = dummyDTO({ actionable: false, options: [] });
    expect(approvalActionable(dto)).toBe(false);
  });
  it('delivery_failed → no CTA (not pending)', () => {
    const dto = dummyDTO({ state: 'delivery_failed', actionable: true });
    expect(approvalActionable(dto)).toBe(false);
  });
  it('expired → no CTA', () => {
    const dto = dummyDTO({ state: 'expired', actionable: true });
    expect(approvalActionable(dto)).toBe(false);
  });
  it('pending + actionable → CTA', () => {
    const dto = dummyDTO({
      state: 'pending', actionable: true,
      options: [{ id: 'allow_once', label: 'Approve', kind: 'approve', requiresInput: false }],
    });
    expect(approvalActionable(dto)).toBe(true);
  });
});

function dummyDTO(overrides: Partial<SafeApproval>): SafeApproval {
  return {
    id: 'x', sessionId: 's', summary: 'Approval requested',
    state: 'pending', actionable: false, options: [],
    createdAt: '2026-07-17T00:00:00Z',
    expiresAt: '2026-07-17T00:05:00Z',
    ...overrides,
  };
}
