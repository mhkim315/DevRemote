# Canonical Timeline CT-PRE Execution Plan

**Status:** AUTHORITATIVE CT FOUNDATION CONTRACT — CT-P0, CT-P1, CT-P1 Amendment, and Steps 4-8 ACCEPTED. Operational foundation complete. CT-P2 and production wiring remain BLOCKED (separate post-PB packet).

> **Product-direction amendment:** The product, authority model, and post-PB
> order now live in
> [`POKIT_NATIVE_SESSION_COORDINATION_ROADMAP.md`](POKIT_NATIVE_SESSION_COORDINATION_ROADMAP.md).
> This document continues to govern CT foundation safety. CT-P1 was
> independently accepted at `317bb0cb76a73bd49bebaaede562bfa77cd1e7bc`.
> PB received independent ACCEPT at
> `5354077afcf30343d9666511e259346d9bea0ad6`. CT-P1 must now be reviewed and,
> if needed, narrowly amended so that
> Canonical Timeline is operational evidence and does not require every future
> operational event to be a provider `AgentEvent`. Do not begin CT-P2 until the
> amendment receives independent ACCEPT and a separate post-PB packet authorizes
> it.

**Architecture name:** **Canonical Timeline**

**Current execution branch:** `feature/canonical-timeline-foundation`; the
historical branch and replay rules are retained below for provenance.

## 1. Verdict and state model

**Final verdict:** **ACCEPT WITH THE REQUIRED BOUNDARIES IN THIS PLAN.**

The additional architecture constraints are all accepted. None is rejected.
They prevent a null-heavy universal envelope, separate semantic identity from
storage order, prevent premature retention of user/provider content, and keep
the Canonical Timeline evidentiary rather than authoritative.

The original CT-PRE relationship is:

```text
Managed provider-native evidence
  -> accepted T0 AgentEvent
  -> minimal Timeline envelope
  -> append-only Canonical Timeline
  -> Transcript / Activity / status projections
```

That path remains valid for accepted provider evidence. Future runtime,
approval, input, workspace, and coordination evidence may require typed source
references rather than fabricated T0 events. CT-P1 should preserve envelope
identity, ordering, versioning, validation, redaction, collision/gap handling,
evidence references, projection boundaries, and zero production composition,
while leaving future source behavior to its actual authority.

`Transcript` means only a user-facing or derived projection. `Activity` and
status are also projections. They are not names for the Canonical Timeline.

The frozen state is:

| Identity or phase | State |
| --- | --- |
| Historical pre-R3 PB candidate | `ab18846622327334300de8436aff600db5c08e17` |
| Historical pre-R3 daemon SHA-256 | `5c1470a0d58072564298572aa8184a9c9565a8f57452eba56e2ebda56b10e580` |
| Historical pre-R3 APK SHA-256 | `934febb17b8de175816413a1aaa1a17b8d1e8f43c2cbfcb4fe26e20fce433c7d` |
| PB-DG-R4 planning baseline | `45d2433dce8b59a025bf3adf7563a7e5fc37d747` |
| Accepted PB device candidate | `059bef181c6c2ef312eee421dbf10f12b15b0326` |
| Accepted PB daemon SHA-256 | `a2753537801536b94d725d274ec94a7f5b0d1b6b7409a2f2e5f5cbbef08e93ea` |
| Accepted PB APK SHA-256 | `9ee0c4763151e080cb50b67b393bae525b4ab79463b24708f3a1cfc57bf97128` |
| PB physical SM-S926N evidence | **8/8 PASS** |
| PB ACCEPT SHA | `5354077afcf30343d9666511e259346d9bea0ad6` |
| CT-PRE | **CT-P0, CT-P1, CT-P1 Amendment ACCEPTED; Steps 4-8 foundation complete** |
| CT-P2 | **BLOCKED — separate post-PB packet required** |
| Production shadow-write | **BLOCKED — CT-P1 Amendment ACCEPTED; separate shadow-wiring authorization required** |
| Canonical Timeline authority/UI cutover | **BLOCKED; not CT-PRE scope** |

