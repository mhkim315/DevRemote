# Base Alpha Dual-Surface Managed Session Execution Plan

**Status:** AUTHORITATIVE — DS-0 COMPLETED, NEXT EXECUTABLE: `DS-CX1`

**Planning baseline:** `21a8d973999631ec1c56abd57b790a51b4ecfb7b`

**Branch:** `feature/canonical-timeline-foundation`

**Provider pins under review:**

- Codex CLI `0.145.0`: **B — shared-core seam exists; required live
  dual-client co-presence is not yet proven**
- Claude Code `2.1.218`: **C — official interactive PTY plus session-bound
  hooks and private, version-sensitive JSONL side evidence**

This document is the sole execution authority for the remaining Base Alpha
Terminal/Transcript remediation. It begins after the implementation already
present through `9.5-TM-R4` (`0332126c7`) and the later research packet
`21a8d9739`. It does not restart R0 through R4.

The old `9.5-TM-R5` packet is **cancelled**. It must not be dispatched,
implemented, revived under another name, or treated as a prerequisite.

## 1. Authority and supersession

This plan supersedes the future execution instructions and incompatible
architecture in:

- `BASE_ALPHA_TERMINAL_MANAGED_REMEDIATION_PLAN.md`; and
- `R1_5_FEASIBILITY_REPORT.md`.

Those documents remain historical records. Their completed implementation
SHAs, test rationale, and evidence are not rewritten. The following old claims
are no longer authoritative:

- `managed` means `headless`;
- `codex_app_server:*` and `claude_headless:*` can never have a Terminal;
- adapter or managed-ID prefix alone determines the mobile surface;
- Codex `0.145.0` is category A;
- `codex app-server proxy` is an official TUI client;
- stock Codex already guarantees complete multi-client live-event broadcast;
- Claude JSONL is a stable public provider protocol; and
- the old `9.5-TM-R5` may proceed.

Historical physical evidence remains valid only for the exact artifacts that
produced it. It is not evidence for any DS implementation or artifact.

### 1.1 Accepted ancestry at the planning baseline

| Work | SHA | Current treatment |
| --- | --- | --- |
| R0 evidence reconciliation | `8aaa0213e5481b1753f15da8d1bf664c917fd458` | Preserve |
| R1 runtime/provider contract | `c6e569f51d2068f5c0cac8f7ac2d74c77ce5cfe2` | Adapt |
| R2 Terminal-first implementation | `270cd2216dca1f0c2bd97e328d3c7923e91dbda7` | Preserve |
| R3 Transcript availability implementation | `d16de88719de235d78c621d4233c23af14e5bde6` | Preserve/adapt |
| R3 test correction | `1dbe85bd0c7717c80d3d9ff0aad228f7f2e50234` | Preserve |
| R4 Codex Transcript projection | `0332126c7e933239fe3ecd9801e84e3d6b075a7b` | Adapt/reverify |
| R1.5 dual-channel research | `21a8d973999631ec1c56abd57b790a51b4ecfb7b` | Supersede conclusions |

The repository ancestry proves that these commits are present. It does not
replace independent V1/V2 verdict records.

### 1.2 Definition of a managed session

> A managed session is a POKIT-owned provider process and runtime generation
> whose identity, lifecycle, input authority, approval authority, evidence and
> cleanup are controlled by POKIT.

Managed does not imply headless. Terminal and Transcript may be two views of
one managed provider incarnation after that provider's conformance gate passes.

The intended product path is:

```text
pokit run codex | pokit run claude
    → official provider TUI locally
    → the same ordered Terminal on mobile
    → structured side evidence from the exact same provider incarnation
    → Transcript / Activity / approval / lifecycle surfaces
```

This is a target, not a current capability claim. It is enabled separately for
each provider.

## 2. Completed-work disposition

| Component | Decision | Required treatment |
| --- | --- | --- |
| Runtime/provider identity contracts | **ADAPT** | Preserve provider-specific identity and exact generation; remove permanent headless/interactive equivalence. |
| `OwnedPTYRuntime` lifecycle | **PRESERVE** | Reuse its authorization, generation reservation, cleanup and reap invariants; provider hosts may compose equivalent owned PTY machinery without weakening it. |
| `TerminalTransport` | **PRESERVE** | It remains the generation-bound input, resize, replay and fan-out handle. |
| Recorder sole-reader authority | **PRESERVE** | Provider observers, JSON-RPC clients, hooks and JSONL readers never read the PTY. |
| Local/mobile ordered fan-out | **PRESERVE** | Both consume one Recorder stream. No second PTY read or duplicate provider process. |
| Slow-subscriber isolation | **PRESERVE/REVERIFY** | Re-run under provider TUI load and prove a slow mobile subscriber cannot stall the provider process or local terminal. |
| Exact-generation input | **PRESERVE** | Every write, resize, interrupt and lease operation remains generation-bound and device-authorized. |
| Single-writer ownership | **PRESERVE/ADAPT** | Extend the accepted lease to local and mobile clients of provider TUI sessions. |
| Transcript availability/degraded states | **PRESERVE** | Use them for missing, partial, private-schema, dropped and observer-failure conditions. |
| Mobile prefix routing | **SUPERSEDE** | Replace surface selection by server-issued, generation-bound capabilities. |
| `ManagedSessionView` | **ADAPT** | It becomes a structured secondary surface or compatibility view, not an exclusive managed-session route. |
| Codex event store | **PRESERVE/ADAPT** | Retain bounded storage; bind every event to exact thread/turn/item identity and source position. |
| R4 Codex projection seam | **ADAPT/REVERIFY** | Remove correlation bypass, preserve native references, represent gaps and reject stale generations. |
| Source position/cursor/gaps | **ADAPT** | Provider-native position where available; daemon append cursor is storage order only; gaps must be explicit. |
| Approval authority | **PRESERVE/ADAPT** | One responder per request; provider TUI and mobile may not race. |
| Lifecycle ownership | **PRESERVE** | POKIT owns stop, kill, exit observation, cleanup and reap for the exact process group/generation. |
| Headless Codex path | **PRESERVE AS FALLBACK** | Remains explicit until a later independently accepted retirement packet. |
| Headless Claude path | **PRESERVE AS FALLBACK** | Remains explicit until a later independently accepted retirement packet. |
| Ambient attach/process discovery | **REMOVE/PROHIBIT** | Must not be restored. |
| Ambient Claude JSONL discovery | **REMOVE/PROHIBIT** | Only the launch-bound path from an authenticated hook is accepted. |

