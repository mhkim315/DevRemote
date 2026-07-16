# Post-Claude Managed-Only Restructuring Plan

Status: **FUTURE AUTHORITATIVE PLAN — ENTRY BLOCKED UNTIL FINAL CLAUDE ACCEPT**  
Branch: `feature/phase10-multi-adapter`  
Product boundary: only POKIT-launched managed runtimes are authoritative

This document records the post-Claude implementation sequence. It is not an
acceptance record and does not authorize cleanup today. A verification agent
must fill the entry ledger in section 1 from the final accepted tree and commit
an independent roadmap-freeze decision before Phase A begins.

The sequence is:

```text
Final managed Codex ACCEPT
+ Final managed Claude C2D/C3D/mobile ACCEPT
→ PF  accepted-state freeze
→ A   managed-only ownership migration
→ B   legacy physical removal
→ C   canonical timeline migration
→ N1  notifications from canonical projections
→ D   common contracts and Grok/ACP research
→ E   Navigator readiness contracts/evaluation
→ O1  deterministic broker
→ O2  executor-verifier workflow
```

No phase may weaken the accepted runtime identity, generation, lifecycle,
approval, privacy, replay, stale-event or provider consumption contracts.

## 1. PF — mandatory accepted-state freeze

### Entry

PF may start only after independent final ACCEPT documents exist for:

- managed Codex implementation and mobile allow/deny;
- managed Claude C2D, C3D and mobile allow/deny;
- live allow, deny and exit behavior for both providers.

The verifier must record, without placeholders:

| Evidence | Required value |
| --- | --- |
| canonical repository HEAD | full SHA |
| remote branch HEAD | same full SHA |
| worktree | clean |
| accepted Codex implementation | full SHA and acceptance document |
| accepted Claude implementation | full SHA and acceptance document |
| accepted mobile allow/deny | full SHA and acceptance document |
| Codex live allow/deny/exit | bounded evidence paths and pinned version |
| Claude live allow/deny/exit | bounded evidence paths and pinned version |
| backend full race | PASS on frozen HEAD |
| mobile TypeScript/Jest | PASS on frozen HEAD |
| Android native gate | PASS, not skipped |
| invariant/secret scan | PASS on frozen HEAD |

If any field is missing, the verdict is REJECT and Phase A remains blocked.

### Exit

Commit an independent PF acceptance document containing the completed ledger,
ancestry checks, exact gate commands and rollback SHA. Do not combine PF with a
Phase A implementation commit.

## 2. Frozen ownership model

### ManagedRuntime

Owns launch, canonical SessionID, runtime incarnation/generation, provider and
version, certified process identity/tree, workspace, stop/kill/delete, final
exit evidence and invalidation.

### ManagedRuntimeCatalog

Owns bounded lookup/listing of POKIT-created runtimes. It is not discovery and
must never contain arbitrary panes, processes or externally attached sessions.

### TerminalTransport

Owns raw byte input/output, resize, bounded live replay, Recorder/VT processing,
subscriber fan-out and reconnect. It does not own semantic runtime state,
approval authority or provider completion.

Connecting a local/mobile/web viewer to a POKIT-owned runtime is retained. It is
terminal streaming, not legacy external attach. Rename ambiguous
`managed-attach` terminology during Phase A without changing its accepted
transport semantics.

### ProviderAdapter

Owns provider-native launch arguments, bounded structured evidence parsing,
event normalization, approval observation/delivery and provider-native
consumption/completion correlation. It must not expose Codex JSON-RPC or Claude
defer/resume as generic platform concepts.

### ApprovalAuthority

The existing provider-neutral A1 Store/claim/delivery/receipt/commit boundary
remains the sole approval authority. Neither runtime catalogs, Timeline,
TerminalTransport nor UI projections may authorize actions.

## 3. Phase A — managed-only ownership migration

### Entry

- PF independently ACCEPTed;
- exact PF SHA checked out locally and remotely;
- clean worktree;
- no concurrent provider remediation.

