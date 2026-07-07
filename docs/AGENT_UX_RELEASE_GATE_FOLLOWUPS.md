# Agent Adapter UX Release Gate Follow-ups

This document tracks mandatory follow-ups that were intentionally split out of Phase A5.

Phase A5 is accepted as Agent Backend Foundation. The items below are not blockers for backend A6 parser expansion, but they are blockers for release/product-complete status.

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

## Gate 3 — A6 must preserve A5 contracts

Status: **MANDATORY FOR A6**

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

## Release rule

Backend A6 may proceed before these gates are fully closed.

Product/release-complete status may not be claimed until all mandatory gates are closed or explicitly replaced by an accepted product decision.
