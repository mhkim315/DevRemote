# POKIT Native Session Coordination Roadmap

**Status:** AUTHORITATIVE PRODUCT AND POST-PB ARCHITECTURE DIRECTION

**Planning base:** `317bb0cb76a73bd49bebaaede562bfa77cd1e7bc`

**Current execution state:** CT-P1 ACCEPTED (`317bb0cb76a73bd49bebaaede562bfa77cd1e7bc`).
PB ACCEPTED (`5354077afcf30343d9666511e259346d9bea0ad6`). CT-P1 operational-evidence
amendment ACCEPTED (`12135bd8072ae284fbe95c71cddf5f331a5d26f5`). Steps 4-8 operational
foundation COMPLETE. CT-P2 and production Timeline wiring remain BLOCKED
(separate post-PB packet required).

The current product state is **Post-PB foundation complete, alpha activation
pending**. Step 9.0 is pending independent closeout. The bounded
execution plan is authoritative in
[`ALPHA_ACTIVATION_ROADMAP.md`](ALPHA_ACTIVATION_ROADMAP.md); neither the Step
9.0 acceptance nor this planning update authorizes Step 9.1 implementation.

This document owns the current product definition, authority boundaries, MVP,
and post-PB execution order. [`POST_PA3_AUTHORITATIVE_ROADMAP.md`](POST_PA3_AUTHORITATIVE_ROADMAP.md)
retains the accepted PA/PB identity ledger. The bounded CT foundation contract
remains in [`CANONICAL_TIMELINE_CT_PRE_EXECUTION_PLAN.md`](CANONICAL_TIMELINE_CT_PRE_EXECUTION_PLAN.md),
subject to the transition rule in section 10 below. Older adapter, provider
expansion, Navigator, Guard, and broad orchestration plans are historical
rationale only.

## 1. Product definition

> **POKIT is a local-first session and workspace control plane for safely
> operating provider-native coding agents from mobile, with exact runtime
> identity, strong inter-session coordination, reproducible repository state,
> evidence provenance, approval, interruption, and recovery.**

User-facing value:

> **Keep local coding-agent work moving safely while away from the
> workstation.**

Technical guarantee:

> **Exact runtime identity, reproducible repository state, attributable
> evidence, controlled approvals, strong session communication, and safe
> recovery.**

POKIT is not primarily a smarter multi-agent harness, a Claude/Codex chat
bridge, a generic provider router, a mobile IDE, or a universal agent
framework.

The integration principle is:

> **Provider-independent control authority, provider-specific native
> integration.**

Claude Code, Codex, and future native agents retain reasoning, planning, tool
use, context management, provider-native subagents, provider-specific execution
behavior, and provider-internal model routing/orchestration. POKIT must not
collect reasoning or chain-of-thought and must not replace the provider's
execution loop.

## 2. Authority matrix

| Domain | Authority | Explicitly not authoritative |
| --- | --- | --- |
| Native execution state | Provider-native runtime services | Timeline, conversation, validator |
| Process/session lifecycle | Provider service and `OwnedPTYRuntime` | Coordination broker, Timeline |
| Exact terminal transport | Generation-captured `TerminalTransport`; `Recorder` remains sole PTY reader | Transcript, message delivery, validator |
| Device identity and permission | Device trust and authorization services | QR role hints, mobile cache, conversation |
| Approval | Approval store and provider-native delivery authority | Timeline approval projection, validator |
| Input delivery | Exact-generation acknowledged input path | A displayed conversation message |
| Workspace state | Workspace controller and cooperative repository write lease | Git worktree naming, Timeline |
| Coordination delivery | Coordination broker/store | Timeline replay, unified conversation |
| Operational history | Append-only Canonical Timeline | Message queue, lifecycle controller |
| Validation | A claim bound to one exact snapshot | Merge, acceptance, or lifecycle authority |
| User experience | Mobile cockpit and unified conversation projections | Every underlying authority above |

