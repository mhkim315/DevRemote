import { classifyEvents, type OutputSpan } from '../src/lib/transcriptClassify';

function makeSegment(overrides: any = {}) {
  return {
    id: 'abc', seq: 0, sessionId: 's',
    kind: 'agent_event', source: 'agent_event', text: 'hello',
    agentKind: 'codex', eventType: 'assistant_message',
    observedAt: '2026-01-01T00:00:00.000Z', contractVersion: 't3.2',
    ...overrides,
  };
}

describe('classifyEvents (production renderer logic)', () => {
  it('classifies agent_event with correct label', () => {
    const spans = classifyEvents([makeSegment()]);
    expect(spans).toHaveLength(1);
    expect(spans[0].isAgentEvent).toBe(true);
    expect(spans[0].agentLabel).toContain('codex');
    expect(spans[0].agentLabel).toContain('assistant_message');
  });

  it('classifies terminal_output', () => {
    const spans = classifyEvents([makeSegment({ kind: 'terminal_output', source: 'byte_stream', text: '$ ls' })]);
    expect(spans).toHaveLength(1);
    expect(spans[0].isAgentEvent).toBe(false);
    expect(spans[0].isDegraded).toBe(false);
    expect(spans[0].text).toBe('$ ls');
  });

  it('classifies input_boundary as divider', () => {
    const spans = classifyEvents([makeSegment({ kind: 'input_boundary', text: undefined })]);
    expect(spans).toHaveLength(1);
    expect(spans[0].isInput).toBe(true);
    expect(spans[0].text).toBe('');
  });

  it('classifies degraded with reason', () => {
    const spans = classifyEvents([makeSegment({ kind: 'degraded', source: 'unknown', degradedReason: 'snapshot suppressed after terminal input' })]);
    expect(spans).toHaveLength(1);
    expect(spans[0].isDegraded).toBe(true);
    expect(spans[0].text).toContain('suppressed');
  });

  it('classifies ui_omitted as degraded', () => {
    const spans = classifyEvents([makeSegment({ kind: 'ui_omitted' })]);
    expect(spans).toHaveLength(1);
    expect(spans[0].isDegraded).toBe(true);
    expect(spans[0].text).toContain('terminal UI omitted');
  });

  it('separates semantic from fallback channels', () => {
    const semSpan = classifyEvents([makeSegment({ kind: 'agent_event', source: 'agent_event' })])[0];
    const fbSpan = classifyEvents([makeSegment({ kind: 'terminal_output', source: 'byte_stream' })])[0];
    expect(semSpan.isAgentEvent).toBe(true);
    expect(fbSpan.isAgentEvent).toBe(false);
  });

  it('handles empty events', () => {
    expect(classifyEvents([])).toHaveLength(0);
  });

  it('handles legacy ActivityEvent format', () => {
    const spans = classifyEvents([{ type: 'terminal_output', text: 'legacy output', seq: 1 }]);
    expect(spans).toHaveLength(1);
    expect(spans[0].text).toBe('legacy output');
  });

  it('handles legacy terminal_input as divider', () => {
    const spans = classifyEvents([{ type: 'terminal_input', seq: 1 }]);
    expect(spans).toHaveLength(1);
    expect(spans[0].isInput).toBe(true);
  });

  it('renders newest-first (inverted order)', () => {
    const events = [
      makeSegment({ id: 'e1', seq: 0, text: 'first' }),
      makeSegment({ id: 'e2', seq: 1, text: 'second' }),
    ];
    const spans = classifyEvents(events);
    expect(spans[0].text).toBe('second');
    expect(spans[1].text).toBe('first');
  });

  it('skips terminal_output with empty text', () => {
    const spans = classifyEvents([makeSegment({ kind: 'terminal_output', text: '' })]);
    expect(spans).toHaveLength(0);
  });
});
