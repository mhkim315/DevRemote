// PokitClient centralises all API calls to the DevRemote daemon.
// Replaces scattered fetch(`${config.BASE_URL}/...`) calls.

let _baseURL = '';

export function setBaseURL(url: string) {
  _baseURL = url;
}

export function getBaseURL(): string {
  return _baseURL;
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
    // R1a: use checkedFetch so classification matches all other API calls.
    const res = await checkedFetch(`${_baseURL}/api/sessions`, { headers: authHeaders(token) });
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
  const res = await checkedFetch(`${_baseURL}/api/sessions`, { headers: authHeaders(token) });
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
// reflects whether the executable is installed on the Mac.
export async function listSessionProfiles(token?: string): Promise<SessionProfile[]> {
  const res = await checkedFetch(`${_baseURL}/api/session-profiles`, { headers: authHeaders(token) });
  const data = await res.json();
  return Array.isArray(data) ? data : [];
}

// createSession launches a daemon-owned controlled_pty session from a profile.
// The client sends ONLY {profileId, name, cwd} — never a canonical id, runner,
// executable, or shell command. The daemon generates the canonical id and only
// reports `running` once the Recorder is ready.
export async function createSession(
  input: { profileId: string; name?: string; cwd?: string },
  token?: string,
): Promise<SessionLifecycle> {
  const res = await checkedFetch(`${_baseURL}/api/sessions`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...authHeaders(token) },
    body: JSON.stringify({ profileId: input.profileId, name: input.name ?? '', cwd: input.cwd ?? '' }),
  });
  return res.json();
}

// createOrUpdateSession is the LEGACY create path. M3a's New Session flow no
// longer calls it; it remains only for the edit/color presentation path until
// M3b replaces the edit-modal lifecycle actions.
export async function createOrUpdateSession(id: string, runner: string, color: string, token?: string) {
  const res = await checkedFetch(`${_baseURL}/api/sessions`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...authHeaders(token) },
    body: JSON.stringify({ id, runner, runnerColor: color }),
  });
  return res.json();
}

export async function deleteSession(id: string, token?: string) {
  const res = await checkedFetch(`${_baseURL}/api/sessions?id=${encodeURIComponent(id)}`, {
    method: 'DELETE',
    headers: authHeaders(token),
  });
  return res.json();
}

export async function getSessionHistory(sessionID: string, token?: string) {
  const res = await checkedFetch(
    `${_baseURL}/api/sessions?history=${encodeURIComponent(sessionID)}`,
    { headers: authHeaders(token) }
  );
  return res.json();
}

// E8f: fetch captured terminal activity (transcript).
export async function getActivityHistory(sessionID: string, token?: string) {
  const res = await checkedFetch(
    `${_baseURL}/api/sessions?activity=${encodeURIComponent(sessionID)}`,
    { headers: authHeaders(token) }
  );
  return res.json();
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
