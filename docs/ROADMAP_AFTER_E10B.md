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
M2.5-2 LAN QR Pairing                          ACCEPT 637dcdf21
M2.5-3 Challenge Auth / Short Session          ACCEPT 8cc1490f9
M2.5-4 Unified REST / WSS Authentication       ACCEPT d6a5733cd (production-boundary proofs complete)
M2.5-5 Revoke / Minimal Audit                  ACCEPT df7a3bd9a (see acceptance doc)
M3-auth-1C Cross-platform DeviceKey contract       ACCEPT 219631e75 (local Expo module; fail-closed providers; native compile gated)
M3-auth-1A Android Keystore identity                ACCEPT 2fa572cdf (Pixel 9 21/21 instrumentation); Samsung TEE/StrongBox smoke = M-track
M3-auth-2A Android pairing + host pinning            ACCEPT cd7f5eed8 (pairing client + QR + host pinning + e2e)
M3-auth-3A Android bearer lifecycle                  ACCEPT e7e9cf754 (singleflight + refreshAfter401 + strict DTO)
M3-auth-4A Android WS-ticket transport               ACCEPT b34c335 (host-bound REST + ticket Terminal + framing/reconnect proofs)
M3-auth-R1 Authenticated Android M3a E2E reverify    ACCEPT a834955c1 (host-bound profile/create; Jest 209; Android product smoke = M-track)
M3  Mobile Lifecycle UX and Real-device Gate         M3a SCOPED ACCEPT; authenticated reverify after auth-4A
M3-auth-1B iOS Secure Enclave identity               ACCEPT a173fcf (hardware-backed provider + runnable physical-device gate; execution = M-track)
M3-auth-2B iOS pairing/bearer/WS-ticket/Terminal      ACCEPT 0755853a2 (SE key provisioning is in the production scan path; identity-bound save/cold-start restore; Jest 237; physical iOS smoke + native gate = M-track)
M3b Mobile lifecycle action UX                        ACCEPT 9cdf2f290 (authoritative lifecycleState + retained Catalog rows; reachable Force Kill/Delete; epoch-safe controller; per-action DTO validation; capability-enforced input; Jest 289 + clean-prebuild full gate PASS; physical-device/LTE smoke = M-track)
R1  Runtime Signal Discovery                    SCOPED ACCEPT (evidence)
T0  Common AgentEvent Contract                  ACCEPT 3ce2604bd (provider-neutral six-op contract; provenance + Seq; bounded cursor/record/batch/metadata; approval/status authority rules; non-bypassable fixed harness; additive-only; clean-prebuild full gate PASS)
T1  Codex Adapter                               ACCEPT 162266f83 (Codex CLI 0.144.1 session JSONL; fixed T0 harness; bounded position cursor; correlation unavailable; full gate PASS)
T2  Claude Adapter                              ACCEPT ef4a162c7 (Claude 2.1.202 session JSONL; capability-aware fixed harness; approval safely unavailable)
R2  Multi-agent Expansion Research            ACCEPT 6f940b03b (8-agent fit/authority matrix; pinned ACP/Gemini fixtures; T0 remains frozen)
D1  Adapter Doctor/Repair                     ACCEPT 8f7c22def (constrained patch/workspace/fixed-suite/review boundary; activation remains fail-closed)
T3  Transcript Integration                    ACCEPT 9ad6f834f (bounded semantic/fallback projection; echo privacy; authenticated API; production mobile renderer; full gate PASS)
S1  Rich Agent Runtime Status Model           ACCEPT b6504bd7d (S1-A..E; final marker 70ef5df; freshness/epoch/gen high-water; full gate PASS)
S1.1 Runtime Status Hardening                 ACCEPT 02c8385 (exact winner/runtime identity/replay; atomic bounded non-bypassable registry)
A1  Approval Safety Core                      FROZEN after independent R11 ACCEPT (implementation 2e70512; R11 evidence d5a965c; report 997a697; provider-positive path absent)
A1.1 Codex Provider-Positive Path             PLANNED / BLOCKING N1 (app-server stdio candidate; capacity stays zero through CP4; real allow+deny E2E required)
A1.2 Claude Approval Extension                OPTIONAL FOLLOW-UP (not automatically an N1 prerequisite; separate evidence and authorization required)
N1  Notifications                             BLOCKED until independent A1.1 ACCEPT
O1  Deterministic Broker                      PLANNED after N1
O2  Developer-Verifier Loop                   PLANNED after O1
P1  Play Store / Distribution readiness
```

Current executor handoff for the M2.5-4 acceptance evidence gaps:

- `docs/NEXT_SESSION_M2_5_4_FINAL_ACCEPTANCE_HANDOFF.md`

The revised critical execution order is:

```text
M3-auth-1B
→ M3-auth-2B
→ M3b lifecycle UX
→ T0 common AgentEvent contract
→ T1 Codex adapter
→ T2 Claude adapter
→ R2 multi-agent expansion research
→ D1 Adapter Doctor/Repair
→ T3 Transcript integration
→ S1 status
→ S1.1 runtime status hardening
→ A1 approval safety core
→ A1.1 Codex provider-positive path
→ N1 notifications
→ O1 deterministic broker
→ O2 developer-verifier loop
```

A1.2 is deliberately outside the critical sequence. If later authorized, it is a
Claude-specific extension review and implementation, not a reason to delay N1 after
A1.1 acceptance and not a generic provider SDK.

M3-auth-1B/M3-auth-2B positions and iOS/auth scope are unchanged by this
sequencing update; their existing status text above remains authoritative. R1
evidence is already scoped-accepted and informs T0.
Distribution remains planned outside the critical sequence above.

R2 is research-only and may begin only after both T1 and T2 have independent
ACCEPT decisions. It compares publicly documented or legally inspectable event,
session, ordering, authority, and version-drift behavior across additional local
coding agents. It does not implement a third production adapter and does not
modify the frozen T0 contract. See
`docs/R2_MULTI_AGENT_EXPANSION_RESEARCH_PLAN.md`.

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

The former Transcript-specific T0 plan is retained as historical input for T3,
not as the next T0 execution contract. T0 now freezes the common AgentEvent and
six-operation adapter boundary before either provider adapter. D1 is specified
in `docs/ADAPTER_DOCTOR_REPAIR_PLAN.md` and is planning-only.

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

## T0-R2-D1-T3 — Agent Adapters, Expansion Research, Repair, and Transcript Integration

Goal: establish a stable agent-event contract, implement Codex and Claude behind
version-specific adapters, add a constrained repair stage for ordinary version
drift, and only then integrate those events with Transcript projection. After
the first two adapters are independently accepted, R2 checks that these designs
are not overfitted to Codex and Claude before D1 or T3 begins.

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
T0  freeze the common AgentEvent model and six-operation adapter contract
T1  implement and accept the Codex version-specific adapter
T2  implement and accept the Claude version-specific adapter
R2  research additional agents and report fit/gaps without implementing an adapter
D1  detect version drift and propose a constrained, tested, user-approved repair
T3  integrate common agent events with bounded Transcript projection/fallback
```