CT-PRE was a bounded pre-PB-ACCEPT exception for offline foundation work only.
PB is now accepted, but that acceptance authorizes only the CT-P1 contract
amendment. It does not itself authorize CT-P2, production composition, or
shadow-write, and no CT work may alter the frozen PB candidate or artifacts.

## 2. Authority and composition boundary

Before PB ACCEPT, CT-PRE may contain only:

- documentation and contracts;
- pure schema types and validators;
- fixture-only Codex and Claude normalizers;
- offline persistence components exercised with temporary/synthetic fixtures;
- pure offline reducers and replay;
- offline equivalence harnesses.

There must be zero production composition. No CT-PRE package may be imported,
constructed, registered, called, or configured from:

- daemon startup or `cmd/devremote` application composition;
- application dependency injection;
- managed runtime callbacks or event pumps;
- approval ingestion, storage, delivery, or resolution paths;
- `TerminalTransport`, `Recorder`, PTY spawn, input, resize, or replay paths;
- REST or WebSocket handlers;
- mobile code;
- production telemetry writers or the existing Transcript service.

Consequently CT-PRE adds zero runtime goroutines, production filesystem writes,
timers, background queues, routes, DTO fields, flags, and startup requirements.
Actual shadow dual-write is not CT-PRE. It begins only in a separately reviewed
post-PB packet.

The Canonical Timeline is evidence history, never authority. It must never:

- authorize terminal input;
- resolve, consume, admit, or deliver approvals;
- drive lifecycle transitions or revive sessions;
- replace or re-resolve `TerminalTransport`;
- read or parse raw PTY bytes into semantic events;
- alter provider cursor consumption;
- become required for daemon startup, session creation, or shutdown;
- override `ManagedRuntimeCatalog`, provider runtime, `OwnedPTYRuntime`,
  `ApprovalAuthority`, `Recorder`, or TerminalTransport state.

Raw PTY bytes remain exclusively in the Recorder/TerminalTransport stream.
Timeline events may later reference structured terminal lifecycle facts only
when an accepted producer exists; they may not store raw terminal bytes or infer
semantics from terminal text.

## 3. Branch, artifact, and rollback rules

1. The pre-R3 candidate `ab18846622327334300de8436aff600db5c08e17` and its
   artifacts remain immutable historical provenance, but are no longer
   authorized for final device acceptance after TERM-C1-R3 was discovered.
2. `PB_DG_R4_CLOSEOUT_PLAN.md` must freeze a replacement production candidate
   and matched daemon/APK artifacts before the bounded SM-S926N smoke.
3. CT-P0 planning/evidence remains documentation-only on the existing branch.
4. After independent CT-P0 ACCEPT, create
   `feature/canonical-timeline-foundation` from the accepted CT-P0 documentation
   head only after proving that every path changed from the PB production base
   is under `docs/` or `companion-daemon/docs/`. Thus its production tree starts
   byte-identical to the PB candidate even though it contains newer plans.
5. Each CT wave uses separate implementation and evidence commits and receives
   independent read-only ACCEPT before the next wave.
6. CT commits never merge into the frozen PB candidate or its artifact branch.
7. The current device-discovered fix is governed only by PB-DG-R4. No CT-P2 or
   production Timeline work may be mixed into its candidate.
8. When the replacement PB candidate is accepted, stop CT-PRE, rebase or replay accepted CT
   changes onto that exact production baseline, rerun every accepted CT gate,
   and obtain fresh acceptance. Old CT evidence cannot be spliced forward.
9. Wave rollback is commit-level removal of that wave back to the preceding
   accepted CT SHA. PB rollback remains the frozen candidate/artifacts and is
   independent of all CT branches.

## 4. Current source freeze that CT-P0 must prove

