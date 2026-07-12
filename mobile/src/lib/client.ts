// PokitClient centralises all API calls to the DevRemote daemon.
// Replaces scattered fetch(`${config.BASE_URL}/...`) calls.

import { authenticatedFetch } from './authTransport';
import { canonicalOrigin } from './authMode';
import { parseLifecycleResult } from './lifecycle';
import type { TokenManager } from './authClient';

let _baseURL = '';

export function setBaseURL(url: string) {
  _baseURL = url;
}

export function getBaseURL(): string {
  return _baseURL;
}

// ── M3-auth-4A: central, HOST-BOUND device-bearer transport ──
//
// When a paired device is active, remote REST reads authenticate with the
// device bearer (with 401 refresh) via the accepted authenticated transport.
// The bearer is bound to the canonical origin of the paired host: apiGet sends
// it ONLY when the current base URL canonicalises to that exact origin, and
// fails closed (no network request) otherwise. This prevents a Host A bearer
// from ever reaching a Host B endpoint the user typed or scanned after pairing.
export interface DeviceAuth {
  tokenManager: TokenManager;
  origin: string; // canonical scheme://host[:port] of the paired host
}

let _deviceAuth: DeviceAuth | null = null;

export function setDeviceAuth(auth: DeviceAuth | null) {
  _deviceAuth = auth;
}

export function hasDeviceAuth(): boolean {
  return _deviceAuth !== null;
}

// apiGet centralises read authentication: the host-bound device bearer when a
// paired device is active (and only for the paired origin), otherwise the
// legacy token. Errors are normalised to PokitError so callers keep the same
// connectivity classification.
async function apiGet(path: string, legacyToken?: string): Promise<Response> {
  if (_deviceAuth) {
    // Host-bound fail-closed: the device bearer is transmitted ONLY to the
    // exact paired origin. A userinfo/query/fragment/path variant or a
    // different host canonicalises to a mismatch and sends nothing.
    const { origin, error } = canonicalOrigin(_baseURL);
    if (error || !origin || origin !== _deviceAuth.origin) {
      throw new PokitError(
        'Refusing to send the device credential to a non-paired host',
        ConnectivityFailure.AuthError,
        0,
      );
    }
    const url = `${_baseURL}${path}`;
    let res: Response;
    try {
      res = await authenticatedFetch(url, undefined, _deviceAuth.tokenManager);
    } catch (e: any) {
      if (e && e.code === 'network_error') {
        throw new PokitError('Daemon unreachable: ' + (e.message || 'request failed'), ConnectivityFailure.NetworkUnreachable);
      }
      // AuthError (host_pin_fail) or any other rejection → auth failure.
      throw new PokitError('Authentication failed. Re-scan the QR code.', ConnectivityFailure.AuthError, (e && e.statusCode) || 401);
    }
    if (!res.ok) {
      throw new PokitError(`API error ${res.status}: ${url}`, ConnectivityFailure.APIError, res.status);
    }
    return res;
  }
  return checkedFetch(`${_baseURL}${path}`, { headers: authHeaders(legacyToken) });
}

