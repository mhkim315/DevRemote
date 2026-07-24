# Base Alpha Terminal-First and Managed Transcript Remediation Plan

> **Superseded for future execution.** Historical R0–R4 rationale and evidence
> remain intact, but no new packet may be dispatched from this document.
> Continue only through
> [`BASE_ALPHA_DUAL_SURFACE_EXECUTION_PLAN.md`](BASE_ALPHA_DUAL_SURFACE_EXECUTION_PLAN.md).
> In particular, the old `9.5-TM-R5` is cancelled.

**Status:** HISTORICAL — FUTURE EXECUTION SUPERSEDED

**Planning baseline:** `b476d8af700bd29a9b47422f0954088d8bbaed59`

**Gate status:** Base Alpha `DOGFOOD READY` is not accepted. The previously
recorded pairing, trust, restart, revoke, and recovery evidence remains useful
for the exact artifacts that produced it, but a new matched candidate and the
remediation gates below are required before Base Alpha acceptance.

**Completed:**
- `9.5-TM-R0` (`8aaa0213e`) — Evidence & authority reconciliation
- `9.5-TM-R1` (`c6e569f51`) — Runtime mode & provider contract freeze

**Authority:** This document narrows Step 9.5. It does not reopen PB, Steps
9.1–9.4, device trust, approval authority, exact-generation input,
`OwnedPTYRuntime`, `TerminalTransport`, or sole-reader `Recorder`.

## 1. Product correction

The Base Alpha operating model is:

> **Interactive sessions show the real Terminal first. Native managed sessions
> show a structured Transcript first. Neither surface pretends to be the
> other.**

The two runtime modes are explicit:

| Mode | Runtime identity | Primary surface | Secondary surface |
| --- | --- | --- | --- |
| Interactive | `controlled_pty:*` | Raw Terminal | Explicitly degraded/best-effort Transcript |
| Native managed Codex | `codex_app_server:*` | Provider-native Transcript | Operational status/evidence; no fake PTY |
| Native managed Claude | `claude_headless:*` | Provider-native Transcript | Operational status/evidence; no fake PTY |

For interactive sessions, mobile and a local workstation client may observe
the same daemon-owned `TerminalTransport`. `Recorder` remains the sole PTY
reader. Observers do not create additional reads from the PTY.

Simultaneous viewing is allowed. Simultaneous uncoordinated writing is not.
Before more than one client may send input, a separate exact-generation writer
arbitration contract must define the current writer, transfer, expiry,
disconnect, and stale-client outcomes. Until that contract is accepted, one
client is the input owner and other clients are read-only.

For native managed sessions, a local `pokit watch` or `pokit run --follow`
surface may render the same bounded structured event cursor as mobile. It is a
terminal-hosted Transcript client, not a PTY attached to the provider process.

No existing unqualified CLI command may silently change runtime mode. The
implementation packet must introduce and test an explicit mode selection
(`terminal` versus `managed`) and define a compatibility transition before any
default changes. Mobile session creation must make the two modes visible and
must not route a managed ID to a Terminal or a PTY ID to a managed Transcript
controller.

## 2. Verified causes at the planning baseline

### 2.1 Final candidate identity is not closed

The final report names production source `9e3822de6`, while the tracked daemon
and APK manifest identifies `1bdf6b6323e405d67a3634b7ae828416986da367`.
Production and mobile files changed between those commits. The existing
physical evidence therefore proves the earlier artifact's security lifecycle,
not every fix in the reported final source.

The next candidate must use one exact production source SHA for daemon, APK,
installed bytes, and physical evidence. Documentation HEAD and evidence HEAD
remain distinct identities.

### 2.2 Claude is routed through a Codex-only managed I/O path

`ManagedSessionView` accepts both `codex_app_server:*` and
`claude_headless:*`, but its event and prompt calls use
`/api/managed-sessions/*`. Those handlers resolve only
`ManagedCodexService`.

`ManagedClaudeService.processLine` currently recognizes stream observations,
approval joins, and denial witnesses, but does not append assistant content to
a managed event store. Its `eventStoreFor` returns no store. There is no
general Claude equivalent of the Codex one-active-turn prompt contract.

Changing the screen title and routing Claude into `ManagedSessionView` did not
implement Claude managed I/O.

### 2.3 Empty Transcript has different causes by runtime mode

