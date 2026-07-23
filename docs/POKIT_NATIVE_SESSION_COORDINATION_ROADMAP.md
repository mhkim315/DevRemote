# POKIT Native Session Coordination Roadmap

**Status:** AUTHORITATIVE PRODUCT AND POST-PB ARCHITECTURE DIRECTION

**Planning base:** `317bb0cb76a73bd49bebaaede562bfa77cd1e7bc`

**Current execution state:** CT-P1 ACCEPTED (`317bb0cb76a73bd49bebaaede562bfa77cd1e7bc`).
PB ACCEPTED (`5354077afcf30343d9666511e259346d9bea0ad6`). CT-P1 operational-evidence
amendment ACCEPTED (`12135bd806e069d1487f7b0dad55bbe2bdb804f3`). Steps 4-8 operational
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
| **REDESIGN** | Canonical Timeline as operational evidence; Activity/Transcript UI as projections; provider integration as narrow native capabilities; provider registry/adapters around proven managed producers; Coordinator as an adaptive phase-local reasoning role subordinate to deterministic execution authority; Executor/Verifier as explicit, independently scoped session purposes rather than a fixed product pipeline; failure detection from accepted operational facts; unified conversation as non-authoritative UX |
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
   `12135bd806e069d1487f7b0dad55bbe2bdb804f3`.
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

## 12. Post-Alpha orchestration direction

**Status:** AUTHORITATIVE PRODUCT AND SAFETY PRINCIPLES; STAGED FUTURE WORK;
NON-EXECUTABLE DURING ALPHA.

**Architectural verdict:** `ACCEPT WITH REQUIRED CHANGES`. The corrections in
this section are part of the accepted direction. Broader automation remains a
hypothesis until the staged experiments below produce evidence.

This section is the single authority for post-Alpha orchestration direction.
It does not authorize O0, change Steps 9.1-9.5, alter Base Alpha acceptance,
or claim that a Control Plane exists today. Current workflow observations are
dogfood evidence, not proof that every proposed role or mechanism belongs in
the product.

The target boundary is:

> **POKIT does not control how capable agents must think. POKIT controls which
> exact local resources they may affect, under which identity and capability,
> and what independently verified evidence is required before project
> authority advances.**

### 12.1 Final judgment and product boundary

POKIT should adopt a three-plane separation:

1. **Intelligence plane:** models reason, plan, revise, challenge, implement,
   verify, and propose changes.
2. **Control plane:** deterministic state and capability checks decide which
   effects and authority transitions are valid.
3. **Resource plane:** repositories, worktrees, provider processes, terminals,
   tests, networks, secrets, and artifacts on which granted capabilities act.

This separation is useful only when control is enforced at actual tool and
resource boundaries. A prompt, role label, worker declaration, or Coordinator
instruction is not a capability grant.

The architecture is not a generic multi-agent hierarchy. Provider-native
planning, reasoning, model routing, and subagents remain provider-owned.
POKIT's durable differentiation is exact local execution identity, bounded
resource authority, reproducible repository state, independent verification,
evidence lineage, safe interruption/recovery, context rotation, and mobile
supervision. Native providers are rapidly adding their own subagents,
parallelism, worktrees, and model selection; POKIT must not duplicate them.

The proposal is accepted only with these corrections:

- start with the smallest deterministic core justified by observed failures;
- preserve same-scope Coordinator discretion instead of serializing every
  operational decision through an authority transition;
- treat the full envelope, policy registry, detailed budgets, automatic
  routing, and Navigator as staged hypotheses;
- make worker completion claims explicitly untrusted until repository and
  artifact pre-gates verify them;
- separate frozen contract authority, accepted implementation, evidence
  documentation, and roadmap/milestone authority;
- preserve original verifier findings and artifact references across context
  rotation rather than replacing them with summaries; and
- prohibit self-referential commit-identity requirements.

### 12.2 Minimal future topology

```text
User
  ↕ strategic decisions and explicit authority expansion
Thin Director (intermittent strategic role)
  ↕ versioned proposals and decisions
Minimal deterministic Control Plane
  ↕ one phase-local authority envelope
Adaptive phase-local Coordinator
  ├─ one active native Executor (+ provider-native subagents)
  ├─ fresh independent V1 Verifier
  └─ bounded Steward evidence mode, then fresh V2 audit when required
Resource Plane: repository/worktree/runtime/tests/artifacts/network/secrets
```

Roles persist conceptually; model sessions are disposable. Director,
Coordinator, Executor, Verifier, and Steward are permission profiles and
responsibilities, not a permanent agent taxonomy. A secondary Structural
Worker profile is supported by current workflow evidence. Navigator,
dedicated Security Verifier classes, and a permanent Plan Steward remain
experimental.

