# PA2c — Managed Lifecycle Ownership Evidence Report

Status: **REVIEW REQUEST** (R1 — remediation of the three rejection blockers)

Implementation SHA: `5af5095bf6372c4a55ef7e15ba5cdc77a58f3f5b` (R1, on top of rejected `5d655775e`)
Evidence/report SHA: (this commit)
Gate execution SHA: `5af5095bf6372c4a55ef7e15ba5cdc77a58f3f5b`

## 0. R1 remediation of the verifier blockers (rejected candidate `f23fbb7c31ac75720c905aa7662a7d4331787b1e`)

**Blocker 1 — generation validated but process resolved late from mutable
Registry.** Fixed: every owned generation is bound to an IMMUTABLE
`mux.ManagedProcess` handle captured exactly once at creation
(`OwnedPTYRuntime.captureHandle`, part of the spawn seam) and stored in the
generation record. `beginStop`/`requestKill` CLAIM the handle under the store
mutex at the decisive transition; the signal goes only to that captured
handle. `managedProcess()` (action-time `FindSession`) is deleted; the arch
gate pins `owned_pty_runtime.go` to exactly one `FindSession` call
(creation-time capture). `finalize(id, gen)` transitions only the exact
generation and skips spawn-seam registry cleanup when the generation is no
longer current (`generationStillCurrent`), so a stale finalize cannot
terminate a replacement's registry session.
Test: `TestPA2c_R1_ReplacementDuringBlockedSignal_NeverSignalsNewProcess`
(deterministic: blocking old-handle signal; replacement registers gen2;
replacement handle signal count asserted 0; old handle exactly 1; gen2 record
running and registry untouched after the stale finalize).

**Blocker 2 — lifecycle locks held across external I/O.** Fixed: the action
lock + store mutex now cover ONLY the decisive transition + handle claim
(`terminate`) and the terminal store transition (`finalize`);
`TerminateGroup`, exit waits, and registry seam cleanup all run with no
lifecycle lock held.
Test: the same deterministic blocking-signal test proves a replacement's
registration completes promptly (2s watchdog) WHILE the stale action is
blocked inside `TerminateGroup`, and that the stale action cannot affect the
replacement afterwards.

**Blocker 3 — arbitrary owner errors + stale inferred from later catalog
reread.** Fixed: closed typed vocabulary `LifecycleOutcome` (`accepted`,
`already_terminal`, `stale_generation`, `not_found`, `not_terminal`,
`unavailable`, `termination_failed`) in `lifecycle_outcome.go`.
`ProviderLifecycleOwner` now RETURNS outcomes; the dispatcher maps them
mechanically via `mapOutcome` (one arm per entry; unknown → fail closed
`unavailable`); `classifyProviderErr` is deleted. The frozen provider
services are adapted by `NewManagedProviderOwner`, which classifies a
refusal from the provider-OWNED `ManagedSessionRegistry` record — the exact
typed store the decisive comparison used — read immediately at the owner
boundary; provider error strings are never parsed and the federated catalog
is never reread for classification.
Tests: `TestPA2c_R1_ProviderWrapper_StaleBeforePublication` (real
`ManagedSessionRegistry` at epoch 7, federated catalog still serving epoch 6
— replacement UNPUBLISHED — dispatch returns exactly
`ErrLifecycleStaleGeneration`, zero signals to the replacement; plus typed
classification of `not_found` and `already_terminal`);
`TestPA2c_StaleGeneration_RejectedWithoutSignalingReplacement` now asserts
the EXACT `ErrLifecycleStaleGeneration` (not merely non-nil) with the stale
outcome produced before any publication.

No PA2d/PA3 work was started; the temporary mux spawn seam remains only in
`Create`/`captureHandle`/`finalize` cleanup as documented.

## 1. Ancestry

| Ancestor | SHA | Role |
| --- | --- | --- |
| PA2b final ACCEPT (rollback SHA) | `2897a9e0943656883a885e75b08513982de507b7` | accepted baseline |
| PA2c initial candidate | `5d655775e236dc2970472e155fa60b0e6a0b6356` | REJECTED (blockers above) |
| PA2c-R1 remediation | `5af5095bf6372c4a55ef7e15ba5cdc77a58f3f5b` | this candidate (6 files: 1 new, 5 modified; all other PA2c content preserved) |

## 2. Contract mapping (docs/PA2_LIFECYCLE_TRANSPORT_CONTRACT.md §PA2c)

