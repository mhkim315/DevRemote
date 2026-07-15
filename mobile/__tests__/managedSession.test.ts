// SP0.5-B — strict managed-session decoder + stale-commit guard tests. The
// fixture is the EXACT backend-marshaled response (cross-verified by the Go
// test TestManagedEventsFixture_MatchesMobileDecoderInput).
import {
  decodeManagedEventsResponse,
  ManagedSessionFeed,
  MANAGED_CONTRACT_VERSION,
} from '../src/lib/managedSession';

// eslint-disable-next-line @typescript-eslint/no-var-requires
const fixture = require('./fixtures/managedEvents.fixture.json');

function clone(): any {
  return JSON.parse(JSON.stringify(fixture));
}

describe('decodeManagedEventsResponse', () => {
  it('accepts the exact backend fixture', () => {
    const r = decodeManagedEventsResponse(fixture);
    expect(r).not.toBeNull();
    expect(r!.contractVersion).toBe(MANAGED_CONTRACT_VERSION);
    expect(r!.session.id).toBe('codex_app_server:codex-app-1');
    expect(r!.session.nativeStatus).toBe('working');
    expect(r!.events.map((e) => e.kind)).toEqual(['working', 'assistant']);
    expect(r!.events[1].text).toBe('READY');
    expect(r!.nextCursor).toBe(2);
  });

  it('rejects an unknown contract version', () => {
    const bad = clone();
    bad.contractVersion = 'pokit.managed.v2';
    expect(decodeManagedEventsResponse(bad)).toBeNull();
    const badEvent = clone();
    badEvent.events[0].contractVersion = 'pokit.managed.v2';
    expect(decodeManagedEventsResponse(badEvent)).toBeNull();
  });

  it('rejects unknown fields at every level', () => {
    const topLevel = clone();
    topLevel.surprise = 1;
    expect(decodeManagedEventsResponse(topLevel)).toBeNull();
    const inSession = clone();
    inSession.session.pid = 1234;
    expect(decodeManagedEventsResponse(inSession)).toBeNull();
    const inEvent = clone();
    inEvent.events[0].rawPayload = {};
    expect(decodeManagedEventsResponse(inEvent)).toBeNull();
  });

  it('rejects unknown event kinds and native statuses', () => {
    const badKind = clone();
    badKind.events[0].kind = 'approval';
    expect(decodeManagedEventsResponse(badKind)).toBeNull();
    const badStatus = clone();
    badStatus.session.nativeStatus = 'root';
    expect(decodeManagedEventsResponse(badStatus)).toBeNull();
  });

  it('rejects non-monotonic seq, cross-session events, and epoch mismatch', () => {
    const dupSeq = clone();
    dupSeq.events[1].seq = 1;
    expect(decodeManagedEventsResponse(dupSeq)).toBeNull();
    const wrongSession = clone();
    wrongSession.events[1].sessionId = 'codex_app_server:other';
    expect(decodeManagedEventsResponse(wrongSession)).toBeNull();
    const wrongEpoch = clone();
    wrongEpoch.events[1].epoch = 2;
    expect(decodeManagedEventsResponse(wrongEpoch)).toBeNull();
  });

  it('rejects oversized text and malformed cursors', () => {
    const bigText = clone();
    bigText.events[1].text = 'x'.repeat(4097);
    expect(decodeManagedEventsResponse(bigText)).toBeNull();
    const badCursor = clone();
    badCursor.nextCursor = -1;
    expect(decodeManagedEventsResponse(badCursor)).toBeNull();
    const floatCursor = clone();
    floatCursor.nextCursor = 1.5;
    expect(decodeManagedEventsResponse(floatCursor)).toBeNull();
  });

  it('rejects non-object and array inputs', () => {
    expect(decodeManagedEventsResponse(null)).toBeNull();
    expect(decodeManagedEventsResponse('str')).toBeNull();
    expect(decodeManagedEventsResponse([fixture])).toBeNull();
  });
});

describe('ManagedSessionFeed', () => {
  it('commits events once and replays idempotently', () => {
    const r = decodeManagedEventsResponse(fixture)!;
    const feed = new ManagedSessionFeed(r.session.id);
    expect(feed.apply(r)).toBe(true);
    expect(feed.getEvents()).toHaveLength(2);
    expect(feed.getCursor()).toBe(2);
    // Replayed response (same cursor range) adds nothing.
    expect(feed.apply(r)).toBe(true);
    expect(feed.getEvents()).toHaveLength(2);
  });

  it('rejects a response for another session (stale commit after switch)', () => {
    const r = decodeManagedEventsResponse(fixture)!;
    const feed = new ManagedSessionFeed('codex_app_server:switched-away');
    expect(feed.apply(r)).toBe(false);
    expect(feed.getEvents()).toHaveLength(0);
  });

  it('rejects a changed epoch (relaunch cannot restore stale state)', () => {
    const r = decodeManagedEventsResponse(fixture)!;
    const feed = new ManagedSessionFeed(r.session.id);
    expect(feed.apply(r)).toBe(true);
    const relaunched = clone();
    relaunched.session.launchGen = 2;
    relaunched.events = [];
    relaunched.nextCursor = 0;
    const r2 = decodeManagedEventsResponse(relaunched)!;
    expect(feed.apply(r2)).toBe(false);
    expect(feed.getEvents()).toHaveLength(2); // unchanged
  });

  it('rejects every commit after close (unmount)', () => {
    const r = decodeManagedEventsResponse(fixture)!;
    const feed = new ManagedSessionFeed(r.session.id);
    feed.close();
    expect(feed.apply(r)).toBe(false);
    expect(feed.getEvents()).toHaveLength(0);
  });
});
