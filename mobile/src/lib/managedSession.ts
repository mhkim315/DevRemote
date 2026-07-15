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
// The daemon bound is BYTES (UTF-8), not UTF-16 code units.
const MAX_EVENT_TEXT_BYTES = 4096;
// Mobile-side feed capacity mirrors the daemon ring; older events are dropped
// behind an explicit gap marker, never silently.
export const MANAGED_FEED_CAP = 256;

// utf8ByteLength counts the UTF-8 encoded size of s (dependency-free; RN
// runtimes do not uniformly ship TextEncoder).
export function utf8ByteLength(s: string): number {
  let bytes = 0;
  for (const ch of s) {
    const c = ch.codePointAt(0)!;
    bytes += c < 0x80 ? 1 : c < 0x800 ? 2 : c < 0x10000 ? 3 : 4;
  }
  return bytes;
}

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
  if (e.text !== undefined && (typeof e.text !== 'string' || utf8ByteLength(e.text) > MAX_EVENT_TEXT_BYTES)) {
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
  private dropped = false; // events were evicted: surface an explicit gap
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

  // getEvents returns the bounded feed; when older events were evicted a
  // synthetic gap marker leads the list so the history is never silently
  // presented as complete.
  getEvents(): ManagedEvent[] {
    if (!this.dropped || this.events.length === 0) return this.events.slice();
    const first = this.events[0];
    const gap: ManagedEvent = {
      contractVersion: MANAGED_CONTRACT_VERSION,
      sessionId: this.sessionId,
      epoch: this.epoch,
      seq: first.seq - 1,
      kind: 'gap',
      observedAt: first.observedAt,
    };
    return [gap, ...this.events];
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
      if (ev.kind === 'gap') this.dropped = true;
    }
    if (this.events.length > MANAGED_FEED_CAP) {
      this.events.splice(0, this.events.length - MANAGED_FEED_CAP);
      this.dropped = true;
    }
    if (resp.nextCursor > this.cursor) this.cursor = resp.nextCursor;
    return true;
  }
}

// ── SP0.5-R1 blocker 1: staleness-safe polling controller ──

export interface ManagedPollerState {
  // status is the last FRESH native status, or 'unavailable' once freshness
  // expired — a dead daemon or broken transport can never keep a positive
  // status looking current.
  status: string;
  current: boolean;
  events: ManagedEvent[];
}

export interface ManagedPollerOptions {
  deadlineMs?: number;  // per-request deadline
  freshnessMs?: number; // max age of the last fresh response before non-current
}

// ManagedSessionPoller drives the bounded event polling with: one request in
// flight at a time, a per-request deadline, and freshness expiry — on
// failure/timeout past the freshness window the status becomes
// 'unavailable' (non-current) until a NEW fresh snapshot restores it.
export class ManagedSessionPoller {
  private readonly feed: ManagedSessionFeed;
  private readonly fetchEvents: (cursor: number) => Promise<unknown>;
  private readonly onUpdate: (state: ManagedPollerState) => void;
  private readonly now: () => number;
  private readonly deadlineMs: number;
  private readonly freshnessMs: number;
  private inFlight = false;
  private closed = false;
  private lastFreshAt = 0;
  private lastStatus = '';

  constructor(
    sessionId: string,
    fetchEvents: (cursor: number) => Promise<unknown>,
    onUpdate: (state: ManagedPollerState) => void,
    opts?: ManagedPollerOptions & { now?: () => number },
  ) {
    this.feed = new ManagedSessionFeed(sessionId);
    this.fetchEvents = fetchEvents;
    this.onUpdate = onUpdate;
    this.now = opts?.now ?? (() => Date.now());
    this.deadlineMs = opts?.deadlineMs ?? 5000;
    this.freshnessMs = opts?.freshnessMs ?? 6000;
  }

  close(): void {
    this.closed = true;
    this.feed.close();
  }

  // tick runs at most one bounded request; overlapping ticks are skipped
  // (one-in-flight). Every failure path re-evaluates freshness.
  async tick(): Promise<void> {
    if (this.closed || this.inFlight) return;
    this.inFlight = true;
    try {
      const raw = await this.withDeadline(this.fetchEvents(this.feed.getCursor()));
      const decoded = decodeManagedEventsResponse(raw);
      if (!decoded || !this.feed.apply(decoded)) {
        this.noteFailure();
        return;
      }
      this.lastFreshAt = this.now();
      this.lastStatus = decoded.session.nativeStatus;
      if (!this.closed) {
        this.onUpdate({ status: this.lastStatus, current: true, events: this.feed.getEvents() });
      }
    } catch {
      this.noteFailure();
    } finally {
      this.inFlight = false;
    }
  }

  private noteFailure(): void {
    if (this.closed) return;
    if (this.lastFreshAt === 0 || this.now() - this.lastFreshAt > this.freshnessMs) {
      this.onUpdate({ status: 'unavailable', current: false, events: this.feed.getEvents() });
    }
  }

  private withDeadline(p: Promise<unknown>): Promise<unknown> {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('managed poll deadline')), this.deadlineMs);
      p.then(
        (v) => { clearTimeout(timer); resolve(v); },
        (e) => { clearTimeout(timer); reject(e); },
      );
    });
  }
}
