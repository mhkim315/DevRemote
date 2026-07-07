# POKIT Product Phase Plan

> Architecture answers "Can we build it?"
> Product answers "Why would people love using it?"

Status:

- Architecture Phase (A0~A10): COMPLETE
- Product Phase: IN PROGRESS
  - P1a Live Dashboard Core: ACCEPTED (3fa4ad3c8)
  - P2 Core Feed Taxonomy: NEXT

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

## MVP Product Path

The MVP path is ordered by dependency, not by impressiveness:

```
P1a  Live Dashboard Core           ✅ ACCEPTED
P2   Core Feed Taxonomy             ← NEXT
P1b  Attention Routing / Push       depends on P2 feed being clear
P3   Mobile Typecheck / Build Gate  depends on all UI being stable
P4   Device UX Smoke                depends on P3
P5   Installer / Onboarding         depends on P4
P6   Alpha Packaging                depends on P5
```

### Why P2 before P1b

P1b routes the user to a specific session. The destination must be clear first.

If the feed still looks like raw logs, a push notification that says "approval required" leads to a screen where the user asks "so what am I looking at?"

P2 makes the feed readable. P1b routes to a readable feed.

### Why P3 before P4

Device smoke needs a reproducible build. P3 establishes that the build gate passes. P4 exercises the built artifact on real devices.

## P1a — Live Dashboard Core ✅

Status: ACCEPTED (3fa4ad3c8).

Accepted:
- Needs Attention section appears first.
- Pending interactions pinned above routine activity.
- Running section shows active agents.
- Recently Completed section shows idle/complete sessions.
- Degraded / View Only section visually distinct.
- Observe-only shown as View Only.
- No vendor-specific mobile branch.
- No backend contract change.

Known gaps:
- Recently Completed uses idle/non-degraded bucket, not actual completion events.
- Observe-only detection uses !live_stream; explicit capability model would be stronger.
- Mobile typecheck not run (node_modules absent).

## P2 — Core Feed Taxonomy

Goal:

When a user opens a session, they understand recent activity as a readable story — not raw logs.

### Scope

Core event categories for bubble rendering:

| Category | Rendered as | Existing event types |
|----------|------------|---------------------|
| Message | 💬 bubble | user_message, assistant_message |
| Thinking | 💭 dimmed bubble | thinking |
| Tool | ⚙ bubble with tool name | tool_call_started, tool_call_finished |
| Interaction | ⚠ attention bubble | approval_requested, approval_resolved |
| Error | ❌ error bubble | failed, error |
| Completed | ✅ completion bubble | completed |
| Unknown | ❓ dimmed fallback | unknown, all future types |

### Acceptance

- Each event type maps to a distinct bubble category.
- Unknown/future event types fall back to a dimmed generic bubble.
- Interaction events are visually prominent (attention-colored border).
- Tool events show the tool name.
- Error events are visually distinct (red).
- No vendor-specific rendering branch.
- No backend parser changes. Existing event contract used as-is.
- Mobile typecheck run if available; otherwise `not-run` explicitly stated.
- A7 unknown event fallback preserved.
- A9 interaction card remains compatible.

### Explicitly not in P2

- Diff viewer
- Deterministic summary
- New parser / new backend event model
- LLM summarization
- Push / deep link routing
- Installer / distribution work
- Agent Cockpit
- Review workflow

## P1b — Attention Routing / Push Deep Link

Goal:

Route user attention to the right session when interaction is required.

Acceptance:
- Pending interaction push routes to the correct session.
- Notification text does not leak prompt, command, token, or secret.
- Deep link from notification opens the FeedScreen for that session.
- Failure states are recoverable.

Out of scope:
- Changing A9 interaction contract.
- New push infrastructure beyond what P1b requires.
- Vendor-specific notification behavior.

## P3 — Mobile Typecheck / Build Gate

Goal:

Establish reproducible build verification.

Acceptance:
- `npx tsc --noEmit` passes in CI or documented environment.
- Build gate is documented and reproducible.
- Known dependency requirements listed.

Out of scope:
- Full CI pipeline.
- Multi-platform build matrix.

## P4 — Device UX Smoke

Goal:

Prove the app works on real devices.

Acceptance:
- Dashboard sections render correctly.
- Feed bubbles render correctly.
- Interaction card works end-to-end.
- Push notification arrives and routes correctly.
- Degraded/unknown states render safely.

## P5 — Installer / Onboarding

Goal:

Zero-friction first install.

Features:
- `brew install pokit`
- `npm install -g pokit`
- QR pairing
- First-run connection flow

## P6 — Alpha Packaging

Goal:

Shippable alpha artifact.

Features:
- Versioned release
- Signed binaries
- Release notes
- Known issues documented

## Post-MVP Product Enhancements

These are intentionally deferred past MVP. They are valid product directions but not required for the first alpha release.

- **Agent Cockpit**: health, capability badges, runtime state, A10 diagnostics visualization.
- **Deterministic Summary**: daily/weekly aggregation of tasks, files, errors. No LLM.
- **Reviewable Work**: diff viewer, file viewer, commit summary, test result viewer. Requires parser work.

## MVP definition

MVP includes:

- Architecture A5~A10
- P1a Live Dashboard Core
- P2 Core Feed Taxonomy
- P1b Attention Routing
- P3 Mobile Build Gate
- P4 Device UX Smoke
- P5 Installer / Onboarding
- P6 Alpha Packaging

MVP should allow a user to:

- see active agents;
- know what needs attention;
- understand recent activity as a story;
- respond to interactions safely;
- install and pair without guidance;

within the first 30 seconds.

## Explicit non-goals

Not in MVP:

- IDE
- Kanban
- Prompt Library
- Cloud Command Center
- Multi-Agent Orchestration
- Project Management
- Agent Cockpit
- Deterministic Summary
- Reviewable Work / Diff Viewer

These may become separate products or post-MVP enhancements.

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