The following is the planning-time inventory. CT-P0 must reproduce it against
its exact baseline with code locations, call graphs, and negative searches;
these statements are not a substitute for CT-P0 evidence.

| Source | Current producer and mapper | Current consumer | Preliminary classification |
| --- | --- | --- | --- |
| Managed Codex | `internal/term/managed_codex.go`: native JSON-RPC `pump`, exact thread/turn checks, `appendEvent` into `managedEventStore` | managed status, bounded managed event read/stream, approval observation | **production-live managed-native source**; not T1 JSONL |
| Managed Claude | `internal/term/managed_claude.go`: stream-json `pump`/`processLine`; `claude_hook_bridge.go` for accepted hook evidence | managed lifecycle/status and authoritative approval flow | **production-live managed-native source**, but semantic event coverage must be inventoried; not automatically T2 JSONL |
| T1 Codex 0.144.1 | `internal/agent/adapters/codex/v0_144_1` converts retained JSONL fixtures to T0 AgentEvent | imported by `term/telemetry_service.go`; helper is reached by tests, while PB removed raw-line producer/polling | **fixture-only unless CT-P0 proves a live managed producer** |
| T2 Claude 2.1.202 | `internal/agent/adapters/claude/v2_1_202` converts retained JSONL fixtures to T0 AgentEvent | same dormant/helper boundary as T1 | **fixture-only unless CT-P0 proves a live managed producer** |
| T3 Transcript | `internal/transcript` AgentEvent and byte projectors/store/API | current production Transcript read model | **surviving authoritative read model for comparison; never a Timeline source** |
| Deleted discovery/external observation | `ResolveAgentLog`, process discovery, arbitrary attach/manual link, external raw JSONL readers | removed by PA4/PB | **deleted; prohibited from restoration** |

CT-P0 must distinguish a package that compiles or is imported from a producer
that is actually invoked by a managed runtime. A source without a proven current
managed-runtime producer remains fixture-only. Gemini is not an accepted source
without a separately accepted production producer.

### T0/T1/T2/T3 reuse rule

- T0 `internal/agent.AgentEvent` and `internal/agent/contract` remain the single
  accepted normalized semantic event model. CT-P1 wraps; it does not replace or
  fork T0.
- T1 and T2 fixtures, version gates, bounds, and conformance tests may be reused
  offline. Their old JSONL discovery/read mechanisms are not reusable production
  sources.
- T3 Transcript remains the public read authority through CT-PRE and supplies an
  equivalence target. CT-PRE must not change its DTO, projector, store, API, or
  mobile consumer.

## 5. Minimal Timeline envelope contract (CT-P1 target)

The envelope has only fields needed for persistence, ordering, isolation, and
validation. Provider/tool/approval/transport fields are typed optional
references, not universal nullable columns.

### Required on every event

- `schemaVersion` — envelope/framing contract version;
- `payloadVersion` — typed payload version;
- `eventID` — stable semantic identity established before persistence;
- `eventKind` — closed Timeline kind compatible with the accepted T0 event;
- managed `sessionID`;
- managed `runtimeID`;
- `launchGeneration`;
- `provider`;
- `sourceIncarnation` — immutable producer/run identity;
- `sourceIdentity` or immutable `sourcePosition` tuple;
- `occurredAt` — source event time when available, otherwise a typed unknown
  policy rather than fabricated precision;
- `observedAt` — ingestion/normalization observation time;
- `redactionPolicyVersion`;
- one bounded typed payload, redacted payload, digest, or reference.

### Conditional typed references

- provider invocation, thread, or turn identity;
- tool-call identity;
- approval-request identity;
- correlation identity;
- causation or parent-event identity;
- transport/stream generation;
- degraded/provenance state.

The wrapper must not duplicate data already authoritatively represented in T0
AgentEvent unless required to persist, validate, order, or isolate it. Tool,
approval, turn, and transport references are forbidden on unrelated kinds.

### Four separate ordering/identity axes

