import type { NotificationStatus } from './client';

// Only an event returned by the status endpoint with the exact locator ID is
// safe to render. Never replace a wrapped target with a different event.
export function exactStatusEvent(status: NotificationStatus | null, eventId?: string) {
  if (!status?.event || !eventId || status.event.eventId !== eventId) return null;
  return status.event;
}