## 3. Provider-neutral managed-session shell

The provider-neutral shell standardizes authority and surfaces, not provider
protocol semantics.

### 3.1 Ownership matrix

| Identity or resource | Authority |
| --- | --- |
| Runtime ID | POKIT daemon; immutable for one launched process group |
| Provider session ID | Provider-native authority, captured and bound by the provider host |
| Generation | POKIT lifecycle authority; monotonic per canonical session |
| Incarnation | Tuple of runtime ID, generation, provider session ID and provider process identity |
| Process group | POKIT provider host; all child/client processes enumerated and reaped |
| PTY | POKIT provider host for the official interactive TUI |
| Recorder | Sole PTY reader, created once per generation |
| TerminalTransport | POKIT generation-bound fan-out/write/resize boundary |
| Structured observer | Provider-specific, read/evidence path; never lifecycle or PTY authority |
| Input writer lease | POKIT TerminalTransport authority |
| Approval responder | One immutable responder mode per generation and one CAS result per provider request |
| Transcript projection | Bounded, redacted projection; never provider execution authority |
| Lifecycle termination/reap | POKIT lifecycle service and provider host |
| Reconnect | Reattach subscriber/observer to the same incarnation; never infer or silently replace |
| Resume | Provider-specific, explicit, and bound to a new POKIT runtime generation when a process is relaunched |
| Capability/availability | Server-issued snapshot bound to runtime, session and generation |

### 3.2 Mandatory invariants

1. Recorder is the sole PTY reader.
2. Local and mobile Terminal views subscribe to the same ordered PTY stream.
3. ANSI is presentation only. It never proves a turn, tool, approval, session,
   resume or completion identity.
4. Only one exact-generation writer lease may mutate provider input.
5. Only one authority may answer one provider approval request.
6. Structured observer failure never terminates or blocks the provider TUI.
7. PTY subscriber failure never replaces or advances the provider generation.
8. Separate provider agent processes are never represented as one session.
   A Codex app-server and its official TUI client may form one managed process
   group only when the app-server is the single provider core/thread authority.
9. Identity is never joined by prompt similarity, timestamp, cwd, label or
   rendered text.
10. Arbitrary attach, ambient process discovery and ambient transcript
    discovery remain prohibited.
11. Missing evidence becomes degraded or unavailable; it is never inferred.
12. Observer or projection replay never submits input, answers approval,
    changes lifecycle state or revives a process.
13. Every mutation revalidates device permission and exact current generation.
14. Failures preserve the existing explicit headless or interactive fallback;
    they do not silently cross modes.

### 3.3 Capability-driven mobile contract

Every capability response is bound to:

```text
sessionID + runtimeID + providerSessionID + generation + capabilityRevision
```

The closed capability fields are:

| Field | Values |
| --- | --- |
| `terminalSurface` | `available`, `temporarily_unavailable`, `unsupported` |
| `transcriptSurface` | `healthy`, `degraded`, `temporarily_unavailable`, `unsupported` |
| `structuredEvidenceClass` | `provider_native_authoritative`, `provider_side_evidence_partial`, `none` |
| `promptInput` | `available`, `read_only`, `temporarily_unavailable`, `unsupported`, `unauthorized` |
| `approvalResponse` | `available`, `observer_only`, `temporarily_unavailable`, `unsupported`, `unauthorized` |
| `lifecycleControl` | `available`, `read_only`, `temporarily_unavailable`, `unauthorized` |

No single `managed`, `orchestration`, adapter prefix, UI label or provider name
may imply these capabilities.

For a dual-surface session:

- Terminal is the default active work surface.
- Transcript is a secondary structured surface.
- Both show the same runtime ID and generation.
- Claude's partial evidence is visibly labelled partial/degraded as applicable.
- Terminal remains usable when structured evidence fails.
- Transcript remains historical/read-only when the TUI exits.

Existing headless sessions remain separately selectable fallback modes. The UI
must not silently convert an old headless session into a dual-surface session.

## 4. Provider contracts

### 4.1 Codex category B contract

```text
POKIT-owned Codex generation
│
├─ stock Codex app-server
│    └─ thread / turn / item authority
│
├─ official TUI through the shipped remote connection path
│    └─ POKIT-owned PTY
│         └─ Recorder
│              └─ TerminalTransport
│
└─ POKIT app-server client
     └─ structured Transcript / Activity / approval evidence
```

Category B is a candidate shared-core seam, not permission to implement.
`DS-CX1` must use an unmodified stock Codex `0.145.0` binary and the shipped
TUI remote connection path. `codex app-server proxy` is not treated as a TUI.

Implementation is authorized only if conformance proves:

- one exact app-server and thread authority;
- official TUI connection using the shipped binary;
- TUI-originated turns visible live to the POKIT client;
- POKIT-originated turns visible live in the TUI;
- identical thread, turn, item and approval identities;
- complete multi-client delivery under normal, slow and reconnect cases;
- deterministic single approval response ownership;
- disconnect/reconnect without a replacement provider session; and
- no provider fork, source patch or unsupported private seam.

If any requirement fails, `DS-CX2` is skipped. Codex remains:

- explicit interactive Terminal mode using the existing controlled PTY path;
  and
- explicit headless structured mode using the existing app-server path.

The UI must disclose that these are separate sessions.

### 4.2 Claude category C contract

```text
POKIT-owned Claude generation
│
└─ claude --session-id <POKIT UUID>
     └─ POKIT-owned PTY
          └─ Recorder
               └─ TerminalTransport

Authenticated session-bound hooks
├─ lifecycle
├─ tool evidence
├─ permission evidence
└─ exact transcript_path binding

Exact launch-owned JSONL
└─ version-pinned redacted normalizer
     └─ partial/degraded Transcript projection
```

POKIT generates the UUID before launch. Each generated hook configuration
contains a random, one-launch secret or nonce bound to runtime ID, session UUID
and generation. The first authenticated hook may bind one exact
`transcript_path`; later hooks must match it.