1. **Semantic identity:** `eventID` is deterministic before persistence from a
   domain-separated provider/source identity tuple. Migration or replay of the
   same event retains it.
2. **Provider-local source order:** immutable provider cursor, native sequence,
   or source-position tuple orders only that source incarnation.
3. **Committed append order:** a monotonically assigned daemon append sequence
   orders successfully committed records. It is not part of `eventID` and may
   change during an explicitly versioned migration.
4. **Time:** `occurredAt` and `observedAt` are evidence/display fields, never
   ordering or deduplication authority.

Same identity and same canonical digest is idempotent. Same identity and a
different digest is quarantined as corruption. Content equality alone never
deduplicates legitimate repeated messages. Cross-provider total order must not
be inferred from timestamps.

### Payload and privacy boundary

CT-PRE fixtures may contain synthetic bounded data. CT-PRE production code may
not persist user messages, tool arguments/results, cwd, environment values,
provider-native payloads, secrets, hidden reasoning, or raw PTY bytes. The
envelope/store must support redacted payloads, digests, and opaque references.
A later privacy/retention contract decides which semantic content may be durable.

The existing T0 bounds are upper safety inputs, not automatic Timeline storage
limits: 1,000 events/read, 4,096-byte cursor, 1 MiB raw record, 5,000 records or
8 MiB per batch, 32 metadata entries with 128-byte keys and 512-byte values.
CT-P1 must define tighter envelope field bounds using accepted fixture evidence
where available; disk quotas, retention, compaction, and segment sizes remain
deferred.

## 6. Wave contracts

Every wave uses the common repository invariants and evidence format in section
7 in addition to its specific gate.

### CT-P0 — Plan amendment and source freeze

**Purpose:** independently authorize only the offline pre-PB exception and prove
the sources that survived PB.

**Allowed files:** authoritative planning/evidence Markdown under `docs/` and
`companion-daemon/docs/` only.

**Forbidden files:** all Go, TypeScript/JavaScript, mobile, test, build, config,
generated, fixture, and runtime files.

**Deliverables:** this contract; minimal authoritative-roadmap amendment;
immutable PB artifact manifest; exact branch/base/rollback procedure; code-level
Codex/Claude producer-to-consumer inventory; live/fixture/deleted classification;
session/runtime/generation binding table; T0/T1/T2/T3 reuse matrix; forbidden
legacy-source matrix; CT package boundary; proposed per-wave file manifest.

**Tests/evidence:** exact HEAD/upstream and clean worktree; ancestry from
`ab188466...`; SHA-256 verification of both frozen artifacts; `git diff` proving
zero non-documentation change; `rg` call-site/import/source searches; deleted
legacy-symbol negative searches; build/vet/race, TypeScript, Jest, and
`git diff --check`; documentation secret scan.

**ACCEPT:** every source has a proven current producer or is fixture-only/deleted;
artifacts match; current production/mobile tree is unchanged; independent review
accepts the exception and branch rule.

**REJECT:** an unproven JSONL/discovery source is called production-live; artifact
identity differs; any non-doc change; order is amended without independent
review.

**Dependencies:** current clean documentation head and frozen PB artifacts.

**Rollback:** revert the CT-P0 plan/evidence commits; PB candidate and artifacts
remain untouched.

### CT-P1 — Minimal Timeline envelope

**Purpose:** implement pure envelope types, stable identity, versions, validation,
and redaction/payload boundaries without creating a second T0 event model.

**Allowed packages:** new `companion-daemon/internal/timeline/contract` and its
tests/testdata only; existing T0 imports are read-only dependencies.

**Forbidden packages:** `cmd/devremote`, `internal/term`, `internal/transcript`,
approval, transport/recorder, REST/WS, mobile, production configuration.

**Deliverables:** required/conditional types; closed kind/reference validation;
stable pre-persistence event ID algorithm; canonical digest; version gates;
bounds; redacted/digest/reference payload variants.

