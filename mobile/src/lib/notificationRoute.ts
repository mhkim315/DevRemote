import type { NotificationStatus } from './client';

export type NotificationRoute = {
  url: string;
  label: string;
};

// The server status is authoritative. These URLs only choose a safe UI
// recovery surface; none of them carry event content or authority.
export function notificationRoute(status: NotificationStatus, sessionID: string, eventID: string): NotificationRoute {
  const sid = encodeURIComponent(sessionID);
  const eid = encodeURIComponent(eventID);
  switch (status.status) {
    case 'actionable':
      return { url: status.activityLink || `pokit://activity/${sid}?event=${eid}`, label: 'Activity' };
    case 'already_resolved':
      return { url: `pokit://session/${sid}?notice=resolved&event=${eid}`, label: 'Resolved session' };
    case 'stale_generation':
      return { url: `pokit://session/${sid}?notice=stale&event=${eid}`, label: 'Updated session' };
    case 'session_unavailable':
      return { url: 'pokit://dashboard?notice=session_unavailable', label: 'Sessions' };
    case 'insufficient_permission':
      return { url: 'pokit://settings?notice=insufficient_permission', label: 'Settings' };
    case 'canonical_event_unavailable':
      return { url: 'pokit://dashboard?notice=event_unavailable', label: 'Sessions' };
    case 'event_degraded_or_gap':
      return { url: `pokit://session/${sid}?notice=degraded&event=${eid}`, label: 'Terminal health' };
    default:
      return { url: `pokit://session/${sid}?notice=unknown&event=${eid}`, label: 'Terminal' };
  }
}

export const notificationStartupMessage = 'Starting up, tap again';
