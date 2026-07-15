# A1.2 C0D — Deferred Tool Research Contract Note

Status: **C0D PROCEED — PreToolUse defer/resume lifecycle proven with exact
`tool_use_id` correlation across all phases. The stable hook surface provides
a defensible exact invocation and consumed-decision boundary that C0H
(`PermissionRequest`) and C0R (`--permission-prompt-tool`) could not.**

Date: 2026-07-15. Executor: A1.2 execution agent, canonical checkout
`/Users/mhk/Documents/codex/DevRemote`, branch `feature/phase10-multi-adapter`.

Baseline: `14f048937d091c8379280934ab8b0534633ad727` (C0D handoff). Pinned
provider: `Claude Code 2.1.209` (`~/.local/share/claude/versions/2.1.209`,
native Mach-O arm64, shasum `59d2de7f…`). Ambient `~/.claude/` auth only.

## 1. Authority owner and inputs

- **The `PreToolUse` hook** is the sole exact-invocation boundary. It receives
  `tool_use_id`, `session_id`, `tool_name`, full bounded `tool_input`,
  `prompt_id`, `permission_mode`, `cwd`, and `hook_event_name` in a single
  blocking subprocess, stdin in / stdout out. The `tool_use_id` is the
  provider-native invocation identity — one subprocess per tool call, exact
  matching through all phases.
- The daemon will own the blocking hook decision and the resume launch.
  The hook subprocess identity (one per invocation, stdin/stdout,
  daemon-owned script) plus the exact `tool_use_id` preserved across
  defer→resume→execution form the complete correlation chain.

## 2. State model (live-proven)

```
Initial PreToolUse(ID, session_id, tool_name, tool_input)
  → hook returns defer
  → Claude exits with stop_reason="tool_deferred" + deferred_tool_use{id, name, input}
  → daemon resumes: --resume <session_id>
  → repeated PreToolUse(SAME ID) fires
  → hook allows → tool executes → PostToolUse(ID) / tool_result(ID, is_error:false)
  → hook denies  → permission_denials[] records the ID + input
                   → tool_result(ID, is_error:true)
```

Every transition preserves the EXACT `tool_use_id`. The hook verified the
identity match on every resume invocation (`RESUME_ID_CHECK` in life log).

## 3. Immutable binding table (candidate)

| Field | Source | Phase(s) |
| --- | --- | --- |
| tool invocation ID | `PreToolUse.tool_use_id` | observe(defer), allow/deny(resume), consumption witness |
| Claude session ID | `PreToolUse.session_id` / `deferred_tool_use` result | observe, resume target |
| tool name | `PreToolUse.tool_name` | closed vocabulary gate |
| tool input | `PreToolUse.tool_input` (full bounded) | structural canonicalization |
| exact decision | `permissionDecision: allow` or `deny` | allow/deny phase |
| consumption witness — allow | `tool_result{is_error:false}` with EXACT `tool_use_id` | post-execution |
| consumption witness — deny | `permission_denials[]` entry with EXACT `tool_use_id` + `tool_result{is_error:true}` | post-denial |
| replay | duplicate resume on the same deferred ID produces a NEW session (fork); the original deferred state is consumed at most once | replay fail-closed |

## 4. Adversarial interleavings (live-proven)

1. **Exact ID match**: the hook verified `tool_use_id` equality on every
   resume — a wrong ID would fail the check (D1/D2 life logs).
2. **Deny → zero execution**: probe file absent, `tool_result.is_error:true`,
   `permission_denials[]` entry with same ID (D2 evidence).
3. **Allow → exact execution**: probe file present, `tool_result.is_error:
   false` with same ID (D1 evidence).
4. **Model retries**: a denied Bash call leads to new tool_use_ids
   (different invocations, each independently handled by the hook —
   proven non-actionable by D2's permission_denials sequence).
5. **Multi-tool**: the model generates a sequential chain of tool calls
   after denial; each gets a DISTINCT `tool_use_id` — no batched/parallel
   tool_use correlation exists in this surface.

## 5. Capacity behavior

C0D is research only. No delivery capacity, no actionable options, no CTA.
The hook is the ONLY binding: at most one tool_use_id per hook invocation,
one decision per tool_use_id, no concurrent decisions within the same
session. Multiplicity (model retries after denial) creates a chain of
distinct invocations, each individually resolvable.

## 6. Non-goals

No production implementation, no actionable Claude CTA, no C1D/C2D/C3D, no
A1 core changes, no Codex modification, no mobile changes, no N1/O1/O2.
C0D PROCEED only authorizes the reviewer to write a new implementation
plan; passing C0D is not permission to begin building.