// apiWrite centralises non-idempotent WRITE authentication (POST/DELETE) with
// the SAME host-bound fail-closed contract as apiGet. It backs the M3a create
// and the M3b Stop/Kill/Delete-History lifecycle actions. A body is sent only
// when one is provided (create); lifecycle actions send none.
//
// Non-idempotent safety (handoff §6): authenticatedFetch proactively refreshes
// via getValidToken() BEFORE the request but never re-issues a POST/DELETE after
// a 401 — a rejected write surfaces a recoverable AuthError WITHOUT replaying the
// action. Exactly one request is ever sent. A non-2xx (404/409/422/500) becomes
// an APIError carrying the status so the caller can branch (e.g. 409 → "Stop the
// session first"); a 401/403 becomes an AuthError carrying the status so the
// caller can distinguish an invalid bearer (401) from missing permission (403).
async function apiWrite(method: 'POST' | 'DELETE', path: string, body?: unknown, legacyToken?: string): Promise<Response> {
  const hasBody = body !== undefined;
  const payload = hasBody ? JSON.stringify(body) : undefined;
  if (_deviceAuth) {
    // Host-bound fail-closed: never send the device bearer (or the request) to
    // a non-paired origin or any URL variant.
    const { origin, error } = canonicalOrigin(_baseURL);
    if (error || !origin || origin !== _deviceAuth.origin) {
      throw new PokitError(
        'Refusing to send the device credential to a non-paired host',
        ConnectivityFailure.AuthError,
        0,
      );
    }
    const url = `${_baseURL}${path}`;
    const init: RequestInit = { method };
    if (hasBody) {
      init.headers = { 'Content-Type': 'application/json' };
      init.body = payload;
    }
    let res: Response;
    try {
      res = await authenticatedFetch(url, init, _deviceAuth.tokenManager);
    } catch (e: any) {
      if (e && e.code === 'network_error') {
        throw new PokitError('Daemon unreachable: ' + (e.message || 'request failed'), ConnectivityFailure.NetworkUnreachable);
      }
      // host_pin_fail (401/403, incl. a member/read-only caller) is NOT retried
      // for this non-idempotent write: surface a recoverable auth error, no
      // duplicate action. statusCode preserves the 401 vs 403 distinction.
      throw new PokitError('Authentication failed. Re-scan the QR code.', ConnectivityFailure.AuthError, (e && e.statusCode) || 401);
    }
    if (!res.ok) {
      throw new PokitError(`API error ${res.status}: ${url}`, ConnectivityFailure.APIError, res.status);
    }
    return res;
  }
  const init: RequestInit = { method, headers: authHeaders(legacyToken) };
  if (hasBody) {
    init.headers = { 'Content-Type': 'application/json', ...authHeaders(legacyToken) };
    init.body = payload;
  }
  return checkedFetch(`${_baseURL}${path}`, init);
}

// apiPost is the POST specialisation of apiWrite (a body is always sent). Kept
// as a thin wrapper so the accepted M3a create path and its tests are unchanged.
async function apiPost(path: string, body: unknown, legacyToken?: string): Promise<Response> {
  return apiWrite('POST', path, body, legacyToken);
}

// ── R1a typed connectivity errors ──

export enum ConnectivityFailure {
  None = '',
  NetworkUnreachable = 'network_unreachable',
  APIError = 'api_error',
  AuthError = 'auth_error',
  Timeout = 'timeout',
}

export class PokitError extends Error {
  failure: ConnectivityFailure;
  statusCode: number;
  constructor(message: string, failure: ConnectivityFailure, statusCode: number = 0) {
    super(message);
    this.name = 'PokitError';
    this.failure = failure;
    this.statusCode = statusCode;
  }
}

async function checkedFetch(url: string, init?: RequestInit): Promise<Response> {
  let res: Response;
  try {
    res = await fetch(url, init);
  } catch (e: any) {
    // fetch itself threw — network-level failure (DNS, refused, timeout).
    const msg = e?.message || String(e);
    if (msg.includes('timed out') || msg.includes('timeout') || msg.includes('abort')) {
      throw new PokitError('Daemon unreachable: connection timed out', ConnectivityFailure.Timeout);
    }
    throw new PokitError('Daemon unreachable: ' + msg, ConnectivityFailure.NetworkUnreachable);
  }
  if (res.status === 401 || res.status === 403) {
    throw new PokitError('Authentication failed. Re-scan the QR code.', ConnectivityFailure.AuthError, res.status);
  }
  if (!res.ok) {
    throw new PokitError(
      `API error ${res.status}: ${url}`,
      ConnectivityFailure.APIError,
      res.status,
    );
  }
  return res;
}

// ── R1a: daemon reachability probe ──

export interface ReachabilityResult {
  reachable: boolean;
  sessionsLoaded: boolean;
  sessionsEmpty: boolean;
  failure: ConnectivityFailure;
  statusCode: number;
  errorMessage: string;
}

/**
 * probeDaemon checks whether the daemon is reachable and returns structured
 * diagnostics. Callers can distinguish:
 *   - daemon unreachable (NetworkUnreachable / Timeout)
 *   - auth error (AuthError)
 *   - sessions API error (APIError)
 *   - daemon reachable, no sessions (reachable + sessionsEmpty)
 *   - daemon reachable, sessions loaded (reachable + sessionsLoaded)
 */