- Native managed runtimes have no PTY or `Recorder`; they require accepted
  provider-native structured evidence projection.
- Managed Codex has a private managed event store, but it is not the existing
  Transcript service/API authority.
- Managed Claude has no assistant-content event store at the baseline.
- Interactive PTY Transcript permanently suppresses new byte-stream projection
  after input to prevent typed or secret content from re-entering history.
  Terminal output continues normally.

An empty managed Transcript is therefore not proof of an idle agent. The UI
must distinguish healthy empty, unsupported projection, degraded source,
suppressed fallback, gap, and endpoint failure.

### 2.4 Two deferred-item explanations are stale

- Terminal TextInput, macros, paste, and xterm keyboard already converge on the
  served page's acknowledged WebSocket input protocol. The remaining defect is
  overlapping input surfaces and operation ownership, not a production
  WebRTC/WebSocket split. The unimported `mobile/src/screens/terminalHtml.ts`
  prototype is not production authority and must not drive the repair.
- Claude Stop, Kill, and Delete already exist and are routed by the shared
  lifecycle dispatcher. The remaining work is end-to-end contract and device
  verification, not adding missing lifecycle methods.

### 2.5 Mobile test execution is order-dependent

At the planning baseline, a full Jest run produced 547/552 PASS, while the two
failing iOS pairing suites passed 23/23 when run together in isolation. Base
Alpha cannot treat a suite whose result depends on mock/order state as a stable
gate.

### 2.6 Codex HTTP/IPC creation mode divergence (found R1)

HTTP `POST /api/sessions` with `profileId=codex` creates `controlled_pty:*`
wrapping the `codex` binary (PTY mode). IPC `{"operation":"create","profileId":"codex"}`
creates `codex_app_server:*` via `ManagedCodexService` (managed mode). The
same profile ID produces fundamentally different adapter types depending on
the caller. Mobile cannot create managed Codex sessions at all. Resolved in
9.5-TM-R1 contract §5.2.

## 3. Provider contract boundary

POKIT must not force Codex and Claude into a common I/O contract when their
native protocols do not share semantics.

### 3.1 Provider ownership matrix

Every semantic domain has exactly one owner. The common layer dispatches to
that owner by canonical adapter prefix; it never infers ownership from labels,
agent kind, or provider string matching.

| Domain | Codex Owner | Claude Owner | Shared/Common |
|--------|-------------|--------------|---------------|
| **Event decoding** | `ManagedCodexService` — JSON-RPC notification parsing, `item/started`, `item/completed`, `turn/*` | `ManagedClaudeService` — stream-json frame decoding, version pinning, content-block assembly | Common Transcript segment kind mapping (after provider normalizer emits) |
| **Prompt submission** | `ManagedCodexService.SubmitPrompt` — one-active-turn enforcement, `turn/start` JSON-RPC via stdin | **NOT IMPLEMENTED** — observation-only until 9.5-TM-R6 proves safe write contract | **None** — no generic SubmitPrompt |
| **Resume** | Codex-owned: app-server session resume, turn continuation | Claude-owned: `ResumeForApproval`, `claudeResumeCoordinator`, `tool_deferred` behavior | **None** — resume is provider-specific |
| **Approval** | Codex-owned: JSON-RPC `permission/request` notifications, approval delivery via `AuthoritativeApprovalStore` | Claude-owned: `tool_deferred` detection, approval join/denial witnesses, `claudeDispatchingApprovalDelivery` | Common `SafeApprovalDTO`, `SafeOption` redacted projection; `AuthoritativeApprovalStore` is shared infrastructure |
| **Completion** | Codex-owned: `turn/completed`, `item/completed` JSON-RPC, exit code classification | Claude-owned: stream-json `message_stop`, `tool_deferred` graceful exit, `SimulateGracefulExit` | Common `LifecycleExited` / `LifecycleKilled` / `LifecycleFailed` states |
| **Lifecycle (Stop/Kill/Delete)** | `ManagedCodexService` via `LifecycleService` dispatcher | `ManagedClaudeService` via `LifecycleService` dispatcher | `LifecycleService.ownerFor` — dispatches by adapter prefix; `ProviderLifecycleOwner` interface is shared |
| **Transcript projection** | Codex-specific normalizer (9.5-TM-R4) | Claude-specific normalizer (9.5-TM-R5) | `transcript.Service` — adapter-agnostic read/write; bounded segment schema |
| **Terminal** | **None** — managed Codex has no PTY | **None** — managed Claude has no PTY | `controlled_pty:*` only — `HandleWS` + `TerminalTransport` |

