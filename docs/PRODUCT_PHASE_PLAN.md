# POKIT Product Phase Plan

> Architecture answers "Can we build it?"
> Product answers "Why would people love using it?"

Status:

- Architecture Phase (A0~A10): COMPLETE
- Product Phase (P1~): IN PROGRESS

## Why this document exists

A0~A10 proved the architecture:

- Pokit can observe multiple AI agents.
- Agent activity can be normalized into a common contract.
- Interactions can be represented safely through capability-aware options.
- Unknown agents, statuses, events, and interactions can degrade gracefully.
- Diagnostics can be exposed through a redacted, tested, local-first boundary.

Product Phase must not reopen architecture questions by default.

Product Phase exists to make the existing architecture obvious, useful, and trustworthy within the first 30 seconds of using the app.

Every product feature must answer one question:

> What confidence does this give the user?

If a feature does not help the user understand what is running, what needs attention, what happened recently, or whether Pokit is healthy, it is not Product MVP work.

## Product principles

1. Local-first always.
2. Do not become an IDE.
3. Do not become a cloud dashboard.
4. Do not become agent-specific.
5. Surface the user's attention, not raw logs.
6. Unknown agents, events, statuses, and interactions must degrade gracefully.
7. Use existing A5~A10 architecture before adding backend features.
8. Product UI must not depend on vendor-specific branches.
9. Capability determines executability; event existence does not.
10. Product Phase must not reopen architecture questions unless a product boundary proves the existing architecture insufficient.

## Lessons carried from A1~A10

The Architecture Phase repeatedly showed that fields, fixtures, and happy-path tests are not enough.

Reviewer and executor retrospectives both converged on the same lessons:

- A field is not a contract unless it travels through the product boundary.
- A fixture test is not proof unless the production path is also covered.
- A UI button is not safe unless the server-provided capability and option contract allows it.
- Scoped Accept is useful only when it states both what was accepted and what was not accepted.
- A good implementation plan needs semantic rules, concrete code boundaries, and reviewer-visible tests.

Product Phase must apply these lessons. A product feature is accepted only when the user-facing boundary proves the intended experience.

## Phase ordering rationale

The Product Phase order is intentionally not "most impressive feature first."

It is ordered by dependency:

1. The user must first see what is happening.
2. Then the user must understand the activity as a story.
3. Then the user can inspect health and capability.
4. Then deterministic summaries can aggregate reliable signals.
5. Then review surfaces can expose richer work products.
6. Only after the core experience is clear should distribution polish expand onboarding.

This is why dashboard and feed come before summaries, review, cockpit polish, and install polish.

## P1 — Live Agent Dashboard

Goal:

Within 5 seconds, the user understands:

- what is running;
- what needs attention;
- what recently completed;
- what is degraded, offline, or view-only.

P1 is the first Product Phase because Pokit must immediately answer:

> Are my AI agents working, and do they need me?

### P1a — Live Dashboard Core

P1a is the first implementation target.

Acceptance:

- Needs Attention section appears first.
- Pending interaction is pinned above routine activity.
- Running section shows active agents/sessions.
- Recently Completed section shows completed or recently idle work.
- Degraded / Offline section is visually distinct.
- Observe-only sessions are shown as View Only, not actionable.
- Capability-aware actions are shown only when existing capabilities/options allow them.
- Unknown agent kind renders gracefully.
- Unknown agent status renders gracefully.
- No vendor-specific mobile behavior branch is introduced.
- No backend contract change is introduced unless explicitly justified and reviewed.
- Existing A9 interaction card remains compatible.

Out of scope:

- feed redesign;
- diff viewer;
- smart summary;
- multi-project dashboard;
- new adapters;
- new parser work;
- new interaction contract;
- new backend abstraction;
- install/distribution work.

### P1b — Attention Routing / Push Polish

P1b follows P1a after the dashboard core is accepted.

Acceptance:

- Pending interaction push routes the user to the relevant session or dashboard state.
- Notification text does not leak raw prompt, command, token, or secret.
- Device UX smoke is documented.
- Failure states are recoverable and clear.

Out of scope:

- changing the A9 interaction contract;
- adding new push infrastructure beyond what P1b requires;
- vendor-specific notification behavior.

## P2 — Human-readable Event Feed

Goal:

Transform terminal events into a readable story.

P2 should not expose raw logs as the primary product experience. It should convert existing common events into understandable bubbles.

Core event categories:

- Message
- Thinking
- Tool
- Interaction
- File
- Diff
- Error
- Completed

Requirements:

- unknown event fallback;
- bubble taxonomy;
- better visual hierarchy;
- no vendor-specific rendering branch;
- no parser rewrite unless a product-boundary gap proves it necessary.

P2 should answer:

> Can I understand what happened without reading a terminal log?

## P3 — Agent Cockpit

Goal:

Expose operational health without turning Pokit into an IDE or cloud dashboard.

Features:

- Agent health
- Capability badges
- Runtime state
- Diagnostics
- Connection status

P3 uses the A10 diagnostics backend. It should not create a second diagnostic model unless A10 proves insufficient.

P3 should answer:

> Can I trust that Pokit and my local agents are connected and healthy?

## P4 — Deterministic Summary

Goal:

Provide quick understanding without reading logs.

Examples:

```text
Today
12 Tasks
18 Files Changed
2 Waiting
1 Error
```

Requirements:

- no LLM summarization in MVP;
- only deterministic aggregation;
- summary values must be traceable to existing events, sessions, interactions, or diagnostics;
- unknown/future events must not break aggregation.

P4 comes after P1/P2/P3 because summaries are only trustworthy when the underlying dashboard, feed, and health signals are already stable.

P4 should answer:

> What is the high-level state of my AI work today?

## P5 — Reviewable Work

Goal:

Review work instead of reading terminals.

Features:

- Diff Bubble
- File Bubble
- Command Result
- Test Result
- Error Summary

Parser improvements may be required, but they should be driven by product-boundary proof, not speculative parser expansion.

P5 should answer:

> Can I review useful outputs from my phone without opening the terminal?

## P6 — Distribution & Local-first Polish

Goal:

Zero-friction onboarding.

Features:

- `npm install -g pokit`
- `brew install pokit`
- `winget install pokit`
- QR Pairing
- Auto Discovery

P6 is intentionally late because distribution polish should not hide an unclear first-run product experience.

P6 should answer:

> Can a new user install, pair, and trust Pokit without guidance?

## MVP definition

MVP includes:

- Architecture A5~A10;
- P1a Live Dashboard Core;
- P2 core event feed;
- A9 interaction contract and card.

The first public MVP should allow a user to:

- see active agents;
- know where interaction is required;
- understand recent activity;
- respond safely;

within the first 30 seconds.

## Explicit non-goals

Not in MVP:

- IDE
- Kanban
- Prompt Library
- Cloud Command Center
- Multi-Agent Orchestration
- Project Management

These may become separate products in the future, but they are intentionally excluded from Pokit's MVP.

## Product acceptance model

Product acceptance is not "the screen exists."

Product acceptance means:

- the user can make the intended decision;
- the UI preserves A7 agent-agnostic behavior;
- the UI preserves A9 capability-aware interaction behavior;
- unknown/future values degrade gracefully;
- sensitive data is not exposed;
- the implementation does not reopen architecture scope unnecessarily.

Every Product Phase review should state:

- accepted scope;
- not accepted scope;
- product boundary evidence;
- known gaps.
