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
E8a — Connection State Correctness
```

Primary handoff document:

- `docs/E8_EXECUTION_PLAN.md`
- `docs/NEXT_SESSION_E8_HANDOFF.md`
- `docs/E8_RUNTIME_RELIABILITY_BUG_REPORT.md`

Supporting observation log:

- `docs/M_TRACK_PHONE_OBSERVATIONS.md`

## Why E8a now

The immediate priority changed.

Mobile E8 validation is blocked because connection failures are hidden as empty
product state:

```text
Stored BASE_URL
→ app marks connected
→ Dashboard opens
→ listSessions() fails
→ empty sessions
→ user sees only NEW AGENT
```

This must be fixed before Mobile WebView E8DIAG validation. A broken connection
must not look like a healthy daemon with no sessions.

## Why E8 still matters

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

## Revised E8 priority order

1. E8a — Connection State Correctness.
2. E8b — Mobile Connectivity Restore.
3. E8c — Mobile WebView E8DIAG Capture.
4. E8d — Terminal Duplication Fix Acceptance.
5. Later E8 follow-ups:
   - input delivery acknowledgement / failed-send state;
   - Activity message boundary and authorship;
   - tmux vs cmux stream/history/screen/event comparison;
   - cmux Activity/color degradation handling;
   - tmux first-entry hydration proof;
   - terminal command output vs agent answer classification.

## E8a acceptance

E8a can be accepted when the executor proves connection-state correctness.

Minimum acceptance:

- restored `BASE_URL` is verified before `isConnected=true`;
- failed `/api/sessions` is shown as a connection error, not empty sessions;
- successful empty `/api/sessions` is distinguishable from failed fetch;
- ConnectScreen shows failed connection feedback;
- retry/rescan path exists;
- `sh scripts/build-gate.sh` passes.
- tests or runtime evidence prove the changed path.

## Explicitly avoid

Do not implement in E8:

- E9 terminal discovery / agent attach flow;
- new terminal backend;
- new agent backend;
- first-time onboarding redesign;
- new tunnel manager;
- new daemon bind flag;
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

1. Implement E8a only.
2. Verify unreachable saved URL does not enter Dashboard as connected.
3. Verify unreachable manual/tunnel URL shows connection error.
4. Verify successful empty sessions are not confused with fetch failure.
5. Run build gate.
6. Document remaining E8b/E8c/E8d work.

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