### 3.2 Shared minimum contract

Only the following may be common:

- provider, canonical session ID, runtime ID, launch generation, and
  configuration epoch;
- lifecycle state and exact lifecycle owner;
- server-issued capabilities and availability/degradation state;
- bounded cursor, event identity, source identity/position, timestamps,
  redaction policy, and gap/collision representation;
- provider-neutral Transcript segment kinds that have proven mappings;
- authorization principal and immutable evidence references.

The common layer must dispatch to a provider owner. It must not call a Codex
concrete service for a Claude ID, infer provider from UI labels, or claim a
capability that the selected provider owner does not implement.

### 3.3 Codex-owned semantics

Codex retains ownership of:

- app-server JSON-RPC initialization and turn protocol;
- one-active-turn prompt submission;
- thread/turn/item identity;
- Codex-native event decoding and completion/error outcomes; and
- Codex resume/cancel behavior.

### 3.4 Claude-owned semantics

Claude retains ownership of:

- stream-json decoding and version pinning;
- one-shot, resume, hook, and `tool_deferred` behavior;
- Claude session and content-block identity;
- approval delivery/consumption witnesses; and
- Claude-specific termination and resume outcomes.

Claude must not expose a generic prompt box until an accepted provider-native
write contract proves how a prompt reaches the exact current Claude
incarnation. If the current process is observation-only or one-shot, the UI
must say so and remain read-only.

### 3.5 Shared Transcript projection

Provider-specific normalizers may emit only accepted, bounded, redacted
Transcript inputs. The existing Transcript service/API remains the Alpha
read authority until a separately accepted consumer migration.

The projection must not include:

- raw PTY bytes from managed sessions;
- inferred reasoning or chain-of-thought;
- terminal-text heuristics;
- bearer/pairing secrets, private keys, unredacted environment values, or
  unbounded native payloads; or
- provider-specific fields disguised as universally supported semantics.

Timeline remains operational evidence, not the full conversation store and not
input, approval, lifecycle, or delivery authority.

## 4. Security Regression Checklist

Every implementation wave must pass these checks before claiming ACCEPT.
These apply to ALL waves; a single failure blocks the wave.

### 4.1 Mutation Authorization

| Check | What to Verify | Applies To |
|-------|---------------|------------|
| **MutationAuthorizer wired** | Every lifecycle, prompt, and create handler receives a non-nil `MutationAuthorizer`. Nil authorizer is a construction error (`create.go`, `lifecycle_service.go:80`, `ipc.go:39`). | All waves |
| **AuthorizeAndCommit called** | Every mutation (Stop, Kill, Delete, SubmitPrompt, approve/reject) goes through `authorizer.AuthorizeAndCommit(deviceID, deviceEpoch, intent, fn)`. The decisive comparison happens inside the lock. | 9.5-TM-R4, R5, R6 |
| **Intent matches operation** | `IntentSessionStop` for Stop, `IntentSessionKill` for Kill, `IntentSessionDelete` for Delete, `IntentPrompt` for prompt. No intent reuse or `""` intent. | 9.5-TM-R6 |
| **Device identity captured before lock** | `deviceID` and `deviceEpoch` are derived from `PrincipalFromContext` or `ipcMutationIdentity` BEFORE entering the authorization lock. They are never read from the request body. | All waves |
| **Principal nil handling** | When `PrincipalFromContext` returns nil (insecure-local-only mode), `deviceID` and `deviceEpoch` are zero-valued. The authorizer's behavior for zero identity is explicit and documented. | All waves |

### 4.2 Epoch and Generation Binding

| Check | What to Verify | Applies To |
|-------|---------------|------------|
| **Epoch from catalog, not request** | The decisive epoch comparison uses the server-derived epoch from `ManagedRuntimeCatalog.Get(id).Epoch` or `OwnedPTYRuntime.Get(id).State`, NEVER the request body epoch alone. | 9.5-TM-R4, R5, R6 |
| **Stale epoch → 409** | A mismatched epoch between the request and the server-derived record returns `ErrLifecycleStaleGeneration` (409). The replacement process is unaffected. | 9.5-TM-R6 |
| **Epoch in event store** | Every event appended to a managed event store carries `epoch` in its identity tuple `(sessionID, epoch, seq)`. Events from a replaced generation are rejected. | 9.5-TM-R4, R5 |
| **Catalog read under lock** | The catalog read (deriveEpoch) and the owner's decisive comparison occur under the same lifecycle lock — no TOCTOU gap between derivation and decision. | 9.5-TM-R6 |

