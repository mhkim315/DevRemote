import { getTranscript } from '../src/lib/client';

beforeEach(() => {
  (global as any).fetch = jest.fn();
});

function mockResponse(status: number, body: any) {
  return { status, ok: status >= 200 && status < 300, json: async () => body } as unknown as Response;
}

function validSegment(overrides: any = {}) {
  return { id: 'abc123', seq: 0, sessionId: 'controlled_pty:test',
    kind: 'agent_event', source: 'agent_event', text: 'hello',
    observedAt: '2026-07-13T10:00:00.000Z', contractVersion: 't3.2', ...overrides };
}

function validResponse(overrides: any = {}) {
  return { sessionId: 'controlled_pty:test', semantic: [validSegment()],
    fallback: [], primarySource: 'agent_event', availability: 'healthy', contractVersion: 't3.2', ...overrides };
}

describe('getTranscript', () => {
  it('calls correct URL and returns valid TranscriptResponse', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200, validResponse()));
    const resp = await getTranscript('controlled_pty:test', 'tok');
    expect(resp).not.toBeNull();
    expect(resp!.sessionId).toBe('controlled_pty:test');
    expect(resp!.semantic).toHaveLength(1);
    expect(resp!.primarySource).toBe('agent_event');
  });

  it('passes after cursor as query param', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200, validResponse()));
    await getTranscript('s', 't', 42);
    expect((global.fetch as jest.Mock).mock.calls[0][0]).toContain('after=42');
  });

  it('throws on non-200', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(500, {}));
    await expect(getTranscript('s', 't')).rejects.toThrow('API error 500');
  });

  it('returns null on invalid JSON body', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200, null));
    expect(await getTranscript('controlled_pty:test', 't')).toBeNull();
  });
});

describe('validateTranscriptResponse', () => {
  it('rejects mismatched sessionId', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200,
      validResponse({ sessionId: 'controlled_pty:other' })));
    expect(await getTranscript('controlled_pty:test', 't')).toBeNull();
  });

  it('rejects missing semantic array', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200,
      { sessionId: 'controlled_pty:test', primarySource: 'agent_event', availability: 'healthy', contractVersion: 't3.2' }));
    expect(await getTranscript('controlled_pty:test', 't')).toBeNull();
  });

  it('rejects oversized semantic', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200,
      validResponse({ semantic: Array(10001).fill(validSegment()) })));
    expect(await getTranscript('controlled_pty:test', 't')).toBeNull();
  });

  it('rejects unknown envelope field', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200,
      { ...validResponse(), extra: 'bad' }));
    expect(await getTranscript('controlled_pty:test', 't')).toBeNull();
  });
});

describe('validateSegment', () => {
  it('rejects cross-session segment', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200,
      validResponse({ semantic: [validSegment({ sessionId: 'controlled_pty:other' })] })));
    expect(await getTranscript('controlled_pty:test', 't')).toBeNull();
  });

  it('rejects segment missing id', async () => {
    const seg = validSegment(); delete (seg as any).id;
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200,
      validResponse({ semantic: [seg] })));
    expect(await getTranscript('controlled_pty:test', 't')).toBeNull();
  });

  it('rejects segment with unknown kind', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200,
      validResponse({ semantic: [validSegment({ kind: 'bad_kind' })] })));
    expect(await getTranscript('controlled_pty:test', 't')).toBeNull();
  });

  it('rejects oversized text', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200,
      validResponse({ semantic: [validSegment({ text: 'x'.repeat(50000) })] })));
    expect(await getTranscript('controlled_pty:test', 't')).toBeNull();
  });

  it('rejects unknown segment field', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200,
      validResponse({ semantic: [validSegment({ extra: 'bad' })] })));
    expect(await getTranscript('controlled_pty:test', 't')).toBeNull();
  });

  it('rejects wrong contract version', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200,
      validResponse({ semantic: [validSegment({ contractVersion: 't0.1' })] })));
    expect(await getTranscript('controlled_pty:test', 't')).toBeNull();
  });

  it('rejects agent_event with wrong source', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200,
      validResponse({ semantic: [validSegment({ kind: 'agent_event', source: 'byte_stream' })] })));
    expect(await getTranscript('controlled_pty:test', 't')).toBeNull();
  });

  it('requires at least one agent_event when primarySource is agent_event', async () => {
    // terminal_output segment but primarySource claims agent_event → reject.
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200, {
      sessionId: 'controlled_pty:test', semantic: [validSegment({ kind: 'terminal_output', source: 'byte_stream' })],
      fallback: [], primarySource: 'agent_event', availability: 'healthy', contractVersion: 't3.2',
    }));
    expect(await getTranscript('controlled_pty:test', 't')).toBeNull();
  });

  it('accepts valid response with semantic and fallback', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200, {
      sessionId: 'controlled_pty:test',
      semantic: [validSegment()],
      fallback: [validSegment({ id: 'def456', seq: 0, kind: 'terminal_output', source: 'byte_stream' })],
      primarySource: 'agent_event', availability: 'healthy', contractVersion: 't3.2',
    }));
    const resp = await getTranscript('controlled_pty:test', 't');
    expect(resp).not.toBeNull();
    expect(resp!.semantic).toHaveLength(1);
    expect(resp!.fallback).toHaveLength(1);
  });
});

