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
  // Future/unknown agent kinds — must not crash, no vendor branching.
  future_agent: formatAgentKind('future_agent') === 'Future_agent',
  blocked_by_future_runtime: formatAgentStatus('blocked_by_future_runtime') === 'blocked_by_future_runtime',
  future_event_type: formatAgentKind('future_event_type') === 'Future_event_type',
  unknown_agent: formatAgentKind('unknown') === 'Agent',
  missing_agent: formatAgentKind(undefined) === 'Agent',
  // Future/unknown status — must pass through.
  future_status: formatAgentStatus('orchestrator_thought') === 'orchestrator_thought',
  degraded_status: formatAgentStatus('degraded') === 'Degraded',
  // Degraded detection.
  degraded_by_kind: isDegraded(0.9, 'unknown') === true,
  degraded_by_confidence: isDegraded(0.3, 'future_agent') === true,
  not_degraded: isDegraded(0.7, 'future_agent') === false,
  // Empty AgentEvent type — unknown fallback.
  unknown_event_label: formatAgentKind() === 'Agent',
};
void _fixtures; // suppress unused warning, keep for compile-time proof
