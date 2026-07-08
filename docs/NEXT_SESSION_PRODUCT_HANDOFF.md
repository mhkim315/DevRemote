# Next Session Product Handoff

> Architecture answers "Can we build it?"
> Product answers "Why would people love using it?"

## Current state

Architecture Phase (A0~A10): COMPLETE.

Accepted executor-verifiable baseline:

- E1 / P1a Live Dashboard Core: ACCEPTED (3fa4ad3c8)
- E2 / P2 Core Feed Taxonomy: ACCEPTED (413734a)
- E3 / P1b Attention Routing Implementation: ACCEPTED as code-verifiable
- E4 / P3 Mobile Typecheck / Build Gate: ACCEPTED (47bed6c)
- E5 Emulator / Simulator Smoke: SCOPED ACCEPT (ccb642f)
- E6 No-login Local Test Branch: ACCEPTED (555e215)
- E7 Local Installer / Packaging Implementation: ACCEPTED (27b5aac)

Execution and manual validation are intentionally separated:

- E-track: source/test/build/runtime/documented evidence.
- M-track: physical-device UX, onboarding quality, first-time pairing, real push,
  discoverability, visual polish.
- R-track: release decision combining E evidence and M evidence.

## Next target

```text
E8 — Runtime Interaction Reliability Diagnostics and Fixes
```

Primary handoff document:

- `docs/E8_RUNTIME_RELIABILITY_BUG_REPORT.md`

Supporting observation log:

- `docs/M_TRACK_PHONE_OBSERVATIONS.md`

## Why E8 now

Phone testing found reliability problems that block useful product evaluation:

- Terminal duplicates output severely on scroll.
- Terminal refresh jumps to bottom.
- Input can appear sent but not produce assistant response.
- Input can be silently lost during unstable connectivity.
- Activity may merge user input into assistant bubbles.
- cmux behaves differently from tmux: cmux Activity is one large block and cmux
  Terminal appears monochrome.
- tmux has a first-entry initial screen/hydration gap.
- Terminal command output such as `ls` can appear as an agent answer.

These are not subjective polish issues. They are runtime trust issues.

## Mandatory reading

Before implementation, read:

- `docs/E8_RUNTIME_RELIABILITY_BUG_REPORT.md`
- `docs/M_TRACK_PHONE_OBSERVATIONS.md`
- `docs/PRODUCT_PHASE_PLAN.md`
- `docs/AGENT_PHASE_A7_ACCEPTANCE.md`
- `docs/AGENT_PHASE_A10_ALPHA_CHECKLIST.md`

## E8 priority order

1. Terminal scroll duplication.
2. Input delivery acknowledgement / failed-send state.
3. Activity message boundary and authorship.
4. tmux vs cmux stream/history/screen/event comparison.
5. cmux Activity/color degradation handling.
6. tmux first-entry hydration proof.
7. Terminal command output vs agent answer classification.

## E8 acceptance

E8 can be accepted when the executor provides objective evidence for the runtime
paths it changes.

Minimum acceptance:

- Terminal scroll does not append duplicate already-rendered content.
- Refresh does not create duplicate terminal output.
- Send failure or pending state is visible or diagnostically provable.
- Activity user messages remain user-authored after assistant response arrives.
- tmux vs cmux behavior is compared with raw evidence:
  - terminal stream;
  - screen/history;
  - `/api/sessions.Events`;
  - ANSI/color preservation.
- If cmux cannot provide structured/color output, it degrades explicitly rather
  than pretending shell output is an agent answer.
- `sh scripts/build-gate.sh` passes.
- Added tests or runtime diagnostics prove the fixed path.

## Explicitly avoid

Do not implement in E8:

- E9 terminal discovery / agent attach flow;
- new terminal backend;
- new agent backend;
- first-time onboarding redesign;
- physical-device-only validation;
- real push notification validation;
- visual polish unrelated to reliability;
- installer/package changes unless required by tests.

## Non-negotiable invariants

- No vendor-specific mobile behavior branch.
- Capability determines executability.
- Observe-only means no control action.
- Unknown agent/status/event degrades gracefully.
- Mobile must not synthesize actions not provided by the server.
- Sensitive prompt, command, token, path, and secret data must not leak.
- E-track acceptance must not claim M-track validation.

## Suggested execution order

1. Reproduce Terminal scroll duplication locally.
2. Inspect Terminal WebView history/live append behavior.
3. Add frame/append instrumentation or regression tests.
4. Fix duplicate append on scroll/refresh/reconnect.
5. Add send attempt / ack / failed-send diagnostics.
6. Compare tmux vs cmux raw stream/screen/history/events.
7. Fix Activity message identity/grouping or prove backend event source issue.
8. Document what was fixed and what remains M-track.

## Completion statement format

```text
E8 implementation complete.

Commit: <sha>

Fixed:
- ...

Validation:
- build gate: ...
- targeted tests: ...
- runtime evidence: ...

Known gaps:
- ...

Remaining M-track:
- physical-device UX
- onboarding quality
- real push/tap behavior
- visual polish
```
