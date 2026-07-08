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
- E8f2 Session-owned Activity Recorder: ACCEPTED (ee1daf0)

Execution and manual validation are intentionally separated:

- E-track: source/test/build/runtime/documented evidence.
- M-track: physical-device UX, onboarding quality, first-time pairing, real push,
  discoverability, visual polish.
- R-track: release decision combining E evidence and M evidence.

## Next target

```text
R1a — Connectivity Baseline for LTE validation
```

Primary handoff document:

- `docs/E8_EXECUTION_PLAN.md`
- `docs/NEXT_SESSION_E8_HANDOFF.md`
- `docs/E8_RUNTIME_RELIABILITY_BUG_REPORT.md`
- `docs/E8F2_RECORDER_PLAN.md`

Supporting observation log:

- `docs/M_TRACK_PHONE_OBSERVATIONS.md`

## Why R1a now

E8f2 fixed the deeper capture-lifecycle problem found during real-device
validation.

Before E8f2, activity capture was tied to WebSocket lifetime:

```text
HandleWS
→ stream.Read(...)
→ ActivityBuffer.Append(...)
```

E8f2 moved output capture to a session-owned recorder:

```text
Session
→ one recorder
→ one PTY reader
→ ActivityBuffer append once
→ N WebSocket subscribers
```

The next blocker is no longer recorder ownership. The next blocker is practical
real-device validation while outside the local network. R1a exists to restore a
minimal, reliable LTE path for testing daemon reachability, sessions, terminal,
and transcript read paths.

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

1. E8f2-pre — Stream / PTY Ownership Audit. ✅ ACCEPTED
2. E8f2 — Session-owned Activity Recorder. ✅ ACCEPTED
3. R1a — Connectivity Baseline for LTE validation. ← NEXT
4. E8g2 — Transcript Readability Polish.
5. E8i — Real-device Transcript Validation.
6. R1b — Connectivity Product Polish.
7. Later runtime follow-ups:
   - input delivery acknowledgement / failed-send state;
   - Activity message boundary and authorship;
   - tmux vs cmux stream/history/screen/event comparison;
   - cmux Activity/color degradation handling;
   - tmux first-entry hydration proof;
   - terminal command output vs agent answer classification.

## R1a scope

R1a is a validation-enabling connectivity baseline, not final onboarding or
release-grade pairing.

In scope:

- verify stored `BASE_URL` before marking the app connected;
- distinguish daemon unreachable from empty sessions;
- distinguish sessions load failure from zero sessions;
- expose terminal WebSocket connection failure clearly;
- expose transcript/activity read failure clearly;
- support a known LTE-capable route such as a tunnel URL or equivalent manual
  remote URL;
- keep enough diagnostics to tell whether failure is auth, daemon reachability,
  sessions API, WebSocket, or transcript read path.

Out of scope:

- polished onboarding;
- final pairing UX;
- push notification delivery;
- installer packaging;
- production-grade tunnel automation;
- login flow redesign;
- transcript readability polish.

## E8f2 acceptance record

E8f2 is accepted at `ee1daf0`.

Verified:

- `go test -race ./internal/term -run TestRecorder_MultipleSubscribers -count=50`
- `go test -race ./internal/term -count=5`
- `go test -race ./... -count=1`
- `sh scripts/build-gate.sh`

Accepted behavior:

- session discovery can start recorder-backed capture without WebSocket;
- one recorder owns the PTY read loop per session;
- multiple WebSocket viewers subscribe without duplicate `OpenStream()` reads;
- recorder appends terminal output before subscriber broadcast;
- stale/dead recorders are removed before reuse;
- terminal input metadata stores byte count only, not raw input text.

## Historical E8f2-pre acceptance

E8f2-pre can be accepted when the executor produces an ownership audit covering:

- current `HandleWS` stream ownership;
- current `stream.Read()` and `ActivityBuffer.Append()` path;
- live output vs snapshot/history/replay path separation;
- adapter capability matrix for localpty, tmux, cmux, and relevant test
  adapters;
- single-reader vs multi-reader behavior;
- whether stream can be opened without WebSocket;
- recommended recorder insertion point;
- session recreation policy;
- late subscriber behavior;
- replay/snapshot/history append rule;
- terminal input ownership policy;
- slow subscriber/backpressure policy;
- recorder lifecycle and session cleanup policy;
- required E8f2 tests.

No runtime behavior changes should be included in E8f2-pre.

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
payload, but the durable model should be activity-oriented.

```text
ActivityEvent is the durable direction.
It is not the full E8f MVP implementation scope.
```

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

E8f MVP must stay narrow:

- support `terminal_output`;
- support `terminal_input` only if safely available;
- support `system` and `status` / degraded state if needed;
- use explicit sequence ordering;
- keep storage/projection read-only and non-authoritative;
- do not implement mobile Transcript Mode UI;
- do not implement tool / approval / artifact parsers;
- do not duplicate Common Event or Interaction Contract semantics;
- link to existing A7/A9 contract data only by id/ref when already available;
- do not parse ANSI beyond plain text and optional basic styling;
- do not emulate VT100 behavior.

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
