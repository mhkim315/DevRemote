# Base Alpha Terminal-First and Managed Transcript Remediation Plan

**Status:** AUTHORITATIVE REMEDIATION PLAN — IMPLEMENTATION NOT STARTED

**Planning baseline:** `b476d8af700bd29a9b47422f0954088d8bbaed59`

**Gate status:** Base Alpha `DOGFOOD READY` is not accepted. The previously
recorded pairing, trust, restart, revoke, and recovery evidence remains useful
for the exact artifacts that produced it, but a new matched candidate and the
remediation gates below are required before Base Alpha acceptance.

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
default changes. Mobile session creation should make the two modes visible and
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

## 3. Provider contract boundary

POKIT must not force Codex and Claude into a common I/O contract when their
native protocols do not share semantics.

### 3.1 Shared minimum contract

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

### 3.2 Codex-owned semantics

Codex retains ownership of:

- app-server JSON-RPC initialization and turn protocol;
- one-active-turn prompt submission;
- thread/turn/item identity;
- Codex-native event decoding and completion/error outcomes; and
- Codex resume/cancel behavior.

### 3.3 Claude-owned semantics

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

### 3.4 Shared Transcript projection

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

## 4. Execution sequence

Every implementation wave requires a frozen contract, implementation
pre-gate, fresh V1 review, deterministic evidence, and fresh V2 closeout where
it changes milestone authority. Implementation and evidence commits remain
separate. No wave may embed its own final commit SHA in content that determines
that SHA.

### R0 — Evidence and authority reconciliation

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

### R1 — Runtime mode and provider-bound contract freeze

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

### R2 — Terminal-first interactive product path

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

### R3 — Transcript availability and truthful mobile UX

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

### R4 — Codex managed Transcript projection

**Purpose**

- Keep Codex app-server parsing and prompt delivery Codex-owned.
- Bind every accepted event to exact session/runtime/generation and native
  source identity.
- Project bounded Codex assistant, tool, lifecycle, and result facts into the
  existing Transcript contract.
- Add local `watch`/`--follow` consumption of the same cursor.

**Tests**

- assistant streaming and completed turn;
- tool request/result relationships;
- duplicate, gap, collision, unknown version, malformed and truncated input;
- reconnect, cursor replay, generation replacement and stale event rejection;
- one-active-turn prompt enforcement;
- mobile and local viewer equivalence;
- no Transcript failure blocks Codex runtime or prompt authority.

### R5 — Claude managed Transcript projection

**Purpose**

- Implement a Claude-specific strict stream-json normalizer.
- Preserve Claude one-shot/resume/hook/approval semantics.
- Project only reviewed Claude content and outcomes into the common Transcript
  segment boundary.
- Keep prompt UI disabled unless R6 proves a safe write capability.

**Tests**

- pinned-version fixtures and exact current production fixture;
- assistant content-block ordering;
- partial message, final result, tool-deferred approval, denial, malformed,
  truncated, duplicate, gap and unknown-version behavior;
- runtime/session/generation/source-position binding;
- restart/resume incarnation separation;
- secret/redaction and bounded-content tests;
- proof that deleted JSONL discovery/external observer readers were not revived.

### R6 — Provider-specific managed input and lifecycle closeout

**Purpose**

- Retain the current Codex prompt contract.
- Decide and implement only a provider-supported Claude write contract.
- Verify shared lifecycle dispatch against both owners.

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

### R7 — Input UX and deterministic test gate

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

### R8 — Matched candidate and bounded physical gate

**Purpose**

- Freeze one new production candidate after R0–R7 ACCEPT.
- Build daemon and APK from that exact source.
- Prove embedded and installed identities.
- Run automated and SM-S926N gates before restoring `DOGFOOD READY`.

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
- installed daemon/APK hashes match the frozen manifest.

**ACCEPT**

- one exact production source, daemon, APK, installation, and evidence chain;
- deterministic daemon and mobile gates;
- zero wrong-session/wrong-generation action;
- no Transcript or Timeline failure blocks native runtime;
- no raw PTY, secret, or hidden reasoning enters managed Transcript;
- independent V1/V2 acceptance and corrected release notes.

## 5. Explicitly deferred

- parsing PTY output to reconstruct provider semantics;
- attaching a PTY to the existing headless app-server/stream-json process
  without a provider-supported dual-channel contract;
- automatic writer arbitration or background ownership transfer;
- autonomous coordination, validation, provider switching, or routing;
- dirty-worktree validation;
- Pairing V2;
- Terminal/Transcript visual merging into one feed;
- removal of existing Transcript or Terminal fallback authority.

## 6. Executor handoff for R0 only

The next executor may modify documentation only.

1. Verify branch, HEAD/upstream, clean worktree, and artifact manifest contents.
2. Reconcile `BASE_ALPHA_FINAL_REPORT.md`, the Step 9.5 status, and the bug
   ledger against repository callers.
3. Do not change historical test artifacts or claim a new candidate.
4. Run documentation checks, `git diff --check`, changed-document secret scan,
   ancestry, upstream, and clean-worktree checks.
5. Submit R0 for independent acceptance.

R1 or production implementation must not begin until R0 receives independent
ACCEPT.