The Director is thin and intermittent. It handles product direction,
milestone entry/exit, scope or policy change, authority expansion, user
choices, and final explanation. It must not consume every worker transcript
or become a shadow execution database. It cannot override repository facts,
verdicts, security boundaries, or user authority.

The Coordinator is intelligent and phase-local, not a passive router. Inside
an existing authority scope it may interpret failure patterns, decompose and
re-decompose work, classify local/structural/documentary/evidentiary failures,
propose architectural redesign, switch among pre-approved model profiles,
stop a silent or stalled worker, require a terminal status, correct trivial
non-authoritative documentation defects, and choose among permitted recovery
strategies.

The Executor receives broad technical autonomy inside its enforced envelope.
It may revise its plan, explore, debug, add tests, change abstractions, discard
an approach, and use provider-native subagents. It cannot expand its
capabilities, write outside scope, change the acceptance contract, or accept
its own result.

The Verifier receives a frozen snapshot, fresh context, separate dispatch
identity, and no production-write capability. It judges observable contract,
invariant, scope, failure-path, wiring, and evidence claims. An unfamiliar
design or style preference is not a REJECT ground. Risk determines verifier
strength; novel, security, permission, concurrency, protocol, persistence,
and recovery work requires a strong independent verifier.

### 12.3 Model judgment, deterministic policy, and user authority

| Owner | Decisions |
| --- | --- |
| **Model judgment** | Technical exploration; implementation strategy; same-scope task split; root-cause and failure classification; materially different attempt proposal; approved model-profile selection; architectural or policy proposal; test and verification interpretation |
| **Deterministic policy/state** | Actual Git and artifact identity; worktree and lease validity; dispatch idempotency; capability and path checks; attempt records; required checks; gate identity; verdict chain; immutable evidence references; monotonic/CAS state transitions; expiry/revocation; mismatch detection |
| **Director or user authority** | Product/roadmap change; writable or security-domain expansion; acceptance-contract/public-API change; unapproved provider/cost profile; production flag change; irreversible external action; policy weakening; final release or baseline acceptance |

Same-scope operational flexibility does not require a new authority revision
when it stays within the current snapshot, writable domain, acceptance
contract, security/permission profile, provider/cost profile, and reversible
action set. A new authority transition is mandatory when any of those
boundaries changes.

The Coordinator may propose an out-of-policy option but cannot execute it or
redefine validity. The Control Plane must not prescribe file-edit order,
technical decomposition, hypotheses, or implementation procedure.

### 12.4 Smallest evidence-backed deterministic core

The first post-Alpha experiment must implement only controls justified by
observed workflow failures:

1. **Implementation pre-gate**
   - compare actual HEAD and upstream identity;
   - inspect tracked and untracked worktree state;
   - verify prerequisite ancestry;
   - run required formatting and basic checks;
   - compare the worker-reported SHA, file list, diff, file content, and test
     artifacts with actual repository state.
2. **SHA and verdict ledger**
   - bind contract authority, implementation attempts, V1 verdicts, frozen
     implementation, evidence commits, V2 verdicts, ancestry, and the next
     permitted transition.
3. **Attempt and failure records**
   - record normalized blocker category and failure signature;
   - record Executor/provider/model identity;
   - record snapshot and diff identity;
   - record whether new evidence or a new strategy exists;
   - classify blind versus materially different attempts from stored facts.

A worker completion report is not execution authority or evidence. A mismatch
prevents Verifier dispatch, creates a normalized failure record, and counts
against blind-retry policy. Repeated recurrence escalates. The system must call
the claim untrusted or inconsistent; it must not infer intent.

This minimum is deliberately smaller than the complete proposed Control
Plane. Detailed token/tool-call budgets, a complete policy registry, automatic
provider selection, a Director approval pipeline for routine operations, a
broad specialist catalog, and full event sourcing remain unsupported
hypotheses.

Transactional current state plus an append-only authority/verdict audit trail
is sufficient initially. Full event-sourced orchestration is not required
unless recovery, audit, or concurrency evidence later proves otherwise.

### 12.5 Future autonomy envelope

An autonomy envelope is a versioned, immutable, content-addressed authority
object enforced by tool/resource boundaries. Its logical target includes:

- envelope, parent, policy, authority-revision, and expiry identity;
- work-item goal, frozen acceptance-contract reference, and immutable
  constraints;
- principal role/provider/runtime/session/generation/configuration identity;
- repository/base/target/tree/worktree/lease identity;
- granted read/write/temp paths, commands, network/secret profiles,
  native-subagent permission, and concurrency;
- forbidden paths, actions, and external systems;
- bounded time/cost/attempt/tool/artifact budgets where evidence justifies
  each bound;
