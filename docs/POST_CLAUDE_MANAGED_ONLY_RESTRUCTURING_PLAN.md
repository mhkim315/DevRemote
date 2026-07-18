# Post-Claude Managed-Only Restructuring Plan

Status: **AUTHORITATIVE PLAN — FINAL CLAUDE ACCEPTED; PF EVIDENCE FREEZE IS NEXT**
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
→ A0  legacy-consumer inventory and ownership contract
→ A1  managed catalog/read migration
→ A2  managed lifecycle and terminal-transport migration
→ A3  mobile/transcript/activity managed-only cutover
→ A4  managed authority-isolation gate
→ B   legacy physical removal
→ C0  minimal canonical event spine
→ N1  notifications from C0 projections and ApprovalAuthority
→ C1  full transcript/activity canonical projection migration
→ D   common contracts and Grok/ACP research
→ E   Navigator readiness contracts/evaluation
→ O1  deterministic broker
→ O2  executor-verifier workflow
```

No phase may weaken the accepted runtime identity, generation, lifecycle,
approval, privacy, replay, stale-event or provider consumption contracts.

## 1. PF — mandatory accepted-state freeze

### Entry

PF may start only after independent final ACCEPT decisions exist for:

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

The final managed Claude decision was recorded at repository HEAD
`33cce5743d4004da3e5cc2d6e1c50576c9068b81`; C3D-B/mobile evidence is rooted at
`3ade9e3c49727e2b472cd9eb6924f879813ec08d`. PF must still assemble the complete
ledger above and verify the accepted Codex evidence. Final Claude ACCEPT does not
by itself complete PF.

If any field is missing, PF remains incomplete and A0/A1 remain blocked.

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

### Packet A0 — inventory and ownership contract (documentation only)

Inventory every production consumer of `mux.Registry`, LinkStore, discovery,
adapter capability checks, observer telemetry, external/localpty session IDs and
mobile external/best-effort branches. For each consumer record its current owner,
target owner, migration packet and deletion criterion. Freeze the minimum method
sets for ManagedRuntime, ManagedRuntimeCatalog and TerminalTransport from actual
call sites. Do not extract a generic provider SDK and do not modify production
code in A0.

### Packet A1 — managed catalog and public reads

Reuse or narrow the accepted managed session registry; do not create a second
store. Migrate REST list/get/status and approval runtime lookup to
ManagedRuntimeCatalog. A managed lookup must never fall through to
`mux.Registry`, discovery or a pane/process heuristic.

### Packet A2 — lifecycle and terminal transport

Migrate stop/kill/delete and local IPC/WebSocket lookup to ManagedRuntime. Move
owned byte input/output, resize, bounded replay and subscriber fan-out behind
TerminalTransport while preserving Recorder as the single PTY reader. Provider
lifecycle and approval semantics remain provider-specific and unchanged.

### Packet A3 — mobile, transcript and activity cutover

Migrate managed session list, lifecycle, terminal, transcript, activity and
approval consumers to managed-only DTOs. Remove external/best-effort mobile
actionability branches. Transcript/Activity may retain their current internal
implementation until C1, but their managed reads must no longer depend on legacy
discovery or observer authority.

### Packet A4 — authority-isolation gate

Delete any temporary read-only comparison facade and prove that no managed
production consumer imports or calls `mux.Registry`. Prove that screen, PTY text,
JSONL fallback and observer telemetry cannot update managed semantic status or
approval authority. Legacy adapters may still compile, but they must be
unreachable from every managed path before Phase B starts.

Each packet uses a separate contract note, implementation commit, focused gate
and independent review stop. Do not combine A0-A4 into one executor task.

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
- independent A4/Phase A ACCEPT on a frozen clean HEAD.

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

## 5. Phase C0 — minimal Canonical Event Spine

### Entry

Phase B independently ACCEPTed; no legacy observer source remains.

### Narrow architecture

```text
provider-native bounded evidence
→ provider-specific parser/normalizer
→ append-only Canonical Event Spine
→ versioned pure minimal projections
→ runtime/approval/failure notification input
```

Managed runtime state remains authoritative for identity/lifecycle.
ApprovalAuthority remains authoritative for actionability/decision/delivery.
The event spine records evidence only. Projections and caches are never
authority.

C0 is deliberately limited to the events N1 needs:

- runtime working, idle and exited;
- approval requested, decided and consumed;
- provider failure and discontinuity.

User/assistant message content, general tool activity, Transcript migration and
Navigator events are not C0 scope.

### Likely packages/files

- new bounded packages such as `internal/timeline` and `internal/projection`;
- Codex app-server and Claude hook/resume ingestion in their existing managed
  runtime/provider files;
- managed runtime, approval and future notification projection services;
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

### C0 provider proof

1. Codex writes the minimal event subset for comparison only.
2. Prove deterministic replay/equivalence and privacy, then switch only the C0
   notification-facing projection.
3. Repeat for Claude.
4. Do not switch Transcript or Activity public reads in C0.

Only one C0 public projection source is active for a provider at a time.
Dual-write must not become dual authority.

### Tests/gates

- parser determinism and malformed input fail-closed;
- duplicate, out-of-order, stale-generation and cursor-gap/resync;
- deterministic replay and reducer-version rebuild for the minimal subset;
- Approval Store versus event-spine consistency audit without event authority;
- privacy/leakage and DTO unknown-field/bound tests;
- backend full race, mobile and Android gates;
- accepted Codex and Claude runtime/approval regression evidence.

### Exit

Both providers emit the minimal subset, N1-facing projections are deterministic,
and an independent C0 ACCEPT records the rollback SHA. Transcript and Activity
remain explicitly outside C0.

## 6. N1 — notifications

N1 starts after C0 ACCEPT. It consumes trusted C0 projections and
ApprovalAuthority-backed requests. It never turns heuristic or Timeline-only
evidence into actionable approval authority. N1 completion is not a prerequisite
for retaining the existing terminal/transcript UI.

## 7. Phase C1 — full Canonical Timeline projections

### Entry

C0 independently ACCEPTed. N1 may be implemented before C1 and must consume only
the bounded C0 contracts while C1 proceeds.

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

## 8. Phase D — common contracts and Grok/ACP research

### Entry

Phase C1 independently ACCEPTed for both providers. N1 may complete before D;
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

## 9. Phase E — Navigator readiness only

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

## 10. Global completion discipline

Every phase requires:

- a pre-implementation authority/ownership note;
- exact accepted prerequisite SHAs;
- narrow implementation commits and independent review stop;
- invariant-to-code-and-test self-audit;
- frozen final HEAD before authoritative gates;
- local/remote equality and clean worktree;
- no skipped native gate in a final phase acceptance;
- explicit rollback commit and no hidden dual-authority feature flag.