### 4.3 Provider Isolation

| Check | What to Verify | Applies To |
|-------|---------------|------------|
| **No cross-provider dispatch** | `HandleManagedSessionPrompt` does not call `ManagedClaudeService`. `HandleManagedSessionEvents` does not call `ManagedCodexService.Registry()` for a Claude session ID. | 9.5-TM-R4, R5, R6 |
| **Adapter prefix dispatch** | Every handler that branches on provider uses `sessionid.ParseSessionID(id).Adapter` for the switch, never `strings.Contains`, `HasPrefix`, or `agentKind` inference. | All waves |
| **Separate registries** | `ManagedCodexService.Registry()` and `ManagedClaudeService.Registry()` are separate instances. Cross-registry lookup (e.g., looking up a `claude_headless:*` ID in the Codex registry) must return `(ManagedSessionRecord{}, false)`. | All waves |
| **ManagedSessionView routing** | `FeedScreen.tsx:97` routes by `session.startsWith('codex_app_server:') \|\| session.startsWith('claude_headless:')`. No `agentKind` or label-based routing. | 9.5-TM-R3 |

### 4.4 Input/Output Boundaries

| Check | What to Verify | Applies To |
|-------|---------------|------------|
| **No PTY for managed sessions** | `HandleWS` at `pty.go:269` returns error for `codex_app_server:*` and `claude_headless:*`. No managed session can open a WebSocket terminal. | All waves |
| **No raw bytes in managed Transcript** | Managed Transcript segments carry only validated, bounded `kind`/`source`/`text` fields. Raw PTY bytes never enter managed Transcript. | 9.5-TM-R4, R5 |
| **Secrets never in Transcript** | Bearer tokens, private keys, environment values, and unredacted provider payloads are rejected by the normalizer before reaching the Transcript segment store. | 9.5-TM-R4, R5 |
| **Input ACK preserves delivery_unknown** | Lost ACKs surface `delivery_unknown`, not silent success. Partial delivery preserves the original text. | 9.5-TM-R2, R7 |

### 4.5 Cleanup and Idempotency

| Check | What to Verify | Applies To |
|-------|---------------|------------|
| **Delete clears all projections** | `LifecycleService.Delete` clears `transcript.ClearTranscript(id)` AND `status.Clear(id)`. Provider-level Delete clears registry + coordinator + approvals. | 9.5-TM-R6 |
| **Running delete rejected** | `LifecycleService.Delete` returns `ErrLifecycleNotTerminal` (409) for non-exited sessions. Zero mutation. | 9.5-TM-R6 |
| **Idempotent terminal outcomes** | Repeated Stop on an exited session returns the current terminal state without error. Repeated Kill same. Repeated Delete after deletion returns 404. | 9.5-TM-R6 |
| **No orphaned runtimes** | Every Create path that fails after launch cleans up the runtime. No background goroutine outlives its session without an exit watcher. | All waves |

## 5. Model Allocation Strategy

Different waves require different reasoning depth. The following table assigns
a model tier per wave type to keep implementation velocity high while
reserving deep reasoning for security-critical phases.

| Wave Type | Model Tier | Rationale |
|-----------|-----------|-----------|
| **Contract design** (R1-style) | Opus / max effort | Architectural decisions with irreversible downstream effects. Requires exhaustive caller inventory and adversarial edge-case search. |
| **Implementation — UX/surface** (R2, R3, R7) | Sonnet / high effort | UI restructuring, capability gating, input unification. Testable via deterministic gate; iteration is cheap. |
| **Implementation — provider normalizer** (R4, R5) | Opus / max effort | Stream parsing, event store semantics, redaction, secret rejection. Provider-specific protocols have many edge cases; a single missed frame boundary corrupts the Transcript. |
| **Implementation — lifecycle/auth** (R6) | Opus / max effort | Mutation authorization, epoch binding, Stop/Kill/Delete closeout. Every path must fail closed. Security regression checklist (§4) is gating. |
| **Review/V1 verification** (all waves) | Opus / medium effort | Adversarial review: try to make it fail. Independent perspective from implementation model. |
| **V2 closeout** (all waves) | Sonnet / high effort | Evidence collection, gate runs, documentation. Deterministic; no novel reasoning required. |
| **Physical gate** (R8B) | Human + scripted | SM-S926N matrix is scripted physical steps. Model assists with log analysis only. |
| **Test fixes** (R7 Jest) | Sonnet / medium effort | Mock isolation, module reset. Mechanical; well-understood failure modes. |

