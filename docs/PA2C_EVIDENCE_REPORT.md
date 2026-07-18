# PA2c — Managed Lifecycle Ownership Evidence Report

Status: **REVIEW REQUEST**

Implementation SHA: `5d655775e236dc2970472e155fa60b0e6a0b6356`
Evidence/report SHA: (this commit)
Gate execution SHA: `5d655775e236dc2970472e155fa60b0e6a0b6356`

## 1. Ancestry

| Ancestor | SHA | Role |
| --- | --- | --- |
| PA2b final ACCEPT (rollback SHA) | `2897a9e0943656883a885e75b08513982de507b7` | accepted baseline; local==remote verified clean before work began |
| PA2c implementation | `5d655775e236dc2970472e155fa60b0e6a0b6356` | this packet (16 files: 2 new, 1 deleted, 13 modified) |

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
- **Typed outcomes, no provider error-string parsing**:
  `classifyProviderErr` re-reads the typed catalog record (`Epoch`, `Exited`)
  to produce `ErrLifecycleStaleGeneration` / `ErrLifecycleNotFound`, else maps
  per-action to `ErrLifecycleTerminateFailed` / `ErrLifecycleNotTerminal`.
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
$ go test ./internal/term -run "TestPA2c" -count=1 -v      → 7/7 PASS
$ go test ./internal/term ./cmd/devremote -count=1          → ok / ok
$ go test -race ./... -count=1                              → exit 0, 12 packages ok
$ go test -race ./internal/term -run "TestPA2c|TestLifecycle" -count=20
    → ok (21.4s, 20/20 stable, race-clean)
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
| Implementation commit | `5d655775e236dc2970472e155fa60b0e6a0b6356` |
| Evidence/report commit | this commit (docs only) |
| Worktree at push | clean |
| Local == Remote after push | verified in worker_done |
| Rollback SHA (per contract) | `2897a9e0943656883a885e75b08513982de507b7` (PA2b final ACCEPT) |