Before opening the file, POKIT verifies:

- current-user ownership;
- regular-file type;
- restrictive permissions;
- no symlink at the final path;
- no symlink traversal in a POKIT-controlled binding/open operation;
- session UUID and launch binding; and
- path continuity for the current generation.

POKIT never predicts the path from cwd, never scans Claude directories and
never adopts an existing ambient transcript.

The normalizer:

- is pinned to the exact supported Claude version;
- handles partial lines without emitting partial events;
- detects truncation, replacement and rotation;
- records a gap/degraded state before resuming after discontinuity;
- rejects or degrades unknown schema;
- retains provider-native IDs only when actually present;
- never invents turn identity from order or time;
- redacts before durable projection; and
- never stores raw JSONL, hidden reasoning, secrets, full tool payloads or an
  unbounded duplicate transcript.

Hooks are the official operational evidence boundary. JSONL remains private,
version-sensitive side evidence. Mobile approval is enabled only if `DS-CL1`
proves exactly one responder; otherwise it is observer-only and the provider
TUI remains the responder.

## 5. Input and approval arbitration contract

No provider implementation may choose different arbitration behavior.

### 5.1 Writer lease

The lease identity is:

```text
sessionID + runtimeID + generation + deviceID + connectionID + leaseEpoch
```

Rules:

1. An unowned lease is claimed by the first authorized local or mobile writer.
2. The same owner refreshes it after each accepted write.
3. A different writer is denied with a redacted current-owner state.
4. Normal transfer requires release by the current owner followed by a new
   claim. A host-local recovery action may revoke a stale lease only with
   explicit local confirmation.
5. The existing 30-second inactivity expiry remains the default.
6. Owner connection disconnect releases its connection-bound lease.
7. Generation replacement, stop, kill or retirement immediately invalidates
   the lease.
8. Stale-generation writes produce zero PTY bytes and zero Transcript mutation.
9. Local and mobile writes use the same TerminalTransport path.
10. Provider structured prompt APIs are disabled for a TUI-mode session; a
    prompt must not be sent once through PTY and again through JSON-RPC.

### 5.2 Prompt and input delivery

- Every mobile input operation has a client operation ID and exact generation.
- A bounded recent-result cache prevents duplicate writes for one connection.
- A repeated identical operation returns its stored outcome.
- Reusing an operation ID with different bytes is invalid.
- Accepted means all bytes were written to the captured exact
  TerminalTransport; it does not mean the provider parsed or completed a
  prompt.
- Lost ACK means `delivery_unknown`; there is no automatic replay.
- Local direct keyboard input also requires the active writer lease.
- Ctrl+C typed through the PTY is input and requires the lease.
- A structured provider interrupt is a separate lifecycle mutation: it requires
  interrupt permission and exact generation, but not input-writer ownership.
  Duplicate interrupt attempts are idempotent.

### 5.3 Approval responder

Each generation selects one immutable mode:

- `pokit_broker`: POKIT is the only provider responder; local/mobile approval
  UIs submit to the same authoritative approval store.
- `provider_tui`: the official TUI is the responder; mobile is observer-only.

An approval key is:

```text
provider + providerSessionID + runtimeID + generation + providerRequestID
```

The first authorized response wins by compare-and-commit. A conflicting or
duplicate response returns `already_resolved` and creates no provider write.
Disconnect, timeout, cancellation, stop and generation replacement close the
request explicitly.

`pokit_broker` is forbidden unless the provider conformance packet proves that
the TUI cannot independently answer the same request. If that cannot be
guaranteed, capability state is `approvalResponse=observer_only`.

## 6. Ordered execution roadmap

Conformance waves produce two independent outputs:

1. a wave verdict: `ACCEPT` or `REJECT`; and
2. on ACCEPT, a provider disposition:
   - `DUAL_SUPPORTED`; or
   - `FALLBACK_REQUIRED`.

`ACCEPT/DUAL_SUPPORTED` authorizes that provider's conditional implementation.
`ACCEPT/FALLBACK_REQUIRED` closes the provider branch for this Alpha, skips its
conditional implementation and preserves the explicit headless/interactive
fallback modes. `REJECT` means the conformance work itself is incomplete,
unsafe or unsupported and must be corrected; it is not permission to proceed.

### DS-0 — Authority and document reconciliation

**Purpose:** Establish this plan as the only future execution authority and
cancel old R5.

**Assumptions:** Planning baseline and ancestry are clean and correct.

**Prerequisites:** `21a8d9739` is HEAD and contains R0–R4 ancestry.

**Exact write scope:**

- `docs/BASE_ALPHA_DUAL_SURFACE_EXECUTION_PLAN.md`
- short supersession notices at the top of:
  - `docs/BASE_ALPHA_TERMINAL_MANAGED_REMEDIATION_PLAN.md`
  - `docs/R1_5_FEASIBILITY_REPORT.md`

**Out of scope:** All production, mobile, test, script, fixture and generated
files.

**Preserved authorities:** All accepted runtime, authentication, PTY, approval,
input, lifecycle and evidence contracts.

**Production contracts affected:** None.

**Automated tests:** None beyond documentation checks and Git identity checks.

**Required evidence:** Branch, baseline, upstream equality, clean worktree,
ancestry proof, docs-only diff, `git diff --check`.

**ACCEPT:** One docs-only commit, no stale active R5 instruction, no Codex A
claim, no circular dependency or unbounded wave.

**REJECT:** Any implementation change, rewritten historical evidence,
self-referential SHA requirement, or ambiguous execution authority.

**Rollback/fallback:** Revert the docs-only commit; production is unchanged.

**Executor:** Authoritative architecture/planning model.

**Independent verifier:** Documentation/architecture verifier is recommended
but this reconciliation commit itself is the authority granted by the product
owner.

**Context Guardian:** Not required before commit; required later at the first
provider authority transition.

**Parallel next wave:** After DS-0, DS-CX1 and DS-CL1 are logically independent,
but the coordinator dispatches them in documented order to preserve one active
writer.

**Handoff artifact:** Plan commit SHA, exact changed-file list, clean upstream
proof and first packet ID `DS-CX1`.

### DS-CX1 — Codex 0.145.0 stock-binary co-presence conformance

