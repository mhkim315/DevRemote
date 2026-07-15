# A1.2 Claude Approval Extension — Closed Research Plan

Status: **BLOCKED — NO C1 IMPLEMENTATION AUTHORIZED**

Frozen baseline: SP1/A1.1 accepted at `2b940a6` with implementation `dd6d05c`.
The provider-neutral A1 core and Codex provider-positive path remain unchanged.

## 1. Independent research outcomes

Both findings are authoritative and independent:

1. **C0H BLOCKED** — the stable `PermissionRequest` hook did not fire in
   headless `claude -p`. Static permission rules handled allow/deny instead.
   Evidence: `docs/A1_2_C0_CLAUDE_EVIDENCE_REPORT.md`.
2. **C0R BLOCKED** — exact Claude Code 2.1.209 accepted
   `--permission-prompt-tool` and connected the isolated MCP server, correcting
   the earlier `--help` inference. However, an exact invocation identity and
   authoritative consumed-decision join were not proven. Evidence:
   `docs/A1_2_C0R_PERMISSION_PROMPT_TOOL_REPORT.md`.

C0R does not remediate, supersede or weaken C0H.

## 2. Product consequence

Stock Claude Code headless approval is unsupported under POKIT's exact-authority
contract. Claude managed observation and prompt submission must not be represented
as actionable approval support. `waiting_approval`, terminal/PTY text, prompt
matching, process name, CWD, transcript order and generic input remain display or
I/O only and never approval authority.

No C1/C2/C3, mobile CTA, actionability capacity, A1 store change or production
Claude delivery boundary is authorized. Interactive key injection is not native
approval authority.

## 3. Exact missing contract

Any future reconsideration must first provide, in a stable official interface:

- provider-issued request/invocation identity;
- exact SessionID/runtime generation and Claude session/turn/tool binding;
- bounded full canonical input;
- exact allow-once or deny targeting;
- a provider-native result carrying the same identity and proving consumption;
- fail-closed timeout, cancellation, replay, replacement and restart behavior.

MCP transport IDs, timestamps, command equality, terminal output, FIFO and
single-flight assumptions do not satisfy this contract.

## 4. Future architecture options — decision required, not implementation

### 4.1 Claude Agent SDK

- Authentication/billing: may use supported Claude authentication modes, but exact
  subscription versus API billing must be verified for the chosen SDK deployment.
- Feature retention: closer to Claude Code's programmatic engine, but not guaranteed
  to preserve every stock CLI/TUI feature or release cadence.
- Identity: current public `canUseTool` contract documents tool name/input and
  cancellation, not an invocation ID; exact consumed-decision proof remains a gate.
- Cost/scope: medium-to-high; replaces the shell-provider boundary and requires a
  separately accepted architecture plan.

### 4.2 Channels permission relay

- Authentication/billing: Claude.ai authentication; exact plan eligibility must be
  certified.
- Feature retention: stock Claude Code integration, but Channels is research preview.
- Identity: supplies a request ID, but the published permission request exposes a
  truncated input preview and custom development channels require explicit unsafe
  enablement. This is insufficient for canonical review today.
- Cost/scope: medium; adds a notification/channel product and preview dependency.

### 4.3 Interactive PTY / terminal-only operation

- Authentication/billing and features: retains the user's normal Claude Code
  experience and subscription.
- Identity/consumption: terminal prompts and key delivery do not provide exact native
  request identity or consumed-decision proof.
- Cost/scope: low for manual operation, but it must stay non-actionable and cannot
  close A1.2.

### 4.4 POKIT-owned generic runtime using Claude models

- Authentication/billing: normally direct model API/provider billing rather than the
  stock Claude Code subscription; must be a deliberate product decision.
- Feature retention: does not automatically inherit latest Claude Code tools,
  policies, hooks, memory or UX.
- Identity/consumption: POKIT can design an exact protocol because it owns the worker
  runtime, but this is a new agent product, not a Claude Code adapter.
- Cost/scope: highest; affects runtime, tools, sandbox, workspace and orchestration.

## 5. Roadmap boundary

A1.2 is closed BLOCKED. N1 may proceed only after the product owner explicitly
records that Claude actionable approval is deferred. Reopening A1.2 requires a new
architecture decision and independent evidence plan; it must not be called C1
remediation of either failed surface.

Explicit exclusions remain: no Agent SDK migration, Channels implementation,
interactive approval injection, generic provider SDK, N1 implementation, O1/O2,
observer cleanup, Windows/SSH expansion or cloud relay redesign in this track.
