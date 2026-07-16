# A1.2 C1D R11 Reject and Next Executor Handoff

Status: **R11 REJECTED — ONE AUTHORITY FIX + ONE PRODUCTION PROOF ONLY**

Reviewed implementation: `b7de9909b855ea2b32c38b63c87a8d5fa40fe185`

This document onboards a new execution agent. It replaces the previous
executor session but does not reopen accepted C0D or completed C1D work.
C2D/C3D remain prohibited and production actionability remains zero.

## 1. Canonical checkout and startup

Use only:

```text
/Users/mhk/Documents/codex/DevRemote
branch: feature/phase10-multi-adapter
remote: https://github.com/mhkim315/DevRemote.git
```

Fetch and fast-forward only. Before editing, require:

- local HEAD equals `origin/feature/phase10-multi-adapter`;
- R11 `b7de9909b855ea2b32c38b63c87a8d5fa40fe185` is an ancestor;
- accepted C0D `e42d4c570e64462ce813861017cc635e338e68bf` is an ancestor;
- the worktree is clean.

Do not use a scratch checkout, rebase accepted history, force-push, or start
from an older remediation commit.

Read, in order:

1. this document;
2. `docs/A1_2_CLAUDE_APPROVAL_EXTENSION_PLAN.md`;
3. `docs/A1_2_C1D_R7_INDEPENDENT_REVIEW_AND_FINALIZATION.md` for the frozen
   scope decision only;
4. `docs/A1_2_C1D_EVIDENCE_REPORT.md`, treating its current `TBD` and gate
   claims as unverified draft text;
5. `companion-daemon/internal/term/approval_store_gen.go`;
6. `companion-daemon/internal/term/managed_claude.go`;
7. `companion-daemon/cmd/devremote/client.go` and existing production live
   tests in `companion-daemon/cmd/devremote`.

Before implementation, write a short contract note with the authority owner,
complete generation binding, state transition, linearization point, capacity
behavior, restart behavior, and both failing interleavings. Commit it with the
implementation packet, not as a claim of completion.

## 2. Independent R11 verdict

Confirmed improvements that must be preserved:

- exact single-token `pokit run claude` now serializes a structured
  `profileId=claude` request;
- multi-token/arbitrary commands retain the legacy fallback;
- focused CLI tests cover both cases;
- the reverse-race seam is placed after Store admission and before active-list
  append;
- Claude-focused race tests pass repeatedly;
- no options, delivery material, CTA, claim or actionability was enabled.

R11 is rejected for three material reasons:

1. termination installs high-water by calling `IngestObserved` with deliberately
   non-authoritative provenance;
2. invalid ingestion mutates Store session/high-water before item validation,
   so the implementation relies on an unsafe authority side effect;
3. the required real CLI live proof and final evidence report are absent, and
   the committed tree fails `git diff --check` due to a blank line at EOF.

## 3. Packet F1 — metadata-only Store generation authority

Authority owner: `AuthoritativeApprovalStore`.

The following is prohibited:

- fake or sentinel `ApprovalIngest` items;
- unknown/non-authoritative provenance used to create Store metadata;
- any Approval record created by termination, replacement or cleanup;
- relying on `atomicTerminated.Load()` as the shared linearization point.

Required behavior:

1. Add or correct one explicit Store-owned runtime-generation transition that
   can create bounded metadata-only session state without an Approval record.
   The exact API name is not frozen.
2. The transition must atomically bind `SessionID + LaunchGeneration +
   StreamGeneration`, supersede older records, and install the high-water used
   by `IngestObserved`.
3. It must have explicit bounded-capacity behavior. Capacity exhaustion must
   fail closed and must not evict live authority in a way that permits stale
   replay.
4. Prefer reserving/binding the runtime generation before observation becomes
   reachable. If reservation is required for safety and fails, managed Claude
   creation or observation must remain unavailable rather than launching an
   unprotected authority path.
