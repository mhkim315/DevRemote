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
  'runtime', 'provider_protocol', 'provider_hook', 'native_log',
  'pty_structural', 'heuristic', 'prompt_hint', 'unknown',
]);
// Strict RFC3339 with a required 'T' separator and a required timezone (Z or ±HH:MM).
// A date-only string such as "2026-07-13" is rejected. Fractional seconds are
// allowed but length-bounded below; calendar validity is checked separately.
const RFC3339 = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(Z|[+-]\d{2}:\d{2})$/;
// RFC3339 with sane fractional seconds fits comfortably; cap the string to reject
// pathological payloads (e.g. 100KB of fractional digits).
const MAX_OBSERVED_AT_LEN = 40;

function daysInMonth(year: number, month: number): number {
  const leap = (year % 4 === 0 && year % 100 !== 0) || year % 400 === 0;
  return [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][month - 1];
}

// isStrictRFC3339 requires a bounded, format-correct, CALENDAR-VALID timestamp.
// It rejects impossible dates (e.g. 2026-02-30) that a lenient Date.parse would
// silently normalize, plus out-of-range time and timezone components.
function isStrictRFC3339(s: string): boolean {
  if (s.length > MAX_OBSERVED_AT_LEN) return false;
  const m = RFC3339.exec(s);
  if (!m) return false;
  const [, y, mo, d, h, mi, se, tz] = m;
  const year = Number(y), month = Number(mo), day = Number(d);
  const hour = Number(h), min = Number(mi), sec = Number(se);
  if (month < 1 || month > 12) return false;
  if (day < 1 || day > daysInMonth(year, month)) return false;
  if (hour > 23 || min > 59 || sec > 60) return false; // 60 permits a leap second
  if (tz !== 'Z') {
    const offH = Number(tz.slice(1, 3)), offM = Number(tz.slice(4, 6));
    if (offH > 23 || offM > 59) return false;
  }
  return !isNaN(Date.parse(s));
}

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
  if (typeof o.observedAt !== 'string' || !isStrictRFC3339(o.observedAt)) return null;
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
  /** validated+formatted activity to show, or null when none/malformed/unavailable. */
  activity: ActivityDisplay | null;
  /** show the legacy heuristic agentStatus ONLY when no accepted DTO exists AND the
      session is not an accepted-adapter (codex/claude) session. */
  showLegacyStatus: boolean;
  /** an accepted DTO was present but failed validation. */
  malformed: boolean;
  /** an accepted-adapter session has no current activity (e.g. after a daemon
      restart, or connectivity loss) — show "Unavailable", never legacy authority. */
  unavailable: boolean;
}

// Accepted-adapter agent kinds. For these, the legacy heuristic agentStatus is
// NEVER a fallback authority — absence of an accepted DTO shows "Unavailable".
const ACCEPTED_AGENT_KINDS = new Set(['claude', 'codex']);

// deriveCardActivity is the AgentCard render decision. When an authoritative
// `agentActivity` DTO is present, it governs the activity dimension: a valid DTO
// renders (forced non-current when the connection is stale), a malformed/future
// DTO renders "Unavailable" (never the legacy heuristic). With NO accepted DTO,
// an accepted-adapter (codex/claude) session shows "Unavailable" — it must not
// present the legacy heuristic as accepted authority (e.g. right after a daemon
// restart) — while a non-accepted session may still show its legacy heuristic.
export function deriveCardActivity(
  session: { agentActivity?: unknown; agentStatus?: string; agentKind?: string },
  opts?: { connectionStale?: boolean },
): CardActivity {
  const connectionStale = !!opts?.connectionStale;
  const hasDTO = session.agentActivity !== undefined && session.agentActivity !== null;
  if (hasDTO) {
    const a = validateAgentActivity(session.agentActivity);
    if (!a) return { activity: null, showLegacyStatus: false, malformed: true, unavailable: false };
    let view = activityDisplay(a);
    if (connectionStale && view.current) {
      // A polling failure / freshness expiry makes a previously-fresh reading
      // non-current — never keep showing "Working" while the daemon is unreachable.
      view = { label: 'Stale activity', stale: true, degraded: view.degraded, current: false };
    }
    return { activity: view, showLegacyStatus: false, malformed: false, unavailable: false };
  }
  if (ACCEPTED_AGENT_KINDS.has(session.agentKind ?? '')) {
    return { activity: null, showLegacyStatus: false, malformed: false, unavailable: true };
  }
  return { activity: null, showLegacyStatus: !!session.agentStatus, malformed: false, unavailable: false };
}

// isConnectionStale reports whether the mobile view should treat its cached
// activity as non-current: a failed last poll, or a last-success older than the
// freshness horizon.
export function isConnectionStale(
  fetchError: string,
  lastSuccessMs: number | null,
  nowMs: number,
  maxAgeMs: number,
): boolean {
  if (fetchError !== '') return true;
  if (lastSuccessMs !== null && nowMs - lastSuccessMs > maxAgeMs) return true;
  return false;
}

// sessionNeedsApproval decides the approval CTA from the approval store ONLY.
// Agent activity (including a `waiting_approval` status) must NEVER create an
// approval CTA — A1 owns approval authority.
export function sessionNeedsApproval(approvals?: { status: string }[]): boolean {
  return (approvals ?? []).some((a) => a.status === 'pending');
}