### Primary production files/packages

- composition: `companion-daemon/cmd/devremote/app.go`, `main.go`, `client.go`;
- legacy consumers: `internal/term/runtime.go`, `create.go`, `ipc.go`, `pty.go`,
  `telemetry.go`, `telemetry_service.go`, `lifecycle_service.go`, `linker.go`;
- managed sources: `internal/term/managed_codex.go`, `managed_claude.go`,
  managed event/API/approval delivery files and `managed_session_registry.go`;
- terminal primitives: `internal/term/recorder.go`, PTY/VT/WebSocket framing and
  subscriber code, plus useful controlled-PTY pieces currently under
  `internal/mux`;
- public consumers: REST handlers, WebSocket routes, IPC routes, Transcript and
  Activity services, approval runtime lookup;
- mobile: `mobile/src/lib/client.ts`, `managedSession.ts`, `lifecycle.ts`,
  `FeedScreen.tsx`, `DashboardScreen.tsx`, transcript/activity/approval
  decoders and their tests.

The executor must begin with an `rg`/call-graph inventory of every production
`*mux.Registry`, LinkStore, discovery, adapter capability and external-session
branch. The inventory is a required committed contract note, not optional
analysis.

### Implementation order

1. Introduce the narrow ownership interfaces above using accepted managed
   services as implementations. Reuse/rename the existing managed registry if
   it satisfies the contract; do not create a redundant store.
2. Migrate REST list/get/status and approval runtime lookup to
   ManagedRuntimeCatalog.
3. Migrate lifecycle operations to ManagedRuntime.
4. Migrate local IPC and WebSocket lookup to ManagedRuntimeCatalog plus
   TerminalTransport.
5. Migrate terminal input/output/resize/replay/subscribers without changing the
   Recorder single-reader invariant.
6. Migrate Transcript/Activity reads for managed sessions away from Registry
   and observer telemetry.
7. Migrate mobile session list, lifecycle, terminal, transcript, activity and
   approval consumers to managed-only DTOs.
8. Remove all managed-runtime production reads from `mux.Registry`.

### Temporary seam

A read-only catalog facade may temporarily merge legacy rows for comparison,
but:

- it cannot accept writes or register managed runtimes into `mux.Registry`;
- managed lookup always comes from ManagedRuntimeCatalog;
- no approval/lifecycle/status decision reads the legacy half;
- it has a named deletion test and must be gone before Phase A ACCEPT.

### Forbidden shortcuts

- registering managed Codex/Claude/PTY sessions back into legacy Registry;
- retaining generic attach semantics inside ManagedRuntime or TerminalTransport;
- deriving managed status from screen, PTY text, JSONL fallback or process
  discovery;
- making provider or UI caches a second runtime/approval authority;
- deleting PTY/Recorder/VT/resize/fan-out functionality with LocalPTY adapter.

### Focused tests

- managed list/get/status never invokes a blocking/failing Registry;
- REST, IPC, WebSocket and lifecycle resolve the same SessionID/generation;
- exact input/resize/replay/subscriber behavior for an owned PTY;
- no arbitrary session ID or pane ID enters the managed catalog;
- Codex/Claude approval lookup remains exact-generation bound;
- mobile contains no external/best-effort actionability fallback;
- Recorder remains the only PTY reader.

### Full gate and live evidence

- backend build, vet and full race;
- mobile TypeScript/Jest and Android native gate;
- invariant and secret scan;
- live Codex and Claude launch/status/terminal/allow/deny/exit;
- local and authenticated mobile viewer reconnect to a POKIT-owned session.

### Rollback

PF SHA is the rollback point. Roll back Phase A by reverting its ordered
commits; do not introduce a runtime flag that re-enables dual authority.

### Exit

- no managed production consumer imports or calls `mux.Registry`;
- all listed consumers use the new ownership boundaries;
- temporary facade deleted;
- legacy adapters may still compile but are unreachable from managed paths;
- independent Phase A ACCEPT on a frozen clean HEAD.

