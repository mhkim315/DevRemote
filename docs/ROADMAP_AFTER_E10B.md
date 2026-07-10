# Pokit Roadmap After E10b

Status: draft execution roadmap  
Branch: `feature/phase10-multi-adapter`  
Baseline commit: `6b40b32` — E10b Claude mobile width / PTY geometry mirror

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

Status: mostly accepted; R0 polish remains.

Outcome:

- default `pokit run` attaches the local terminal.
- local/mobile/web are recorder subscribers.
- late attach uses raw PTY bootstrap via recorder ring buffer.
- local attach exit/reset has been improved.
- mobile Send behavior has been adjusted for Codex.
- mobile reconnect behavior has been improved.
- mobile terminal now mirrors PTY geometry rather than forcing a phone-width or fixed 100-col terminal.

## Roadmap

```text
R0  Finish E10b polish
T1  Transcript Projection Contract
S1  Runtime Status Model
A1  Mobile-first Approval System
N1  Notifications
O1  Orchestrator
P1  Play Store / Distribution readiness
```

The next phases should move faster than E8/E9/E10. Most remaining work is
product behavior, not core runtime architecture.

## R0 — Finish E10b polish

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

R0 acceptance should be product-level:

- `pokit run bash` behaves like a normal local CLI and exits cleanly.
- `pokit run codex` local attach and mobile Send work.
- `pokit run claude` renders acceptably on mobile with horizontal/both-axis scroll.
- mobile reconnect after foreground/network recovery works.
- ended sessions do not expose misleading input controls.
- `+ new session` no longer fails with agent profile save error, or the failure is clearly diagnosed and scoped.

## T1 — Transcript Projection Contract

Goal: make Transcript useful without corrupting TUI output.

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

T1 acceptance should prove:

- byte_stream transcript does not produce giant merged TUI repaint lines.
- simple shell output remains readable.
- `terminal_input` raw text remains unstored.
- cmux remains best-effort and visibly degraded.
- Transcript failure does not break Live Terminal.

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

