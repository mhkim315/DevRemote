// PokitClient centralises all API calls to the DevRemote daemon.
// Replaces scattered fetch(`${config.BASE_URL}/...`) calls.

let _baseURL = '';

export function setBaseURL(url: string) {
  _baseURL = url;
}

export function getBaseURL(): string {
  return _baseURL;
}

function authHeaders(token?: string): Record<string, string> {
  const h: Record<string, string> = {};
  if (token) h['Authorization'] = `Bearer ${token}`;
  return h;
}

export async function listSessions(token?: string) {
  const res = await fetch(`${_baseURL}/api/sessions`, { headers: authHeaders(token) });
  return res.json();
}

export async function createOrUpdateSession(
  id: string, runner: string, color: string, token?: string
) {
  const res = await fetch(`${_baseURL}/api/sessions`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...authHeaders(token) },
    body: JSON.stringify({ id, runner, runnerColor: color }),
  });
  if (!res.ok) throw new Error('API Error');
  return res.json();
}

export async function deleteSession(id: string, token?: string) {
  const res = await fetch(`${_baseURL}/api/sessions?id=${encodeURIComponent(id)}`, {
    method: 'DELETE',
    headers: authHeaders(token),
  });
  if (!res.ok) throw new Error('API Error');
  return res.json();
}

export async function getSessionHistory(sessionID: string, token?: string) {
  const res = await fetch(
    `${_baseURL}/api/sessions?history=${encodeURIComponent(sessionID)}`,
    { headers: authHeaders(token) }
  );
  return res.json();
}

export async function sendDebugCommand(sessionID: string, command: string, token?: string) {
  const res = await fetch(
    `${_baseURL}/debug/cmd?session=${encodeURIComponent(sessionID)}`,
    { method: 'POST', headers: authHeaders(token), body: command }
  );
  return res;
}

export async function registerPushToken(token: string, pushToken: string) {
  const res = await fetch(
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