describe('TranscriptResponse channels', () => {
  it('semantic-only with agent_event', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200, {
      sessionId: 's', semantic: [{ ...validSegment({ sessionId: 's' }), kind: 'agent_event', source: 'agent_event' }],
      fallback: [], primarySource: 'agent_event', availability: 'healthy', contractVersion: 't3.2',
    }));
    const r = await getTranscript('s', 't');
    expect(r).not.toBeNull();
    expect(r!.semantic).toHaveLength(1);
    expect(r!.semantic[0].kind).toBe('agent_event');
  });

  it('fallback-only with terminal_output', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200, {
      sessionId: 's', semantic: [],
      fallback: [{ ...validSegment({ id: 'f1', sessionId: 's' }), kind: 'terminal_output', source: 'byte_stream' }],
      primarySource: 'byte_stream', availability: 'healthy', contractVersion: 't3.2',
    }));
    const r = await getTranscript('s', 't');
    expect(r).not.toBeNull();
    expect(r!.fallback).toHaveLength(1);
    expect(r!.primarySource).toBe('byte_stream');
  });

  it('both channels with distinct sources', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200, {
      sessionId: 's',
      semantic: [{ ...validSegment({ sessionId: 's' }), source: 'agent_event' }],
      fallback: [{ ...validSegment({ id: 'f1', sessionId: 's' }), kind: 'terminal_output', source: 'byte_stream' }],
      primarySource: 'agent_event', availability: 'healthy', contractVersion: 't3.2',
    }));
    const r = await getTranscript('s', 't');
    expect(r).not.toBeNull();
    expect(r!.semantic[0].source).toBe('agent_event');
    expect(r!.fallback![0].source).toBe('byte_stream');
  });

  it('byteStreamSuppressed flag present', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200, {
      sessionId: 's', semantic: [], fallback: [],
      primarySource: 'byte_stream', byteStreamSuppressed: true, availability: 'healthy', contractVersion: 't3.2',
    }));
    const r = await getTranscript('s', 't');
    expect(r).not.toBeNull();
    expect(r!.byteStreamSuppressed).toBe(true);
  });
});
describe('TranscriptResponse channels', () => {
  it('semantic-only with agent_event', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200, {
      sessionId: 's', semantic: [validSegment({ sessionId: 's', kind: 'agent_event', source: 'agent_event' })],
      fallback: [], primarySource: 'agent_event', availability: 'healthy', contractVersion: 't3.2',
    }));
    const r = await getTranscript('s', 't');
    expect(r).not.toBeNull();
    expect(r!.semantic).toHaveLength(1);
    expect(r!.semantic[0].kind).toBe('agent_event');
  });

  it('fallback-only with terminal_output', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200, {
      sessionId: 's', semantic: [],
      fallback: [validSegment({ id: 'f1', sessionId: 's', kind: 'terminal_output', source: 'byte_stream' })],
      primarySource: 'byte_stream', availability: 'healthy', contractVersion: 't3.2',
    }));
    const r = await getTranscript('s', 't');
    expect(r).not.toBeNull();
    expect(r!.fallback).toHaveLength(1);
    expect(r!.primarySource).toBe('byte_stream');
  });

  it('both channels with distinct sources', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200, {
      sessionId: 's',
      semantic: [validSegment({ id: 's1', sessionId: 's', source: 'agent_event' })],
      fallback: [validSegment({ id: 'f1', sessionId: 's', kind: 'terminal_output', source: 'byte_stream' })],
      primarySource: 'agent_event', availability: 'healthy', contractVersion: 't3.2',
    }));
    const r = await getTranscript('s', 't');
    expect(r).not.toBeNull();
    expect(r!.semantic[0].source).toBe('agent_event');
    expect(r!.fallback![0].source).toBe('byte_stream');
  });

  it('byteStreamSuppressed flag present', async () => {
    (global.fetch as jest.Mock).mockResolvedValueOnce(mockResponse(200, {
      sessionId: 's', semantic: [], fallback: [],
      primarySource: 'byte_stream', byteStreamSuppressed: true, availability: 'healthy', contractVersion: 't3.2',
    }));
    const r = await getTranscript('s', 't');
    expect(r).not.toBeNull();
    expect(r!.byteStreamSuppressed).toBe(true);
  });
});
