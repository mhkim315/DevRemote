# A1.2 C0 — Claude Approval Surface Contract Note

Status: **C0 BLOCKED — stable PermissionRequest hook does not fire in headless
print mode; exact invocation boundary NOT provable through this surface.**

Date: 2026-07-15. Executor: A1.2 execution agent, canonical checkout
`/Users/mhk/Documents/codex/DevRemote`, branch `feature/phase10-multi-adapter`.

Baseline: `a9afce9a92c2501be4b2216a2b03e19b05790a00` (A1.2 C0 handoff +
SP1 final acceptance at `2b940a6`).

Pinned provider: `Claude Code 2.1.209`
(`~/.local/share/claude/versions/2.1.209`, native Mach-O arm64, shasum
`59d2de7f…`). Global binary unchanged. Ambient `~/.claude/` auth only; no
credential copies.

## 1. Authority owner and surface attempt

- The candidate provider-positive binding was the documented blocking
  `PermissionRequest` command hook (code.claude.com/docs/en/hooks):
  one subprocess per permission dialog, input on stdin, JSON decision on
  stdout, `behavior: allow|deny`, exit code 2 = deny.
- A session-scoped `--settings` file injected the hook into a headless
  `-p` (print) session with `--setting-sources ""` to isolate from user
  and project settings. `--permission-mode default` was used so the
  default permission dialog would trigger.
- The hook script was a deterministic bash process that recorded a life
  marker (`life.log`) on EVERY invocation and then captured the full stdin
  input, enabling a non-vacuous test of whether the hook ever ran.

## 2. Finding — hook never fires headless

Two traces were run:

| Trace | Decision | Hook life.log | Provider outcome |
| --- | --- | --- | --- |
| allow (t1) | — | **ABSENT** | "needs your approval…" — hung waiting for interactive dialog, never resolved |
| deny (t2) | — | **ABSENT** | "denied by your permission system" — the static permission system denied, not the hook |

The hook script was never executed in either trace. The headless `-p`
mode with `--permission-mode default` never reaches a state where
`PermissionRequest` fires — the permission dialog is an interactive-UI
feature not activated in print mode.

## 3. Exact missing invariant

`PermissionRequest` cannot serve as the **exact provider invocation
boundary** for headless managed sessions because it never fires in `-p`
mode. The hook fire is conditional on the interactive permission dialog
appearing, which `-p` does not trigger. Without this single hook for one
blocking session, there is no stable non-interactive per-invocation
surface.

## 4. Alternate-surface assessment (per plan §6)

- **`--permission-prompt-tool`**: not listed in `claude --help` for this
  build (`2.1.209`). A later build may expose it for print/SDK mode, but
  it is not available directly for this pinned version.
- **Agent SDK**: would replace the headless `-p` provider surface
  entirely with a programmatic user-input callback. This is a different
  architecture (no shell CLI, no PermissionRequest hook) and requires a
  separately reviewed plan expansion per the handoff §6. C0 does not
  assess it beyond noting it as the plausible next development path.
- **Channel permission relay**: remains research preview with truncated
  `input_preview` (200 chars per the plan), requires a dangerous
  development/allowlist flag, and is explicitly excluded from the
  initial production surface (plan §2.4).

## 5. Verdict and capacity

**C0 BLOCKED — stable hook headless fire not provable.**

No production code, no actionability, no A1 store changes, no mobile
changes, no N1/O1/O2. The bounded probe harness is recorded in
`docs/a1_2_c0_evidence/`.

The next executor may:
- Re-assess against a newer Claude Code build where `--permission-prompt-tool`
  is documented and available (a distinct pinned version certification), or
- Begin an Agent SDK plan expansion if the product decision is to remove the
  shell-provider model for Claude, or
- Leave Claude approval as non-actionable observation (P1-level only) until a
  stable exact-invocation surface is available.