- required gates, verifier profile, and evidence references; and
- closed exits: `DONE`, `BLOCKED`, `SPLIT_REQUIRED`,
  `SCOPE_CHANGE_PROPOSED`, and `POLICY_EXCEPTION_PROPOSED`.

This is a target schema, not a claim of current support and not the mandatory
scope of the first experiment. Initial enforcement should cover exact
snapshot/worktree/lease, one writer, path and command scope, dispatch identity,
required gates, expiration/revocation, and externally visible side effects.
Network, secret, fine-grained cost, tool-call, and artifact-byte enforcement
must be added only with a real enforcing proxy and a reviewed failure model.

Provider-native subagents remain inside the parent Executor envelope. POKIT
records only parent work-item/runtime/session/generation, whether delegation
occurred, provider child identity when available, provider/model/configuration
provenance, aggregate cost/time, external capabilities and tools used, file
and artifact provenance, and child failure/cancellation effect. When the
provider exposes no child identity, record `opaque_native_delegation`; do not
infer a child graph or collect chain-of-thought.

### 12.6 Retry and escalation

A blind retry keeps materially the same snapshot, hypothesis, affected area,
failure signature, evidence, architecture, and provider strategy. Prompt-only
restatement or an inconsistent completion claim is not a new attempt.

A materially different attempt has stored evidence of at least one of:

- a new root-cause hypothesis;
- a new failing test or reproducer;
- task decomposition or reduced blocker scope;
- a different architecture;
- new technical evidence;
- an approved provider/model change; or
- a substantially different diff identity tied to the failure.

Initial policy:

- no third blind retry;
- immediately reject inconsistent completion claims at pre-gate;
- escalate when the same failure class persists across different Executors;
- continue only with materially new evidence or strategy;
- allow Coordinator discretion to propose structural redesign when repeated
  local fixes do not reduce the blocker;
- allow bounded policy-defined extension for measurable progress; and
- require Director or user approval above total cost/time or authority ceilings.

The attempt store, not a Coordinator assertion, supports classification.
Deterministic limits must not terminate a materially advancing attempt merely
because a raw turn count increased.

### 12.7 Contract, implementation, evidence, and milestone gates

The target flow is:

```text
Frozen contract
→ bounded implementation
→ deterministic implementation pre-gate
→ fresh V1 contract/code verification
→ V1 ACCEPT and implementation freeze
→ deterministic evidence manifest
→ optional path-restricted Evidence mode
→ evidence pre-gate
→ fresh V2 milestone/authority audit
```

Contract documentation is frozen before implementation and is an input to V1.
Evidence documentation is produced after V1 ACCEPT from deterministic facts
and must not rewrite the accepted implementation. Roadmap/milestone authority
changes at V2 or an equivalent milestone gate and is not re-litigated during
every implementation round.

V1 and V2 may be modes of one verifier subsystem, but they use separate
dispatches, fresh contexts, and distinct frozen inputs. Trivial
non-authoritative corrections do not automatically require a dedicated
Evidence dispatch.

The deterministic manifest owns changed files, exact SHA chain, commands and
outcomes, ancestry, worktree/upstream identity, snapshot identity, and
artifact hashes. A model may explain these facts but must not originate them
from memory.

> **Self-referential SHA invariant:** no artifact may require its own final
> commit identity to be embedded in content that determines that same commit
> identity.

Evidence identifies the frozen implementation. Evidence-commit identity and
verdict lineage are external ledger facts or are derived from repository
state; this roadmap does not mandate one resolver implementation.

### 12.8 Context rotation

Roles persist; model instances are replaceable. Rotation requires capability
revocation from the old principal, compare-and-swap acquisition by the new
principal, and independent confirmation of repository and authority identity.

Machine-owned handoff facts include current/upstream SHA, branch/worktree
state, active phase/gate, worker/session/model, contract/implementation/evidence
identities, attempt count, required gate results, current verdict, and
current-versus-historical verdict separation.

Model interpretation includes blocker classification, escalation rationale,
task decomposition rationale, and unresolved architectural tension.

Immutable references must preserve complete verifier findings with exact file
and line references, complete worker `BLOCKED` or completion reports, the
accepted SHA/verdict chain, and original test/artifact output. Per-Executor
failure-signature history and normalized blocker category are mandatory.
A bounded summary is navigation metadata; it never replaces source findings
or evidence.

### 12.9 Policy lifecycle and experimental roles

Models may propose but may not mutate active policy:

```text
Observed friction
→ PolicyChangeProposal with exact evidence
→ offline replay/counterfactual evaluation
→ independent policy review
→ Director decision
→ user approval when authority expands or safety weakens
→ shadow/canary
→ measured comparison
→ activation at a future authority revision
→ rollback and sunset
```

Policy versions record proposer, rationale, affected envelopes, authority,
security and cost deltas, evaluations, activation revision, rollback version,
and review/sunset date. New policy never retroactively changes an ACCEPT.

