// SP0.5-B — native managed session DTO: strict fail-closed validation of the
// daemon's bounded managed event surface, plus the stale-commit guard used by
// the polling view. The managed session is JSON-RPC-native (no PTY): the
// mobile client consumes ONLY this projected DTO — never terminal bytes or
// provider protocol objects.

// The managed contract version is validated EXACTLY; any other version is
// rejected (mirrors the daemon's managedEventContractVersion).
export const MANAGED_CONTRACT_VERSION = 'pokit.managed.v1';

export interface ManagedSessionStatus {
  id: string;
  provider: string;
  version: string;
  nativeStatus: string;
  launchGen: number;
  createdAt: string;
  statusChangedAt: string;
  exited: boolean;
}

export interface ManagedEvent {
  contractVersion: string;
  sessionId: string;
  epoch: number;
  seq: number;
  kind: string;
  text?: string;
  observedAt: string;
}

export interface ManagedEventsResponse {
  contractVersion: string;
  session: ManagedSessionStatus;
  events: ManagedEvent[];
  nextCursor: number;
}

const SESSION_FIELDS = new Set([
  'id', 'provider', 'version', 'nativeStatus', 'launchGen', 'createdAt', 'statusChangedAt', 'exited',
]);
const EVENT_FIELDS = new Set([
  'contractVersion', 'sessionId', 'epoch', 'seq', 'kind', 'text', 'observedAt',
]);
const RESPONSE_FIELDS = new Set(['contractVersion', 'session', 'events', 'nextCursor']);
// Closed event vocabulary (daemon ManagedEventKind values).
export const MANAGED_EVENT_KINDS = new Set(['assistant', 'working', 'completed', 'exited', 'gap']);
// Closed native status vocabulary (daemon ManagedNativeStatus values).
const NATIVE_STATUSES = new Set(['idle', 'working', 'completed', 'exited']);
const MAX_EVENT_TEXT = 4096;

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

function hasUnknownFields(obj: Record<string, unknown>, known: Set<string>): boolean {
  return Object.keys(obj).some((k) => !known.has(k));
}

function decodeSession(v: unknown): ManagedSessionStatus | null {
  if (!isRecord(v) || hasUnknownFields(v, SESSION_FIELDS)) return null;
  const s = v as Record<string, unknown>;
  if (
    typeof s.id !== 'string' || s.id === '' ||
    typeof s.provider !== 'string' ||
    typeof s.version !== 'string' ||
    typeof s.nativeStatus !== 'string' || !NATIVE_STATUSES.has(s.nativeStatus) ||
    typeof s.launchGen !== 'number' || !Number.isInteger(s.launchGen) || s.launchGen < 1 ||
    typeof s.createdAt !== 'string' ||
    typeof s.statusChangedAt !== 'string' ||
    typeof s.exited !== 'boolean'
  ) {
    return null;
  }
  return s as unknown as ManagedSessionStatus;
}

function decodeEvent(v: unknown): ManagedEvent | null {
  if (!isRecord(v) || hasUnknownFields(v, EVENT_FIELDS)) return null;
  const e = v as Record<string, unknown>;
  if (
    e.contractVersion !== MANAGED_CONTRACT_VERSION ||
    typeof e.sessionId !== 'string' || e.sessionId === '' ||
    typeof e.epoch !== 'number' || !Number.isInteger(e.epoch) || e.epoch < 1 ||
    typeof e.seq !== 'number' || !Number.isInteger(e.seq) || e.seq < 1 ||
    typeof e.kind !== 'string' || !MANAGED_EVENT_KINDS.has(e.kind) ||
    typeof e.observedAt !== 'string'
  ) {
    return null;
  }
  if (e.text !== undefined && (typeof e.text !== 'string' || e.text.length > MAX_EVENT_TEXT)) {
    return null;
  }
  return e as unknown as ManagedEvent;
}

// decodeManagedEventsResponse validates the untrusted response FAIL-CLOSED:
// exact contract version, closed field sets (unknown fields rejected), closed
// event vocabulary, integer cursors, bounded text. Returns null on ANY
// violation — the caller must treat null as "no data", never partial data.
export function decodeManagedEventsResponse(input: unknown): ManagedEventsResponse | null {
  if (!isRecord(input) || hasUnknownFields(input, RESPONSE_FIELDS)) return null;
  if (input.contractVersion !== MANAGED_CONTRACT_VERSION) return null;
  const session = decodeSession(input.session);
  if (!session) return null;
  if (!Array.isArray(input.events)) return null;
  if (typeof input.nextCursor !== 'number' || !Number.isInteger(input.nextCursor) || input.nextCursor < 0) {
    return null;
  }
  const events: ManagedEvent[] = [];
  let lastSeq = 0;
  for (const raw of input.events) {
    const ev = decodeEvent(raw);
    if (!ev) return null;
    // Events must belong to this session/epoch and be strictly ordered.
    if (ev.sessionId !== session.id || ev.epoch !== session.launchGen) return null;
    if (ev.seq <= lastSeq) return null;
    lastSeq = ev.seq;
    events.push(ev);
  }
  return { contractVersion: MANAGED_CONTRACT_VERSION, session, events, nextCursor: input.nextCursor };
}

// ManagedSessionFeed accumulates decoded events for ONE session+epoch and
// refuses stale commits: a response for a different session or epoch (e.g. a
// poll that resolved after the user switched sessions, or after a relaunch
// bumped the epoch) is rejected without mutating state. Duplicate/replayed
// cursors are idempotent (already-seen seqs are skipped).
export class ManagedSessionFeed {
  readonly sessionId: string;
  private epoch = 0;
  private cursor = 0;
  private events: ManagedEvent[] = [];
  private closed = false;

  constructor(sessionId: string) {
    this.sessionId = sessionId;
  }

  // close marks the feed unmounted: every later commit is rejected.
  close(): void {
    this.closed = true;
  }

  getCursor(): number {
    return this.cursor;
  }

  getEvents(): ManagedEvent[] {
    return this.events.slice();
  }

  // apply commits one decoded response. Returns false (no mutation) for a
  // closed feed, a different session, or a changed epoch.
  apply(resp: ManagedEventsResponse): boolean {
    if (this.closed) return false;
    if (resp.session.id !== this.sessionId) return false;
    if (this.epoch === 0) {
      this.epoch = resp.session.launchGen;
    } else if (resp.session.launchGen !== this.epoch) {
      return false;
    }
    for (const ev of resp.events) {
      if (ev.seq <= this.cursor) continue; // replay-idempotent
      this.events.push(ev);
      this.cursor = ev.seq;
    }
    if (resp.nextCursor > this.cursor) this.cursor = resp.nextCursor;
    return true;
  }
}