5. `IngestObserved` must not create a session, advance generation, or supersede
   records for an item that fails authoritative provenance or structural
   validation. Add a non-vacuous negative test for this exact counterexample.
6. Termination/replacement must use the metadata transition, never ingestion.
   After it wins, a late StreamGen 0 observation must be rejected inside the
   Store.

Do not redesign the approval state machine, claims, delivery, receipts or
mobile DTOs. If satisfying item 5 requires touching the frozen A1 Store, keep
the change to validation ordering/metadata admission and run all A1/SP1
regressions.

### Deterministic concurrency evidence

Use barriers/channels, never sleeps, and prove both orders:

- **forward:** pause immediately before the real Store ingest; terminate and
  install metadata high-water; resume; assert zero new records, zero pending,
  zero active, and no sentinel/public DTO;
- **reverse:** pause immediately after Store admission and before active append;
  terminate; resume; assert the admitted record is non-current/invalidated,
  Store public lists contain no current approval, and runtime pending/active are
  both zero.

Include a known-bad control or fault mode showing that omitting the metadata
high-water makes the forward test expose the stale record. A final-state-only
test is insufficient.

## 4. Packet F2 — exact production CLI proof

The proof must originate in `cmd/devremote` from the actual
`buildRunCreateRequest([]string{"claude"}, ...)` and
`createRequestViaSocketAt` path. Handwritten JSON in an internal package is not
the production serializer.

Required bounded path:

```text
actual pokit-run request builder
  -> 0600 Unix socket
  -> StartIPCServer production composition
  -> ManagedClaudeService
  -> pinned Claude Code 2.1.209 in requested CWD
  -> PreToolUse defer + matching tool_deferred
  -> AuthoritativeApprovalStore
```

The environment-gated live proof must:

- verify exact version, canonical resolved path and configured pre-launch
  digest;
- use bounded polling/deadlines, not a fixed sleep;
- require exactly one matching non-actionable record;
- verify exact session/provider/version/epoch binding;
- verify zero options, delivery material, CTA and claim path;
- prove raw command, tool input, CWD, hook token and provider payload do not
  enter public DTO/log evidence;
- after Stop/exit, require zero current approvals, exited registry state,
  removed hook/socket artifacts and no managed child;
- leave no processes or temporary directories.

If pinned 2.1.209, ambient authentication or a harmless bounded provider turn
is unavailable, report **C1D BLOCKED**. Do not substitute fixtures, fake
launchers or a skipped test.

## 5. Evidence and exact final gate

Update `docs/A1_2_C1D_EVIDENCE_REPORT.md` only after the implementation SHA is
frozen. It must contain:

- the exact implementation SHA, command and environment classification;
- bounded redacted live output and exact assertion results;
- explicit PASS/SKIP/BLOCKED distinctions;
- cleanup evidence;
- accurate test counts;
- `REVIEW REQUEST: A1.2 C1D Managed Claude Observation — <implementation SHA>`.

Before the implementation commit, re-read this handoff and map every required
invariant to production code and non-vacuous tests. Freeze HEAD and run:

- `gofmt` and `git diff --check`;
- focused Claude race tests repeatedly;
- backend build/vet and full relevant race suites;
- frozen A1/SP1 regressions, including invalid-ingest/no-mutation coverage;
- CLI serializer and real IPC production-path tests;
- documentation secret scan.

Run the live proof against that exact implementation SHA. Commit only the
redacted report afterward, then rerun diff/document/secret gates on the final
report HEAD. Push that exact tree and verify local/remote equality, ancestry and
clean worktree.

## 6. Hard exclusions and stop condition

- no C2D/C3D allow/deny or resume delivery;
- no options, mobile CTA, ClaimForExecution or delivery capacity;
- no N1, O1/O2, Agent SDK, Channels or terminal key injection;
- no Codex/A1 state-machine redesign;
- no generic provider SDK, Windows implementation or observer cleanup;
- no new attestation research in C1D.

Stop after the C1D review request. Do not continue in the same session.
