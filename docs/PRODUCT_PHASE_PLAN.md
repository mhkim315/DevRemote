# POKIT Product Phase Plan

> Architecture answers "Can we build it?"
> Product answers "Why would people love using it?"

Status:

- Architecture Phase (A0~A10): COMPLETE
- Product Phase: IN PROGRESS
  - P1a Live Dashboard Core: ACCEPTED (3fa4ad3c8)
  - P2 Core Feed Taxonomy: ACCEPTED (413734a)
  - P1b Attention Routing Implementation: ACCEPTED as code-verifiable (c162c33)
  - P3 Mobile Typecheck / Build Gate: ACCEPTED (47bed6c)
  - E5 Emulator / Simulator Smoke: SCOPED ACCEPT (ccb642f)

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

## Product verification model

The original Product path treated "Device UX Smoke" as one executor-owned phase.
That assumption was only partially true.

Executor agents can verify reproducible builds, emulator/simulator behavior, local
development builds, direct daemon connectivity, UI rendering, and locally
reproducible interaction flows.

Executor agents cannot genuinely verify physical-device UX, real push delivery,
notification tap behavior on a real device, real first-time login, or first-time
pairing/onboarding quality. Those are manual product validation responsibilities.

Product work is therefore split by ownership and verification boundary:

```text
E-track  Executor-verifiable implementation and local validation
M-track  Manual product validation by the product owner
R-track  Alpha release decision gates
```

Scoped Accept is mandatory across these tracks. An E-track pass must not be
reported as M-track or release readiness.

## MVP Product Path

The MVP path is ordered by verification responsibility, not by the old P-numbering:

```
E1  Live Dashboard Core                 ✅ ACCEPTED
E2  Core Feed Taxonomy                   ✅ ACCEPTED
E3  Attention Routing Implementation     ✅ ACCEPTED as code-verifiable
E4  Mobile Typecheck / Build Gate        ✅ ACCEPTED
E5  Emulator / Simulator Smoke           ✅ SCOPED ACCEPT
E6  No-login Local Test Branch           ← NEXT
E7  Installer / Packaging Implementation

M1  Physical Device Smoke                manual product validation
M2  Real Push + Notification Tap         manual product validation
M3  First-time Pairing / Onboarding      manual product validation
M4  Real Interaction UX                  manual product validation

R1  Alpha Candidate Gate                 release decision
```

### Historical rationale: why P2 came before P1b

P1b routes the user to a specific session. The destination must be clear first.

If the feed still looks like raw logs, a push notification that says "approval required" leads to a screen where the user asks "so what am I looking at?"

P2 makes the feed readable. P1b routes to a readable feed.

### Historical rationale: why P3 came before E5

Emulator/simulator smoke needs a reproducible build. E4 establishes that the
build gate passes. E5 exercises the built artifact in executor-accessible local
runtimes. Real-device validation moved to M-track.

### Why E6 before manual onboarding

The test branch should remove login from the executor path before continuing.

Login, real account setup, first-time onboarding, and first-time pairing quality are
manual product validation items. They require a product owner using a real device,
not an executor running scripted local checks.

E6 exists so the executor can continue useful validation without being blocked by
auth or account state. The test branch should boot directly into the local daemon
connection path or a deterministic test connection path, then run the verifiable
dashboard/feed/interaction/build checks.

E6 must not remove login from production scope. It only defines the test-branch
validation route.

## E1 — Live Dashboard Core ✅

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

## E2 — Core Feed Taxonomy ✅

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

## E3 — Attention Routing / Push Deep Link Implementation ✅

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
- Claiming real push delivery or physical-device tap behavior. Those belong to M2.

## E4 — Mobile Typecheck / Build Gate ✅

Goal:

Establish reproducible build verification.

Acceptance:
- `npx tsc --noEmit` passes in CI or documented environment.
- Build gate is documented and reproducible.
- Known dependency requirements listed.

Out of scope:
- Full CI pipeline.
- Multi-platform build matrix.

## E5 — Emulator / Simulator Smoke ✅ Scoped Accept

Goal:

Prove the app can be built, installed, launched, and locally exercised in
executor-accessible runtimes.

Acceptance:
- Dashboard sections render correctly.
- Feed bubbles render correctly.
- Local daemon connectivity is documented.
- Android emulator behavior is documented when available.
- iOS simulator behavior is documented when available.
- Degraded/unknown states render safely.
- Build/install/launch commands are documented.
- Known gaps explicitly distinguish executor limits from product failures.

