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
E8e — Transcript Read Mode Scope
```

Primary handoff document:

- `docs/E8_EXECUTION_PLAN.md`
- `docs/NEXT_SESSION_E8_HANDOFF.md`
- `docs/E8_RUNTIME_RELIABILITY_BUG_REPORT.md`

Supporting observation log:

- `docs/M_TRACK_PHONE_OBSERVATIONS.md`

## Why E8e now

The immediate priority changed after runtime evidence narrowed the terminal
duplication issue.

Connection-state correctness and E8 instrumentation are already treated as
accepted execution evidence. The remaining terminal-scroll problem is not a
backend replay issue, not a WebSocket reconnect issue in the idle-scroll
scenario, and not a terminal-data integrity issue.

The current evidence points to Android WebView + xterm.js viewport rendering
instability during upward touch scroll. Repeated xterm scrollback patches had
diminishing returns.

Therefore the next useful work is to stop treating xterm.js as the mobile
history reader and document the transcript/read-mode split before
implementation.

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

1. E8e — Read Mode Architecture.
2. E8f — Activity Capture Pipeline.
3. E8g — Mobile Read Mode UI with explicit mode separation.
4. E8h — Fallback / Compatibility.
5. Later runtime follow-ups:
   - input delivery acknowledgement / failed-send state;
   - Activity message boundary and authorship;
   - tmux vs cmux stream/history/screen/event comparison;
   - cmux Activity/color degradation handling;
   - tmux first-entry hydration proof;
   - terminal command output vs agent answer classification.

## E8e acceptance

E8e can be accepted when the executor documents the transcript/read-mode pivot.

Minimum acceptance:

- xterm.js remains live interactive terminal;
- Pokit transcript renderer handles mobile historical reading;
- Transcript Mode is a read projection over captured activity events, not a
  terminal emulator;
- live xterm remains the source of truth for interactive terminal state;
- transcript capture/render failure cannot break live terminal interaction;
- E8g MVP uses explicit Live Terminal Mode and Transcript Mode;
- E8g MVP does not merge transcript and xterm into one continuous scroll surface;
- unsupported/degraded terminal semantics are listed;
- the plan explicitly says not to build a custom VT100/xterm emulator;
- E8f/E8g/E8h follow-up phases are defined;
- `sh scripts/build-gate.sh` passes.
- no implementation of the full transcript renderer is included.

## Explicitly avoid

Do not implement in E8:

- E9 terminal discovery / agent attach flow;
- new terminal backend;
- new agent backend;
- first-time onboarding redesign;
- new tunnel manager;
- new daemon bind flag;
- new xterm mobile scrollback patch;
- full custom terminal emulator;
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

1. Implement E8e documentation only.
2. Define live terminal vs transcript responsibilities.
3. Define transcript as an activity-event read projection.
4. Define transcript unsupported/fallback behavior.
5. Define source-of-truth and failure-isolation invariants.
6. Define E8f/E8g/E8h follow-up phases.
7. Run build gate.
8. Do not implement transcript renderer yet.

## E8f capture model direction

E8f should not be a raw text-line store only. Terminal output can be the first
payload, but the durable model should be activity-oriented:

```text
ActivityEvent
→ terminal_output / terminal_input
→ agent_message
→ tool_call
→ approval_request
→ artifact
→ error / status
```

The transcript UI should render a read projection over these events. It should
not own live terminal state, terminal cursor state, or byte-level terminal
mutation semantics.

## E8g implementation constraint

When E8g implementation starts, use explicit mode separation:

```text
Live Terminal Mode
→ xterm.js only
→ current prompt/input
→ full terminal behavior

Transcript Mode
→ read-only history
→ mobile-native list rendering
→ explicit Return to Live Terminal action
→ read projection over captured activity events
```

Do not build a seamless transcript+xterm scroll surface in the MVP. Avoid shared
scroll containers, boundary-line stitching, off-by-one sync logic, and
ANSI/cursor/progress state transfer between modes.

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
