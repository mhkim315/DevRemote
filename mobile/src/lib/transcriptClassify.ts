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

// PA3 Step 1: bounded dedup of adjacent same-AgentEventRef spans.
// Non-adjacent duplicates (interleaved) render as-is.
const MAX_DEDUP_REFS = 128;

export function classifyEvents(events: any[], seenRefs?: Set<string>): OutputSpan[] {
  const result: OutputSpan[] = [];
  let lastAgentEventRef = '';
  for (let i = events.length - 1; i >= 0; i--) {
    const e = events[i];
    if (e.kind) {
      switch (e.kind) {
        case 'agent_event': {
          // PA3 Step 1: collapse adjacent same-agentEventRef spans.
          const ref = e.agentEventRef || '';
          if (ref && ref === lastAgentEventRef) {
            // Skip duplicate — same cursor-replayed event.
            break;
          }
          if (ref) {
            lastAgentEventRef = ref;
            if (seenRefs) {
              if (seenRefs.has(ref)) break; // non-adjacent, already rendered
              seenRefs.add(ref);
              if (seenRefs.size > MAX_DEDUP_REFS) seenRefs.clear(); // bounded
            }
          }
          result.push({
            text: e.text || '',
            isInput: false, isDegraded: false, isAgentEvent: true,
            agentLabel: e.agentKind ? `${e.agentKind} · ${e.eventType || ''}` : (e.eventType || ''),
            key: `ae${e.seq}`,
          });
          break;
        }
        case 'terminal_output':
          lastAgentEventRef = '';
          if ((e.text || '').length > 0) {
            result.push({
              text: e.text, isInput: false, isDegraded: false, isAgentEvent: false,
              key: `to${e.seq}`,
            });
          }
          break;
        case 'input_boundary':
          lastAgentEventRef = '';
          result.push({ text: '', isInput: true, isDegraded: false, isAgentEvent: false, key: `ib${e.seq}` });
          break;
        case 'degraded':
          lastAgentEventRef = '';
          result.push({
            text: e.degradedReason || 'Transcript degraded',
            isInput: false, isDegraded: true, isAgentEvent: false, key: `dg${e.seq}`,
          });
          break;
        case 'ui_omitted':
          lastAgentEventRef = '';
          result.push({
            text: '[terminal UI omitted]',
            isInput: false, isDegraded: true, isAgentEvent: false, key: `uo${e.seq}`,
          });
          break;
        default:
          lastAgentEventRef = '';
          if ((e.text || '').length > 0) {
            result.push({
              text: e.text, isInput: false, isDegraded: false, isAgentEvent: false, key: `un${e.seq}`,
            });
          }
      }
      continue;
    }
    // Legacy ActivityEvent format.
    lastAgentEventRef = '';
    if (e.type === 'terminal_output' && (e.text || '').length > 0) {
      result.push({ text: e.text, isInput: false, isDegraded: false, isAgentEvent: false, key: `o${e.seq || i}` });
    } else if (e.type === 'terminal_input') {
      result.push({ text: '', isInput: true, isDegraded: false, isAgentEvent: false, key: `i${e.seq || i}` });
    }
  }
  return result;
}