T0 owns the stable operations:

```text
detect
discoverSessions
readEvents
normalizeEvent
detectApproval
getStatus
```

T1/T2 may modify provider-specific implementations but must not change this
contract. D1 may patch only the version-specific adapter, must preserve older
fixtures, prevent approval false positives, show diff/test evidence, and require
explicit user activation. It cannot repair data that was removed, encrypted, or
made inaccessible. See `docs/ADAPTER_DOCTOR_REPAIR_PLAN.md`.

R2 findings do not automatically revise T0. A provider-specific concept must
first be handled adapter-locally, through bounded metadata, or as an unknown
event. A common-contract revision may be proposed only in a separate review
after comparison against Codex, Claude, and at least one additional provider.

T3 acceptance should preserve the earlier Transcript safety requirements:

- byte_stream transcript does not produce giant merged TUI repaint lines.
- simple shell output remains readable.
- `terminal_input` raw text remains unstored.
- cmux remains best-effort and visibly degraded.
- Transcript failure does not break Live Terminal.

Do not implement perfect command/tool/agent semantics from raw PTY text. Complex TUI output
may safely collapse to a bounded marker directing the user to Live Terminal.

## S1 — Runtime Status Model

Goal: introduce structured agent/session state.

Historical execution handoff: `docs/NEXT_SESSION_S1_RUNTIME_STATUS_HANDOFF.md`.
Final acceptance: `docs/S1_FINAL_ACCEPTANCE.md`.

