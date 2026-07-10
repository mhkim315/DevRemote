# Pokit Roadmap After E10b

Status: active product roadmap
Branch: `feature/phase10-multi-adapter`
Validated runtime baseline: `6b40b32` — E10b Claude mobile width / PTY geometry mirror

## Product definition

Pokit is now an Agent Runtime + Mobile Cockpit.

The core runtime model is:

```text
pokit run <agent-or-command>
        ↓
Controlled PTY Runtime
        ↓
Recorder = single PTY reader
        ↓
Subscribers:
  - local terminal
  - mobile terminal
  - web terminal
        ↓
Derived products:
  - Transcript
  - Activity
  - Status
  - Approval
  - Notification
```

The invariant remains:

```text
Recorder is the single PTY reader.
No viewer independently opens or reads the PTY.
```

This invariant is now more important than terminal polish. Any change that makes
local/mobile/web a second PTY reader is a blocker.

## Current accepted foundation

### E8f2 — Session-owned Recorder

Status: accepted.

Outcome:

- WebSocket is no longer the owner of capture.
- Recorder is session-owned.
- Multiple viewers subscribe.
- Output is captured even when mobile is disconnected.

### E8g5/E8g6 — Adapter Capability Reclassification

Status: accepted.

Current adapter contract:

| Adapter | Capabilities | Transcript mode |
| --- | --- | --- |
| `controlled_pty` | observe, control, input, liveTerminal, reliableTranscript | byte_stream |
| `tmux` | observe, control, input, liveTerminal, reliableTranscript | byte_stream |
| `localpty` | observe, control, input, liveTerminal, reliableTranscript | byte_stream |
| `cmux` | observe, bestEffortTranscript | screen_snapshot_delta / observe-only |

cmux must not be pushed toward reliable terminal or reliable transcript behavior.

### E9 — Controlled PTY Runtime

Status: accepted.

Outcome:

- Pokit can own a PTY.
- Safe CWD handling exists.
- Recorder-backed Activity/Transcript exists.
- No-WebSocket capture works.

### E10 — `pokit run` Launch UX

Status: accepted.

Outcome:

- `pokit run <command>` creates `controlled_pty` sessions through daemon API.
- `--detach` creates the session without local attach.

### E10b — Local/Mobile Attach Stabilization

Status: accepted for the controlled PTY runtime/product boundary.

Outcome:

- default `pokit run` attaches the local terminal.
- local/mobile/web are recorder subscribers.
- late attach uses raw PTY bootstrap via recorder ring buffer.
- local attach exit/reset has been improved.
- mobile Send behavior has been adjusted for Codex.
- mobile reconnect behavior has been improved.
- mobile terminal now mirrors PTY geometry rather than forcing a phone-width or fixed 100-col terminal.

Real-device validation now confirms:

- `pokit run bash` works;
- local and mobile share the same controlled PTY;
- bidirectional input/output works;
- Terminal.app and VS Code Integrated Terminal work;
- Codex and Claude mobile terminal rendering/horizontal scrolling work;
- background/foreground reconnect works;
- process exit restores the local terminal.

## Roadmap

```text
R0  Finish E10b polish                         ACCEPTED
M0  Mobile Session Lifecycle Contract          IMPLEMENTED / FIX REQUIRED
M1  Safe Session Creation and Profiles         REJECT e4e2704d0
M1.5 Session Ownership / Access Contract       AFTER M0/M1 FIX
M2  Stop / Kill / Delete Lifecycle
M3  Mobile Lifecycle UX and Real-device Gate
T0  Transcript Contract Reset
T1  Byte-stream Transcript Foundation
T2  Codex / Claude TUI Safe Degradation
T3  Semantic Transcript Enrichment
S1  Rich Agent Runtime Status Model
A1  Mobile-first Approval System
N1  Notifications
O1  Orchestrator
P1  Play Store / Distribution readiness
```

