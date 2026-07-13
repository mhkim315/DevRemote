// S1-D — mobile agent-activity DTO: strict validation + display + card-render
// decision. The daemon exposes an additive `agentActivity` object on each
// /api/sessions row. It is the ADVISORY agent-activity dimension ONLY, kept
// separate from lifecycle (`lifecycleState`) and poll health (`stale`). This
// module validates the untrusted DTO fail-closed, formats stale/degraded records
// as explicitly non-current, and decides card rendering so the legacy heuristic
// `agentStatus` is never a peer authority when an accepted DTO is present.

import { formatAgentStatus } from './agentDisplay';

// The activity DTO carries the frozen T0 contract version (mirrors the daemon's
// contract.ContractVersion). Validated exactly — a different version is rejected.
export const ACTIVITY_CONTRACT_VERSION = 't0.1';

export interface AgentActivity {
  contractVersion: string;
  status: string;
  provenance: string;
  confidence: number;
  degraded: boolean;
  observedAt: string;
  stale: boolean;
}

const ACTIVITY_KNOWN_FIELDS = new Set([
  'contractVersion', 'status', 'provenance', 'confidence', 'degraded', 'observedAt', 'stale',
]);
const KNOWN_STATUSES = new Set([
  'unknown', 'idle', 'thinking', 'working', 'waiting_approval', 'waiting_input',
  'completed', 'failed', 'interrupted', 'degraded',
]);
const KNOWN_PROVENANCES = new Set([
  '', 'runtime', 'provider_protocol', 'provider_hook', 'native_log',
  'pty_structural', 'heuristic', 'prompt_hint', 'unknown',
]);
// Strict RFC3339 with a required 'T' separator and a required timezone (Z or ±HH:MM).
// A date-only string such as "2026-07-13" is rejected.
const RFC3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/;

// validateAgentActivity strictly validates the untrusted DTO and returns a typed
// copy or null. It FAILS CLOSED: every mandatory field must be present and typed,
// the contract version must match exactly, status/provenance must be in the closed
// vocabulary, confidence must be finite in [0,1], observedAt must be strict RFC3339,
// and unknown fields are rejected. A missing field is a rejection, never a default.
export function validateAgentActivity(raw: unknown): AgentActivity | null {
  if (!raw || typeof raw !== 'object') return null;
  const o = raw as Record<string, unknown>;

  for (const k of Object.keys(o)) {
    if (!ACTIVITY_KNOWN_FIELDS.has(k)) return null; // reject unknown envelope fields
  }
  // Every mandatory field must be present.
  for (const k of ACTIVITY_KNOWN_FIELDS) {
    if (!(k in o)) return null;
  }

  if (o.contractVersion !== ACTIVITY_CONTRACT_VERSION) return null;
  if (typeof o.status !== 'string' || !KNOWN_STATUSES.has(o.status)) return null;
  if (typeof o.provenance !== 'string' || !KNOWN_PROVENANCES.has(o.provenance)) return null;
  if (typeof o.confidence !== 'number' || !isFinite(o.confidence) || o.confidence < 0 || o.confidence > 1) {
    return null;
  }
  if (typeof o.degraded !== 'boolean') return null;
  if (typeof o.observedAt !== 'string' || !RFC3339.test(o.observedAt) || isNaN(Date.parse(o.observedAt))) {
    return null;
  }
  if (typeof o.stale !== 'boolean') return null;

  return {
    contractVersion: o.contractVersion,
    status: o.status,
    provenance: o.provenance,
    confidence: o.confidence,
    degraded: o.degraded,
    observedAt: o.observedAt,
    stale: o.stale,
  };
}

export interface ActivityDisplay {
  label: string;
  stale: boolean;
  degraded: boolean;
  /** current is true only for a live, non-stale, non-degraded reading. */
  current: boolean;
}

// activityDisplay formats a validated activity. A stale or degraded record is
// NEVER shown with an active status name ("Working"/"Thinking"); it is labelled
// as an explicit non-current state instead.
export function activityDisplay(a: AgentActivity): ActivityDisplay {
  const current = !a.stale && !a.degraded;
  let label: string;
  if (a.stale) label = 'Stale activity';
  else if (a.degraded) label = 'Degraded activity';
  else label = formatAgentStatus(a.status) || a.status;
  return { label, stale: a.stale, degraded: a.degraded, current };
}

export interface CardActivity {
  /** validated+formatted activity to show, or null when none/malformed. */
  activity: ActivityDisplay | null;
  /** show the legacy heuristic agentStatus ONLY when no accepted DTO exists. */
  showLegacyStatus: boolean;
  /** an accepted DTO was present but failed validation. */
  malformed: boolean;
}

// deriveCardActivity is the AgentCard render decision. When an authoritative
// `agentActivity` DTO is present, it governs the activity dimension: a valid DTO
// renders, a malformed/future DTO renders nothing (never the legacy heuristic),
// and the legacy `agentStatus` is NOT shown as a peer authority. Only a session
// with NO accepted DTO may still show the legacy heuristic status.
export function deriveCardActivity(session: { agentActivity?: unknown; agentStatus?: string }): CardActivity {
  const hasDTO = session.agentActivity !== undefined && session.agentActivity !== null;
  if (hasDTO) {
    const a = validateAgentActivity(session.agentActivity);
    return { activity: a ? activityDisplay(a) : null, showLegacyStatus: false, malformed: !a };
  }
  return { activity: null, showLegacyStatus: !!session.agentStatus, malformed: false };
}

// sessionNeedsApproval decides the approval CTA from the approval store ONLY.
// Agent activity (including a `waiting_approval` status) must NEVER create an
// approval CTA — A1 owns approval authority.
export function sessionNeedsApproval(approvals?: { status: string }[]): boolean {
  return (approvals ?? []).some((a) => a.status === 'pending');
}