**Purpose:** Decide whether category B is sufficient for a real Codex
dual-surface session.

**Assumptions:** Installed or staged binary hash identifies stock Codex
`0.145.0`; no source patch.

**Prerequisites:** DS-0 complete.

**Exact write scope:**

- a new bounded Codex conformance test package under
  `companion-daemon/internal/term/`;
- provider-specific synthetic fixtures under that package's `testdata/`; and
- one DS-CX1 evidence document under `docs/`.

No existing production Go file may change.

**Out of scope:** Codex host implementation, mobile code, provider fork,
private source patch, app-server protocol redesign and approval UX.

**Preserved authorities:** Existing headless Codex runtime, controlled PTY,
Recorder, TerminalTransport, input and approval stores.

**Production contracts affected:** None; conformance only.

**Automated tests:**

- stock binary/version/hash rejection;
- official `codex --remote` TUI connection;
- one app-server and one provider thread;
- TUI-originated turn observed by the peer client;
- peer-originated turn live-rendered in TUI;
- exact thread/turn/item identity equality;
- slow peer and slow TUI behavior;
- observer disconnect/reconnect;
- TUI disconnect/reconnect without thread replacement;
- approval request recipient and duplicate-response behavior;
- app-server exit, TUI exit and socket cleanup.

**Live/fixture evidence:** PTY transcript hash, redacted JSON-RPC trace with
native IDs, process tree, binary hash/version, socket identity, reconnect trace,
approval trace and negative tests. Raw secrets and user content are excluded.

**ACCEPT:** Evidence is complete and independently reproducible. Record:

- `DUAL_SUPPORTED` only when every required live direction and identity matches
  using unmodified stock `0.145.0`, one responder is proven and no loss/private
  seam exists; or
- `FALLBACK_REQUIRED` when valid stock-binary evidence conclusively proves that
  one or more required semantics are unavailable, and existing explicit
  headless/interactive fallback regressions pass.

**REJECT:** Incomplete or non-reproducible evidence, prompt/time correlation,
source patch, proxy treated as TUI, binary/version uncertainty, untested
approval ownership or failure to prove the declared disposition. Missing
fan-out, different thread IDs, hydration-only behavior, ambiguous approval or
unbounded event loss require `FALLBACK_REQUIRED`, not `DUAL_SUPPORTED`.

**Rollback/fallback:** Delete/ignore conformance-only code; retain explicit
interactive and headless Codex modes. Mark `DS-CX2` skipped.

**Executor:** Frontier protocol/concurrency model.

**Independent verifier:** Strong, fresh Codex protocol verifier using the
frozen conformance SHA and stock binary digest.

**Context Guardian:** Required if the result is ACCEPT or if the executor
proposes an unsupported seam.

**Parallel next wave:** `DS-CL1` proceeds regardless of ACCEPT or REJECT.
`DS-CX2` cannot begin until DS-CX1 independent ACCEPT.

**Handoff artifact:** Frozen conformance SHA, stock binary digest, command
manifest, raw evidence references and independent verdict.

### DS-CX1B — Codex Single-Client Inline Proxy Conformance

**Purpose:** Prove whether a transparent single-client JSON-RPC inline proxy inserted between standard Codex TUI and `codex app-server` enables real-time dual-surface observation (PTY Terminal + structured events) for a single provider session, overcoming the multi-socket fan-out limitation of stock Codex 0.145.0 without requiring binary forks or patches.

**Assumptions:** Installed stock Codex `0.145.0` binary (`/opt/homebrew/bin/codex`); standard JSON-RPC 2.0 stdio/socket IPC transport; fail-open stream tap.

**Prerequisites:** DS-CX1 conformance completed (proving multi-socket fan-out limitation).

**Exact write scope:**
- a new bounded Codex inline proxy conformance test package under `companion-daemon/internal/term/dscx1b/`;
- synthetic/probe JSON-RPC fixtures under that package's `testdata/`; and
- one DS-CX1B evidence document under `docs/DS_CX1B_EVIDENCE.md`.

No production Go file may change in this wave (probe/fixture only).

**Architectural Compatibility Rules:**
1. **Stock Binary Contract:** Must execute unmodified Homebrew Codex 0.145.0 binary over stdio or Unix socket. Zero binary modifications or source patches.
2. **Dual-Surface Alignment:** POKIT PTY wraps TUI process for Terminal output; inline proxy taps JSON-RPC stream for Transcript/Timeline structured events.
3. **Exact Identity:** Thread ID, turn ID, item IDs, and approval IDs must be observed directly from the single-client JSON-RPC stream without prompt correlation or timestamp synthesis.
4. **Recorder Authority:** Recorder remains sole reader of PTY bytes. Inline proxy reads JSON-RPC IPC bytes only.
5. **Single Process Group:** One TUI process, one app-server process (or unified process group), single session UUID.
6. **Fail-Open Observation:** Parsing or inspection failures in the inline proxy must pass raw bytes through untouched, ensuring zero disruption to human TUI interaction.
7. **Bounded Risk:** Low-risk, zero-mutation stdio/socket byte stream proxying.

**Automated Tests:**
- Stock binary identity and flag validation;
- Transparent JSON-RPC 2.0 message pass-through (request/response/notification);
- Real-time observation of `turn/started`, `item/started`, `item/*/delta`, `turn/completed` events from inline stream;
- Observer stream tap without state mutation or message corruption;
- Fail-open behavior on malformed or unrecognized JSON-RPC frames;
- Clean process group teardown and socket/pipe cleanup.

**Disposition:**
- `ACCEPT/DUAL_SUPPORTED` when inline proxy proves exact live identity observation alongside PTY Terminal without breaking stock TUI operation; or
- `ACCEPT/FALLBACK_REQUIRED` if inline proxy reveals protocol incompatibility with stock TUI IPC.

### DS-CL1 — Claude 2.1.218 exact-session side-evidence conformance

**Purpose:** Prove a single POKIT-launched interactive Claude incarnation can
bind official hooks and one exact private JSONL file without discovery.

**Assumptions:** Stock Claude `2.1.218`; test account and synthetic content.

**Prerequisites:** DS-0 complete; DS-CX1 verdict recorded or explicitly
skipped due environment unavailability.