export async function probeDaemon(token?: string): Promise<ReachabilityResult> {
  try {
    // Device-auth aware: paired devices probe with the device bearer.
    const res = await apiGet('/api/sessions', token);
    const data = await res.json();
    const sessions = Array.isArray(data) ? data : [];
    return {
      reachable: true,
      sessionsLoaded: true,
      sessionsEmpty: sessions.length === 0,
      failure: ConnectivityFailure.None,
      statusCode: 200,
      errorMessage: '',
    };
  } catch (e: any) {
    if (e instanceof PokitError) {
      // checkedFetch already classified: AuthError, APIError, Timeout, NetworkUnreachable.
      return {
        reachable: e.failure !== ConnectivityFailure.NetworkUnreachable && e.failure !== ConnectivityFailure.Timeout,
        sessionsLoaded: false,
        sessionsEmpty: false,
        failure: e.failure,
        statusCode: e.statusCode,
        errorMessage: e.message,
      };
    }
    return {
      reachable: false,
      sessionsLoaded: false,
      sessionsEmpty: false,
      failure: ConnectivityFailure.NetworkUnreachable,
      statusCode: 0,
      errorMessage: e?.message || String(e),
    };
  }
}

// ── Auth helpers ──

function authHeaders(token?: string): Record<string, string> {
  const h: Record<string, string> = {};
  if (token) h['Authorization'] = `Bearer ${token}`;
  return h;
}

// ── API functions ──

export async function listSessions(token?: string): Promise<any[]> {
  const res = await apiGet('/api/sessions', token);
  return res.json();
}

// ── M3a: daemon-owned session lifecycle (typed) ──

// SessionProfile is the UI-safe launch profile. Executable paths are resolved
// server-side and never returned; the client only ever sends a profile id.
export interface SessionProfile {
  id: string;
  label: string;
  available: boolean;
}

// SessionLifecycle mirrors the daemon SessionLifecycle DTO
// (companion-daemon/internal/term/lifecycle.go). State is the authoritative
// server lifecycle: starting | running | stopping | exited | killed | failed.
export interface SessionLifecycle {
  id: string;
  adapter: string;
  profileId?: string;
  name?: string;
  state: string;
}

// listSessionProfiles returns the daemon-owned launch profiles. Availability
// reflects whether the executable is installed on the Mac. Paired devices read
// it over the host-bound device transport (apiGet); the legacy token path is
// used only in explicit_local_dev.
export async function listSessionProfiles(token?: string): Promise<SessionProfile[]> {
  const res = await apiGet('/api/session-profiles', token);
  const data = await res.json();
  return Array.isArray(data) ? data : [];
}

// createSession launches a daemon-owned controlled_pty session from a profile.
// The client sends ONLY {profileId, name, cwd} — never a canonical id, runner,
// executable, or shell command. The daemon generates the canonical id and only
// reports `running` once the Recorder is ready. Paired devices create over the
// host-bound device transport (apiPost); a rejected create is never retried and
// never produces a duplicate session.
export async function createSession(
  input: { profileId: string; name?: string; cwd?: string },
  token?: string,
): Promise<SessionLifecycle> {
  const res = await apiPost(
    '/api/sessions',
    { profileId: input.profileId, name: input.name ?? '', cwd: input.cwd ?? '' },
    token,
  );
  return res.json();
}

// ── M3b: daemon-owned session lifecycle actions (typed) ──

// LifecycleActionResult mirrors the daemon LifecycleResult DTO
// (companion-daemon/internal/term/lifecycle_service.go). `state` is the
// AUTHORITATIVE server lifecycle after the action: running | stopping | exited |
// killed | failed. The client never invents this value.
export interface LifecycleActionResult {
  sessionId: string;
  action: string; // "stop" | "kill" | "delete"
  state: string;
}

// stopSession gracefully stops a managed running session's whole process group
// (daemon SIGTERM→SIGKILL escalation). It is NOT a terminal keystroke, Ctrl-C,
// `exit` string, or WebSocket close — it is the dedicated lifecycle endpoint.
// History/Activity/Transcript are retained. Paired devices ride the host-bound
// device transport; a rejected stop is never replayed. The daemon's authoritative
// state is returned; a repeated Stop is idempotent (returns current state).
export async function stopSession(id: string, token?: string): Promise<LifecycleActionResult> {
  const res = await apiWrite('POST', `/api/sessions/${encodeURIComponent(id)}/stop`, undefined, token);
  return parseLifecycleResult(id, 'stop', await res.json());
}

