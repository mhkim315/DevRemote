# A1.2 Claude Approval Extension — Deferred Tool Research Plan

Status: **C0D EVIDENCE AUTHORIZED — NO PRODUCTION IMPLEMENTATION OR
ACTIONABILITY**

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

## 2. New independent checkpoint: C0D

Current official Claude Code documentation exposes a third, structurally different
surface: a `PreToolUse` hook in non-interactive `-p` mode may return
`permissionDecision: "defer"`. Claude Code then exits with
`stop_reason: "tool_deferred"` and a bounded `deferred_tool_use` carrying the exact
provider `id`, tool name and input. Resuming the exact session replays the same tool
call through `PreToolUse`; successful execution is subsequently reported through
`PostToolUse` or `PostToolUseFailure`, which also carries `tool_use_id`.

This is **C0D**, a new research checkpoint. It is not remediation or reinterpretation
of C0H or C0R. Exact Claude Code 2.1.209 contains the documented deferred-tool paths,
but no live provider trace has yet certified their identity and lifecycle behavior.

Official reference: <https://code.claude.com/docs/en/hooks>.

### 2.1 C0D candidate authority binding

| Field | Required authoritative source |
| --- | --- |
| provider artifact | exact pinned executable version and digest |
| Claude session ID | initial structured result and exact `--resume` target |
| tool invocation ID | `PreToolUse.tool_use_id` and `deferred_tool_use.id` |
| tool name and input | full bounded `PreToolUse` input, strict type-specific canonicalization |
| runtime ownership | exact probe-owned Claude process/session; no attached runtime |
| decision | stored `allow_once` or `deny`, bound to session + tool-use ID + input digest |
| consumption witness | matching `PostToolUse`/`PostToolUseFailure.tool_use_id`, or a separately proven exact denial result carrying the same ID |

MCP IDs, transcript ordering, command equality, timing, terminal output, PID alone,
FIFO and single-flight assumptions remain non-authoritative.

### 2.2 C0D state model

```text
observed PreToolUse(ID)
  → defer
  → provider result tool_deferred(session, ID, name, input)
  → external decision bound to exact session + ID + digest
  → resume exact session
  → repeated PreToolUse(same ID)
  → allow_once → exact execution → PostToolUse/PostToolUseFailure(same ID)
  → deny       → exact provider denial result(same ID) and zero execution
```

Any missing/mismatched ID, changed input, stale session/runtime, duplicate decision,
ambiguous resume, unsupported tool, timeout, cancellation, process exit or multi-tool
turn must fail closed.

### 2.3 Mandatory C0D evidence

C0D is a research/evidence packet only. Against exact pinned 2.1.209, prove:

1. user-controlled ambient authentication is present without copying credentials;
2. direct headless launch with isolated session settings and a `PreToolUse` hook;
3. initial `PreToolUse.tool_use_id` equals `deferred_tool_use.id`;
4. exact session resume re-fires `PreToolUse` with the same ID and unchanged canonical
   input;
5. allow-once yields one execution and a `PostToolUse` or `PostToolUseFailure` with
   the same ID;
6. deny yields zero execution and a stable provider-native result linked to the same
   ID;
7. duplicate resume/decision cannot execute twice;
8. wrong ID, wrong session, modified input, delayed decision, timeout, stop/delete,
   Claude exit and runtime replacement cannot execute;
9. two distinct sequential requests retain distinct IDs;
10. parallel/batched or multiple tool calls are rejected/non-actionable because the
    documented defer contract supports only a single tool call per turn;
11. malformed hook output, hook timeout and hook crash fail closed;
12. committed evidence is bounded and excludes raw prompts, commands, secrets, home
    paths, transcript paths and authentication material.

Use deterministic barriers for resume/termination and duplicate interleavings. A
side effect corroborates execution but is not the authority witness.

### 2.4 C0D decision

- **C0D PROCEED** only if exact ID continuity and authoritative allow and deny
  consumption are proven without heuristics.