**Exact write scope:**

- a new bounded Claude conformance test package under
  `companion-daemon/internal/term/`;
- synthetic version-pinned JSONL/hook fixtures under its `testdata/`; and
- one DS-CL1 evidence document under `docs/`.

No existing production Go file may change.

**Out of scope:** Production tailer, production hook server, mobile UI,
directory scanning, inferred path construction and headless-path deletion.

**Preserved authorities:** Existing managed Claude path, controlled PTY,
approval store, lifecycle and security boundaries.

**Production contracts affected:** None.

**Automated tests:**

- POKIT-generated UUID passed through `--session-id`;
- launch nonce/token authentication;
- first-hook transcript binding;
- mismatched UUID, runtime, generation, nonce and path rejection;
- current-user, mode, regular-file and symlink checks;
- no directory scan;
- partial line, malformed line, oversized line and unknown schema;
- truncation, replacement and rotation;
- JSONL/hook tool ID agreement when present;
- honest absence of turn ID;
- hook timeout/retry/duplicate delivery;
- PermissionRequest/TUI responder behavior;
- redaction before projection.

**Live/fixture evidence:** Stock binary digest/version, launch argv with secret
redacted, authenticated hook trace, exact file identity, synthetic JSONL corpus,
rotation trace and permission trace.

**ACCEPT:** Evidence is complete and independently reproducible. Record:

- `DUAL_SUPPORTED` only with one exact process/session/path binding, safe
  partial evidence, closed degraded behavior and a proven approval responder
  mode; or
- `FALLBACK_REQUIRED` when stock Claude cannot satisfy those conditions and
  existing explicit headless/interactive fallback regressions pass.

**REJECT:** Incomplete or non-reproducible evidence, ambient scan, predicted
path, untrusted hook, unknown schema accepted as healthy, secret persistence,
invented turn identity, untested responder behavior or failure to prove the
declared disposition. A proven provider limitation must be recorded as
`FALLBACK_REQUIRED`.

**Rollback/fallback:** Remove/ignore conformance-only additions; retain explicit
headless Claude and controlled PTY modes. Mark `DS-CL2` skipped.

**Executor:** Frontier security/protocol model.

**Independent verifier:** Strong, fresh Claude security/privacy verifier.

**Context Guardian:** Required if ACCEPT or if the requested architecture
changes identity, privacy or approval authority.

**Parallel next wave:** Provider implementation packets are serialized.
DS-CX2 is evaluated first when eligible; DS-CL2 follows or is skipped.

**Handoff artifact:** Frozen conformance SHA, Claude binary digest, fixture
manifest, hook/file evidence references and independent verdict.

### DS-CX2 — Conditional Codex provider-TUI host

**Purpose:** Implement one POKIT-owned Codex app-server generation with the
official remote TUI PTY and a structured peer client.

**Assumptions:** DS-CX1 proved all required semantics without provider changes.

**Prerequisites:** DS-CX1 independent `ACCEPT/DUAL_SUPPORTED` and Context
Guardian ACCEPT.

**Exact write scope:**

- `companion-daemon/internal/term/managed_codex.go`
- a new Codex TUI host file in `companion-daemon/internal/term/`
- bounded Codex host tests in the same package
- `companion-daemon/cmd/devremote/app.go`

`TerminalTransport`, `Recorder`, common lifecycle and approval files may change
only through a separately identified minimal hunk required to compose the
existing interfaces. Mobile files are forbidden.

**Out of scope:** Claude, mobile routing, generic provider abstraction,
provider patch, headless retirement and new approval policy.

**Preserved authorities:** Recorder sole-reader, exact generation, device
authorization, existing lifecycle service, headless Codex fallback.

**Production contracts affected:** Codex create/start topology, process group,
Terminal availability and structured observer composition.

**Automated tests:**

- one app-server/thread and one TUI client process group;
- exact runtime/provider/generation binding;
- startup rollback at every boundary;
- PTY/Recorder/TerminalTransport single creation;
- local/mobile fan-out and slow subscriber;
- observer failure is fail-open;
- TUI failure and app-server failure have distinct lifecycle outcomes;
- socket ownership/mode/cleanup;
- stale process, event and generation rejection;
- R4 native ID/cursor/gap correction;
- full headless fallback regression.

**Live/fixture evidence:** Stock binary integration run, process/socket
manifest, native ID trace, output order digest and failure matrix.

**ACCEPT:** One exact Codex provider core and thread, official TUI Terminal,
complete structured evidence, no authority duplication and all fallback tests.

**REJECT:** Two provider cores, identity inference, second PTY reader,
observer-blocked TUI, source patch, ambiguous approval or headless regression.

**Rollback/fallback:** Disable the new capability and use existing explicit
headless/interactive modes without data migration.

**Executor:** Frontier architecture/concurrency model.

**Independent verifier:** Strong, fresh implementation V1; fresh V2 after
manifest/evidence.

**Context Guardian:** Required after V1 before any shared/mobile integration.

**Parallel next wave:** DS-CL2 must wait because composition files and
authority boundaries overlap.

**Handoff artifact:** Implementation SHA, deterministic changed-file/test
manifest, V1 verdict, optional evidence SHA, V2 verdict and capability record.

### DS-CL2 — Conditional Claude interactive host and exact evidence

**Purpose:** Implement the accepted category C interactive Claude host,
authenticated hook binding and exact-file normalizer.

**Assumptions:** DS-CL1 proved safe exact-session binding and responder mode.

**Prerequisites:** DS-CL1 independent `ACCEPT/DUAL_SUPPORTED` and Context
Guardian ACCEPT;
DS-CX2 closed as ACCEPT or SKIPPED.

**Exact write scope:**

- `companion-daemon/internal/term/managed_claude.go`
- new Claude interactive host, hook-ingress and exact-file reader files under
  `companion-daemon/internal/term/`
- bounded tests in the same package
- `companion-daemon/cmd/devremote/app.go`

Common Transcript contracts may change only to represent partial provenance,
gap and degraded states. Mobile files are forbidden.

**Out of scope:** Codex behavior, ambient readers, complete JSONL retention,
generic provider prompt API, UI routing and headless deletion.