**Tests:** deterministic identity across replay and changed append order; source
incarnation separation; same-ID/same-digest idempotence; collision rejection;
required and forbidden conditional fields; unknown schema/payload version;
cross-session/runtime/generation rejection; timestamp non-authority; every bound
at `N-1/N/N+1`; secret/redaction fixtures; fuzz/property tests for validators.

**Evidence:** exact API snapshot and dependency graph proving no production
import; T0 reuse mapping; test/race/fuzz seed results; repository-wide gates.

**ACCEPT:** minimal wrapper passes all validation and identity proofs with zero
production composition or durable user content.

**REJECT:** append sequence/timestamp enters semantic identity; wrapper forks T0;
unrelated nullable refs are accepted; secrets/raw payloads are retained.

**Dependencies:** CT-P0 ACCEPT.

**Rollback:** remove CT-P1 implementation/evidence commits to CT-P0 ACCEPT.

### CT-P2a — Pure Codex normalizer (DEFERRED; NOT AUTHORIZED)

> CT-P2 and every later wave in this historical CT-PRE decomposition are
> blocked by the current product roadmap. Their retained contracts are planning
> inputs only. PB ACCEPT is satisfied, but do not execute them before the CT-P1
> operational-evidence amendment receives independent ACCEPT and a new reviewed
> post-PB packet authorizes the wave.

**Purpose:** map only the accepted pinned Codex fixture/native shape into T0 and
then the Timeline envelope as a pure function.

**Allowed packages:** new `internal/timeline/normalize/codex` plus synthetic or
already accepted redacted fixtures and tests; read-only imports of T0/T1/CT-P1.

**Forbidden packages:** all production composition and sources listed in section
2; no filesystem discovery, home-directory scan, JSONL resolver, or process scan.

**Deliverables/tests:** exact accepted fixture source and pinned provider version;
session/runtime/launch generation binding; native thread/turn/invocation identity;
immutable source position; duplicate/replay/collision/gap/malformed/truncated/
unknown-version/redaction cases; deterministic one-shot versus paged results;
negative proof that deleted external JSONL readers are absent.

**Evidence/ACCEPT:** fixture provenance and hash; full behavior matrix; no live
producer claim unless CT-P0 proved it; zero production imports/writes; independent
ACCEPT.

**REJECT:** resurrected discovery, unbound generation, fabricated identities,
silent gap/unknown version, provider content leak.

**Dependencies:** CT-P1 ACCEPT and Codex source classification from CT-P0.

**Rollback:** revert CT-P2a commits to CT-P1 ACCEPT.

### CT-P2b — Pure Claude normalizer (DEFERRED; NOT AUTHORIZED)

**Purpose/allowed/forbidden:** same isolation as CT-P2a in a separately accepted
`internal/timeline/normalize/claude` package, using only a proven managed shape
or pinned accepted Claude fixtures.

**Deliverables/tests:** pinned version and fixture hash; exact managed session,
runtime, launch generation, hook/resume/invocation identity where applicable;
duplicates, replay, collision, gap, malformed, truncated, unknown version,
redaction, and one-shot/paged determinism. Approval-shaped evidence remains
observational and can never call or replace ApprovalAuthority. Deleted raw JSONL
discovery remains absent.

**Evidence/ACCEPT:** same as CT-P2a, independently for Claude.

**REJECT:** approval authority leakage; identity joins not proven; external
observer restoration; any production composition or sensitive payload retention.

**Dependencies:** CT-P1 ACCEPT; CT-P2a need not be coupled, but both must be
accepted before CT-P3a.

**Rollback:** revert CT-P2b commits to the last accepted preceding CT SHA.

### CT-P3a — Offline persistence format and recovery contract (DEFERRED; NOT AUTHORIZED)

**Purpose:** freeze logical framing and recovery using temporary/offline fixtures,
not a production service.

**Allowed packages:** new `internal/timeline/store/format` and offline tests.

**Forbidden packages:** production DI/startup/runtime/handlers/telemetry/mobile;
no default durable path and no user-home writes.