The immediate execution order is deliberately lifecycle-first:

```text
Mobile lifecycle MVP
→ Transcript projection foundation
→ Transcript semantic enrichment
```

Transcript data-model work is not a prerequisite for mobile lifecycle. The two
projects share stable session identity and timestamps, but lifecycle state must
not be derived from Transcript events.

## R0 — Finish E10b polish

Status: accepted.

Goal: close remaining terminal usability issues without changing runtime architecture.

Scope:

- Claude mobile wrapping / horizontal scroll validation after `6b40b32`.
- `+ new session` / agent profile save failure.
- real-device validation checklist for local attach, mobile attach, reconnect, exit.
- small UX polish around reconnect and ended sessions.

Do not:

- change recorder ownership.
- reopen cmux reliability work.
- add new runtime abstractions.
- fix Transcript by altering raw Live Terminal behavior.

Validated product boundary:

- `pokit run bash` behaves like a normal local CLI and exits cleanly.
- `pokit run codex` local attach and mobile Send work.
- `pokit run claude` renders acceptably on mobile with horizontal/both-axis scroll.
- mobile reconnect after foreground/network recovery works.
- ended sessions do not expose misleading input controls.
- the remaining `+ new session` failure is not an E10b runtime failure; it is
  replaced by the M0-M3 mobile lifecycle contract below.

## M0-M3 — Mobile Session Lifecycle MVP

Goal: allow mobile to safely create, detach from, stop, force-kill, and later
delete daemon-owned controlled PTY sessions without overloading one ambiguous
"close" operation.

The detailed plan, API contract, state machine, security policy, tests, and
rollback points are defined in:

- `docs/MOBILE_SESSION_LIFECYCLE_AND_TRANSCRIPT_PLAN.md`
- `docs/NEXT_SESSION_M0_M1_HANDOFF.md`

Required distinction:

```text
detach viewer       WebSocket/subscriber disconnect only
stop session        graceful process-group termination, timeout, then SIGKILL
natural process exit
force kill          explicit destructive fallback
delete history      terminal-state catalog/history deletion only
```

Stopping a session must not delete Transcript or Activity history. Recorder
must stop exactly once, and natural exit and requested stop must converge on
the same cleanup path.

### M0/M1 review status

Commit `e4e2704d0` is rejected at the product boundary. Its profile/argv model,
canonical ID generation, CWD validation, lifecycle types, and CLI compatibility
are useful foundations, and the targeted race suite passes. The blockers are:

1. HTTP custom execution is gated by `InsecureLocalOnly`, but a Cloudflare
   tunnel reaches the same localhost daemon. Request origin is therefore not
   proven local. The legacy HTTP payload also bypasses the custom-profile denial
   by omitting `profileId` and sending a command string.
2. Recorder startup/lookup failures are silently ignored and the create API
   still returns `state=running`.
3. Invalid legacy `command` JSON is ignored and can create a default shell
   instead of rejecting the request.

Correction instructions:

- `docs/NEXT_SESSION_M0_M1_FIX_HANDOFF.md`
- `docs/M0_M1_E4E2704_REVIEW.md`

M2 must not start until the corrected M0/M1 boundary is accepted.

## M1.5 — Session Ownership and Local Host Contract

M1.5 is documentation/contract work after the M0/M1 correction and before M2.
It separates two axes that were previously mixed:

```text
Session/runtime source:
  controlled_pty = Pokit-managed lifecycle
  tmux           = externally owned, attachable byte stream
  cmux           = externally owned, best-effort observer

Local terminal host:
  Terminal.app / VS Code / iTerm2 / Ghostty / Warp
  = UI hosting `pokit run`, not runtime adapters
```

Detailed contract:

- `docs/SESSION_OWNERSHIP_AND_LOCAL_HOST_CONTRACT.md`

M2 Stop/Kill/Delete applies only to sessions advertising managed lifecycle.