Not accepted by E5:
- physical-device UX;
- real push notification delivery;
- notification tap behavior on a real device;
- real login/account onboarding;
- first-time pairing quality;
- real interaction UX on a physical device.

These belong to M-track.

## E6 — No-login Local Test Branch

Goal:

Allow the executor to run meaningful product validation without being blocked by
login, account setup, Expo account ownership, physical device availability, or
manual pairing.

Scope:
- test branch only;
- remove or bypass login from the executor validation path;
- start from a deterministic local connection screen or preconfigured daemon URL;
- preserve production login/onboarding scope unless explicitly changed in a later
  product decision;
- make dashboard/feed/interaction smoke executable by an agent in emulator,
  simulator, or local dev runtime;
- document the exact bypass so reviewers can distinguish test scaffolding from
  product behavior.

Acceptance:
- executor can launch the app without completing login;
- executor can connect to a local daemon or deterministic test endpoint;
- dashboard renders with real or fixture-backed local session data;
- feed renders existing event taxonomy;
- interaction card path can be exercised with controlled local data if available;
- build gate still passes;
- no production claim is made about real onboarding or account UX;
- no vendor-specific mobile behavior branch is introduced.

Out of scope:
- real account login;
- production onboarding UX;
- QR pairing quality;
- real push notification delivery;
- physical-device tap behavior;
- app-store or public release packaging.

## E7 — Installer / Packaging Implementation

Goal:

Prepare repeatable local-first installation and packaging steps that an executor
can validate mechanically.

Features:
- `brew install pokit`
- `npm install -g pokit`
- QR pairing
- install/uninstall scripts
- version metadata
- release artifact dry-run

Not accepted by E7:
- first-time user onboarding quality;
- real pairing comfort;
- physical-device installation quality.

## M1 — Physical Device Smoke

Owner: product owner / human.

Acceptance:
- physical iOS or Android device runs the app;
- daemon is reachable from the real device;
- dashboard is visible on a real network;
- feed opens a real session;
- degraded/offline states are understandable;
- app survives basic background/foreground usage.

## M2 — Real Push + Notification Tap

Owner: product owner / human.

Acceptance:
- Expo push token is acquired on a real device;
- push notification arrives;
- notification body is redacted;
- tap opens the app;
- cold-start and warm-start notification tap behavior are checked;
- tap routes to the correct session when data is available.

## M3 — First-time Pairing / Onboarding

Owner: product owner / human.

Acceptance:
- fresh install;
- first connection to daemon;
- QR/manual URL pairing if implemented;
- login/account flow if present;
- clear error message when daemon is unavailable;
- user understands the next action without developer guidance.

## M4 — Real Interaction UX

Owner: product owner / human.

Acceptance:
- a session with pending interaction exists;
- mobile shows server-provided options;
- observe-only sessions do not show control actions;
- approve/reject/input flow works where capability allows;
- expired/duplicate interaction behavior is understandable.

## R1 — Alpha Candidate Gate

Goal:

Decide whether the current product is shippable as an alpha.

Acceptance:
- E1-E7 pass;
- M1-M3 pass;
- M4 passes or is explicitly deferred from alpha scope;
- A10 diagnostics/redaction still pass;
- known issues are documented;
- release artifact is reproducible;
- release notes clearly state limitations.

## Post-MVP Product Enhancements

These are intentionally deferred past MVP. They are valid product directions but not required for the first alpha release.

- **Agent Cockpit**: health, capability badges, runtime state, A10 diagnostics visualization.
- **Deterministic Summary**: daily/weekly aggregation of tasks, files, errors. No LLM.
- **Reviewable Work**: diff viewer, file viewer, commit summary, test result viewer. Requires parser work.

## MVP definition

MVP includes:

- Architecture A5~A10
- E1 Live Dashboard Core
- E2 Core Feed Taxonomy
- E3 Attention Routing Implementation
- E4 Mobile Build Gate
- E5 Emulator / Simulator Smoke
- E6 No-login Local Test Branch
- E7 Installer / Packaging Implementation
- M1 Physical Device Smoke
- M2 Real Push + Notification Tap
- M3 First-time Pairing / Onboarding
- R1 Alpha Candidate Gate

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
