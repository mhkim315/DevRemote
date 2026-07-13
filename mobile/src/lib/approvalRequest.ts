// A1-E — strict bounded decoder for the authoritative approval DTO.
//
// The daemon exposes an `approvals` array on each /api/sessions row, projected
// from the generation-bound AuthoritativeApprovalStore. It is UNTRUSTED input:
// this module validates it fail-closed so a malformed, foreign-session, unknown-
// status, or oversized record can never render an actionable CTA. It mirrors the
// backend's frozen closed vocabularies (public approval status + option kinds).
// Only a `pending` request is actionable; every other status is display/terminal.

import { AgentApproval, InteractionOption, InputSchema } from './client';
import { isStrictRFC3339 } from './agentActivity';

// Closed public status vocabulary — EXACTLY the daemon's publicApprovalStatusValid.
export const APPROVAL_STATUSES = new Set([
  'pending', 'approved', 'rejected', 'resolved', 'delivery_failed', 'expired', 'invalidated',
]);

// Closed option-kind vocabulary — mirrors the InteractionOption kinds mobile styles.
export const OPTION_KINDS = new Set(['approve', 'reject', 'neutral', 'open', 'cancel']);

const APPROVAL_FIELDS = new Set([
  'id', 'sessionId', 'agentKind', 'kind', 'status', 'prompt', 'options',
  'default', 'source', 'confidence', 'createdAt', 'resolvedAt',
]);
const OPTION_FIELDS = new Set(['id', 'label', 'kind', 'payload', 'input']);
const INPUT_FIELDS = new Set(['required', 'placeholder', 'multiline']);

// Bounds — reject pathological payloads before they reach the UI.
const MAX_ID_LEN = 256;
const MAX_SHORT_LEN = 512;
const MAX_PROMPT_LEN = 4096;
const MAX_OPTIONS = 32;
const MAX_PLACEHOLDER_LEN = 512;

function boundedString(v: unknown, max: number): boolean {
  return typeof v === 'string' && v.length <= max;
}

function validateInput(raw: unknown): InputSchema | null {
  if (raw === undefined) return null; // absence is valid (no input contract)
  if (!raw || typeof raw !== 'object') return null;
  const o = raw as Record<string, unknown>;
  for (const k of Object.keys(o)) if (!INPUT_FIELDS.has(k)) return null;
  if (typeof o.required !== 'boolean') return null;
  if (o.placeholder !== undefined && !boundedString(o.placeholder, MAX_PLACEHOLDER_LEN)) return null;
  if (o.multiline !== undefined && typeof o.multiline !== 'boolean') return null;
  const out: InputSchema = { required: o.required };
  if (o.placeholder !== undefined) out.placeholder = o.placeholder as string;
  if (o.multiline !== undefined) out.multiline = o.multiline as boolean;
  return out;
}

function validateOption(raw: unknown): InteractionOption | null {
  if (!raw || typeof raw !== 'object') return null;
  const o = raw as Record<string, unknown>;
  for (const k of Object.keys(o)) if (!OPTION_FIELDS.has(k)) return null;
  if (!boundedString(o.id, MAX_ID_LEN) || (o.id as string) === '') return null;
  if (!boundedString(o.label, MAX_SHORT_LEN)) return null;
  if (!boundedString(o.kind, MAX_SHORT_LEN) || !OPTION_KINDS.has(o.kind as string)) return null;
  if (o.payload !== undefined && !boundedString(o.payload, MAX_SHORT_LEN)) return null;
  const hasInput = 'input' in o && o.input !== undefined;
  let input: InputSchema | undefined;
  if (hasInput) {
    const v = validateInput(o.input);
    if (!v) return null;
    input = v;
  }
  const out: InteractionOption = { id: o.id as string, label: o.label as string, kind: o.kind as string };
  if (o.payload !== undefined) out.payload = o.payload as string;
  if (input) out.input = input;
  return out;
}