## T0-T3 — Transcript Projection Refactor

Goal: replace the current Recorder/ActivityBuffer text-cleanup side effect with
an independent, bounded, asynchronous read projection.

Current guidance: it may be better to restart Transcript projection from a simpler
contract rather than continue layering heuristics from cmux tuning.

New contract:

```text
Live Terminal:
  - raw PTY / xterm-compatible
  - terminal state
  - not a human-readable document

Transcript:
  - readable document projection
  - plain text / structured segments
  - should not flatten complex TUI repaint into giant lines
```

Rules:

- Simple command output should be preserved:
  - `pwd`
  - `ls`
  - `echo`
  - tests
  - errors
- Complex TUI/cursor-heavy output should be:
  - filtered,
  - summarized,
  - or replaced with a placeholder such as `[terminal UI output — view in Terminal]`.
- cmux best-effort logic must not contaminate byte_stream transcript logic.
- controlled_pty/tmux/localpty transcript should use a simpler byte_stream line-oriented path.
- T1 must not change the live terminal stream to make Transcript prettier.

The staged implementation is:

```text
T0  audit/reset the current Transcript boundary and collect redacted fixtures
T1  byte-stream projector validated with bash: pwd, ls, echo hello
T2  Codex/Claude TUI safe degradation
T3  optional semantic enrichment from agent-native events/logs
```

T1 acceptance should prove:

- byte_stream transcript does not produce giant merged TUI repaint lines.
- simple shell output remains readable.
- `terminal_input` raw text remains unstored.
- cmux remains best-effort and visibly degraded.
- Transcript failure does not break Live Terminal.

Do not implement perfect command/tool/agent semantics in T1. Complex TUI output
may safely collapse to a bounded marker directing the user to Live Terminal.

## S1 — Runtime Status Model

Goal: introduce structured agent/session state.

Example states:

- starting
- running
- thinking
- awaiting_for_input
- awaiting_for_approval
- running_command
- editing
- completed
- failed
- exited

Sources:

- runtime lifecycle
- PTY output patterns
- JSONL / agent logs as optional enhancers

JSONL is secondary/enrichment data, not the source of truth.

## A1 — Mobile-first Approval System

Goal: safe mobile approval/control.

Examples:

- Approve
- Reject
- Send message
- Continue
- Stop
- Resume
- Retry

Approval must be grounded in runtime state and agent-specific signals, not blind
transcript parsing alone.

## N1 — Notifications

Goal: notify the user when intervention is needed.

Examples:

- agent waiting for input
- approval required
- command failed
- long-running task completed
- session disconnected/exited

## O1 — Orchestrator

Goal: support higher-level workflows.

Potential model:

- user
- orchestrator
- execution agent
- verification agent
- runtime sessions
- approval gates

This is where Pokit becomes more than a terminal.

## P1 — Play Store / Distribution

Goal: prepare closed/internal testing.

Needs:

- stable APK build process
- onboarding
- local daemon install instructions
- tunnel/connection guidance
- privacy/security copy
- crash logs or basic diagnostics
- clear limitations:
  - cmux observe-only
  - Transcript TUI limitations
  - Windows not yet supported
  - local daemon required

## Validation policy

Earlier strict architecture rejection was correct because the runtime contract was
unstable. Going forward, most work is product-feature implementation.

Classify findings as:

### BLOCKER

Use for:

- recorder single-reader violation
- data loss/corruption
- security issue
- broken launch/runtime
- broken local/mobile attach
- broken session lifecycle
- broken API/product contract

### FOLLOW-UP

Use for:

- polish
- optional extra tests
- helper refactors
- testability-only changes
- minor UX inconsistency
- future maintainability

### SCOPED ACCEPT

Use when:

- product behavior is acceptable for the current phase
- known limitations are documented
- no runtime invariant is violated

Avoid forcing architecture churn unless product correctness or core contracts are
at risk.
