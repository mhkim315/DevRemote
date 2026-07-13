// ── Transcript classification: pure rendering logic ──
// Extracted from TranscriptRenderer.tsx for testability without RN imports.

export type OutputSpan = {
  text: string;
  isInput: boolean;
  isDegraded: boolean;
  isAgentEvent: boolean;
  agentLabel?: string;
  key: string;
};

export function classifyEvents(events: any[]): OutputSpan[] {
  const result: OutputSpan[] = [];
  for (let i = events.length - 1; i >= 0; i--) {
    const e = events[i];
    if (e.kind) {
      switch (e.kind) {
        case 'agent_event':
          result.push({
            text: e.text || '',
            isInput: false, isDegraded: false, isAgentEvent: true,
            agentLabel: e.agentKind ? `${e.agentKind} · ${e.eventType || ''}` : (e.eventType || ''),
            key: `ae${e.seq}`,
          });
          break;
        case 'terminal_output':
          if ((e.text || '').length > 0) {
            result.push({
              text: e.text, isInput: false, isDegraded: false, isAgentEvent: false,
              key: `to${e.seq}`,
            });
          }
          break;
        case 'input_boundary':
          result.push({ text: '', isInput: true, isDegraded: false, isAgentEvent: false, key: `ib${e.seq}` });
          break;
        case 'degraded':
          result.push({
            text: e.degradedReason || 'Transcript degraded',
            isInput: false, isDegraded: true, isAgentEvent: false, key: `dg${e.seq}`,
          });
          break;
        case 'ui_omitted':
          result.push({
            text: '[terminal UI omitted]',
            isInput: false, isDegraded: true, isAgentEvent: false, key: `uo${e.seq}`,
          });
          break;
        default:
          if ((e.text || '').length > 0) {
            result.push({
              text: e.text, isInput: false, isDegraded: false, isAgentEvent: false, key: `un${e.seq}`,
            });
          }
      }
      continue;
    }
    // Legacy ActivityEvent format.
    if (e.type === 'terminal_output' && (e.text || '').length > 0) {
      result.push({ text: e.text, isInput: false, isDegraded: false, isAgentEvent: false, key: `o${e.seq || i}` });
    } else if (e.type === 'terminal_input') {
      result.push({ text: '', isInput: true, isDegraded: false, isAgentEvent: false, key: `i${e.seq || i}` });
    }
  }
  return result;
}
