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
M0  Mobile Session Lifecycle Contract          ACCEPT a99070015
M1  Safe Session Creation and Profiles         ACCEPT a99070015
M1.5 Session Ownership / Access Contract       ACCEPT 57718aa39
M2  Stop / Kill / Delete Lifecycle             ACCEPT eae1e96c9
M2.5-0 Device Trust Security Contract          COMPLETE (docs)
M2.5-1 Host Identity / Device Registry         ACCEPT b937cbe0f
M2.5-2 LAN QR Pairing                          REWORK after 740d56716
M2.5-3 Challenge Auth / Short Session            READY (plan only)
M2.5-4 Unified REST / WSS Authentication
M2.5-5 Revoke / Minimal Audit
M3  Mobile Lifecycle UX and Real-device Gate       READY (plan only)
R1  Runtime Signal Discovery                    SCOPED ACCEPT (evidence)
T0  Transcript Contract Reset                 READY (plan/audit only)
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
→ minimum device-trust security
→ runtime signal discovery
→ Transcript projection foundation
→ Transcript semantic enrichment
```

Transcript data-model work is not a prerequisite for mobile lifecycle. The two
projects share stable session identity and timestamps, but lifecycle state must
not be derived from Transcript events.

R1 is a time-boxed research gate after M3 and before T0. It investigates
official hooks/protocols, native logs, and generic PTY structural signals so T0
does not commit to text heuristics where stronger evidence already exists. R1
requires official source references and practical redacted runtime captures; it
is not a documentation-summary exercise. It does not replace the generic
byte-stream Transcript fallback and does not add production integration code.
This post-M3 R1 is distinct from the already accepted historical `R1a
Connectivity Baseline`.

Independent R1 evidence was completed early by explicit authorization. The
result is scoped because only Claude Code and Codex were locally executable;
uninstalled products are documented-source findings, not runtime proof. See:

- `docs/R1_RUNTIME_SIGNAL_MATRIX.md`
- `docs/R1_RUNTIME_SIGNAL_EVIDENCE_MANIFEST.md`

T0 is separately specified in `docs/T0_TRANSCRIPT_CONTRACT_RESET_PLAN.md`.
It is an audit/contract phase, not permission to add more legacy
Recorder/ActivityBuffer cleanup heuristics. Its execution handoff is
`docs/NEXT_SESSION_T0_TRANSCRIPT_HANDOFF.md`.

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

Detailed mobile product contract and staged execution handoff:

- `docs/M3_MOBILE_SESSION_LIFECYCLE_UX_PLAN.md`
- `docs/NEXT_SESSION_M3_MOBILE_LIFECYCLE_HANDOFF.md`

### M0/M1 review status

Initial commit `e4e2704d0` was rejected at the product boundary. Corrective
commit `a99070015` is accepted. It closes all three blockers:

1. HTTP creation is preset-only; custom and controlled_pty legacy command shapes
   are denied. Arbitrary `pokit run` creation moved to the privileged `0600`
   Unix socket.
2. `running` is returned only after Recorder readiness. Startup failure cleans
   the created runtime and returns failed/non-success.
3. malformed/missing/non-string local legacy commands are rejected and create
   no session.

Verification:

```text
go test -race ./internal/term ./internal/mux ./cmd/devremote -count=1
PASS

scripts/build-gate.sh
ALL GATES PASSED
```

Acceptance record: `docs/M0_M1_A990700_ACCEPTANCE.md`.

## M1.5 — Session Ownership and Local Host Contract

M1.5 is a narrow capability/contract phase before M2.
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

## M2 acceptance

Corrective commit `eae1e96c9` is accepted. It closes the legacy query-DELETE
bypass, requires confirmed Recorder EOF before terminal finalize, reconciles
fast natural exit using the exact Recorder, serializes Delete with lifecycle
operations, and removes ended controlled PTY sessions from the adapter.

Acceptance record: `docs/M2_EAE1E96_ACCEPTANCE.md`.

## M2.5 — Minimum Device Trust Security

M3 must not expose remote Create/Stop/Kill/Delete over the current static
`dev-token`. Before M3, Pokit builds only the security skeleton that later
features would otherwise have to retrofit:

```text
host/device identity
→ LAN-only pairing
→ device challenge authentication
→ short-lived in-memory session token
→ common REST/WSS authentication
→ revoke and minimal audit
```

Security trust statement for personal MVP / initial closed beta:

> Cloudflare is trusted as the HTTPS/WSS transport confidentiality processor
> and may technically observe terminal traffic. Pokit daemon remains the final
> device authentication and authorization authority. End-to-end encryption
> against Cloudflare is not provided in this phase.

Detailed scope and deferred hardening:

- `docs/M2_5_DEVICE_TRUST_SECURITY_PLAN.md`
- `docs/NEXT_SESSION_M2_5_1_HANDOFF.md`

E2EE/Noise, frame encryption, hardware attestation, advanced multi-device roles,
host-key recovery/rotation, discovery, and Push are explicitly deferred. The
authentication/client boundaries introduced now must allow those additions
without rewriting product screens or lifecycle handlers.

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
