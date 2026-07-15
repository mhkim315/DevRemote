// SP0.5-R1 blocker 4 — the host-bound mobile device transport drives the SAME
// managed production routes the backend route tests authorize: exact paths,
// methods, and bodies, and the host-binding fail-closed refusal.
jest.mock('../src/lib/authTransport', () => ({
  authenticatedFetch: jest.fn(async () => ({
    ok: true,
    status: 200,
    json: async () => ({}),
  })),
}));

import { authenticatedFetch } from '../src/lib/authTransport';
import { canonicalOrigin } from '../src/lib/authMode';
import {
  setBaseURL, setDeviceAuth, getManagedEvents, postManagedPrompt,
} from '../src/lib/client';

const mockFetch = authenticatedFetch as jest.Mock;
const BASE = 'https://127.0.0.1:9171';
const pairedOrigin = () => canonicalOrigin(BASE).origin!;

describe('managed routes over the host-bound device transport', () => {
  afterEach(() => {
    setDeviceAuth(null);
    mockFetch.mockClear();
  });

  it('reads events and posts prompts on the exact production routes', async () => {
    setBaseURL(BASE);
    setDeviceAuth({ tokenManager: {} as any, origin: pairedOrigin() });

    await getManagedEvents('codex_app_server:x', 1, 5);
    expect(mockFetch.mock.calls[0][0]).toBe(
      `${BASE}/api/managed-sessions/codex_app_server%3Ax/events?epoch=1&cursor=5`,
    );

    await postManagedPrompt('codex_app_server:x', 1, 'hi');
    const [url, init] = mockFetch.mock.calls[1];
    expect(url).toBe(`${BASE}/api/managed-sessions/codex_app_server%3Ax/prompt`);
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body)).toEqual({ epoch: 1, text: 'hi' });
  });

  it('refuses to send the device credential to a non-paired host', async () => {
    setBaseURL('https://attacker:9171');
    setDeviceAuth({ tokenManager: {} as any, origin: pairedOrigin() });

    await expect(getManagedEvents('codex_app_server:x', 1, 0)).rejects.toThrow(/non-paired host/);
    await expect(postManagedPrompt('codex_app_server:x', 1, 'hi')).rejects.toThrow(/non-paired host/);
    expect(mockFetch).not.toHaveBeenCalled();
  });
});
