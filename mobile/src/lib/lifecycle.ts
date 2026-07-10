// Pure, typed helpers for the mobile session-lifecycle UX (M3a+).
// Kept free of React/JSX so the branching logic is testable and reusable.

import type { SessionProfile, SessionLifecycle } from './client';

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