## 4. Phase B — legacy physical removal

### Entry

Phase A independently ACCEPTed and its SHA recorded.

### Delete or retire

- `internal/mux/tmux_adapter.go`, `cmux_adapter.go`, discovery, command runners,
  snapshot/delta logic and corresponding tests/fixtures;
- Registry/Adapter/session abstractions after all useful terminal primitives are
  moved behind TerminalTransport;
- external tmux/cmux/localpty attach/create/discovery routes;
- LinkStore/manual link loading and `/api/v2/links` routes;
- observer-only telemetry/status, pane/process snapshots and screen authority;
- LocalPTY adapter discovery while preserving PTY creation, byte I/O, resize,
  Recorder, VT processing, subscriber fan-out and reconnect;
- cmux snapshot Transcript and best-effort projection;
- legacy CLI flags, adapter capability branches, installation checks, tests,
  fixtures and active documentation.

Historical acceptance/evidence documents may remain under an explicitly marked
archive policy; active architecture and handoff documents must not describe
tmux/cmux as supported products.

### Deletion criteria

- no production import of legacy mux registry/adapter packages;
- no `tmux:`, `cmux:` or external LocalPTY ID accepted by production APIs;
- no link/unlink or discovery route registered;
- no mobile external/best-effort branch;
- no observer evidence can update managed semantic status;
- terminal streaming regression suite remains green;
- build artifacts and install scripts require no tmux/cmux executable/socket.

### Gates and evidence

Run Phase A full gates again after physical deletion plus repository-wide
forbidden-reference checks. Repeat live Codex/Claude allow, deny, terminal,
mobile and final-exit evidence on the exact deletion HEAD.

### Rollback and exit

Phase A ACCEPT SHA is the rollback point. Use commit reverts, not a dormant
legacy production flag. Exit requires independent Phase B ACCEPT and a clean
managed-only architecture document.

## 5. Phase C — append-only Canonical Timeline

### Entry

Phase B independently ACCEPTed; no legacy observer source remains.

### Architecture

```text
provider-native bounded evidence
→ provider-specific parser/normalizer
→ append-only Canonical Timeline
→ versioned pure projections
→ Transcript / Activity / Approval UI / Status / Notifications
```

Managed runtime state remains authoritative for identity/lifecycle.
ApprovalAuthority remains authoritative for actionability/decision/delivery.
Timeline records evidence only. Projections and caches are never authority.

### Likely packages/files

- new bounded packages such as `internal/timeline` and `internal/projection`;
- Codex app-server and Claude hook/resume ingestion in their existing managed
  runtime/provider files;
- existing Transcript, Activity, managed event and notification projection
  services;
- REST DTO/handlers and mobile decoders/renderers/tests.

Exact package names are chosen in the Phase C contract note, not pre-frozen
here.

### Event contract

Every event includes schema version, stable EventID, managed RuntimeID and
incarnation/generation, provider/version, provider event ID when available,
provider sequence/cursor, daemon append sequence, occurred-at and observed-at,
correlation/causation references and optional one-way authority reference.

Daemon append sequence orders committed timeline entries. Provider timestamps
are evidence, not ordering authority. Sequence reuse across restart is separated
by runtime/provider incarnation.

Store only private bounded evidence or a digest/reference needed for replay and
debugging. Unrestricted raw payload storage is forbidden. Raw command, cwd,
prompt, token and provider payload never enter public projections. PTY bytes
remain TerminalTransport history, not semantic Timeline events.

Reducers are pure and versioned. A reducer-version mismatch requires rebuild.
Ring buffers are live replay aids, not durable history.

### Provider-by-provider migration

1. Codex dual-write for comparison only; current public reads remain old.
2. Prove deterministic replay/equivalence and privacy.
3. Atomically switch Codex public reads to Timeline projection.
4. Remove old Codex public ingestion/read path.
5. Repeat the same sequence for Claude.
6. Delete independent Transcript and Activity ingestion.

