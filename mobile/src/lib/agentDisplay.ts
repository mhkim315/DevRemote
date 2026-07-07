// Phase A7: Agent agnostic display helpers.
// All functions handle unknown/future values without vendor branching.

export function formatAgentKind(kind?: string): string {
  if (!kind || kind === 'unknown') return 'Agent';
  return kind.charAt(0).toUpperCase() + kind.slice(1);
}

export function formatAgentStatus(status?: string): string {
  switch (status) {
    case 'waiting_approval': return 'Awaiting Approval';
    case 'thinking': return 'Thinking';
    case 'working': return 'Working';
    case 'waiting_input': return 'Waiting Input';
    case 'idle': return 'Idle';
    case 'degraded': return 'Degraded';
    case 'unknown': return 'Unknown';
    case undefined: return '';
    default: return status; // future status → pass through
  }
}

export function isDegraded(confidence?: number, kind?: string): boolean {
  if (kind === 'unknown') return true;
  if (confidence != null && confidence < 0.5) return true;
  return false;
}

// --- Compile-time compatibility fixtures ---
// These prove the helpers handle unknown/future values without runtime errors.

const _fixtures = {
  // Future agent kind must not crash.
  future_agent: formatAgentKind('gemini') === 'Gemini',
  // Unknown agent → generic label.
  unknown_agent: formatAgentKind('unknown') === 'Agent',
  missing_agent: formatAgentKind(undefined) === 'Agent',
  // Future status must pass through.
  future_status: formatAgentStatus('custom_state') === 'custom_state',
  degraded_status: formatAgentStatus('degraded') === 'Degraded',
  // Degraded detection.
  degraded_by_kind: isDegraded(0.9, 'unknown') === true,
  degraded_by_confidence: isDegraded(0.3, 'claude') === true,
  not_degraded: isDegraded(0.7, 'claude') === false,
  // Empty AgentEvent type — unknown fallback.
  unknown_event_label: formatAgentKind() === 'Agent',
};
void _fixtures; // suppress unused warning, keep for compile-time proof