The earlier example list below mixed daemon lifecycle and agent activity. It is
retained as historical product intent but superseded by the accepted authority
split:

```text
daemon lifecycle: starting | running | stopping | exited | killed | failed
agent activity: unknown | idle | thinking | working | waiting_input |
                waiting_approval | completed | failed | interrupted | degraded
observation health: fresh | stale | unavailable/degraded
approval action state: deferred to A1
```

S1 was staged as S1-A audit/contract, S1-B internal status state, S1-C accepted
T1/T2 production wiring, S1-D authenticated API/mobile consumer, and S1-E
integrated regression/safety verification. Intermediate checkpoints are not
independent acceptance points; request review only after the complete S1 path.

S1 is independently accepted at implementation
`b6504bd7d5c634f0c0459ae87503b82d17c1537b`, canonical marker
`70ef5df28dede7b0f3025eeaab7826f76a229fbf`. Its accepted contract is recorded in
`docs/S1_FINAL_ACCEPTANCE.md`.

S1.1 is independently accepted at implementation `02c8385`. Its final contract
is recorded in `docs/S1_1_RUNTIME_STATUS_HARDENING_FINAL_ACCEPTANCE.md`. Proceed
to A1 only through `docs/NEXT_SESSION_A1_APPROVAL_SAFETY_HANDOFF.md`.

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

Agent-activity authority comes only from accepted, version/correlation-gated
T1/T2 AgentEvents resolved through frozen T0 `GetStatus`/`ResolveStatus`.
Lifecycle remains a separate daemon authority. PTY, screen, prompt, CWD, process,
and Transcript signals are not agent-status authority; process evidence may only
support identity/correlation under the accepted rules.

## S1.1 — Runtime Status Hardening

Goal: harden bounded winning-evidence traceability, launch/process identity, and
recovery/replay behavior after S1 acceptance without changing frozen T0 status
authority or expanding the public DTO without a demonstrated consumer.

Plan: `docs/S1_1_RUNTIME_STATUS_HARDENING_PLAN.md`.
Final acceptance: `docs/S1_1_RUNTIME_STATUS_HARDENING_FINAL_ACCEPTANCE.md`.

S1.1 is accepted independently from S1. A1 is now the next milestone.

Support terminology at this boundary is intentionally strict:

- the ordinary local `pokit run <command>` path is **managed-lifecycle**;
- the accepted preset profile path may be a **recognized managed-agent** when
  exact provider/version, launch generation, correlation, and process identity
  checks all pass;
- no current path is **orchestration-certified** before O1 adds typed dispatch,
  acknowledgement, completion authority, and dynamic eligibility.

## A1 / A1.1 — Mobile-first Approval System and Codex positive path

Goal: safely resolve exact, accepted provider approval requests through one
generation-bound, authenticated, single-use action path.

Frozen provider-neutral plan: `docs/A1_APPROVAL_SAFETY_PLAN.md`.
A1.1 provider plan: `docs/A1_CODEX_PROVIDER_POSITIVE_PATH_PLAN.md`.
Current execution handoff: `docs/NEXT_SESSION_A1_1_CODEX_PROVIDER_POSITIVE_HANDOFF.md`.