// validateApproval strictly validates one untrusted approval DTO. It FAILS CLOSED:
// unknown envelope fields, a missing/mistyped mandatory field, an out-of-vocabulary
// status or option kind, an out-of-bounds string/array, a non-finite confidence,
// a bad timestamp, or (when expectSessionId is given) a session-binding mismatch
// all yield null. A null result must never render an actionable CTA.
export function validateApproval(raw: unknown, expectSessionId?: string): AgentApproval | null {
  if (!raw || typeof raw !== 'object') return null;
  const o = raw as Record<string, unknown>;

  for (const k of Object.keys(o)) if (!APPROVAL_FIELDS.has(k)) return null;
  // Mandatory fields (resolvedAt is the only optional one).
  for (const k of APPROVAL_FIELDS) {
    if (k === 'resolvedAt') continue;
    if (!(k in o)) return null;
  }

  if (!boundedString(o.id, MAX_ID_LEN) || (o.id as string) === '') return null;
  if (!boundedString(o.sessionId, MAX_ID_LEN) || (o.sessionId as string) === '') return null;
  if (expectSessionId !== undefined && o.sessionId !== expectSessionId) return null; // exact binding
  if (!boundedString(o.agentKind, MAX_SHORT_LEN)) return null;
  if (!boundedString(o.kind, MAX_SHORT_LEN)) return null;
  if (!boundedString(o.status, MAX_SHORT_LEN) || !APPROVAL_STATUSES.has(o.status as string)) return null;
  if (!boundedString(o.prompt, MAX_PROMPT_LEN)) return null;
  if (!Array.isArray(o.options) || o.options.length > MAX_OPTIONS) return null;
  const options: InteractionOption[] = [];
  for (const raw of o.options) {
    const opt = validateOption(raw);
    if (!opt) return null; // one bad option fails the whole record (fail closed)
    options.push(opt);
  }
  if (!boundedString(o.default, MAX_ID_LEN)) return null;
  if (!boundedString(o.source, MAX_SHORT_LEN)) return null;
  if (typeof o.confidence !== 'number' || !isFinite(o.confidence) || o.confidence < 0 || o.confidence > 1) return null;
  if (!boundedString(o.createdAt, 40) || !isStrictRFC3339(o.createdAt as string)) return null;
  if (o.resolvedAt !== undefined) {
    if (!boundedString(o.resolvedAt, 40) || !isStrictRFC3339(o.resolvedAt as string)) return null;
  }

  const out: AgentApproval = {
    id: o.id as string,
    sessionId: o.sessionId as string,
    agentKind: o.agentKind as string,
    kind: o.kind as string,
    status: o.status as string,
    prompt: o.prompt as string,
    options,
    default: o.default as string,
    source: o.source as string,
    confidence: o.confidence,
    createdAt: o.createdAt as string,
  };
  if (o.resolvedAt !== undefined) out.resolvedAt = o.resolvedAt as string;
  return out;
}

// decodeApprovals validates a session's raw approvals array, dropping any record
// that fails validation or does not bind to the expected session. Non-array input
// yields an empty list.
export function decodeApprovals(raw: unknown, expectSessionId: string): AgentApproval[] {
  if (!Array.isArray(raw)) return [];
  const out: AgentApproval[] = [];
  for (const item of raw) {
    const a = validateApproval(item, expectSessionId);
    if (a) out.push(a);
  }
  return out;
}

// approvalActionable reports whether a validated approval may render an actionable
// CTA. Only `pending` is actionable; every terminal/display status shows no CTA.
export function approvalActionable(status: string): boolean {
  return status === 'pending';
}

// pendingApprovals returns only the actionable (pending) validated approvals for a
// session, the ONLY records that may drive an approval CTA.
export function pendingApprovals(raw: unknown, sessionId: string): AgentApproval[] {
  return decodeApprovals(raw, sessionId).filter((a) => approvalActionable(a.status));
}
