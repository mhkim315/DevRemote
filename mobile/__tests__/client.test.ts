import {
  setBaseURL, createSession, listSessionProfiles, ConnectivityFailure, PokitError,
} from '../src/lib/client';

// Minimal fetch Response stub for checkedFetch (uses status, ok, json()).
function mockResponse(status: number, body: any) {
  return {
    status,
    ok: status >= 200 && status < 300,
    json: async () => body,
  } as unknown as Response;
}

describe('createSession (payload + contract)', () => {
  beforeEach(() => {
    setBaseURL('http://daemon.test');
    (global as any).fetch = jest.fn();
  });

  it('POSTs only {profileId,name,cwd} with a bearer header — no id/runner/command', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(
      mockResponse(200, { id: 'controlled_pty:shell-1', adapter: 'controlled_pty', name: 'Shell', state: 'running' }),
    );
    const res = await createSession({ profileId: 'shell', name: 'work', cwd: '/tmp' }, 'tok-123');

    expect(global.fetch).toHaveBeenCalledTimes(1);
    const [url, init] = (global.fetch as jest.Mock).mock.calls[0];
    expect(url).toBe('http://daemon.test/api/sessions');
    expect(init.method).toBe('POST');
    expect(init.headers.Authorization).toBe('Bearer tok-123');
    const body = JSON.parse(init.body);
    expect(body).toEqual({ profileId: 'shell', name: 'work', cwd: '/tmp' });
    expect(body).not.toHaveProperty('id');
    expect(body).not.toHaveProperty('runner');
    expect(body).not.toHaveProperty('command');
    expect(body).not.toHaveProperty('executable');
    expect(res.state).toBe('running');
  });

  it('sends empty name/cwd when omitted (daemon derives the default name)', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(
      mockResponse(200, { id: 'controlled_pty:shell-2', adapter: 'controlled_pty', name: 'Shell', state: 'running' }),
    );
    const res = await createSession({ profileId: 'shell' }, 'tok');
    const body = JSON.parse((global.fetch as jest.Mock).mock.calls[0][1].body);
    expect(body).toEqual({ profileId: 'shell', name: '', cwd: '' });
    // The server-derived default name is returned.
    expect(res.name).toBe('Shell');
  });

  it('maps 401 to a PokitError AuthError (re-auth surface)', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(401, {}));
    await expect(createSession({ profileId: 'shell' }, 'expired')).rejects.toMatchObject({
      name: 'PokitError',
      failure: ConnectivityFailure.AuthError,
    });
  });

  it('propagates a network failure as PokitError NetworkUnreachable', async () => {
    (global.fetch as jest.Mock).mockRejectedValueOnce(new Error('connection refused'));
    await expect(createSession({ profileId: 'shell' })).rejects.toBeInstanceOf(PokitError);
  });
});

describe('listSessionProfiles', () => {
  beforeEach(() => {
    setBaseURL('http://daemon.test');
    (global as any).fetch = jest.fn();
  });
  it('returns the profile array', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(
      mockResponse(200, [{ id: 'shell', label: 'Shell', available: true }, { id: 'codex', label: 'Codex', available: false }]),
    );
    const profiles = await listSessionProfiles('tok');
    expect(profiles).toHaveLength(2);
    expect(profiles[1].available).toBe(false);
  });
  it('returns [] for a non-array response', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200, { unexpected: true }));
    expect(await listSessionProfiles()).toEqual([]);
  });
});
