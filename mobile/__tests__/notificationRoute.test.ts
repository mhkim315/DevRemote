import { notificationRoute, notificationStartupMessage } from '../src/lib/notificationRoute';
import type { NotificationStatus } from '../src/lib/client';
import { exactStatusEvent } from '../src/lib/notificationEvent';

const base = (status: NotificationStatus['status']): NotificationStatus => ({
  eventId: 'event 1', currentGeneration: 7, notificationGeneration: 7, status,
});

describe('N1 notification fallback routes', () => {
  it('opens the exact Activity link only for actionable events', () => {
    expect(notificationRoute({ ...base('actionable'), activityLink: 'pokit://activity/s?event=e' }, 's', 'e').url)
      .toBe('pokit://activity/s?event=e');
  });

  it.each([
    ['already_resolved', 'pokit://session/session%201?notice=resolved&event=event%201'],
    ['stale_generation', 'pokit://session/session%201?notice=stale&event=event%201'],
    ['session_unavailable', 'pokit://dashboard?notice=session_unavailable'],
    ['insufficient_permission', 'pokit://settings?notice=insufficient_permission'],
    ['canonical_event_unavailable', 'pokit://dashboard?notice=event_unavailable'],
    ['event_degraded_or_gap', 'pokit://session/session%201?notice=degraded&event=event%201'],
  ] as const)('routes %s to its distinct safe recovery surface', (status, want) => {
    expect(notificationRoute(base(status), 'session 1', 'event 1').url).toBe(want);
  });

  it('uses terminal for an unknown server outcome', () => {
    expect(notificationRoute({ ...base('actionable'), status: 'future_status' as any }, 's', 'e').url)
      .toBe('pokit://session/s?notice=unknown&event=e');
  });

  it('provides explicit cold-start retry feedback', () => {
    expect(notificationStartupMessage).toBe('Starting up, tap again');
  });

  it('uses only the exact status event and never a different retained session event', () => {
    const exact = { eventId: 'target', sessionId: 's', runtimeId: 'r', generation: 2, kind: 'approval_requested', occurredAt: '2026-01-01T00:00:00Z' };
    expect(exactStatusEvent({ ...base('actionable'), event: exact }, 'target')).toEqual(exact);
    expect(exactStatusEvent({ ...base('actionable'), event: { ...exact, eventId: 'other' } }, 'target')).toBeNull();
    expect(exactStatusEvent({ ...base('canonical_event_unavailable') }, 'target')).toBeNull();
  });
});