// killSession force-terminates a managed session's process group (daemon
// SIGKILL). It is the explicit destructive fallback surfaced only after a Stop
// is in progress / has failed, behind a destructive confirmation. Same
// host-bound transport and no-replay contract as stopSession.
export async function killSession(id: string, token?: string): Promise<LifecycleActionResult> {
  const res = await apiWrite('POST', `/api/sessions/${encodeURIComponent(id)}/kill`, undefined, token);
  return parseLifecycleResult(id, 'kill', await res.json());
}

// deleteSessionHistory removes a TERMINAL managed session's retained catalog row
// and Activity/Transcript history via the canonical PATH form
// `DELETE /api/sessions/{id}` — never the legacy query form. The daemon rejects a
// running/stopping session with 409 (surfaced as PokitError statusCode 409 so the
// UI can say "Stop the session first" and refresh instead of retrying). This is a
// record deletion, not a process lifecycle action.
export async function deleteSessionHistory(id: string, token?: string): Promise<LifecycleActionResult> {
  const res = await apiWrite('DELETE', `/api/sessions/${encodeURIComponent(id)}`, undefined, token);
  return parseLifecycleResult(id, 'delete', await res.json());
}

// createOrUpdateSession is the LEGACY create path. M3a's New Session flow no
// longer calls it; it remains only for the edit/color presentation path.
export async function createOrUpdateSession(id: string, runner: string, color: string, token?: string) {
  const res = await checkedFetch(`${_baseURL}/api/sessions`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...authHeaders(token) },
    body: JSON.stringify({ id, runner, runnerColor: color }),
  });
  return res.json();
}

// deleteSession is the LEGACY query-form DELETE (`/api/sessions?id=...`). It must
// NOT be used for mobile lifecycle actions — use deleteSessionHistory (path form,
// device transport, terminal-state gated). Retained only for compatibility.
export async function deleteSession(id: string, token?: string) {
  const res = await checkedFetch(`${_baseURL}/api/sessions?id=${encodeURIComponent(id)}`, {
    method: 'DELETE',
    headers: authHeaders(token),
  });
  return res.json();
}

export async function getSessionHistory(sessionID: string, token?: string) {
  const res = await apiGet(`/api/sessions?history=${encodeURIComponent(sessionID)}`, token);
  return res.json();
}

// E8f: fetch captured terminal activity (transcript).
export async function getActivityHistory(sessionID: string, token?: string) {
  const res = await apiGet(`/api/sessions?activity=${encodeURIComponent(sessionID)}`, token);
  return res.json();
}

// T3: versioned Transcript response envelope with separated semantic/fallback channels.
export interface TranscriptSegment {
  id: string;
  seq: number;
  sessionId: string;
  kind: 'agent_event' | 'terminal_output' | 'input_boundary' | 'degraded' | 'ui_omitted' | 'unknown';
  source: 'agent_event' | 'byte_stream' | 'snapshot_delta' | 'unknown';
  text?: string;
  agentEventRef?: string;
  agentKind?: string;
  eventType?: string;
  toolName?: string;
  confidence?: number;
  byteCount?: number;
  degradedReason?: string;
  observedAt: string;
  contractVersion: string;
}

export interface TranscriptResponse {
  sessionId: string;
  semantic: TranscriptSegment[];
  fallback?: TranscriptSegment[];
  primarySource: 'agent_event' | 'byte_stream' | 'snapshot_delta' | 'unknown';
  contractVersion: string;
}

const VALID_KINDS = ['agent_event', 'terminal_output', 'input_boundary', 'degraded', 'ui_omitted', 'unknown'];
const VALID_SOURCES = ['agent_event', 'byte_stream', 'snapshot_delta', 'unknown'];

function validateSegment(seg: any, expectedSessionID: string): TranscriptSegment | null {
  if (!seg || typeof seg !== 'object') return null;
  if (seg.sessionId !== expectedSessionID) return null;
  if (typeof seg.id !== 'string' || seg.id.length === 0 || seg.id.length > 64) return null;
  if (typeof seg.seq !== 'number' || seg.seq < 0) return null;
  if (!VALID_KINDS.includes(seg.kind)) return null;
  if (!VALID_SOURCES.includes(seg.source)) return null;
  if (seg.text !== undefined && typeof seg.text !== 'string') return null;
  if (seg.text && seg.text.length > 40000) return null; // bounded
  if (seg.contractVersion !== 't3.1') return null;
  return seg as TranscriptSegment;
}

