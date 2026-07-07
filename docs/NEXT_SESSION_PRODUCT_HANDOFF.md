# Next Session Product Handoff

> Architecture answers "Can we build it?"
> Product answers "Why would people love using it?"

## Current state

Architecture Phase (A0~A10): COMPLETE.

Accepted architecture baseline:

- A5: Agent Backend Foundation
- A6: Codex backend slice
- A7: Agent Agnostic UX gate
- A8: Third Agent Backend Ingestion / Observe Slice
- A9: Capability-aware, option-based Interaction Request UX
- A10: Diagnostics / Alpha Release Gate backend slice

The next phase is Product Phase.

Do not continue as if this is another architecture phase.

## Next target

P1a — Live Dashboard Core.

Mission:

Improve first-use experience.

When opening Pokit, within 5 seconds the user should know:

- what is running;
- what needs attention;
- what they can interact with;
- what is view-only, degraded, or offline.

## Mandatory reading

Before implementation, read:

- `docs/PRODUCT_PHASE_PLAN.md`
- `docs/AGENT_PHASE_A7_ACCEPTANCE.md`
- `docs/AGENT_PHASE_A8_BACKEND_ACCEPTANCE.md`
- `docs/AGENT_PHASE_A10_ALPHA_CHECKLIST.md`

Keep the A1~A10 retrospective lessons in mind:

- fields are not contracts unless they reach the product boundary;
- tests must prove the production path, not only helper behavior;
- events do not imply capability;
- option IDs do not carry semantic meaning;
- scoped accept must not be presented as full product completion.

## Implementation scope

Implement P1a only.

Build a dashboard experience using existing data from:

- `/api/sessions`
- `agentKind`
- `agentStatus`
- `agentConfidence`
- `capabilities`
- `approvals`
- `approval.options`
- existing A9 interaction card behavior

Prefer mobile/UI changes over backend changes.

Backend changes are allowed only if the existing product boundary is demonstrably insufficient. If backend changes are needed, document the reason in the commit message and tests.

## P1a acceptance

P1a is accepted only if:

- Needs Attention appears first.
- Pending interaction is pinned above routine activity.
- Running sessions are visible.
- Recently completed or recently idle sessions are visible.
- Degraded / Offline states are visually distinct.
- Observe-only sessions are shown as View Only.
- Capability-aware actions appear only when server-provided options/capabilities allow them.
- Unknown agent kind renders safely.
- Unknown agent status renders safely.
- No vendor-specific mobile behavior branch is introduced.
- A9 interaction card remains compatible.
- Mobile typecheck or equivalent validation is run if dependencies are available.

## Explicitly avoid

Do not implement:

- new adapters;
- new parser support;
- new interaction contract;
- new backend abstraction;
- new diagnostics model;
- summary feature;
- diff viewer;
- review workflow;
- install/distribution work;
- multi-project dashboard;
- cloud dashboard;
- IDE-like features.

Do not introduce mobile logic like:

```ts
if (agentKind === 'claude') { ... }
if (agentKind === 'codex') { ... }
if (agentKind === 'antigravity') { ... }
```

Display names, icons, and generic labels are allowed only if they do not change behavior by vendor.

## Non-negotiable invariants

Product work must preserve these architecture invariants:

- Mobile must not synthesize actions that the server did not provide.
- Capability determines executability.
- Observe-only means no control action.
- Unknown agent kind/status/event must degrade gracefully.
- `option.kind` drives interaction styling and status semantics, not `option.id`.
- Sensitive prompt, command, token, path, and secret data must not leak in notifications, diagnostics, or dashboard surfaces.
- Product UI must use existing architecture before requesting new backend work.

## Reviewer checklist

Reviewer will check:

- no vendor-specific behavior branch in mobile;
- pending interactions are surfaced first;
- observe-only is view-only;
- unknown agent/status fallback works;
- capability-aware action visibility is preserved;
- no backend contract changes without explicit justification;
- no A9 interaction regression;
- no A10 diagnostic/redaction regression;
- product outcome is visible within the first 5 seconds;
- implementation does not expand into P2/P3/P4/P5/P6.

## Suggested execution order

1. Inspect current mobile dashboard/session list components.
2. Identify the smallest UI change that creates the P1a sections.
3. Preserve existing event feed and interaction card behavior.
4. Add lightweight helpers for grouping sessions if needed.
5. Add tests or type-level checks where the project already supports them.
6. Run available validation commands.
7. Commit as a P1a implementation slice.

## Suggested validation commands

Run what is available in the local environment:

```sh
cd companion-daemon
go test ./...
go vet ./...
```

For mobile:

```sh
cd mobile
npm test
npm run typecheck
```

If mobile dependencies are unavailable, report `not-run` explicitly and include why.

## Completion statement format

When handing back P1a, report:

```text
P1a implementation complete.

Commit: <sha>

Changed:
- ...

Validation:
- ...

Known gaps:
- ...

Not done:
- P1b push polish
- P2 feed redesign
- P3 cockpit
- P4 summary
- P5 review
- P6 distribution
```