Timeline replay must never resend instructions, resolve approvals, revive
sessions, reacquire a write lease, or act as a message queue. A validator result
must never automatically authorize merge, acceptance, approval, or lifecycle
transition. Unified conversation must expose rather than hide provider,
runtime, session, generation, model, snapshot, and evidence provenance.

## 3. Product classification

| Decision | Capabilities |
| --- | --- |
| **KEEP** | Managed native runtime; `OwnedPTYRuntime`; `TerminalTransport`; device trust; approval authority; exact session/runtime/generation identity; crash/reconnect recovery; mobile notification and intervention |
| **REDESIGN** | Canonical Timeline as operational evidence; Activity/Transcript UI as projections; provider integration as narrow native capabilities; provider registry/adapters around proven managed producers; coordinator as deterministic routing/state component; executor/validator as explicit session purposes rather than fixed roles; failure detection from accepted operational facts; unified conversation as non-authoritative UX |
| **DEFER** | Automatic provider selection; automatic validator dispatch; external parallel agents; forked recovery worktrees; Grok; ACP; semantic stagnation detection; enterprise governance; container/VM isolation; automatic provider switching and revision loops |
| **DELETE FROM AUTHORITATIVE ROADMAP** | Custom model provider; generic coding-agent runtime; fixed Planner -> Executor -> Validator product pipeline; autonomous Navigator phase; generic always-on Guard/scorer; provider-native subagent control; universal provider reasoning abstraction; automatic raw-transcript handoff |

Deletion from the authoritative roadmap does not authorize immediate source
deletion. Fixture, compatibility, or migration code is removed only after exact
production and test consumer inventories prove it safe. Accepted lifecycle,
transport, authorization, approval, pairing, generation, and artifact evidence
contracts remain frozen.

## 4. Managed session configuration and provenance

Managed session creation must support explicit configuration of:

- provider;
- exact provider-native model ID;
- provider-native reasoning or effort setting;
- user-facing preset such as Fast, Balanced, or Deep;
- role or task purpose;
- cost, token, or time budget;
- permission profile;
- workspace mode;
- context-transfer policy.

Persist both the requested preset and the effective provider-native
configuration. Unsupported model or reasoning settings fail closed with a
reported incompatibility and require an explicit alternative; there is no
silent downgrade. Provider, model, reasoning/effort, budget, permissions, and
workspace policy are runtime provenance. Material changes require a new
configuration epoch or, when execution identity changes, a new runtime
generation. Automatic model/provider selection remains deferred. User-defined
defaults and explicit role presets are allowed.

## 5. Weak orchestration, strong coordination

POKIT core coordination supports explicit and attributable communication
between independent managed sessions:

- instruction and question;
- status and result;
- validation request and finding;
- revision request;
- explicit handoff and cancellation;
- failure-recovery handoff.

The core substrate owns structured identity, exact source and target generation
binding, authorization, bounded content or immutable evidence reference, reply
and causation linkage, expiry/cancellation, delivery state, and audit evidence.
It does not decide when to spawn a validator, select or switch providers,
repeat revisions, synthesize results, or adopt a result.

Delivery state is closed and at least includes:

- `queued`;
- `delivery_accepted`;
- `delivery_failed`;
- `delivery_unknown`;
- `runtime_acknowledged`, only when a reviewed native protocol proves it;
- `response_received`;
- `expired`;
- `stale_target`.

`delivery_accepted` means only that the defined exact-generation delivery
boundary accepted the message. It never means the model read, understood,
followed, or correctly completed the instruction. ACK loss is not success and
does not authorize automatic replay.

### 5.1 Minimum coordination envelope

- message version and ID;
- task ID and closed message type;
- source provider/runtime/session/generation;
- target provider/runtime/session/generation;
- repository ID and workspace mode;
- snapshot ID;
- base and current SHA;
- tree hash and diff digest;
- handoff/evidence reference;
- content digest and bounded redacted summary;
- redaction-policy version;
- reply-to and causation message IDs;
- creation and expiration time;
- required target capability;
- delivery state.