function validateTranscriptResponse(data: any, expectedSessionID: string): TranscriptResponse | null {
  if (!data || typeof data !== 'object') return null;
  if (data.sessionId !== expectedSessionID) return null;
  if (!Array.isArray(data.semantic)) return null;
  if (data.contractVersion !== 't3.1') return null;
  if (!VALID_SOURCES.includes(data.primarySource)) return null;
  // Validate channel consistency: primarySource must match content.
  if (data.primarySource === 'agent_event' && data.semantic.length > 0) {
    const hasNonAgent = data.semantic.some((s: any) => s.kind !== 'agent_event' && s.kind !== 'unknown');
    if (hasNonAgent) return null; // channel/source mismatch
  }
  // Validate each semantic segment.
  for (const seg of data.semantic) {
    if (!validateSegment(seg, expectedSessionID)) return null;
  }
  // Validate fallback if present.
  if (data.fallback !== undefined) {
    if (!Array.isArray(data.fallback)) return null;
    for (const seg of data.fallback) {
      if (!validateSegment(seg, expectedSessionID)) return null;
    }
  }
  return data as TranscriptResponse;
}

export async function getTranscript(sessionID: string, token?: string, after?: number): Promise<TranscriptResponse | null> {
  let path = `/api/sessions/${encodeURIComponent(sessionID)}/transcript`;
  if (after !== undefined && after > 0) {
    path += `?after=${after}`;
  }
  const res = await apiGet(path, token);
  const raw = await res.json();
  return validateTranscriptResponse(raw, sessionID);
}

export async function sendDebugCommand(sessionID: string, command: string, token?: string) {
  const res = await checkedFetch(
    `${_baseURL}/debug/cmd?session=${encodeURIComponent(sessionID)}`,
    { method: 'POST', headers: authHeaders(token), body: command }
  );
  return res;
}

// --- Phase A9: Interaction Request Types and API ---

export interface InputSchema {
  required: boolean;
  placeholder?: string;
  multiline?: boolean;
}

export interface InteractionOption {
  id: string;
  label: string;
  kind: string;     // semantic: "approve", "reject", "neutral", "open", "cancel"
  payload?: string;
  input?: InputSchema;
}

export interface AgentApproval {
  id: string;
  sessionId: string;
  agentKind: string;
  kind: string;     // "approval" | "interaction" | "info"
  status: string;   // "pending", "approved", "rejected"
  prompt: string;
  options: InteractionOption[];
  default: string;
  source: string;
  confidence: number;
  createdAt: string;
  resolvedAt?: string;
}

export async function resolveApproval(
  sessionID: string,
  approvalID: string,
  action: string,
  input?: string,
  token?: string
): Promise<Response> {
  const body: Record<string, string> = { action };
  if (input) body.input = input;
  const res = await checkedFetch(
    `${_baseURL}/api/sessions/${encodeURIComponent(sessionID)}/approvals/${encodeURIComponent(approvalID)}`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...authHeaders(token) },
      body: JSON.stringify(body),
    }
  );
  return res;
}

export async function registerPushToken(token: string, pushToken: string) {
  const res = await checkedFetch(
    `${_baseURL}/push/register?token=${encodeURIComponent(pushToken)}`,
    { headers: authHeaders(token) }
  );
  return res;
}

export function terminalURL(sessionID: string, token?: string): string {
  const params = new URLSearchParams({ session: sessionID });
  if (token) params.set('token', token);
  return `${_baseURL}/term/?${params.toString()}`;
}

export function terminalWebSocketURL(sessionID: string, token?: string): string {
  const wsBase = _baseURL.replace(/^https?/, 'ws');
  const params = new URLSearchParams({ session: sessionID });
  if (token) params.set('token', token);
  return `${wsBase}/term/ws?${params.toString()}`;
}

// terminalWebSocketTicketURL constructs a WebSocket URL that authenticates via a
// one-time ticket (device-auth remote mode). The ticket is acquired by the
// authenticated transport layer (authTransport.getWSTicket).
export function terminalWebSocketTicketURL(sessionID: string, ticket: string): string {
  const wsBase = _baseURL.replace(/^https?/, 'ws');
  return `${wsBase}/term/ws?session=${encodeURIComponent(sessionID)}&ticket=${encodeURIComponent(ticket)}`;
}
