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

// ── SP0.5-R1/R2 blocker 1: staleness-safe polling with REAL cancellation ──

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

// ManagedEventsFetcher performs one bounded events request. The signal MUST
// be forwarded to the underlying HTTP request — deadline/close abort the real
// network request, not just a wrapper promise.
export type ManagedEventsFetcher = (cursor: number, signal: AbortSignal) => Promise<unknown>;

// ManagedSessionPoller drives the bounded event polling with: one request in
// flight at a time (enforced against the REAL request via AbortController), a
// per-request deadline that aborts the underlying fetch, and freshness expiry
// — on failure/timeout past the freshness window the status becomes
// 'unavailable' (non-current) until a NEW fresh snapshot restores it.
export class ManagedSessionPoller {
  private readonly feed: ManagedSessionFeed;
  private readonly fetchEvents: ManagedEventsFetcher;
  private readonly onUpdate: (state: ManagedPollerState) => void;
  private readonly now: () => number;
  private readonly deadlineMs: number;
  private readonly freshnessMs: number;
  private inFlight = false;
  private activeReq: AbortController | null = null;
  private closed = false;
  private lastFreshAt = 0;
  private lastStatus = '';

  constructor(
    sessionId: string,
    fetchEvents: ManagedEventsFetcher,
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
    // Abort the REAL outstanding request, if any.
    this.activeReq?.abort();
  }

  // tick runs at most one bounded request; overlapping ticks are skipped
  // (one-in-flight). The deadline ABORTS the underlying request, so at most
  // one network request ever exists per poller even across repeated
  // timeouts. Every failure path re-evaluates freshness.
  async tick(): Promise<void> {
    if (this.closed || this.inFlight) return;
    this.inFlight = true;
    const req = new AbortController();
    this.activeReq = req;
    const deadline = setTimeout(() => req.abort(), this.deadlineMs);
    try {
      const raw = await this.fetchEvents(this.feed.getCursor(), req.signal);
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
      clearTimeout(deadline);
      this.activeReq = null;
      this.inFlight = false;
    }
  }

  private noteFailure(): void {
    if (this.closed) return;
    if (this.lastFreshAt === 0 || this.now() - this.lastFreshAt > this.freshnessMs) {
      this.onUpdate({ status: 'unavailable', current: false, events: this.feed.getEvents() });
    }
  }
}

// ── SP0.5-R2 blocker: generation-guarded bootstrap + timer install ──

// ManagedStatusFetcher performs the bootstrap native-status request; the
// signal MUST reach the underlying HTTP request.
export type ManagedStatusFetcher = (signal: AbortSignal) => Promise<unknown>;

export interface ManagedControllerDeps {
  fetchStatus: ManagedStatusFetcher;
  fetchEvents: (epoch: number, cursor: number, signal: AbortSignal) => Promise<unknown>;
  onUpdate: (state: ManagedPollerState) => void;
  onError: (message: string) => void;
  // schedule/cancel are injectable for deterministic tests; production uses
  // setInterval/clearInterval.
  schedule?: (fn: () => void, ms: number) => unknown;
  cancelSchedule?: (handle: unknown) => void;
  now?: () => number;
}

export interface ManagedControllerOptions extends ManagedPollerOptions {
  pollMs?: number;
}

// ManagedSessionController owns the WHOLE per-session lifecycle: the bounded
// bootstrap status request, poller construction, and the poll timer. close()
// (unmount or session switch) aborts the REAL in-flight bootstrap request and
// guarantees a late bootstrap response installs NOTHING — no poller, no
// timer, no state update. One controller instance per mounted session is the
// generation: there is no shared mutable slot a stale response could reclaim.
export class ManagedSessionController {
  readonly sessionId: string;
  private readonly deps: ManagedControllerDeps;
  private readonly pollMs: number;
  private readonly deadlineMs: number;
  private readonly freshnessMs: number;
  private closed = false;
  private bootstrapReq: AbortController | null = null;
  private poller: ManagedSessionPoller | null = null;
  private timerHandle: unknown = null;
  private epoch = 0;

  constructor(sessionId: string, deps: ManagedControllerDeps, opts?: ManagedControllerOptions) {
    this.sessionId = sessionId;
    this.deps = deps;
    this.pollMs = opts?.pollMs ?? 1500;
    this.deadlineMs = opts?.deadlineMs ?? 5000;
    this.freshnessMs = opts?.freshnessMs ?? 6000;
  }

  getEpoch(): number {
    return this.epoch;
  }

  // start performs the generation-guarded bootstrap. Every step after an
  // await re-checks closed BEFORE installing anything.
  async start(): Promise<void> {
    if (this.closed) return;
    const req = new AbortController();
    this.bootstrapReq = req;
    const deadline = setTimeout(() => req.abort(), this.deadlineMs);
    let st: any;
    try {
      st = await this.deps.fetchStatus(req.signal);
    } catch {
      clearTimeout(deadline);
      this.bootstrapReq = null;
      if (!this.closed) this.deps.onError('Managed session unavailable.');
      return;
    }
    clearTimeout(deadline);
    this.bootstrapReq = null;
    if (this.closed) return; // late response after switch/unmount: inert
    if (!st || typeof st.launchGen !== 'number' || !Number.isInteger(st.launchGen) || st.launchGen < 1) {
      this.deps.onError('Managed session unavailable.');
      return;
    }
    this.epoch = st.launchGen;
    const poller = new ManagedSessionPoller(
      this.sessionId,
      (cursor, signal) => this.deps.fetchEvents(this.epoch, cursor, signal),
      this.deps.onUpdate,
      { deadlineMs: this.deadlineMs, freshnessMs: this.freshnessMs, now: this.deps.now },
    );
    if (this.closed) {
      // closed between the check above and here (synchronous close cannot,
      // but keep the install atomic against any future await insertion).
      poller.close();
      return;
    }
    this.poller = poller;
    void poller.tick();
    const schedule = this.deps.schedule ?? ((fn: () => void, ms: number) => setInterval(fn, ms));
    this.timerHandle = schedule(() => void poller.tick(), this.pollMs);
  }

  // close aborts the real in-flight bootstrap request, closes the poller
  // (aborting its real request too), and cancels the timer. Idempotent.
  close(): void {
    this.closed = true;
    this.bootstrapReq?.abort();
    this.bootstrapReq = null;
    this.poller?.close();
    if (this.timerHandle !== null) {
      const cancel = this.deps.cancelSchedule ?? ((h: unknown) => clearInterval(h as ReturnType<typeof setInterval>));
      cancel(this.timerHandle);
      this.timerHandle = null;
    }
  }
}