**Rule:** No wave that touches `ManagedCodexService`, `ManagedClaudeService`,
`LifecycleService`, `MutationAuthorizer`, `AuthorizeAndCommit`, or the managed
event store may be implemented below Opus effort. The security regression
checklist (§4) gates every such wave.

## 6. Execution sequence

Every implementation wave requires a frozen contract, implementation
pre-gate, fresh V1 review, deterministic evidence, and fresh V2 closeout where
it changes milestone authority. Implementation and evidence commits remain
separate. No wave may embed its own final commit SHA in content that determines
that SHA.

### 9.5-TM-R0 — Evidence and authority reconciliation

**Status:** COMPLETED (`8aaa0213e`)

**Purpose**

- Replace the unsupported `DOGFOOD READY` claim with a blocked remediation
  state.
- Record the exact tested artifact source and preserve its valid security
  lifecycle evidence.
- Classify every reported bug as fixed, partially wired, stale diagnosis,
  unverified, or open.
- Record the full-suite Jest order-dependence.

**ACCEPT**

- No final-source claim points to an earlier daemon/APK.
- Claude I/O is not described as implemented.
- Claude lifecycle is not described as absent.
- Historical evidence remains immutable and correctly scoped.

**Deliverable:** `docs/BASE_ALPHA_FINAL_REPORT.md` (reconciled)

### 9.5-TM-R1 — Runtime mode and provider-bound contract freeze

**Status:** COMPLETED (`c6e569f51`)

**Purpose**

- Inventory every live caller for session creation, status, events, prompt,
  lifecycle, Transcript, Terminal, and local IPC.
- Freeze explicit `terminal` and `managed` runtime-mode behavior without
  silently changing existing CLI semantics.
- Define the minimal shared contract and separate Codex/Claude owners.
- Define capability states including healthy, degraded, unavailable,
  unauthorized, and read-only/provider-unsupported.

**Mandatory decisions**

- Exact CLI and mobile mode selection.
- Default-selection migration and rollback.
- Whether the local workstation managed view is `watch`, `--follow`, or both.
- One-writer policy for multi-client Terminal input.

**REJECT**

- A lowest-common-denominator prompt/resume API.
- Adapter choice based on labels or inferred agent kind.
- Any managed-to-PTY or PTY-to-managed hidden fallback.

**Deliverable:** `docs/R1_CONTRACT.md`

### 9.5-TM-R2 — Terminal-first interactive product path

**Purpose**

- Make Terminal the initial surface for `controlled_pty:*`.
- Preserve exact-generation TerminalTransport, Input-B ACK, geometry, reconnect,
  and `Recorder` ownership.
- Add a local attach/view path through the existing transport so workstation
  and mobile can observe the same output.
- Make input ownership/read-only state explicit.

**Tests**

- one PTY read with multiple subscribers;
- identical ordered output to local and mobile viewers;
- exact-generation reconnect and stale-client rejection;
- single-writer enforcement and writer disconnect;
- Ctrl+C, paste, macro, direct keyboard, and command-bar paths;
- ACK loss remains `delivery_unknown`, with no replay;
- slow/disconnected observer cannot block the PTY or other observer.

**Stop condition**

Any change to PTY bytes, terminal rendering, lifecycle authority, or geometry
made solely to improve Transcript output.

### 9.5-TM-R3 — Transcript availability and truthful mobile UX

**Purpose**

- Introduce closed Transcript availability/degradation states.
- Keep Terminal and Transcript as distinct views.
- Make managed sessions open Transcript and interactive sessions open Terminal.
- Preserve an always-reachable Terminal for interactive sessions.

**Required outcomes**

- `healthy`;
- `healthy_empty`;
- `provider_projection_unavailable`;
- `temporarily_unavailable`;
- `gap_or_degraded`;
- `byte_stream_suppressed_after_input`;
- `unauthorized`;
- `session_or_generation_stale`.

