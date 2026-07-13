// S1-D — mobile agent-activity DTO: strict validation + display helpers.
//
// The daemon exposes an additive `agentActivity` object on each /api/sessions row.
// It is the ADVISORY agent-activity dimension ONLY and is kept separate from
// session lifecycle (`lifecycleState`) and poll health (`stale`). This module
// validates the untrusted DTO with strict bounds/unknown-field rejection, and
// formats it for display WITHOUT ever letting it drive lifecycle or approval
// actions. A stale record is surfaced as last-known, never as current activity.

import { formatAgentStatus } from './agentDisplay';

// The S1 activity DTO schema version the daemon emits. Validated exactly so a
// future/incompatible shape is rejected rather than partially trusted.
export const ACTIVITY_CONTRACT_VERSION = 's1.1';

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
// Closed frozen-T0 status vocabulary — a value outside this set is rejected.
const KNOWN_STATUSES = new Set([
  'unknown', 'idle', 'thinking', 'working', 'waiting_approval', 'waiting_input',
  'completed', 'failed', 'interrupted', 'degraded',
]);
// Closed provenance vocabulary ('' allowed = provenance not established).
const KNOWN_PROVENANCES = new Set([
  '', 'runtime', 'provider_protocol', 'provider_hook', 'native_log',
  'pty_structural', 'heuristic', 'prompt_hint', 'unknown',
]);
const MAX_OBSERVED_AT_LEN = 40;

// validateAgentActivity strictly validates the untrusted DTO. It returns a typed,
// bounded copy or null. A wrong contract version, unknown field, out-of-vocabulary
// status/provenance, out-of-range confidence, unparseable/oversized observedAt, or
// any wrong type all fail closed to null so a malformed DTO never renders.
export function validateAgentActivity(raw: unknown): AgentActivity | null {
  if (!raw || typeof raw !== 'object') return null;
  const o = raw as Record<string, unknown>;

  for (const k of Object.keys(o)) {
    if (!ACTIVITY_KNOWN_FIELDS.has(k)) return null; // reject unknown envelope fields
  }

  if (o.contractVersion !== ACTIVITY_CONTRACT_VERSION) return null; // exact version

  if (typeof o.status !== 'string' || !KNOWN_STATUSES.has(o.status)) return null; // closed vocab

  const provenance = o.provenance === undefined ? '' : o.provenance;
  if (typeof provenance !== 'string' || !KNOWN_PROVENANCES.has(provenance)) return null;

  const confidence = o.confidence === undefined ? 0 : o.confidence;
  if (typeof confidence !== 'number' || !isFinite(confidence) || confidence < 0 || confidence > 1) {
    return null;
  }

  const degraded = o.degraded === undefined ? false : o.degraded;
  if (typeof degraded !== 'boolean') return null;

  const observedAt = o.observedAt === undefined ? '' : o.observedAt;
  if (typeof observedAt !== 'string' || observedAt.length > MAX_OBSERVED_AT_LEN) return null;
  if (observedAt !== '' && isNaN(Date.parse(observedAt))) return null;

  const stale = o.stale === undefined ? false : o.stale;
  if (typeof stale !== 'boolean') return null;

  return {
    contractVersion: ACTIVITY_CONTRACT_VERSION,
    status: o.status, provenance, confidence, degraded, observedAt, stale,
  };
}

export interface ActivityDisplay {
  label: string;
  stale: boolean;
  degraded: boolean;
  /** current is true only when the record is a live, non-stale reading. */
  current: boolean;
}

// activityDisplay formats a validated activity for the UI. A stale record is
// labelled as such and is never reported as current; a degraded record is
// likewise flagged so it is not shown as a confident live status.
export function activityDisplay(a: AgentActivity): ActivityDisplay {
  const base = formatAgentStatus(a.status) || a.status;
  let label = base;
  if (a.stale) label = `${base} · stale`;
  else if (a.degraded) label = `${base} · degraded`;
  return { label, stale: a.stale, degraded: a.degraded, current: !a.stale && !a.degraded };
}

// sessionNeedsApproval decides the approval CTA from the approval store ONLY.
// Agent activity (including a `waiting_approval` status) must NEVER create an
// approval CTA — A1 owns approval authority. This helper deliberately ignores
// agentActivity and reads only pending approvals.
export function sessionNeedsApproval(approvals?: { status: string }[]): boolean {
  return (approvals ?? []).some((a) => a.status === 'pending');
}