**Deliverables:** versioned length-delimited framing; checksum scope and algorithm;
canonical encoded envelope; append-record sequence distinct from event ID;
dedup index semantics; collision quarantine record; torn-tail recovery model;
unknown-version handling; deterministic scan/replay contract.

**Tests:** golden framing; checksum mutation; partial header/body/checksum at every
byte boundary; append sequence monotonicity; migration preserving event IDs;
same-ID idempotence; collision quarantine; unknown versions; deterministic
restart scan; synthetic concurrent ordering model.

**Evidence/ACCEPT:** format specification, golden hashes, mutation matrix,
measurement report for representative accepted Codex/Claude fixtures, zero
production construction.

**REJECT:** recovery silently skips corruption/gaps; event ID depends on append
order; format persists prohibited content; quota/segment/retention guessed.

**Dependencies:** CT-P2a and CT-P2b ACCEPT.

**Rollback:** revert CT-P3a to both accepted normalizers.

### CT-P3b — Hardened standalone filesystem store (DEFERRED; NOT AUTHORIZED)

**Purpose:** implement the CT-P3a contract only as an explicitly constructed
offline component and prove its filesystem failure matrix.

**Allowed packages:** new `internal/timeline/store/fs` and tests using isolated
temporary directories.

**Forbidden packages:** every production composition path; no package init side
effects, global singleton, default path, runtime callback, REST, or mobile use.

**Deliverables:** explicit open/append/read/recover/close API; owner-only files and
directories; no-follow/symlink-safe creation; atomic sequence assignment;
single-writer or explicitly proven concurrency model; fsync/error contract;
collision quarantine; restart recovery.

**Tests:** permissions and umask; malicious symlink/hardlink/path traversal;
concurrent append under race; process-style reopen; torn writes; checksum errors;
disk full, short write, read-only directory, permission loss, rename/fsync/close
failure; no partial success claim; no data loss before last acknowledged append.

**Evidence/ACCEPT:** fault-injection matrix and platform assumptions; clean race;
offline-only import graph; measured sizes/throughput without freezing quota,
retention, compaction, or segment limits.

**REJECT:** unsafe path handling; acknowledged append can disappear under the
specified durability contract; corruption becomes an unexplained gap; any
production startup/write.

**Dependencies:** CT-P3a ACCEPT.

**Rollback:** revert CT-P3b to CT-P3a ACCEPT and delete only test temp data.

### CT-P4 — Offline reducer and replay (DEFERRED; NOT AUTHORIZED)

**Purpose:** derive disposable Transcript/Activity/status-shaped projections with
pure versioned reducers, without making them public or authoritative.

**Allowed packages:** new `internal/timeline/reducer` and offline tests/fixtures.

**Forbidden packages:** existing Transcript/status/approval/lifecycle mutation,
handlers, mobile, runtime/transport/recorder, store production registration.

**Deliverables:** reducer version; full and incremental replay; checkpoint/snapshot
format marked disposable and rebuildable; stale-generation exclusion; unknown
version/degraded propagation; deterministic mapping to private comparison DTOs.

**Tests:** full replay equals incremental replay; restart/checkpoint rebuild;
random batch boundaries; duplicate idempotence; collision/gap/degraded/unknown
version fail-closed; stale generation cannot affect current projection; ordering
by committed sequence while preserving source-order metadata; reducer panic/error
cannot mutate source/store or any authority.

**Evidence/ACCEPT:** deterministic fixture hashes and projection snapshots; no
authority imports/calls; rebuild from Timeline alone; repository-wide gates.

**REJECT:** snapshot becomes authority; stale/unknown/gap is silently accepted;
raw PTY parsing; approval/lifecycle/input decisions derived from history.

**Dependencies:** CT-P3b ACCEPT.

**Rollback:** revert CT-P4; delete disposable offline snapshots only.

### CT-P5 — Offline equivalence (DEFERRED; NOT AUTHORIZED)