No state may invent an event or present unavailable managed output as a healthy
empty conversation.

### 9.5-TM-R4 — Codex managed Transcript projection

**Purpose**

- Keep Codex app-server parsing and prompt delivery Codex-owned.
- Bind every accepted event to exact session/runtime/generation and native
  source identity.
- Project bounded Codex assistant, tool, lifecycle, and result facts into the
  existing Transcript contract.
- Add local `watch`/`--follow` consumption of the same cursor.

**Provider ownership (Codex):**
- **Event decoding:** `ManagedCodexService` pump loop parses JSON-RPC
  notifications. No common-layer JSON-RPC parsing.
- **Prompt:** `ManagedCodexService.SubmitPrompt` enforces one-active-turn.
  No common SubmitPrompt.
- **Resume:** Codex-owned app-server session resume.
- **Completion:** `turn/completed`, `item/completed` → `LifecycleExited`.
  Provider-owned exit classification.

**Tests**

- assistant streaming and completed turn;
- tool request/result relationships;
- duplicate, gap, collision, unknown version, malformed and truncated input;
- reconnect, cursor replay, generation replacement and stale event rejection;
- one-active-turn prompt enforcement;
- mobile and local viewer equivalence;
- no Transcript failure blocks Codex runtime or prompt authority.

### 9.5-TM-R5 — Claude managed Transcript projection

**Purpose**

- Implement a Claude-specific strict stream-json normalizer.
- Preserve Claude one-shot/resume/hook/approval semantics.
- Project only reviewed Claude content and outcomes into the common Transcript
  segment boundary.
- Keep prompt UI disabled unless 9.5-TM-R6 proves a safe write capability.

**Provider ownership (Claude):**
- **Event decoding:** `ManagedClaudeService` stream-json normalizer.
  Version-pinned, fail-closed on unknown frame types. No common-layer
  stream-json parsing.
- **Prompt:** NOT IMPLEMENTED. Claude is observation-only at this wave.
- **Resume:** `claudeResumeCoordinator`, `ResumeForApproval`, `tool_deferred`.
  Provider-owned.
- **Approval:** `tool_deferred` detection, approval join/denial witnesses.
  Provider-owned delivery witnesses.
- **Completion:** `message_stop`, `tool_deferred` graceful exit →
  `SimulateGracefulExit`. Provider-owned.

**Tests**

- pinned-version fixtures and exact current production fixture;
- assistant content-block ordering;
- partial message, final result, tool-deferred approval, denial, malformed,
  truncated, duplicate, gap and unknown-version behavior;
- runtime/session/generation/source-position binding;
- restart/resume incarnation separation;
- secret/redaction and bounded-content tests;
- proof that deleted JSONL discovery/external observer readers were not revived.

### 9.5-TM-R6 — Provider-specific managed input and lifecycle closeout

**Purpose**

- Retain the current Codex prompt contract.
- Decide and implement only a provider-supported Claude write contract.
- Verify shared lifecycle dispatch against both owners.

**Provider ownership:**
- **Prompt:** Codex retains `ManagedCodexService.SubmitPrompt`. Claude:
  observation-only is acceptable if safe input cannot be proven; otherwise
  Claude-specific write contract (not a generic SubmitPrompt).
- **Lifecycle:** `LifecycleService.ownerFor` dispatches by adapter prefix.
  `ProviderLifecycleOwner` interface is shared; implementations are separate.
  Stop/Kill/Delete remain exact-generation, authorized, and provider-owned.
- **Completion:** Delete requires terminal state and clears only the exact
  session's projections (Transcript + status + registry + approvals +
  coordinator).

**Rules**

- No generic `SubmitPrompt` claim unless both owners meet the same observable
  semantics.
- Claude observation-only is an acceptable closed capability state if safe
  input cannot be proven in this wave.
- Stop/Kill/Delete remain exact-generation, authorized, and provider-owned.
- Delete requires terminal state and clears only the exact session's
  projections.

**Tests**

- Codex and Claude create/status/stop/kill/delete;
- running delete rejection with zero mutation;
- stale generation and wrong provider rejection;
- idempotent terminal outcomes;
- registry, approval, coordinator, Transcript and status cleanup;
- physical Claude create → stop → delete → list disappearance.