- **C0D PROCEED WITH CONTRACT CHANGES** only if the single-tool limitation remains
  fail-closed and every supported invocation is still targeted and consumed exactly.
- **C0D BLOCKED** if deny consumption, ID continuity, replay safety or fail-closed
  multi-tool behavior is ambiguous.

Passing C0D does not authorize production code. It authorizes a separate reviewed
implementation plan with bounded C1D/C2D/C3D packets.

## 3. Current product consequence

Stock Claude Code headless approval is unsupported under POKIT's exact-authority
contract. Claude managed observation and prompt submission must not be represented
as actionable approval support. `waiting_approval`, terminal/PTY text, prompt
matching, process name, CWD, transcript order and generic input remain display or
I/O only and never approval authority.

No C1/C2/C3 or C1D/C2D/C3D, mobile CTA, actionability capacity, A1 store
change or production Claude delivery boundary is authorized. Interactive key
injection is not native approval authority.

## 4. Exact missing contract

C0D must close the following contract through a stable official interface before
any implementation plan is authorized:

- provider-issued request/invocation identity;
- exact SessionID/runtime generation and Claude session/turn/tool binding;
- bounded full canonical input;
- exact allow-once or deny targeting;
- a provider-native result carrying the same identity and proving consumption;
- fail-closed timeout, cancellation, replay, replacement and restart behavior.

MCP transport IDs, timestamps, command equality, terminal output, FIFO and
single-flight assumptions do not satisfy this contract.

## 5. Future architecture options — only if C0D blocks

### 5.1 Claude Agent SDK

- Authentication/billing: may use supported Claude authentication modes, but exact
  subscription versus API billing must be verified for the chosen SDK deployment.
- Feature retention: closer to Claude Code's programmatic engine, but not guaranteed
  to preserve every stock CLI/TUI feature or release cadence.
- Identity: current public `canUseTool` contract documents tool name/input and
  cancellation, not an invocation ID; exact consumed-decision proof remains a gate.
- Cost/scope: medium-to-high; replaces the shell-provider boundary and requires a
  separately accepted architecture plan.

### 5.2 Channels permission relay

- Authentication/billing: Claude.ai authentication; exact plan eligibility must be
  certified.
- Feature retention: stock Claude Code integration, but Channels is research preview.
- Identity: supplies a request ID, but the published permission request exposes a
  truncated input preview and custom development channels require explicit unsafe
  enablement. This is insufficient for canonical review today.
- Cost/scope: medium; adds a notification/channel product and preview dependency.

### 5.3 Interactive PTY / terminal-only operation

- Authentication/billing and features: retains the user's normal Claude Code
  experience and subscription.
- Identity/consumption: terminal prompts and key delivery do not provide exact native
  request identity or consumed-decision proof.
- Cost/scope: low for manual operation, but it must stay non-actionable and cannot
  close A1.2.

### 5.4 POKIT-owned generic runtime using Claude models

- Authentication/billing: normally direct model API/provider billing rather than the
  stock Claude Code subscription; must be a deliberate product decision.
- Feature retention: does not automatically inherit latest Claude Code tools,
  policies, hooks, memory or UX.
- Identity/consumption: POKIT can design an exact protocol because it owns the worker
  runtime, but this is a new agent product, not a Claude Code adapter.
- Cost/scope: highest; affects runtime, tools, sandbox, workspace and orchestration.

## 6. Roadmap boundary

A1.2 is reopened only for C0D evidence. N1 remains blocked until C0D receives an
independent verdict and either the resulting Claude path is accepted or the product
owner explicitly defers Claude actionable approval. C0D must not be called remediation
of either failed surface.

Explicit exclusions remain: no Agent SDK migration, Channels implementation,
interactive approval injection, generic provider SDK, N1 implementation, O1/O2,
observer cleanup, Windows/SSH expansion or cloud relay redesign in this track.
