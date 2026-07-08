# Next Session Product Handoff

> Architecture answers "Can we build it?"
> Product answers "Why would people love using it?"

## Current state

Architecture Phase (A0~A10): COMPLETE.
Product Phase is now split by verification responsibility:

- E-track: executor-verifiable implementation and local validation
- M-track: manual product validation by the product owner
- R-track: alpha release decision gates

Accepted executor-verifiable baseline:

- E1 / P1a Live Dashboard Core: ACCEPTED (3fa4ad3c8)
- E2 / P2 Core Feed Taxonomy: ACCEPTED (413734a)
- E3 / P1b Attention Routing Implementation: ACCEPTED as code-verifiable
- E4 / P3 Mobile Typecheck / Build Gate: ACCEPTED (47bed6c)
- E5 Emulator / Simulator Smoke: SCOPED ACCEPT (ccb642f)

Current baseline:

- P1a Live Dashboard Core has four sections: Needs Attention, Running, Recently Completed, Degraded / View Only.
- A9 interaction card is compatible and rendered within the dashboard.
- Observe-only sessions show VIEW ONLY badge.
- No vendor-specific mobile branches exist.

## Next target

E6 — No-login Local Test Branch.

Do not implement manual product validation, production onboarding redesign, real
push delivery proof, physical-device UX proof, installer packaging, or alpha
release packaging in this slice.

## Why E6 now

The previous P4 attempt proved that an executor can validate emulator/simulator
behavior but cannot genuinely validate physical-device UX, real push delivery,
notification tap behavior on a real device, real login, or first-time onboarding.

Those are M-track manual product validation responsibilities.

Before continuing, the test branch should remove login from the executor path so
the executor can run useful local tests first.

E6 is not a production-login decision. It is a test-branch validation route.

## Orchestrator comment

Do not spend the next executor cycle trying to prove login, account setup,
physical-device push, or first-time pairing.

Those are manual product validation gates.

For E6, remove login from the executor's test path on the test branch and make
the locally verifiable product checks run first. The desired outcome is not
"login is unnecessary." The desired outcome is "the executor can repeatedly test
dashboard, feed, interaction, daemon connectivity, and build gates without being
blocked by human-only product validation."

## Mandatory reading

Before implementation, read:

- `docs/PRODUCT_PHASE_PLAN.md` (Product verification model and E6 section)
- `docs/AGENT_PHASE_A7_ACCEPTANCE.md`
- `docs/AGENT_PHASE_A10_ALPHA_CHECKLIST.md`
- `docs/P4_DEVICE_SMOKE_REPORT.md`

## E6 scope

### Goal

Create an executor-verifiable local product test path that does not require login,
account state, Expo account ownership, physical devices, or manual pairing.

### Acceptance

- App can launch in the test branch without completing login.
- Executor can connect to a local daemon or deterministic test endpoint.
- Dashboard renders with real or controlled local session data.
- Feed renders the accepted E2 event taxonomy.
- Interaction card path can be exercised with controlled local data if available.
- Build gate still passes.
- The login bypass is clearly marked as test-branch-only.
- No production claim is made about real onboarding, account UX, physical device UX, or real push delivery.
- No vendor-specific mobile behavior branch is introduced.

### Explicitly avoid

Do not implement:

- production login removal;
- production onboarding redesign;
- real account login;
- QR pairing quality;
- physical-device validation;
- real push delivery validation;
- notification tap proof on a real device;
- installer / distribution;
- alpha packaging;
- new adapters;
- new parser or backend event model;
- LLM summarization;
- Agent Cockpit;
- deterministic summary;
- review/diff workflow.

Do not introduce:

```ts
if (agentKind === 'claude') { ... }
if (agentKind === 'codex') { ... }
switch (agentKind) { ... }
```

The local test path may use fixtures or controlled local data, but product behavior
must still be agent-agnostic. UI behavior must not branch by vendor.

## Non-negotiable invariants

- A7: unknown agent/status/event fallback preserved.
- A9: interaction events do not imply control capability.
- A9: interaction card remains compatible.
- E3: notification/deep link implementation remains redacted and sessionId-based.
- E4: strict build gate remains reproducible.
- E5: emulator/simulator smoke remains scoped and must not be presented as physical-device proof.
- No vendor-specific rendering branch.
- No backend contract change unless the existing product boundary is demonstrably insufficient and documented.

## Suggested execution order

1. Read `docs/PRODUCT_PHASE_PLAN.md` E6 section.
2. Identify where login/account state blocks local executor validation.
3. Add a test-branch-only bypass or deterministic local route.
4. Clearly label the bypass so it cannot be mistaken for production onboarding.
5. Preserve existing dashboard, feed, interaction, notification, and build-gate behavior.
6. Run the strict build gate.
7. Run emulator/simulator smoke where available.
8. Update the smoke report with what was actually verified.
9. Commit as an E6 implementation slice.

## Reviewer checklist

Reviewer will check:

- login/account state is not required for executor local validation;
- bypass is test-branch-only and clearly documented;
- production login/onboarding is not silently removed;
- dashboard/feed/interaction behavior still works;
- no vendor-specific mobile behavior branch exists;
- no A9 capability-aware interaction regression;
- no A10 redaction regression;
- strict build gate passes;
- emulator/simulator scope is clearly distinguished from physical-device validation.

## Completion statement format

```text
E6 implementation complete.

Commit: <sha>

Changed:
- ...

Validation:
- strict build gate: ...
- emulator/simulator smoke: ...
- login bypass scope: test-branch-only / production-safe
- vendor branch grep: ...

Known gaps:
- ...

Not done:
- M1 physical-device smoke
- M2 real push + notification tap
- M3 first-time pairing / onboarding
- M4 real interaction UX
- E7 installer / packaging
- R1 alpha candidate gate
```