**Preserved authorities:** Recorder, TerminalTransport, exact generation,
device trust, lifecycle, redaction and headless Claude fallback.

**Production contracts affected:** Claude create/start topology, hook ingress,
partial Transcript projection, approval capability and cleanup.

**Automated tests:** All DS-CL1 negatives plus production composition,
startup/crash rollback, file descriptor cleanup, tail restart, redaction,
generation replacement, hook replay, permission mode, observer fail-open,
TUI exit and fallback regression.

**Live/fixture evidence:** Stock interactive run with synthetic bounded
content, process/PTY identity, hook/file binding manifest, projection output,
redaction and failure traces.

**ACCEPT:** One exact Claude process/session, official TUI, authenticated hooks,
safe exact-file partial projection, truthful capability and no responder race.

**REJECT:** Scan/discovery, unsafe file open, raw JSONL persistence, false
healthy state, duplicate approval response or TUI blocked by observer failure.

**Rollback/fallback:** Disable the new capability; preserve headless Claude and
controlled PTY modes.

**Executor:** Frontier security/concurrency model.

**Independent verifier:** Strong, fresh implementation V1 and fresh V2.

**Context Guardian:** Required after V1 before shared/mobile integration.

**Parallel next wave:** DS-UI3 waits until both provider branches are
ACCEPT/SKIPPED.

**Handoff artifact:** Implementation SHA, deterministic manifest, V1/V2
verdicts, evidence references and capability record.

### DS-UI3 — Capability-driven Terminal/Transcript routing

**Purpose:** Present Terminal and Transcript based on server authority rather
than adapter prefixes.

**Assumptions:** At least one provider host is accepted or both are explicitly
fallback-only.

**Prerequisites:** Each provider branch is closed as either:

- conditional implementation ACCEPT; or
- conformance `ACCEPT/FALLBACK_REQUIRED`, with conditional implementation
  recorded `SKIPPED_BY_CONFORMANCE`.

**Exact write scope:**

- bounded session/capability DTO handlers under `companion-daemon/internal/term/`
- `mobile/src/lib/client.ts`
- `mobile/src/screens/FeedScreen.tsx`
- `mobile/src/components/ManagedSessionView.tsx`
- directly corresponding daemon/mobile tests

**Out of scope:** Provider process logic, approval policy, writer policy,
headless retirement, visual redesign and Activity redesign.

**Preserved authorities:** Existing Transcript API authority, TerminalTransport,
server-issued permissions, lifecycle stores and fallback routes.

**Production contracts affected:** Capability DTO and mobile session surface
routing.

**Automated tests:**

- all closed capability states;
- capability snapshot exact-generation binding;
- no prefix-only routing;
- Terminal default for dual-surface sessions;
- Transcript secondary and same generation;
- Claude partial/degraded disclosure;
- observer failure fallback;
- headless compatibility;
- unauthorized/read-only behavior;
- stale capability response rejection.

**Live/fixture evidence:** Deterministic render matrix for Codex accepted/
fallback and Claude accepted/fallback states.

**ACCEPT:** Every surface follows server capabilities, never a label/prefix;
Terminal is default only when available; fallbacks remain accessible.

**REJECT:** Global managed boolean, inferred capability, hidden degradation,
wrong-generation surface or fallback loss.

**Rollback/fallback:** Restore old consumers while keeping new capability DTO
unused; provider hosts remain operable through explicit compatibility routes.

**Executor:** Normal strong mobile/full-stack executor.

**Independent verifier:** Fresh mobile/API V1 and bounded V2.

**Context Guardian:** Not required unless the implementation changes authority
instead of presentation.

**Parallel next wave:** DS-ARB4 cannot begin until routing ACCEPT.

**Handoff artifact:** Implementation SHA, render/test manifest, capability
schema, V1/V2 verdicts.

### DS-ARB4 — Exact-generation input and approval arbitration

**Purpose:** Apply the frozen §5 policies to local/mobile TUI control.

**Assumptions:** Provider capability records truthfully state which responder
mode each generation supports.

**Prerequisites:** DS-UI3 independent ACCEPT.

**Exact write scope:**

- `companion-daemon/internal/term/terminal_transport.go`
- bounded terminal WebSocket/IPC and approval delivery files under
  `companion-daemon/internal/term/`
- corresponding handlers in `companion-daemon/cmd/devremote/app.go` only if
  route wiring is required
- mobile input/approval controllers and their tests

Provider normalizers and process launch topology are forbidden.

**Out of scope:** New policy, automatic owner promotion, automatic retry,
approval inference and provider fallback.

**Preserved authorities:** Device trust, exact-generation Input-B ACK,
AuthoritativeApprovalStore, lifecycle permissions and provider responder modes.

**Production contracts affected:** Writer lease, input result, interrupt and
approval response behavior.

**Automated tests:**

- local/mobile claim, refresh, transfer, expiry and disconnect;
- stale generation and retired transport;
- exact-once operation cache and ID collision;
- ACK loss/delivery unknown;
- simultaneous claims and writes under race detector;
- interrupt versus Ctrl+C;
- same approval from local/mobile/TUI;
- first response wins, duplicate/conflict closed outcome;
- observer-only mobile;
- stop/kill/generation replacement cleanup;
- no Transcript mutation on rejected input.

**Live/fixture evidence:** Concurrent local/mobile test trace for every provider
mode and fallback mode.

**ACCEPT:** One writer and one approval responder at every interleaving; zero
stale or duplicate provider mutations.

**REJECT:** Race-dependent winner without recorded authority, automatic replay,
silent denial, duplicate response, or provider-specific policy divergence.

**Rollback/fallback:** Set dual TUI input and mobile approval to read-only;
retain observation, Terminal fan-out and provider-TUI approval.

**Executor:** Frontier concurrency/security model.

**Independent verifier:** Strong fresh concurrency/security V1; separate V2.

**Context Guardian:** Required.

**Parallel next wave:** No; DS-STAB5 begins after ACCEPT.

**Handoff artifact:** Implementation SHA, race/interleaving manifest,
closed-outcome matrix, V1/V2 verdicts.

### DS-STAB5 — Regression and deterministic full-suite stabilization

**Purpose:** Prove new provider paths and all historical safety contracts
together without changing product architecture.

