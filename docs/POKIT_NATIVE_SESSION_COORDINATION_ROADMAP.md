# POKIT Native Session Coordination Roadmap

**Status:** AUTHORITATIVE PRODUCT AND POST-PB ARCHITECTURE DIRECTION

**Planning base:** `317bb0cb76a73bd49bebaaede562bfa77cd1e7bc`

**Current execution state:** CT-P1 is independently accepted at
`317bb0cb76a73bd49bebaaede562bfa77cd1e7bc`; PB physical-device evidence is
pending; PB ACCEPT is **UNSET**; the operational-evidence amendment, CT-P2, and
all production Timeline wiring are blocked.

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

## 9. MVP boundary and mobile cockpit

MVP includes:

- native Codex and Claude sessions through `pokit run`;
- explicit provider/model/reasoning configuration;
- exact runtime/session/generation/configuration provenance;
- mobile status, notifications, approval/deny, input, stop, and interrupt;
- Operational Canonical Timeline;
- repository and clean snapshot identity;
- manual structured cross-provider request/result delivery;
- frozen clean-snapshot independent validation and stale detection;
- bounded handoff and immutable evidence references;
- emergency terminal access when structured evidence is insufficient.

MVP excludes automatic provider/model routing, automatic validator spawning,
automatic revision or switching, external parallel execution, forked writable
recovery, dirty-worktree validation, container/VM orchestration, native subagent
control, Navigator, generic scorer, unified multi-agent planning, and reasoning
capture.

The mobile cockpit is the primary product surface for runtime/session state,
approval and denial, input-delivery state, blocked/failed/completed
notifications, snapshot/validation identity, stale findings, manual validation
and bounded revision requests, evidence navigation, and emergency terminal
access. Unified conversation is a non-authoritative UX projection and always
shows its provider/session/generation/model/snapshot/evidence origin.

## 10. Authoritative implementation order

1. **COMPLETE:** CT-P1 finished without disruption and received independent
   ACCEPT at `317bb0cb76a73bd49bebaaede562bfa77cd1e7bc`.
2. Complete PB physical-device evidence and obtain final independent PB ACCEPT.
3. Review CT-P1 against this operational-evidence direction; add a narrow
   contract amendment if required and independently accept the revised CT-P1.
4. Add fail-open Operational Canonical Timeline shadow wiring.
5. Add workspace identity, clean-snapshot contract, and cooperative repository
   write lease.
6. Add the manual inter-session coordination envelope and broker.
7. Add frozen clean-snapshot independent validation.
8. Build the mobile operational cockpit.
9. Dogfood and ship an alpha/beta before expanding policy automation.
10. Consider forked recovery worktrees and policy automation only after measured
    demand.

Operational Timeline and workspace identity may be adjacent foundation work,
but communication and validation cannot precede exact workspace identity.
CT-P2 is blocked. CT-P1 remains production-unwired through steps 1-3, and no
production Timeline shadow wiring begins before PB ACCEPT plus independent
acceptance of the revised CT-P1 contract.

## 11. Stop and expansion gates

Stop if any packet weakens accepted lifecycle, generation, transport, device
trust, approval, input, pairing, recovery, or artifact-provenance contracts;
uses Timeline as authority; sends secrets or unbounded content; overstates
isolation; treats delivery as comprehension; validates an unidentified or stale
snapshot; or begins CT-P2/production wiring before the gates above.

Do not add automatic routing, validators, provider switching, parallel agents,
Grok/ACP, semantic stagnation detection, forked recovery, or enterprise policy
until the single-provider mobile control plane has measured repeated use and the
specific feature has outcome data, bounded failure behavior, authorization,
provenance, rollback, and independent acceptance evidence.
