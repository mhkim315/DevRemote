// M3-auth-4A: one-time WebSocket ticket acquisition.
//
// Tickets are issued by the daemon's M2.5-4 POST /api/device-auth/ws-ticket
// endpoint. They are strictly in-memory, single-use, and never stored or logged.
// The daemon's TTL is the authoritative bound (default 30s).

import { AuthError, TokenManager } from './authClient';

export interface WSTicket {
  ticket: string;
  expiresAt: Date;
}

// getWSTicket obtains a single-use WebSocket ticket from the authenticated
// daemon. The ticket is bound to the target session, device, host, and bearer
// by the daemon; the mobile only provides the session ID.
export async function getWSTicket(
  sessionId: string, tokenMgr: TokenManager, baseURL: string,
): Promise<WSTicket> {
  const token = await tokenMgr.getValidToken();
  let res: Response;
  try {
    res = await fetch(`${baseURL}/api/device-auth/ws-ticket?session=${encodeURIComponent(sessionId)}`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
    });
  } catch {
    throw new AuthError('network_error', 'ws-ticket request failed');
  }
  if (!res.ok) {
    if (res.status === 401 || res.status === 403) throw new AuthError('host_pin_fail', 'unauthorized', res.status);
    if (res.status === 429) throw new AuthError('network_error', 'too many tickets', res.status);
    throw new AuthError('network_error', `ws-ticket server returned ${res.status}`, res.status);
  }
  let data: any;
  try { data = await res.json(); } catch {
    throw new AuthError('network_error', 'non-JSON ticket response');
  }
  return validateTicketResponse(data);
}

export const WS_TICKET_MAX_TTL_MS = 30_000; // daemon default TTL

function validateTicketResponse(d: any): WSTicket {
  if (typeof d.ticket !== 'string' || d.ticket.length === 0 || !/^[0-9a-fA-F]+$/.test(d.ticket)) {
    throw new AuthError('network_error', 'invalid ticket in response');
  }
  if (typeof d.expiresAt !== 'string') throw new AuthError('network_error', 'missing expiresAt');
  const expiresAt = new Date(d.expiresAt);
  if (isNaN(expiresAt.getTime())) throw new AuthError('network_error', 'invalid expiresAt');
  if (expiresAt <= new Date()) throw new AuthError('network_error', 'ticket already expired');
  const ttl = expiresAt.getTime() - Date.now();
  if (ttl > WS_TICKET_MAX_TTL_MS) throw new AuthError('network_error', 'ticket TTL exceeds daemon contract');
  // Reject unknown fields.
  const allowed = new Set(['ticket', 'expiresAt']);
  for (const k of Object.keys(d)) {
    if (!allowed.has(k)) throw new AuthError('network_error', `unexpected field in ticket response: ${k}`);
  }
  return { ticket: d.ticket, expiresAt };
}

// wsTicketURL returns a safe WebSocket URL carrying the ticket.
// No bearer, userinfo, fragment, or other query params are included.
export function wsTicketURL(ticket: string, sessionId: string, baseURL: string): string {
  const wsBase = baseURL.replace(/^https/, 'wss').replace(/^http:/, 'ws:');
  return `${wsBase}/term/ws?session=${encodeURIComponent(sessionId)}&ticket=${encodeURIComponent(ticket)}`;
}