**Assumptions:** All authority-changing implementation waves are closed.

**Prerequisites:** DS-ARB4 ACCEPT, or DS-ARB4 explicitly skipped because no
provider dual-surface implementation was accepted.

**Exact write scope:** Test files only, plus minimal production corrections
that receive a separate bounded defect packet and fresh V1. No feature work.

**Out of scope:** New capabilities, UX redesign, provider version upgrade and
fallback retirement.

**Preserved authorities:** All contracts in §§3–5.

**Production contracts affected:** None unless a separately accepted defect
packet is required.

**Automated tests:**

- complete daemon unit/integration/race/vet/build suites;
- complete mobile TypeScript/Jest suites;
- repeated order-sensitive PTY/event tests;
- provider fallback matrix;
- crash/restart/file-descriptor/socket cleanup;
- no ambient discovery;
- privacy/secret scan;
- docs and diff checks.

**Live/fixture evidence:** Deterministic test manifest with exact commands,
outcomes, repetitions and environment identity.

**ACCEPT:** All required suites pass from a clean worktree with no flakes,
hidden skips or unexpected writes.

**REJECT:** Flake, order dependence, inconsistent manifest, unreviewed product
change or provider-version drift.

**Rollback/fallback:** Revert the specific failing implementation packet or
disable its capability; never weaken a test to accept unsafe behavior.

**Executor:** Normal executor for mechanical fixes; strong executor for
concurrency/security defects.

**Independent verifier:** Fresh full-suite V1 and V2 audit.

**Context Guardian:** Required before artifact freeze.

**Parallel next wave:** No.

**Handoff artifact:** Stabilization SHA, deterministic full-suite manifest,
known-issue list and independent verdicts.

### DS-ART6 — Matched daemon/APK artifact freeze

**Purpose:** Create one reproducible device candidate from one accepted source.

**Assumptions:** No implementation remains open.

**Prerequisites:** DS-STAB5 ACCEPT and Context Guardian ACCEPT.

**Exact write scope:** No production changes. Artifact/evidence output only in
the approved external artifact directory and approved docs evidence path.

**Out of scope:** Code fixes, dependency upgrades, rebuild from another SHA,
reuse of historical artifacts and device testing.

**Preserved authorities:** Accepted source and fallback disclosure.

**Production contracts affected:** None.

**Automated tests:** Clean checkout build, daemon build/vet/race, mobile
typecheck/Jest/release build, order-sensitive suites, diff check, security and
secret scans, provenance extraction.

**Required evidence:**

- exact source SHA and branch;
- clean worktree and upstream identity;
- daemon and APK built from that same source;
- full SHA-256 digests;
- embedded source revision and `vcs.modified=false`;
- toolchain/dependency identity;
- fallback modes and provider pins;
- deterministic test manifest.

**ACCEPT:** One complete immutable artifact bundle and manifest; no missing,
rebuilt or mismatched member.

**REJECT:** Different source identities, dirty build, missing digest, hidden
fallback, untracked binary or post-build mutation.

**Rollback/fallback:** Discard the entire bundle. Never repair it in place.

**Executor:** Reproducibility/build executor.

**Independent verifier:** Artifact/provenance verifier.

**Context Guardian:** Required to freeze `DS_DEVICE_CANDIDATE_SHA`.

**Parallel next wave:** No.

**Handoff artifact:** Immutable artifact directory, machine-readable manifest,
daemon/APK digests and frozen candidate SHA.

### DS-DEV7 — SM-S926N dual-surface physical verification

**Purpose:** Prove the exact frozen product on the supported physical device.

**Assumptions:** SM-S926N is connected, unlocked and reports the expected ADB
identity.

**Prerequisites:** DS-ART6 ACCEPT; exact frozen artifacts available.

**Exact write scope:** Device state and external evidence directory only.
Evidence docs may be updated after the run. No repository code/test changes.

**Out of scope:** Rebuilding artifacts, fixing code in place, substituting an
emulator and reusing historical physical evidence.

**Preserved authorities:** Frozen source/artifact identity and all provider
fallbacks.

**Production contracts affected:** None.

**Automated/device tests for each supported provider mode:**

- official TUI visible locally;
- identical ordered Terminal visible on mobile;
- mobile disconnect/reconnect;
- local subscriber disconnect while runtime continues;
- writer claim and transfer;
- stale input rejection;
- approval responder behavior;
- Transcript content, partial disclosure and degraded states;
- structured observer failure while TUI continues;
- lifecycle stop, kill and cleanup;
- no cross-session Transcript contamination;
- exact generation after reconnect;
- exact installed artifact provenance.

If Codex dual-surface was rejected, verify both explicit Codex fallback modes
and do not claim dual-surface. The same rule applies independently to Claude.

**Required evidence:** Timestamped daemon/mobile/device logs, local Terminal
capture, screenshots, structured event references, artifact hashes and a
closed matrix with NOT SUPPORTED distinguished from FAIL.

**ACCEPT:** Every supported capability passes on the exact artifacts; rejected
provider dual-surface capability is honestly absent; no safety failure.

**REJECT:** Wrong artifacts, identity mismatch, stale mutation, duplicate
approval, blocked TUI after observer failure, cross-session data, unexplained
gap or undocumented fallback.

**Rollback/fallback:** Keep Base Alpha blocked; return to the smallest responsible
implementation wave and create a new artifact candidate after fixes.

**Executor:** Physical-device test executor.

**Independent verifier:** Independent device-evidence verifier.

**Context Guardian:** Required after evidence collection.

**Parallel next wave:** No.

**Handoff artifact:** Complete DS-DEV7 evidence bundle, matrix, installed
digests and proposed verdict.

### DS-AUD8 — Final independent milestone audit

**Purpose:** Decide whether the Base Alpha dual-surface/fallback candidate may
advance.

**Assumptions:** No active implementation or device mutation.

**Prerequisites:** DS-DEV7 closed with complete evidence.

**Exact write scope:** Approved milestone ledger/roadmap documents only after
the verdict. No production/test changes.

**Out of scope:** Defect repair, evidence invention, retroactive contract
change and provider capability expansion.

**Preserved authorities:** Contract, implementation, evidence and artifact SHA
separation.

