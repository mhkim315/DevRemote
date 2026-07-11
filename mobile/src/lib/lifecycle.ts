// Pure, typed helpers for the mobile session-lifecycle UX (M3a+).
// Kept free of React/JSX so the branching logic is testable and reusable.

import type { SessionProfile, SessionLifecycle } from './client';

// ── M3b: authoritative lifecycle state + action policy (pure, testable) ──

// ManagedLifecycleState is the public daemon lifecycle
// (companion-daemon/internal/term/lifecycle.go). `unknown` is the fail-closed
// value used before any authoritative signal arrives.
export type ManagedLifecycleState =
  | 'starting' | 'running' | 'stopping' | 'exited' | 'killed' | 'failed' | 'unknown';

const TERMINAL_STATES: ReadonlySet<ManagedLifecycleState> = new Set([
  'exited', 'killed', 'failed',
]);

// isTerminalState reports whether a lifecycle state is an end state (no further
// action except Delete History).
export function isTerminalState(s: ManagedLifecycleState): boolean {
  return TERMINAL_STATES.has(s);
}

// Session capability/state shape the policy reads. Kept structural so both the
// telemetry DTO (adapterCapabilities) and tests can supply it.
export interface SessionCapabilityView {
  adapterCapabilities?: string[];
}

// readCapabilities extracts the two independent dimensions the lifecycle UX gates
// on — process-lifecycle ownership (managedLifecycle) and keystroke delivery
// (input) — from the daemon adapterCapabilities. Never inferred from adapter
// names. Missing/unknown capabilities fail closed to false (View Only).
export function readCapabilities(session: SessionCapabilityView | null | undefined): { managed: boolean; inputCapable: boolean } {
  const caps = session?.adapterCapabilities;
  if (!Array.isArray(caps)) return { managed: false, inputCapable: false };
  return {
    managed: caps.includes('managedLifecycle'),
    inputCapable: caps.includes('input'),
  };
}

// Monotonic rank so state can only advance starting→running→stopping→terminal.
// A terminal state never regresses; server-authoritative values win ties.
const STATE_RANK: Record<ManagedLifecycleState, number> = {
  unknown: 0, starting: 1, running: 2, stopping: 3, exited: 4, killed: 4, failed: 4,
};

// reconcileState applies an incoming authoritative state monotonically. The
// server (action response or list refresh) is authoritative, but a later signal
// must not REGRESS a session that is already stopping/terminal (e.g. a stale list
// snapshot that still shows the process as live). A terminal state is sticky.
// This never invents a terminal state — callers pass only real server values.
export function reconcileState(prev: ManagedLifecycleState, incoming: ManagedLifecycleState): ManagedLifecycleState {
  if (isTerminalState(prev)) return prev; // terminal is final
  if (STATE_RANK[incoming] >= STATE_RANK[prev]) return incoming;
  return prev;
}

// stateFromActionResult maps a daemon LifecycleActionResult to a display state.
// The daemon returns running|stopping|exited|killed|failed; an unrecognised value
// is treated as `unknown` (fail closed — never a fabricated terminal state).
export function stateFromActionResult(result: { state?: string } | null | undefined): ManagedLifecycleState {
  const s = result?.state as ManagedLifecycleState | undefined;
  if (s && s in STATE_RANK) return s;
  return 'unknown';
}

// PendingAction is the single in-flight lifecycle request, used to block
// duplicate taps and disable other actions while one is running.
export type PendingAction = 'stop' | 'kill' | 'delete' | null;

export interface ActionPolicyInput {
  managed: boolean;
  inputCapable: boolean;
  state: ManagedLifecycleState;
  pending: PendingAction;
}

