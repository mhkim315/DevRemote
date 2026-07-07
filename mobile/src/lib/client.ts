// PokitClient centralises all API calls to the DevRemote daemon.
// Replaces scattered fetch(`${config.BASE_URL}/...`) calls.

let _baseURL = '';

export function setBaseURL(url: string) {
  _baseURL = url;
}

export function getBaseURL(): string {
  return _baseURL;
}

async function checkedFetch(url: string, init?: RequestInit): Promise<Response> {
  const res = await fetch(url, init);
  if (!res.ok) throw new Error(`API ${res.status}: ${url}`);
  return res;
}

function authHeaders(token?: string): Record<string, string> {
  const h: Record<string, string> = {};
  if (token) h['Authorization'] = `Bearer ${token}`;
  return h;
}

export async function listSessions(token?: string): Promise<any[]> {
  const res = await checkedFetch(`${_baseURL}/api/sessions`, { headers: authHeaders(token) });
  return res.json();
}

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
  token?: string
): Promise<Response> {
  const res = await checkedFetch(
    `${_baseURL}/api/sessions/${encodeURIComponent(sessionID)}/approvals/${encodeURIComponent(approvalID)}`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...authHeaders(token) },
      body: JSON.stringify({ action }),
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