Large diffs, logs, tool output, and artifacts are immutable evidence objects
referenced by digest. Automatically transferred content must exclude executor
chain-of-thought, entire conversation transcripts, bearer tokens, pairing
secrets, unbounded environment dumps, full approval action payloads, and
unlimited tool output.

### 5.2 Minimum bounded handoff

- objective, acceptance criteria, and constraints;
- repository and workspace identity;
- base/current SHA and snapshot/tree/diff identity;
- changed files;
- test commands and results;
- unresolved findings;
- approval state without execution secrets;
- source provider/runtime/session/generation;
- evidence provenance;
- artifact hash, type, and size;
- redaction policy and expiration.

A structured handoff reduces ambiguity and makes distortion detectable. It does
not prove that the receiving model understood or followed it.

## 6. Workspace and validation model

Repository mode and execution-isolation profile are separate declarations.
POKIT must disclose the enforced level and must not claim isolation that it does
not implement.

### 6.1 Shared sequential workspace

- one POKIT-managed writer at a time;
- a cooperative repository write lease, not an exclusive filesystem lock;
- a snapshot manifest before validation;
- drift detection during validation;
- findings become stale when repository state changes.

The lease does not block external editors, shells, Git clients, formatters,
background processes, or human writers. It serializes cooperating POKIT-managed
writers and detects unexpected drift.

The cooperative lease records owner runtime/session/generation, repository ID,
expected snapshot ID, lease epoch, expiry/heartbeat, compare-and-release,
stale-owner rejection, external drift invalidation, and daemon-crash recovery.

### 6.2 Frozen validation snapshot

This is the preferred independent-validation mode:

- clean conversational context; executor reasoning is not transferred;
- immutable source snapshot;
- separate writable temporary build/cache area;
- objective, acceptance criteria, contracts, snapshot/diff identity, tests, and
  evidence are explicitly supplied;
- findings bind to that exact snapshot;
- isolation strength is disclosed honestly.

Initial support is limited to a clean committed snapshot. Dirty-worktree
validation is deferred until synthetic commits or content-addressed snapshots
can represent the index and untracked files without ambiguity.

### 6.3 Forked writable worktree

This is post-MVP and reserved for measured recovery or alternative-
implementation demand. It requires separate branch ownership, base snapshot,
artifact namespace, process/port policy, merge authority, conflict/cleanup
policy, and environment/credential disclosure.

### 6.4 Isolation profiles

- `repo-only`;
- `repo+process`;
- `repo+network-policy`;
- `containerized`;
- `VM-isolated`.

Without corresponding enforcement, POKIT must not promise complete filesystem,
network, credential, database, process, secret, or side-channel isolation. A
Git worktree alone is repository layout, not a security sandbox.

## 7. Validation identity and staleness

Where applicable, a validation result binds:

- repository ID;
- base and target commit SHA;
- tree hash;
- index hash;
- untracked-manifest digest;
- diff digest;
- snapshot ID and cooperative write-lease epoch;
- validator provider/model;
- validator runtime/session/generation;
- validator configuration epoch;
- relevant evidence and artifact digests.

A finding becomes stale when repository, snapshot, lease, evidence, validator
generation/configuration, or relevant artifact identity changes. Stale findings
remain visible as history but cannot authorize acceptance, merge, or lifecycle
transition.

## 8. Canonical Timeline direction

Canonical Timeline is append-only operational evidence, not a universal
provider reasoning model. It retains envelope identity, versioning, validation,
semantic identity distinct from append order, redaction, collision/gap
representation, evidence references, projection boundaries, and fail-open
operation.

It does not store raw PTY bytes, infer semantics from terminal text, collect
reasoning, alter provider cursor consumption, authorize input/approval, control
lifecycle, or become a daemon startup requirement. Transcript, Activity,
status, mobile cockpit, and unified conversation are projections.