### 9.5-TM-R7 — Input UX and deterministic test gate

**Purpose**

- Give xterm keyboard, command bar, macros, and paste one shared mobile input
  controller and one operation-state model.
- Remove or clearly relabel duplicate input UI.
- Fix Jest module/mock isolation so repeated full-suite runs are deterministic.

**Tests**

- text plus Enter remains one UI operation with two exact ACKs;
- overlapping command attempts fail locally without stealing ACK state;
- partial delivery preserves original text and reports possible partial write;
- owner/member and capability fail-closed behavior;
- randomized and repeated full Jest suite order;
- iOS and Android pairing-store tests do not leak mock state.

### 9.5-TM-R8A — Matched candidate freeze and artifact build

**Purpose**

- Freeze one new production candidate after 9.5-TM-R0 through 9.5-TM-R7 ACCEPT.
- Build daemon and APK from that exact source.
- Prove embedded identities (`vcs.revision`, `vcs.modified=false`).
- Record daemon and APK SHA256 hashes in the artifact manifest.
- Publish `HANDOFF.env` with expected hashes, device target, and test profile.

**ACCEPT**

- One exact production source SHA for daemon, APK, and evidence chain.
- `vcs.modified=false` for both daemon and APK builds.
- Daemon `vcs.revision` matches the frozen source SHA.
- Artifact manifest (`pokit-alpha-device-artifacts/MANIFEST.json`) is
  committed and pushed.
- No source changes between freeze and build.
- Documentation HEAD and evidence HEAD remain distinct identities.

**Deliverable:** `pokit-alpha-device-artifacts/` manifest updated with new
candidate identity.

### 9.5-TM-R8B — Physical SM-S926N gate

**Purpose**

- Run the full physical-device matrix against the 9.5-TM-R8A frozen candidate.
- Every test steps through the security regression checklist (§4).
- Restore `DOGFOOD READY` only after every matrix row passes.

**Required physical matrix**

- clean install, QR pairing, local approval and role/permission display;
- cold restart identity and bearer restoration;
- interactive Codex and Claude Terminal-first sessions;
- simultaneous local/mobile Terminal observation and explicit input owner;
- managed Codex Transcript output and prompt;
- managed Claude Transcript output and truthful input capability;
- Transcript healthy, unavailable, gap/degraded, and suppression states;
- N1 exact-event navigation;
- approval/deny/input/interrupt/stop/kill/delete;
- revoke blocks all old mutation authority;
- installed daemon/APK hashes match the 9.5-TM-R8A frozen manifest.

**ACCEPT**

- one exact production source, daemon, APK, installation, and evidence chain
  (verified against 9.5-TM-R8A manifest);
- deterministic daemon and mobile gates;
- zero wrong-session/wrong-generation action;
- no Transcript or Timeline failure blocks native runtime;
- no raw PTY, secret, or hidden reasoning enters managed Transcript;
- independent V1/V2 acceptance and corrected release notes.

**DOGFOOD READY restored only after 9.5-TM-R8B ACCEPT.**

## 7. Explicitly deferred

- parsing PTY output to reconstruct provider semantics;
- attaching a PTY to the existing headless app-server/stream-json process
  without a provider-supported dual-channel contract;
- automatic writer arbitration or background ownership transfer;
- autonomous coordination, validation, provider switching, or routing;
- dirty-worktree validation;
- Pairing V2;
- Terminal/Transcript visual merging into one feed;
- removal of existing Transcript or Terminal fallback authority.

## 8. Executor handoff

9.5-TM-R0 and 9.5-TM-R1 are complete. The next executor begins at 9.5-TM-R2.

1. Verify branch `feature/canonical-timeline-foundation`, HEAD, upstream, and
   clean worktree.
2. Read `docs/R1_CONTRACT.md` — the frozen R1 contract is authoritative for
   all subsequent implementation.
3. Read `docs/BASE_ALPHA_FINAL_REPORT.md` — the R0-reconciled bug
   classification and evidence record.
4. Every implementation wave must pass the Security Regression Checklist (§4)
   before claiming ACCEPT.
5. Every wave that touches mutation or provider-owned semantics must be
   implemented at Opus effort per the Model Allocation Strategy (§5).
6. Implementation and evidence commits remain separate.
7. No implementation begins before its contract receives independent ACCEPT.