**Purpose:** compare private Canonical Timeline projections only with surviving
authoritative read models without requiring meaningless byte equality.

**Allowed packages:** new offline equivalence harness/tests and read-only test
imports of current Transcript, managed status/lifecycle projections, and
observational approval projection where semantically valid.

**Forbidden packages:** modifications to those read models, production wiring,
ActivityBuffer/EventStore restoration, UI/DTO changes, runtime callback capture.

**Deliverables:** field/event mapping matrix for every exact, merged, omitted,
lossy, or differently shaped output; fixture runner; explained-difference report;
identity/generation/tool/approval/lifecycle/ordering preservation rules wherever
the current contract exposes them.

**Tests:** Codex and Claude positive/negative controls; missing/extra/reordered/
misbound events; generation swap; duplicate/collision/gap/degraded/unknown schema;
intentional merge/omission examples; current projection version drift.

**Evidence/ACCEPT:** zero unexplained differences; every allowed difference names
a reviewed mapping rule; no drops, gaps, collisions, degraded input, or unknown
version in a passing run; no production composition.

**REJECT:** byte-for-byte comparison hides semantic differences; unexplained
loss is waived; a degraded/gapped/colliding run passes; comparison changes the
existing authority or consumes live evidence.

**Dependencies:** CT-P4 ACCEPT.

**Rollback:** revert CT-P5 to CT-P4 ACCEPT; no runtime state exists to migrate.

## 7. Common gate and evidence contract

After every wave, evidence must name exact implementation and evidence SHAs,
their parent/ancestry, changed files, local/upstream equality, and clean worktree.
It must reproduce:

```bash
git diff --check <previous-accepted-sha>..<implementation-sha>
git diff --name-status <previous-accepted-sha>..<implementation-sha>
rg -n 'timeline' companion-daemon/cmd companion-daemon/internal/term mobile
rg -n 'ResolveAgentLog|NewActivityBuffer|NewMemoryEventStore|manual_link' companion-daemon
cd companion-daemon && go build ./...
cd companion-daemon && go vet ./...
cd companion-daemon && go test -race ./... -count=1
cd mobile && npx tsc --noEmit
cd mobile && npm test -- --runInBand
```

The `timeline` import/construction search must return zero outside the explicitly
allowed new offline packages/tests/docs. The legacy search must have zero
production restoration; historical docs/tests must be classified rather than
hidden. Use repository-provided equivalent commands when scripts define the
canonical gate.

Every wave must prove:

- production composition remains zero;
- daemon and mobile behavior/DTOs are unchanged;
- no new runtime goroutine, timer, queue, filesystem write, route, flag, or
  startup/shutdown dependency exists;
- no approval, terminal, lifecycle, input, provider cursor, or status authority
  changes;
- build, vet, full race, TypeScript, Jest, formatting, and diff checks pass;
- implementation and evidence commits contain only scoped files;
- worktree is clean and local SHA equals the tracked remote after push.

Any violation is a wave REJECT and immediate stop. A test may not be skipped,
returned early, weakened to logging, or replaced with a nil/vacuous fixture.

## 8. Historical acceptance matrix and current authorization

| Wave | Minimum acceptance result | Independent boundary |
| --- | --- | --- |
| CT-P0 | artifacts/source/order/docs independently verified; zero code diff | required before any CT code |
| CT-P1 | **ACCEPT** at `317bb0cb76a73bd49bebaaede562bfa77cd1e7bc`; operational source-boundary amendment still required post-PB | required before any later CT work |
| CT-P2a | **DEFERRED / NOT AUTHORIZED** | requires a new post-PB packet |
| CT-P2b | **DEFERRED / NOT AUTHORIZED** | requires a new post-PB packet |
| CT-P3a | **DEFERRED / NOT AUTHORIZED** | requires a new post-PB packet |
| CT-P3b | **DEFERRED / NOT AUTHORIZED** | requires a new post-PB packet |
| CT-P4 | **DEFERRED / NOT AUTHORIZED** | requires a new post-PB packet |
| CT-P5 | **DEFERRED / NOT AUTHORIZED** | requires a new post-PB packet |