The final CT-P1 boundary must not assume every operational event is a provider
`AgentEvent`. It must allow reviewed typed source references such as:

- `ProviderEvidenceRef`;
- `RuntimeEvidenceRef`;
- `ApprovalEvidenceRef`;
- `InputEvidenceRef`;
- `WorkspaceEvidenceRef`;
- `CoordinationEvidenceRef`.

CT-P1 establishes an extensible typed source boundary; it must not speculate
about future workspace or coordination state machines before their authoritative
producers exist. The coordination broker/store, not Timeline, owns delivery.

## 9. Base Alpha boundary and product surfaces

The product boundary is:

> **Managed native-session control first, structured operational views second,
> Terminal as fallback, manual orchestration optional.**

The first Base Alpha includes:

- native Codex and Claude sessions through `pokit run`;
- explicit provider/model/reasoning configuration;
- exact runtime/session/generation/configuration provenance;
- mobile status, notifications, approval/deny, input, stop, and interrupt;
- minimal fail-open Operational Canonical Timeline activation;
- distinct Activity and Transcript projections converging on shared canonical
  identity;
- N1 exact-event notification-to-action;
- secure accountless onboarding;
- existing Transcript authority, reconnect behavior, and stale-generation
  rejection;
- permanently accessible Terminal fallback; and
- reproducible daemon/APK identity and real-device safety verification.

Manual coordination, manual validation, workspace lease activation, frozen
validation activation, and Cockpit are not Base Alpha prerequisites. They may
be activated only in a separately accepted later Manual Alpha wave. Automatic
provider/model routing, validator dispatch, revision/switching, external
parallel execution, Navigator/Guard/scorer behavior, native subagent control,
and reasoning capture remain unavailable throughout alpha.

Activity is the condensed operational projection and intervention surface.
Transcript is the detailed chronological conversation and structured execution
projection. They remain separate views while converging on event identity,
runtime/session/generation, provenance, ordering/cursor, request/result and
approval relationships, reconnect continuity, and gap/degraded representation.
The default per-session view is a dogfood decision. Terminal remains the raw
PTY detail, diagnosis, recovery, and emergency surface. Cockpit is optional
cross-session/workspace aggregation; it is neither the primary entry point nor
a replacement for Activity, Transcript, N1, or Terminal.

N1 is alpha-critical user-visible routing, not autonomous Navigator behavior.
Its notification locator binds canonical event, session, runtime, exact
generation, and event kind. Mobile re-queries current server authority and
permissions on tap and handles actionable, resolved, stale-generation,
unavailable-session, insufficient-permission, unavailable-event, and
degraded/gap outcomes without replaying an invalid action.

## 10. Authoritative implementation order

1. **COMPLETE:** CT-P1 contract, all-payload privacy boundary, T0 field bounds.
   ACCEPT at `317bb0cb76a73bd49bebaaede562bfa77cd1e7bc`.
2. **COMPLETE:** PB physical-device evidence. ACCEPT at
   `5354077afcf30343d9666511e259346d9bea0ad6` for candidate
   `059bef181c6c2ef312eee421dbf10f12b15b0326`.
3. **COMPLETE:** CT-P1 operational-evidence amendment accepted at
   `12135bd8072ae284fbe95c71cddf5f331a5d26f5`.
4. **COMPLETE:** Fail-open Timeline shadow writer (IMPL `6d1a72d35`,
   EVID `58eb55b92`). Ring-buffer mailbox model, zero goroutines.
5. **COMPLETE:** Workspace identity, clean-snapshot contract, cooperative
   lease. Standalone; consumer NONE. (IMPL `404a3e882`, EVID `ad83bae10`).
6. **COMPLETE:** Coordination envelope + broker with capability-bound auth
   (IMPL `3b1a2c7ed`, EVID `b7eeba499`, final comment fix `6c13aafe7`).