**Production contracts affected:** None.

**Automated tests:** Manifest validation, ancestry, changed-file
classification, artifact digest comparison and evidence completeness.

**Required evidence:** Complete SHA/verdict chain for every DS wave, frozen
contract references, artifact manifest, device matrix, known issues and
fallback disclosures.

**ACCEPT:** Zero unexplained identity/evidence gaps; every provider capability
matches its conformance verdict; hard safety gates pass.

**REJECT:** Missing independent verdict, stale evidence, unmatched artifacts,
unsupported capability claim, unresolved safety defect or altered authority.

**Rollback/fallback:** PB/Base Alpha status remains unchanged and the responsible
wave reopens through a new bounded packet.

**Executor:** None; audit only.

**Independent verifier:** Strong milestone V2 auditor with fresh context.

**Context Guardian:** This wave is the final Context Guardian/milestone audit.

**Parallel next wave:** None.

**Handoff artifact:** Final verdict, accepted source/artifact/evidence SHAs,
known-issue ledger and next-authorized-action record.

## 7. Dependency and conditional-flow summary

```text
DS-0
  ↓
DS-CX1 ── ACCEPT/DUAL_SUPPORTED → DS-CX2
   │        ACCEPT/FALLBACK_REQUIRED → Codex fallback modes, skip DS-CX2
   │        REJECT → correct DS-CX1
   ↓
DS-CL1 ── ACCEPT/DUAL_SUPPORTED → DS-CL2
            ACCEPT/FALLBACK_REQUIRED → Claude fallback modes, skip DS-CL2
            REJECT → correct DS-CL1

both provider branches CLOSED (ACCEPT or SKIPPED)
  ↓
DS-UI3
  ↓
DS-ARB4
  ↓
DS-STAB5
  ↓
DS-ART6
  ↓
DS-DEV7
  ↓
DS-AUD8
```

One provider's conformance REJECT does not block the other provider. It only
blocks that provider's conditional implementation.

## 8. Global stop conditions

Stop the pipeline immediately when:

- branch, upstream, worktree or prerequisite ancestry differs from handoff;
- a worker completion claim disagrees with Git, file content or test artifacts;
- a write escapes the active wave's scope;
- a provider binary/version/hash differs from the accepted pin;
- conformance uses a provider patch or private unsupported seam;
- two provider cores or two PTY readers appear;
- identity requires prompt/time/rendered-text correlation;
- approval responder ownership is ambiguous;
- ambient process or transcript discovery appears;
- unknown/missing evidence is represented as healthy;
- a structured observer failure blocks the provider TUI;
- a stale generation causes any mutation;
- a conditional implementation is dispatched before conformance ACCEPT;
- the coordinator attempts to merge, reorder or reinterpret waves; or
- historical physical evidence is offered for a new artifact.

The executor returns `BLOCKED` with exact evidence. The coordinator may
redispatch only within the same authority scope or request an explicit plan
amendment.

## 9. Coordinator dispatch rules

1. Dispatch one active writer at a time.
2. Give the executor the exact wave text, baseline SHA, write scope and required
   handoff.
3. Run a deterministic pre-gate before every verifier dispatch.
4. Treat worker claims as untrusted until repository and artifact comparison.
5. Require a fresh independent verifier wherever specified.
6. Record ACCEPT, REJECT or SKIPPED with exact SHA and evidence references.
7. Do not begin the next dependency until the previous gate is closed.
8. Do not convert a conformance REJECT into an implementation workaround.
9. Do not change arbitration, provider identity or fallback policy inside an
   implementation packet.
10. Keep implementation, evidence and milestone verdict identities separate.
11. Never require an artifact to embed its own content-determining commit SHA.
12. Pause and request a plan amendment if an authority-affecting choice is not
    explicitly closed here.

## 10. Headless-path retirement gate

The existing headless Codex or Claude path may be removed only by a future,
separate migration packet after all of the following are true for that
provider:

1. provider conformance received independent ACCEPT;
2. provider host implementation and arbitration received V1/V2 ACCEPT;
3. matched-artifact physical evidence passed;
4. all live consumers and persisted session types are inventoried;
5. compatibility and rollback behavior are documented and tested;
6. capability negotiation no longer relies on the headless path;
7. approval, lifecycle, resume and delete parity is proven;
8. users receive explicit deprecation/migration behavior; and
9. an independent verifier confirms physical deletion does not remove the only
   safe fallback.

No DS wave in this plan authorizes that deletion.

## 11. Decision log

### D1 — Codex A reduced to B

The research packet correctly found a shipped remote-TUI and app-server shared
core seam. It incorrectly treated `app-server proxy` as the TUI and inferred
complete live multi-client fan-out and approval broadcast without a stock
binary proof. The shipped seam is therefore category B until DS-CX1 proves the
required operational contract. A theoretical or patched implementation is not
Base Alpha authority.

### D2 — Claude remains C

Claude official hooks expose session identity, transcript path, tool/lifecycle
and permission events. The JSONL content is private and version-sensitive.
Therefore hooks may provide authoritative operational evidence while JSONL
provides only bounded partial Transcript evidence. Missing turn identity is
reported, not invented.

### D3 — Common authority, separate integrations

Codex and Claude share POKIT ownership, generation, PTY, Recorder,
TerminalTransport, capability, lifecycle and projection boundaries. They do
not share a fabricated provider I/O protocol, prompt contract, approval
mechanism or resume implementation.

### D4 — Terminal first, Transcript secondary

For an accepted dual-surface generation, the official TUI Terminal is the
default work surface and Transcript is structured side evidence. This does not
make ANSI authoritative and does not permit raw PTY parsing for semantic
identity.

### D5 — Fallback before forced unification

Failed conformance keeps safe explicit headless and interactive modes. POKIT
does not run two provider agents and call them one session, fork providers or
hide a capability gap to achieve a uniform UI.

## 12. First executable packet

After this plan commit is pushed and the worktree/upstream are clean, the first
executable packet is:

> **DS-CX1 — Codex 0.145.0 stock-binary co-presence conformance**

It is conformance-only. It may add bounded tests, fixtures and evidence in the
listed scope. It may not modify production code or begin the Codex TUI host.

The old `9.5-TM-R5` remains cancelled.