**Dispatch table implemented** (`internal/term/lifecycle_service.go`):

| Adapter prefix | Stop/Kill target | Delete target | Evidence |
| --- | --- | --- | --- |
| `codex_app_server` | `ManagedCodexService` | `ManagedCodexService` | `TestPA2c_ProviderRoutes_DispatchWithDerivedEpoch` |
| `claude_headless` | `ManagedClaudeService` | `ManagedClaudeService` | same |
| `controlled_pty` | `OwnedPTYRuntime` | `OwnedPTYRuntime` | `TestLifecycle_*` suite rewritten onto the owner |
| unknown / legacy | fail closed | fail closed | `TestLifecycle_DispatchTable_FailClosed` |

- **No `*mux.Registry` in LifecycleService**: struct has no registry field;
  `lifecycle_service.go` imports no `internal/mux` (static gate
  `TestPA2c_ArchGate_NoRegistryNoSessionCatalog` parses imports).
- **Server-derived generation**: providers — `ManagedRuntimeCatalog.Get(id).Epoch`;
  controlled PTY — the owned store's record generation. Clients assert nothing.
- **Decisive comparison at the owner**: frozen provider services compare epoch
  at their lifecycle lock (unchanged code); `OwnedPTYRuntime` compares at the
  store mutex in `beginStop`/`requestKill`/`finalizeRecord`. No lock is held
  across `TerminateGroup`, exit waits, or any I/O (signal + wait happen after
  unlock, matching the pre-PA2c structure).
- **Typed outcomes, no provider error-string parsing** (R1): owners return
  the closed `LifecycleOutcome` vocabulary; `mapOutcome` translates
  mechanically to the handler sentinels. Provider refusals are classified by
  `NewManagedProviderOwner` from the provider-owned `ManagedSessionRegistry`
  record at the owner boundary (`classifyProviderErr` deleted).
  HTTP mapping: 404 / 422 / 409 (not-terminal AND stale-generation) / 500.
  `lifecycle_handlers.go` is structurally unchanged apart from the one added
  typed 409 mapping.
- **`OwnedPTYRuntime`** (`internal/term/owned_pty_runtime.go`): owns
  controlled-PTY launch identity + generation-bound lifecycle store (former
  SessionCatalog controlled-PTY portion; `CatalogEntry` JSON row shape
  preserved byte-for-byte; `Generation` field is `json:"-"`), Stop/Kill with
  bounded KILL escalation, Delete with history cleanup, exactly-once terminal
  convergence with the Recorder-EOF natural-exit watcher. It is NOT a
  TerminalTransport. The mux PTY spawn primitive remains ONLY as the
  documented temporary creation/termination seam until PA2d.
- **SessionCatalog removed** (`catalog.go` deleted): `LifecycleService.Register`,
  `IsManaged`, and provider rows are gone. Provider rows come exclusively from
  `ManagedRuntimeCatalog` (`appendCatalogRows` — unchanged);
  `mergeLifecycleState` reads controlled-PTY rows from the owned store with an
  unchanged DTO projection.
- **Bypass removed** (`pty.go`): legacy query-DELETE routes managed canonical
  prefixes through the same dispatcher; external adapters keep legacy behavior.
- **Creation**: structured Claude creation stays provider-owned (create.go no
  longer calls Register); controlled-PTY creation (HTTP profile + 0600 socket)
  goes through `OwnedPTYRuntime.Create`. `app.go` wires the three owners;
  provider wiring is typed-nil-safe after catalog construction.
- **Non-recovering restart**: unchanged — no persistence added.

## 3. Preserved boundaries

- §5 frozen files untouched: `managed_codex.go`, `managed_claude.go`,
  `managed_catalog.go`, `managed_registry.go`, activation/delivery files,
  `internal/agent/`, `mobile/`, tmux/cmux/localpty adapters (verified via
  `git diff --name-only`: none present).
- ApprovalAuthority, RuntimeRef, provider RuntimeOf: unmodified.
- PA2a behavior: zero-reference static gate re-run → 0 production matches;
  PA2a telemetry tests pass in the full suite.
- PA2b behavior: duplicate-parser gate re-run → 0 matches outside
  internal/sessionid; all `TestPA2b*` pass.

## 4. Focused tests (contract tests 1–7)