// ActionPolicy is the pure, presentational decision for the lifecycle controls.
// UI hiding is defense in depth — the daemon remains the authorization boundary.
export interface ActionPolicy {
  canStop: boolean;        // managed + running (no action in flight)
  canForceKill: boolean;   // managed + stopping (explicit destructive fallback)
  canDelete: boolean;      // managed + terminal (record deletion only)
  inputEnabled: boolean;   // keystroke delivery permitted right now
  viewOnly: boolean;       // no managed lifecycle controls at all
  terminal: boolean;       // session is in an end state
  statusLabel: string;     // human status for the header
}

// computeActionPolicy is the single source of truth for which lifecycle controls
// are shown/enabled, derived ONLY from declared capability + authoritative state
// + any in-flight action. Fails closed: a non-managed or unknown-capability
// session is View Only with no destructive controls; every destructive flag is
// false while an action is pending (duplicate-tap prevention).
export function computeActionPolicy(input: ActionPolicyInput): ActionPolicy {
  const { managed, inputCapable, state, pending } = input;
  const busy = pending !== null;
  const terminal = isTerminalState(state);

  if (!managed) {
    // External/observe-only/unknown: no managed lifecycle controls. Declared
    // input (e.g. external tmux) is retained; observe-only/unknown have no input
    // capability so this is false anyway.
    return {
      canStop: false, canForceKill: false, canDelete: false,
      inputEnabled: inputCapable,
      viewOnly: true, terminal: false,
      statusLabel: inputCapable ? 'External' : 'View only',
    };
  }

  return {
    canStop: state === 'running' && !busy,
    canForceKill: state === 'stopping' && !busy,
    canDelete: terminal && !busy,
    // Managed input is allowed only while running (never starting/stopping/terminal).
    inputEnabled: inputCapable && state === 'running',
    viewOnly: false,
    terminal,
    statusLabel: managedStatusLabel(state),
  };
}

function managedStatusLabel(state: ManagedLifecycleState): string {
  switch (state) {
    case 'starting': return 'Starting…';
    case 'running': return 'Running';
    case 'stopping': return 'Stopping…';
    case 'exited': return 'Session ended';
    case 'killed': return 'Killed';
    case 'failed': return 'Failed';
    default: return 'View only';
  }
}

// ── M3a: New Session create/profile helpers ──

// canCreateProfile reports whether a launch profile can be selected. A profile
// whose executable is not installed on the Mac is shown but not selectable.
export function canCreateProfile(p: SessionProfile): boolean {
  return !!p && p.available === true;
}

// isRunnable gates navigation to Live Terminal: the create response must be a
// Recorder-ready RUNNING controlled_pty session with a canonical id. Any other
// 2xx shape (missing id, wrong adapter, non-running state) is an API contract
// error — the caller must stay in the form, not open a broken session.
export function isRunnable(created: Partial<SessionLifecycle> | null | undefined): created is SessionLifecycle {
  return !!created
    && typeof created.id === 'string' && created.id.length > 0
    && created.adapter === 'controlled_pty'
    && created.state === 'running';
}

// validateName performs friendly client-side validation of an optional display
// name. The daemon remains authoritative. Returns an error string, or null when
// acceptable (empty is acceptable — the daemon will default it).
export function validateName(name: string): string | null {
  const trimmed = (name ?? '').trim();
  if (trimmed.length === 0) return null; // optional
  if (trimmed.length > 64) return 'Name must be 64 characters or fewer.';
  if (/[\n\r\t]/.test(trimmed)) return 'Name must not contain line breaks or tabs.';
  return null;
}

// validateCwd performs friendly client-side validation of an optional working
// directory. Empty is acceptable (daemon default). A non-empty value must look
// like an absolute path; the daemon performs the authoritative CWD check.
export function validateCwd(cwd: string): string | null {
  const trimmed = (cwd ?? '').trim();
  if (trimmed.length === 0) return null; // optional
  if (!trimmed.startsWith('/')) return 'Working directory must be an absolute path (start with /).';
  if (/[\n\r\t]/.test(trimmed)) return 'Working directory must not contain line breaks or tabs.';
  return null;
}