Scaffolding to test periodically for removal includes fixed decomposition
templates, mandatory intermediate plans, model-specific prompts, retry
thresholds, mandatory specialists, command-by-command approval, static routing,
oversized handoff summaries, and model-generated evidence narratives.

The following invariants do not weaken as models improve: exact snapshot
identity, one valid writer lease, verifier independence, idempotent dispatch,
monotonic authority transition, enforced capability scope, and artifact/
evidence lineage.

Navigator/Challenger remains disabled and experimental. One bounded read-only
dispatch may be evaluated only after dogfood records structural deadlock: the
same contract failure twice without a materially new hypothesis; Executor and
Verifier deadlock; increasing patch size without decreasing blockers;
repeated local workarounds around one structural defect; a policy-exception
proposal; recurrence after context rotation; substantial budget consumption
without new evidence; or implementation/roadmap conflict. It cannot write,
dispatch, grant capabilities, change policy/gates, or ACCEPT/REJECT.

### 12.10 Staged post-Alpha validation

No stage below has implementation authority until a future independently
accepted contract explicitly opens it.

| Stage | Scope | Explicitly excluded |
| --- | --- | --- |
| **O0 — evaluation baseline** | Build a work-item/failure dataset; measure current coordination cost; define authority-violation and autonomy-versus-rigidity evaluations | Automatic dispatch, routing, or policy mutation |
| **O1 — minimum deterministic experiment** | One bounded work item, one Coordinator, one Executor, implementation pre-gate, fresh V1, implementation freeze, deterministic evidence manifest, optional Evidence mode, fresh V2, attempt/failure records, safe Coordinator handoff | Full envelope proxy, automatic provider selection, Navigator, specialist catalog |
| **O2 — enforced envelope pilot** | Enforce exact snapshot/worktree/lease, one writer, path/command capability, dispatch identity, expiry/revocation, required gate, and external-side-effect checks | Broad network/secret/budget enforcement without a real proxy |
| **O3 — adaptive Coordinator operations** | Same-scope task splits, attempt classification, approved model-profile switching, stall termination, recovery proposals, authority-expansion requests | Gate override or out-of-policy execution |
| **O4 — context rotation** | CAS state revision, revoke/acquire capabilities, replace Coordinator safely, preserve pending attempts/delivery and exact source references | Summary-only handoff |
| **O5 — evidence pipeline** | Deterministic manifest, restricted Evidence mode, separate evidence commit when needed, fresh V2 audit | Model-invented facts or implementation amendment |
| **O6 — native-subagent observability** | Parent attribution, opaque delegation, aggregate resource and side-effect evidence | Reimplementation of provider subagent orchestration |
| **O7 — policy lifecycle and conditional routing** | Evidence-backed proposals, replay/evaluation, canary, rollback/sunset, initially user-confirmed model routing | Silent policy mutation or automatic authority expansion |
| **O8 — optional Navigator experiment** | One-shot read-only structural challenge after demonstrated deadlock | Persistent role or execution authority |

The O1 experiment is sufficient to validate the direction's first claim:
whether verified repository truth, attempt records, deterministic evidence,
and safe handoff reduce coordination loss more than they add overhead. Compare
it with the current manual baseline using implementation rounds,
evidence-only rounds, pre-gate catches, blind retries, inconsistent worker
claims, avoided Verifier dispatches, handoff loss, and total coordination
overhead. It is not sufficient to validate automatic routing, a full policy
registry, Navigator, or full Control Plane enforcement.

### 12.11 Stop conditions and Alpha exclusion

All post-Alpha orchestration work is excluded from Base Alpha Steps 9.1-9.5.
Do not begin O0 before Base Alpha acceptance and a separate contract. Do not
reuse accepted foundation code as implied authorization.

Stop a future orchestration packet if it:

- changes Alpha scope or accepted runtime/Terminal/Transcript/approval/input/
  pairing/generation authority;
- treats a prompt or report as a capability grant;
- lets a Coordinator change scope, snapshot, gate meaning, or repository truth;
- serializes same-scope technical reasoning into Control-Plane approvals;
- trusts worker claims without repository/artifact comparison;
- allows an Executor to accept itself or a Verifier to modify production;
- loses exact source findings during rotation;
- revives provider-native subagent control or chain-of-thought collection;
- introduces automatic routing, Navigator, or policy mutation before its
  measured prerequisite; or
- claims enforcement that the Resource Plane cannot actually provide.

Post-Alpha product positioning:

> **POKIT is the provider-neutral local execution authority for capable native
> coding agents: agents retain their intelligence, while POKIT binds their
> effects to exact identities, resources, repository states, verification, and
> recoverable evidence.**