Stop at the accepted CT-P1 foundation except for the now-authorized narrow
operational-evidence contract amendment. Neither the original CT-P1 acceptance
nor PB ACCEPT authorizes CT-P2, shadow-write, or cutover.

## 9. Deferred decisions

The following remain explicitly unresolved and cannot be decided implicitly by
CT-PRE:

- which managed-native producer shapes become live Timeline inputs;
- production dual-write queue, backpressure, degradation, metrics, and failure
  policy;
- durable semantic payload allowlist and treatment of user/assistant messages,
  tool arguments/results, cwd, environment, provider payloads, and reasoning;
- privacy review, encryption-at-rest, access/audit, deletion/export policy;
- disk quota, retention, compaction, segment size, and operational migration;
- common `AcceptedRecordSource` interface (allowed later only if multiple proven
  surviving sources need it; never for discovery compatibility);
- public DTO/API and mobile migration;
- Transcript/Activity/status read-authority cutover and rollback window;
- notification consumption, common provider contract, Grok/ACP, Navigator, and
  durable orchestration;
- terminal lifecycle reference admission and any cross-provider total-order rule.

Production shadow-write, even fail-open/non-authoritative, changes runtime CPU,
memory, disk, goroutines, startup/shutdown, and failure behavior. It therefore
requires PB ACCEPT plus a new reviewed packet. That later design must write only
after the primary authority commits, never hold authority locks across Timeline
I/O, never call back into authority, use bounded failure isolation, and treat any
drop/gap as an invalid equivalence run.

## 10. Exact transition gate after PB ACCEPT

PB-DG-R4 and its exact matched artifacts have satisfied conditions 1-2 below.
At the accepted CT-P1 SHA, all Canonical Timeline implementation remains stopped
except for the narrow contract amendment. Do not begin CT-P2 or create a production
writer, shadow queue, startup/DI registration, live normalizer callback, route,
DTO, mobile consumer, or cutover plan until:

1. **SATISFIED:** PB-DG-R4 passed and the bounded SM-S926N smoke used candidate
   `059bef181c6c2ef312eee421dbf10f12b15b0326` and its matched artifacts;
2. **SATISFIED:** independent PB ACCEPT is
   `5354077afcf30343d9666511e259346d9bea0ad6`;
3. CT-P1 is reviewed against the operational-evidence direction in
   `POKIT_NATIVE_SESSION_COORDINATION_ROADMAP.md` and narrowly amended if
   needed;
4. the revised CT-P1 foundation is independently accepted; and
5. a new privacy/retention and production-shadow contract is reviewed.

## 11. Historical CT-P0 executor handoff (COMPLETED)

This handoff is retained for acceptance provenance. CT-P0 completed and was
independently accepted at `d4b4d99baa2ab769ab48dea025c7343a870baf61`.
It is not a current executor authorization:

1. verify branch, upstream equality, clean worktree, exact PB ancestry, and both
   frozen artifact hashes;
2. enumerate exact Codex/Claude managed-native producers, mapper entry points,
   consumers, session/runtime/generation bindings, and live/fixture/deleted state;
3. prove whether T1/T2 helpers have any current production caller after PB;
4. record T0/T1/T2/T3 reuse and forbidden legacy-source matrices;
5. prove zero non-documentation change from the planning baseline;
6. produce a documentation-only implementation commit and a separate evidence
   commit, push fast-forward, and request independent read-only CT-P0 review.

The historical CT-P0 executor prohibition on CT-P1 was satisfied before CT-P1
started. Current work follows the stop and transition rules in sections 8-10
and `POKIT_NATIVE_SESSION_COORDINATION_ROADMAP.md`. Current work is the CT-P1
operational-evidence amendment; CT-P2 remains blocked.
