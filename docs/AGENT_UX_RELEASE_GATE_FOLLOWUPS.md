# Agent Adapter UX Release Gate Follow-ups

This document tracks mandatory follow-ups that were intentionally split out of Phase A5 and reaffirmed after Phase A6/A7/A8.

Phase A5 is accepted as Agent Backend Foundation.
Phase A6 is accepted as Codex Backend Slice.
Phase A7 is accepted as Agent Agnostic UX/Product Gate.
Phase A8 is accepted as Third Agent Backend Slice.

These acceptances do not mean product completion. The items below are blockers for release/product-complete status.

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

Status: **CLOSED FOR A7 AGNOSTIC RENDERING / KEEP AS REGRESSION**

Current accepted proof:

- mobile TypeScript compile passes;
- no direct mobile agent-name behavior branch was found in the latest smoke grep.

Required proof:

- mobile rendering/schema proof for:
  - `agentKind`;
  - `agentStatus=waiting_approval`;
  - passive `approval_requested` event rendering;
  - degraded/unknown states;
- prove mobile behavior is driven by common status/event/capability data, not agent names;
- ensure unknown future agents do not crash rendering;
- ensure optional agent fields are safe when absent.

Closure:

- A7 acceptance is documented in `docs/AGENT_PHASE_A7_ACCEPTANCE.md`.
- Interactive approval actions remain out of scope for this gate and are tracked by Gate 5 / Phase A9.

## Gate 3 — Agent Agnostic UX compatibility

Status: **CLOSED FOR A7 ACCEPTANCE / KEEP AS REGRESSION**

A7 closed the product-boundary gap left after A6 Backend Slice acceptance.

Required proof:

- unknown `AgentKind` graceful rendering;
- unknown `AgentStatus` graceful rendering;
- degraded state visualization;
- schema evolution compatibility;
- no vendor-specific branching in mobile behavior;
- backend contract unchanged when future agent kinds appear.

Closure:

- A7 acceptance is documented in `docs/AGENT_PHASE_A7_ACCEPTANCE.md`.
- Keep these checks as regression requirements for A8 and later.

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

## Gate 4b — A8 must preserve A7 agnostic UX and backend contract

Status: **CLOSED FOR A8 BACKEND ACCEPTANCE / KEEP AS REGRESSION**

A8 Antigravity backend expansion must:

- use actual redacted Antigravity fixtures;
- connect detector/resolver/parser to the production telemetry path;
- expose Antigravity events through `EventStore` and `/api/sessions.Events`;
- preserve `agentKind="antigravity"` through the activity path;
- avoid adding mobile/common vendor-specific behavior branches;
- keep unknown/future agent fallback intact.

Closure:

- A8 backend acceptance is documented in `docs/AGENT_PHASE_A8_BACKEND_ACCEPTANCE.md`.
- Keep these checks as regression requirements for A9 and later.

Follow-up:

- The term-layer Antigravity parser currently normalizes `ERROR_MESSAGE` through
  the legacy `tool_result` path, while the agent-layer parser maps
  `ERROR_MESSAGE` to `failed`. This did not block A8 because the backend
  slice proved production flow and terminal survival. A9/A10 should decide the
  product-level representation for failed/degraded agent events and align the
  parsers if needed.

## Gate 5 — Approval UX

Status: **MANDATORY FOR A9**

A7 proves the common UI can render `approval_requested` safely. It does not prove that a user can approve or reject safely from mobile.

Required proof:

- common approval card;
- approve/reject action;
- pending/expired/resolved state handling;
- duplicate tap prevention;
- backend idempotency;
- recoverable failure UI;
- audit log;
- no sensitive command/prompt/token leakage in push, mobile, logs, or diagnostics.

## Gate 6 — Diagnostics / Alpha Release Gate

Status: **MANDATORY FOR A10**

Required proof:

- daemon health;
- terminal adapter status;
- active sessions;
- agent detector/parser confidence;
- last parser/resolver error;
- mobile connection state;
- exportable diagnostic bundle;
- redaction-safe output;
- Alpha/release checklist.

## Release rule

A6/A7/A8 acceptance may stand before approval and diagnostics gates are fully closed.

Product/release-complete status may not be claimed until all mandatory gates are closed or explicitly replaced by an accepted product decision.