7. **COMPLETE:** Frozen clean-snapshot staleness check (contract-only,
   `--enable-frozen-validation`). Consumer NONE. ValidationStore belongs to
   Step 8 (embedded under `--enable-cockpit`). (IMPL `a753e126c`, EVID `814868b5b`).
8. **COMPLETE:** Mobile cockpit projection — ring-buffer polling, zero
   goroutines (EVID `093e03f04`).
9. **PENDING INDEPENDENT CLOSEOUT (9.0):** Authority reconciliation,
   foundation/capability/live-state audit, and ledger correction. Resolve the
   candidate with
   `git log -1 --format=%H -- docs/STEP9_0_LEDGER.md`; do not embed a
   self-referential candidate SHA in the ledger.
10. **PLANNED (9.1):** Minimal Operational Timeline staging — bounded Activity/N1
    producer composition, failure isolation, mailbox/backpressure,
    drop/gap/degraded evidence, restart/filesystem failure, and capability
    state. No consumer-wide cutover.
11. **PLANNED (9.2):** Canonical projection convergence — preserve Transcript
    authority, compare Timeline-derived Transcript/Activity projections, and
    switch at most one bounded consumer/endpoint per accepted packet with
    rollback and fallback.
12. **PLANNED (9.3):** N1 exact-event notification-to-action — exact identity,
    authority re-query, closed outcomes, contextual actions, and safe fallback.
13. **PLANNED (9.4):** Secure accountless onboarding — setup, provider/daemon
    readiness, pairing/restoration, and first managed-session visibility.
14. **PLANNED (9.5):** Base Alpha candidate — matched daemon/APK artifacts,
    automated hard gates, SM-S926N onboarding-to-N1 matrix, known issues,
    release notes, independent acceptance, and dogfood start.
15. **LATER MANUAL ALPHA:** Explicit user-triggered coordination and frozen
    validation, workspace snapshot/lease only when required, optional Cockpit
    expansion, and explicit handoff.
16. **FUTURE:** Assisted policy, forked recovery worktrees, and any automation
    only after measured demand and separate authorization.

Steps 1–8 are accepted foundations, not proof of product-live composition.
Step 9.0 is pending independent closeout and Step 9.1 remains blocked. After
that closeout, Steps 9.1–9.5 are governed by
[`ALPHA_ACTIVATION_ROADMAP.md`](ALPHA_ACTIVATION_ROADMAP.md). Do not
automatically resume the historical full CT-P2 plan: only separately accepted,
bounded Timeline producer composition, projection comparison, controlled
consumer cutover, degradation reporting, fallback, and rollback are eligible.

## 11. Stop and expansion gates

Stop if any packet weakens accepted lifecycle, generation, transport, device
trust, approval, input, pairing, recovery, or artifact-provenance contracts;
uses Timeline as authority; sends secrets or unbounded content; overstates
isolation; treats delivery as comprehension; validates an unidentified or stale
snapshot; hides a Timeline gap; or begins Timeline production composition or a
consumer cutover outside an independently accepted Step 9 packet.

Base Alpha has no dependency on coordination, validation, lease, or Cockpit.
There is no global orchestration flag: each capability reports healthy,
degraded, unavailable, disabled, or unauthorized state where relevant and may
not activate another authority implicitly. Timeline is intended to become
default-on only after staging evidence; it always remains fail-open,
non-authoritative, outside authority locks, and unnecessary for daemon/session
startup. Existing Transcript and Terminal remain safe fallbacks.

Do not add automatic routing, validators, provider switching, parallel agents,
Grok/ACP, semantic stagnation detection, forked recovery, or enterprise policy
until Base Alpha has measured repeated use and the specific feature has outcome
data, bounded failure behavior, authorization, provenance, rollback, and
independent acceptance evidence. Automatic orchestration is not a mandatory
final architecture.