The independent plan review returned `ACCEPT WITH REQUIRED PLAN CHANGES`; its
documentation remediation froze atomic `ClaimForExecution`, canonical
ActionDigest/idempotency, server-derived requester binding, an approval-specific
delivery receipt, bounded public DTOs, and the complete acceptance gate. The
first implementation `3b56c4f` was independently rejected for not implementing
those boundaries despite a green existing suite. The B1-B8 remediation at
`ed466094` retained the safe ingestion, non-actionable production default,
device-auth transport and bounded backend DTO, but re-verification rejected its
claim/idempotency/receipt machinery and confirmed that the mandatory positive
provider delivery path is still unavailable. Remediation 2 at `0521c382` fixed
store digest/permission checks, cross-Approval ledger reuse, receipt field checks,
capacity fail-closed and mobile UTF-8 bounds. Remediation 3 at `0ac1acf3` added
full replay comparison, canonical payload binding, bounded retry and log
redaction, but re-verification reproduced claims with empty host/boot context and
delivery after registry disappearance. It also found an unconstrained sink call
under the transition lock and a secret-scan exclusion. Those findings were handed
to the now-superseded remediation-4 handoff. The positive provider path and N1
remain blocked. Remediation 4 at `5360ec61` fixed complete
requester presence, registry-disappearance cleanup and callback-under-lock, but
re-verification found accepted queue entries are lost on generation replacement,
queue/drain entries are not fully bound, receipt entropy/resource bounds fail
closed incompletely, and final report HEAD `d3aa0b09` fails secret scan. That
round used the now-superseded remediation-5 handoff. Remediation 5 at
`0f95c7c3` introduced captured handles, typed queue items and fixed bounds, but
later review found payload mismatch, destructive endpoint pressure and repeated
handle rotation. Remediations 6 and 7 added pre-append payload validation,
non-destructive admission, idempotent same-runtime activation and aggregate
metadata accounting. Remediation 8 implementation `8ec3630` and report `ebdc507`
added digest/token validation and fail-closed capacity, but identity checks remain
length-only, one Activate test is vacuous, aggregate-bound evidence is incomplete,
and the required production/concurrency packet was explicitly deferred. The
independent verdict is recorded in
`docs/A1_APPROVAL_SAFETY_REVERIFICATION_8.md`. Remediations 9 and 10 then closed
canonical identity validation, retained-memory/accounting bounds, production
telemetry evidence and deterministic contested-state coverage. R11 added the
missing deep queue-content snapshot and exact production endpoint/order ownership
assertions. `docs/A1_APPROVAL_SAFETY_REVERIFICATION_11.md` records the independent
R11 ACCEPT at report HEAD `997a697`; the provider-neutral A1 safety core is now
frozen. This does not complete the product milestone: production mapping remains
empty and delivery capacity remains zero.

A1.1 is the bounded completion track. It uses the exact Codex `0.144.1` app-server
v2 stdio candidate described in the A1.1 plan, keeps capacity zero through CP4 and
requires independently verified real allow-once and deny paths before CP5 can
activate the exact certified tuple. N1 remains blocked until independent A1.1
acceptance.

A1.2 is only a possible later Claude-specific extension. It must first prove exact
hook invocation identity, one-response ownership and provider consumption. It must
not modify the frozen A1 core or extract a speculative generic plugin SDK, and it is
not automatically an N1 prerequisite.

Authority boundary: `waiting_approval` status is display-only. Approval authority
must come from ApprovalStore state bound to exact session ID, approval ID,
authoritative approval provenance, and current generation. S1/S1.1 status is
never authorization input.

Scope examples are limited to exact provider approval options whose
evidence-to-action and action-to-delivery mappings have controlled, accepted
fixtures. If a mapping is unproven, the signal is non-actionable display only.
No blind Y/N or arbitrary terminal payload is synthesized.

Existing Stop/Kill remain in the accepted lifecycle contract. Generic Send
Message, Continue, Resume, Retry, Task/Dispatch, worker flow, and orchestration
actions are not A1 and remain deferred to O1/O2 or a separately reviewed
contract.

Approval must originate only from a currently correlated accepted adapter with
`CapApprovalDetection`, `DetectApproval`, and `SafeApprovalGate`. Runtime status,
legacy parser text, transcript/screen/PTY/prompt text, process name, CWD, and
heuristic evidence never create approval authority.

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