| # | Requirement | Test |
| --- | --- | --- |
| 1 | Managed routes hit provider services, not legacy Registry | `TestPA2c_ProviderRoutes_DispatchWithDerivedEpoch` (exact epochs asserted); arch gate proves no Registry dependency exists to hit |
| 2 | Unknown adapter fail closed | `TestLifecycle_DispatchTable_FailClosed` (tmux/cmux/localpty/unknown/malformed × stop/kill/delete → `ErrLifecycleUnsupported`; note: an unknown adapter that DECLARED managedLifecycle capability passed the old capability gate and now fails closed — the contract's dispatch table deliberately replaces capability probing) |
| 3 | Stale generation rejected | providers: `TestPA2c_StaleGeneration_TypedOutcome`; owned store: `TestPA2c_OwnedPTY_StaleGenerationRejected` (stale beginStop + stale finalize no-op) |
| 4 | Codex/Claude focused lifecycle tests pass unchanged | `managed_codex_test.go` / `managed_claude_test.go` untouched, pass in full race suite |
| 5 | Controlled PTY reaches OwnedPTYRuntime, never TerminalTransport/mux.Registry | rewritten `TestLifecycle_*` suite drives the owner; `TestPA2c_ArchGate_NoRegistryNoSessionCatalog` (no mux import in dispatcher; deletion targets absent) |
| 6 | Replacement interleaving rejects stale without signalling the replacement | `TestPA2c_StaleGeneration_RejectedWithoutSignalingReplacement` + `TestPA2c_StaleGeneration_TypedOutcome` (`signalsToCurrent == 0` asserted) |
| 7 | Exactly-once terminal convergence; no provider rows in SessionCatalog | `TestPA2c_OwnedPTY_ExactlyOnceTerminalConvergence` (EndedAt recorded once across natural exit + Stop); `TestPA2c_OwnedStore_NoProviderRows`; SessionCatalog deleted (arch gate) |

## 5. Exact commands and results (at gate SHA)

```
$ go test ./internal/term -run "TestPA2c" -count=1 -v      → 8/8 PASS (R1: +2 new
    deterministic tests; 1 provider-wrapper test replaces the onReject fake)
$ go test ./internal/term ./cmd/devremote -count=1          → ok / ok (24.0s / 32.1s)
$ go test -race ./... -count=1                              → exit 0, 12 packages ok
$ go test -race ./internal/term -run "TestPA2c|TestLifecycle" -count=20
    → ok (21.2s, 20/20 stable, race-clean)
```

Flake fixed during stabilization: `TestLifecycle_Stop_UnconfirmedTermination_Fails`
reused one fixed session id across `-count` iterations while the global
recorder map unregisters asynchronously — a pre-existing test-isolation
hazard (recorder machinery untouched by PA2c) exposed by the new 20×
repetition gate; fixed with a unique per-run id.

## 6. Gate results

| Gate | Result |
| --- | --- |
| `go build ./...` / `go vet ./...` | PASS |
| Focused PA2c + lifecycle suites (`-count=1`, `-count=20 -race`) | PASS |
| `go test -race ./... -count=1` (full backend) | PASS (exit 0, 12 ok) |
| `gofmt -l` (all changed/new files) | CLEAN |
| `git diff --check` | PASS |
| Secret scan (changed files) | CLEAN — one pre-existing match (`agent_activity_api_test.go:179` "Bearer not-a-real-token", introduced in S1-D commit `0b8e14afd`, a non-secret test literal; the PA2c diff introduces no credential-shaped content) |
| PA2a zero-production-reference regression | 0 matches |
| PA2b duplicate-parser + forbidden-import regression | 0 matches / clean; all TestPA2b pass |
| Lifecycle route dispatch audit | dispatch table + fail-closed + typed-outcome tests above; handlers structurally unchanged |
| Invariant: vendor branch / ID inference scans (`mobile/src`) | CLEAN |
| Mobile `npx tsc --noEmit` | **not-run** — zero `mobile/` changes in this packet (`git diff --name-only HEAD -- mobile/` empty) and `mobile/node_modules` not installed; per CLAUDE.md backend gate ran, mobile reported not-run with reason |

## 7. Final state

| Condition | Value |
| --- | --- |
| Implementation commits | `5d655775e` (initial, rejected) + `5af5095bf6372c4a55ef7e15ba5cdc77a58f3f5b` (R1 remediation) |
| Evidence/report commit | this commit (docs only) |
| Worktree at push | clean |
| Local == Remote after push | verified in worker_done |
| Rollback SHA (per contract) | `2897a9e0943656883a885e75b08513982de507b7` (PA2b final ACCEPT) |
