// A1 remediation (B6) — strict bounded decoder for the safe approval DTO.
//
// The daemon exposes an `approvals` array of bounded, redacted SafeApproval DTOs on
// each /api/sessions row. It is UNTRUSTED input: this module validates it fail-closed
// so a malformed, foreign-session, unknown-state, or oversized record can never
// render a CTA. Only an actionable, pending request may drive action buttons; every
// non-actionable record is display-only intervention information. The shape mirrors
// the daemon's SafeApprovalDTO exactly (no raw prompt/payload/label ever crosses).

import { SafeApproval, SafeOption } from './client';
import { isStrictRFC3339 } from './agentActivity';

// Closed public status vocabulary — EXACTLY the daemon's publicApprovalStatusValid.
export const APPROVAL_STATES = new Set([
  'pending', 'approved', 'rejected', 'resolved', 'delivery_failed', 'expired', 'invalidated',
]);

// Closed option-kind vocabulary.
export const OPTION_KINDS = new Set(['approve', 'reject', 'neutral', 'open', 'cancel']);

const APPROVAL_FIELDS = new Set([
  'id', 'sessionId', 'summary', 'state', 'actionable', 'options', 'createdAt', 'expiresAt',
]);
const OPTION_FIELDS = new Set(['id', 'label', 'kind', 'requiresInput', 'inputPlaceholder']);

const MAX_ID_LEN = 256;
const MAX_SUMMARY_LEN = 200;
const MAX_LABEL_LEN = 64;
const MAX_PLACEHOLDER_LEN = 128;
const MAX_OPTIONS = 8;
const MAX_TS_LEN = 40;

// byteLen returns the UTF-8 byte length so mobile bounds match the backend's Go
// len() (bytes), not JavaScript's UTF-16 .length. A multibyte string (Korean,
// emoji, combining marks) that fits within a UTF-16 count but exceeds the backend
// byte bound is correctly rejected.
function byteLen(v: string): number {
  return new TextEncoder().encode(v).length;
}

function boundedString(v: unknown, maxBytes: number): boolean {
  return typeof v === 'string' && byteLen(v) <= maxBytes;
}

function validateSafeOption(raw: unknown): SafeOption | null {
  if (!raw || typeof raw !== 'object') return null;
  const o = raw as Record<string, unknown>;
  for (const k of Object.keys(o)) if (!OPTION_FIELDS.has(k)) return null;
  if (!boundedString(o.id, MAX_LABEL_LEN) || (o.id as string) === '') return null;
  if (!boundedString(o.label, MAX_LABEL_LEN)) return null;
  if (!boundedString(o.kind, MAX_LABEL_LEN) || !OPTION_KINDS.has(o.kind as string)) return null;
  if (typeof o.requiresInput !== 'boolean') return null;
  if (o.inputPlaceholder !== undefined && !boundedString(o.inputPlaceholder, MAX_PLACEHOLDER_LEN)) return null;
  const out: SafeOption = {
    id: o.id as string,
    label: o.label as string,
    kind: o.kind as string,
    requiresInput: o.requiresInput as boolean,
  };
  if (o.inputPlaceholder !== undefined) out.inputPlaceholder = o.inputPlaceholder as string;
  return out;
}

// validateApproval strictly validates one untrusted SafeApproval DTO, failing closed
// on unknown fields, a missing/mistyped field, an out-of-vocabulary state or option
// kind, an out-of-bounds string/array, a bad timestamp, or a session-binding mismatch.
export function validateApproval(raw: unknown, expectSessionId?: string): SafeApproval | null {
  if (!raw || typeof raw !== 'object') return null;
  const o = raw as Record<string, unknown>;

  for (const k of Object.keys(o)) if (!APPROVAL_FIELDS.has(k)) return null;
  for (const k of APPROVAL_FIELDS) {
    if (!(k in o)) return null;
  }

  if (!boundedString(o.id, MAX_ID_LEN) || (o.id as string) === '') return null;
  if (!boundedString(o.sessionId, MAX_ID_LEN) || (o.sessionId as string) === '') return null;
  if (expectSessionId !== undefined && o.sessionId !== expectSessionId) return null;
  if (!boundedString(o.summary, MAX_SUMMARY_LEN)) return null;
  if (!boundedString(o.state, MAX_LABEL_LEN) || !APPROVAL_STATES.has(o.state as string)) return null;
  if (typeof o.actionable !== 'boolean') return null;
  if (!Array.isArray(o.options) || o.options.length > MAX_OPTIONS) return null;
  const options: SafeOption[] = [];
  for (const raw of o.options) {
    const opt = validateSafeOption(raw);
    if (!opt) return null;
    options.push(opt);
  }
  if (!boundedString(o.createdAt, MAX_TS_LEN) || !isStrictRFC3339(o.createdAt as string)) return null;
  if (!boundedString(o.expiresAt, MAX_TS_LEN) || !isStrictRFC3339(o.expiresAt as string)) return null;

  return {
    id: o.id as string,
    sessionId: o.sessionId as string,
    summary: o.summary as string,
    state: o.state as string,
    actionable: o.actionable as boolean,
    options,
    createdAt: o.createdAt as string,
    expiresAt: o.expiresAt as string,
  };
}

// decodeApprovals validates a session's raw approvals array, dropping invalid or
// foreign-session records.
export function decodeApprovals(raw: unknown, expectSessionId: string): SafeApproval[] {
  if (!Array.isArray(raw)) return [];
  const out: SafeApproval[] = [];
  for (const item of raw) {
    const a = validateApproval(item, expectSessionId);
    if (a) out.push(a);
  }
  return out;
}

// approvalActionable reports whether a validated approval may render action buttons:
// it must be BOTH pending AND daemon-flagged actionable. A non-actionable record is
// display-only intervention information.
export function approvalActionable(a: SafeApproval): boolean {
  return a.state === 'pending' && a.actionable === true;
}

// actionableApprovals returns only the validated approvals that may drive a CTA.
export function actionableApprovals(raw: unknown, sessionId: string): SafeApproval[] {
  return decodeApprovals(raw, sessionId).filter(approvalActionable);
}

// pendingDisplayApprovals returns validated pending approvals for DISPLAY (including
// non-actionable intervention information), newest first is caller's concern.
export function pendingDisplayApprovals(raw: unknown, sessionId: string): SafeApproval[] {
  return decodeApprovals(raw, sessionId).filter((a) => a.state === 'pending');
}
