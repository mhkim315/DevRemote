# Agent Adapter UX Release Gate Follow-ups

This document tracks mandatory follow-ups that were intentionally split out of Phase A5 and reaffirmed after Phase A6.

Phase A5 is accepted as Agent Backend Foundation.
Phase A6 is accepted as Codex Backend Slice.

Neither acceptance means product completion. The items below are blockers for release/product-complete status.

## Gate 1 — Degraded diagnostics UX

Status: **MANDATORY FOLLOW-UP**

Current accepted proof:

```text
malformed log does not hide or fail the terminal session
```

Still required:

- expose parser degraded status or diagnostics at a safe product boundary;
- distinguish degraded parser state from no activity;
- ensure diagnostics are redacted:
  - no raw prompt;
  - no token;
  - no absolute user path unless explicitly redacted;
  - no source code or command payload leakage;
- add regression coverage showing terminal sessions remain listed when degraded metadata is present.

If the implementation chooses not to use `agentStatus=degraded`, it must document the alternative product contract and add tests for it.

## Gate 2 — Mobile Agent UX rendering/schema proof

Status: **MANDATORY FOLLOW-UP**

Current accepted proof:

- mobile TypeScript compile passes;
- no direct mobile agent-name behavior branch was found in the latest smoke grep.

Still required:

- mobile rendering/schema proof for:
  - `agentKind`;
  - `agentStatus=waiting_approval`;
  - `approval_requested`;
  - degraded/unknown states;
- prove mobile behavior is driven by common status/event/capability data, not agent names;
- ensure unknown future agents do not crash rendering;
- ensure optional agent fields are safe when absent.

## Gate 3 — Agent Agnostic UX compatibility

Status: **MANDATORY FOR A7**

A7 must close the product-boundary gap left after A6 Backend Slice acceptance.

Required proof:

- unknown `AgentKind` graceful rendering;
- unknown `AgentStatus` graceful rendering;
- degraded state visualization;
- schema evolution compatibility;
- no vendor-specific branching in mobile behavior;
- backend contract unchanged when future agent kinds appear.

Required fixture/test cases:

- known agent with known status;
- unknown/future agent kind, for example `future_agent`;
- unknown status, for example `blocked_by_future_runtime`;
- missing optional agent fields:
  - no `agentKind`;
  - no `agentStatus`;
  - no `agentConfidence`;
  - no `events`;
- unknown event type;
- degraded parser/diagnostic state.

Status visualization rule:

```text
Status UI represents the current state, not the vendor.
```

Therefore:

- `agentStatus` may influence badge/color/copy;
- `agentKind` may influence a neutral display label only;
- `agentKind` must not decide approval CTA, degraded copy, or session availability.

Rejected patterns:

```text
if agentKind == "claude" { ... }
if agentKind == "codex" { ... }
switch agentKind { case "gemini": ... }
```

Those patterns are only acceptable inside agent-specific parser/detector registration code, not common/mobile UX behavior.

## Gate 4 — A6 must preserve A5 contracts

Status: **CLOSED FOR A6 BACKEND ACCEPTANCE / KEEP AS REGRESSION**

A6 Codex/GPT backend expansion must:

- reuse common `AgentEvent` types;
- reuse common `AgentStatus` values;
- avoid changing Claude parser behavior unless the change is a shared contract fix;
- avoid adding mobile agent-name-specific behavior;
- keep malformed-log survival behavior.

Minimum A6 regression checks:

- Claude A5 backend proof still passes;
- new Codex/GPT parser fixtures pass the common parser contract;
- `/api/sessions.Events` exposes common event types for the new agent;
- no new mobile name branch is introduced.

Closure:

- A6 backend acceptance is documented in `docs/AGENT_PHASE_A6_BACKEND_ACCEPTANCE.md`.
- Keep these checks as regression requirements for A7 and later.

## Release rule

A6 backend acceptance may stand before these gates are fully closed.

Product/release-complete status may not be claimed until all mandatory gates are closed or explicitly replaced by an accepted product decision.