Only one public read source is active for a provider at a time. Dual-write must
not become dual authority.

### Tests/gates

- parser determinism; malformed input fail-closed;
- duplicate, out-of-order, stale-generation and cursor-gap/resync;
- deterministic replay and reducer-version rebuild;
- projection equivalence before each read switch;
- Approval Store versus Timeline consistency audit without Timeline authority;
- privacy/leakage and DTO unknown-field/bound tests;
- backend full race, mobile and Android gates;
- live Codex and Claude replay/equivalence evidence.

### Rollback and exit

Phase B ACCEPT SHA is the structural rollback point. Each provider read switch
also has its own last-equivalent commit; rollback switches one provider back to
its previous read source only while that source still exists. Exit requires
both providers on shared Timeline projections and deletion of independent
Transcript/Activity ingestion.

## 6. N1 — notifications

N1 is resequenced after Phase C. It consumes trusted canonical projections and
ApprovalAuthority-backed requests. It never turns heuristic or Timeline-only
evidence into actionable approval authority.

## 7. Phase D — common contracts and Grok/ACP research

### Entry

Phase C independently ACCEPTed for both providers. N1 may complete before D;
it must not modify provider/runtime authority contracts.

### Boundaries

Extract only behavior proven by accepted Codex and Claude implementations:

- common: managed identity/generation, capability profile, canonical events,
  canonical approval identity, claim/idempotency, delivery result, consumption
  evidence, completion/failure evidence, cleanup and public privacy rules;
- provider-specific: Codex JSON-RPC/app-server IDs and resolution; Claude
  defer/resume/tool-use/hooks/permission-denial lifecycle.

Capability is evaluated per provider version + launch certification + runtime
generation + active transport + action shape + available consumption/completion
evidence. Never assign one static trusted tier to an entire provider.

Use explicit capability levels: managed lifecycle; structured events; exact
approval delivery/consumption; exact completion/recovery. Unsupported levels
fail closed.

### Grok/ACP checkpoint

Research only first: exact IDs, ordering/incarnation, approval identity,
decision delivery, consumed-decision proof, completion/failure evidence,
restart/recovery and privacy. Grok may enter at a lower level. No accepted
Codex/Claude contract may be weakened to obtain parity.

### Tests/gates/exit

Add provider conformance suites over genuinely common contracts, capability
negative tests and public provider-independent DTO tests. Run full repository
gates. Exit requires an independent research verdict before any Grok production
implementation and removal only of duplication whose semantics are proven
identical.

## 8. Phase E — Navigator readiness only

### Entry

Phase D common contracts and Timeline semantics independently ACCEPTed.

### Scope

Define contracts and evaluation only:

```text
canonical event deltas
→ deterministic local guards
→ optional LLM Navigator
→ bounded attributable intervention event
→ executor continues
→ validator remains final authority
```

Guards/Navigator cannot modify files, execute commands, grant approvals, bypass
the validator or become ApprovalAuthority. They consume Timeline deltas rather
than rereading full transcripts, are independently disableable, and emit bounded
events containing cause EventIDs plus guard/model/version metadata.

### Evaluation

Compare identical repository SHA, task, model and token budget using first-pass
validator acceptance, reject count, tokens, elapsed time, repeated failures,
unverified mutations and scope drift. Persist evaluation provenance; do not use
Navigator output as execution authority.

### Exit

Independent contract/evaluation-plan ACCEPT only. Navigator implementation is a
later separately authorized milestone.

## 9. Global completion discipline

Every phase requires:

- a pre-implementation authority/ownership note;
- exact accepted prerequisite SHAs;
- narrow implementation commits and independent review stop;
- invariant-to-code-and-test self-audit;
- frozen final HEAD before authoritative gates;
- local/remote equality and clean worktree;
- no skipped native gate in a final phase acceptance;
- explicit rollback commit and no hidden dual-authority feature flag.

