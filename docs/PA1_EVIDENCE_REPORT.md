# PA1 — Managed Runtime Catalog Evidence Report

Status: **REVIEW REQUEST**

Final implementation HEAD: `5d7d4abfdb92b254f25d24b499688a2fb3ae3e2a`

## 1. Prerequisites verified before editing

| Condition | Value |
| --- | --- |
| Canonical remote | `https://github.com/mhkim315/DevRemote.git` |
| Canonical branch | `feature/phase10-multi-adapter` |
| PA1 baseline | `d8e663c0ccdb263f2747a4953908657ff27924e9` (ancestor: YES) |
| PF ancestry | `33cce5743d4004da3e5cc2d6e1c50576c9068b81` (ancestor: YES) |
| Baseline worktree | clean |
| Handoff read | `docs/NEXT_EXECUTOR_PA1_MANAGED_CATALOG_HANDOFF.md` |
| Mandatory docs read | All 10 items from handoff section 2 |

## 2. Commit sequence

| # | Commit | Purpose |
| --- | --- | --- |
| 1 | `5ebe47a` | PA1 contract note |
| 2 | `5d7d4ab` | catalog + production migration + focused tests |
| 3 | (this commit) | evidence report |

## 3. Invariant-to-code-and-test mapping

### Invariant 1: Codex and Claude records returned from same read-only catalog with deterministic ordering

- **Production**: `managedRuntimeCatalog.List()` at `managed_catalog.go:113-145` — merges both registries, sorts by `CreatedAt` asc then `SessionID` asc.
- **Test**: `TestCatalog_List_DualRegistryDeterministicOrder` — asserts 2 records, same order across multiple calls, sorted invariant.
- **Classification**: production-wired.

### Invariant 2: Exact Get returns only the canonical provider record

- **Production**: `managedRuntimeCatalog.Get()` at `managed_catalog.go:71-107` — dispatches by `mux.ParseSessionID().Adapter` to the exact provider registry; cross-checks the other registry for ambiguity.
- **Test**: `TestCatalog_Get_ExactProviderRecord` — verifies codex Get returns codex provider, claude Get returns claude provider, no cross-contamination.
- **Classification**: production-wired.

### Invariant 3: Unknown, malformed, pane-style and arbitrary attached IDs return not found

- **Production**: `Get` default case at `managed_catalog.go:104-105` — any adapter prefix other than `codex_app_server` or `claude_headless` returns `(zero, false)`.
- **Test**: `TestCatalog_Get_UnknownAndMalformedIDs` — 10 sub-cases including empty string, tmux:, cmux:, controlled_pty:, localpty:, arbitrary text, and malformed IDs.
- **Classification**: production-wired.

### Invariant 4: Duplicate/ambiguous ID fails closed

- **Production**: `Get` cross-check at `managed_catalog.go:82-87` and `97-101` — if the non-selected registry also holds the ID, returns `(zero, false)` with a log message.
- **Test**: `TestCatalog_Get_AmbiguousDuplicateFailsClosed` — registers same ID in both registries, verifies Get fails closed, verifies clean ID still works.
- **Classification**: production-wired.

### Invariant 5: Blocking/panicking legacy Registry cannot be invoked by managed list/get/native-status

- **Production**: `HandleManagedNativeStatus` at `managed_api.go:67-83` uses `h.Catalog.Get(id)` — no fallback to `h.Managed` or `h.ManagedClaude`; `HandleManagedSessions` at `managed_api.go:95-110` uses `h.Catalog.List()`; `HandleSessionsV2` at `telemetry.go:339-351` uses `appendCatalogRows` which calls `catalog.List()` — no fallback to `appendManagedRows`/`appendClaudeManagedRows`.
- **Test**: `TestCatalog_LegacyRegistryIsolation` — builds a `Handlers` with both a `mux.Registry` and a `Catalog`, verifies managed-sessions, native-status, and sessions-v2 all serve catalog rows without touching the legacy registry.
- **Classification**: production-wired.

### Invariant 6: Managed rows in /api/sessions are projected from catalog and cannot be overwritten by legacy row

- **Production**: `appendCatalogRows` at `managed_catalog.go:174-218` — builds `managedIDs` set from catalog records, drops any snapshot row that collides, appends catalog-derived `SessionTelemetry` rows.
- **Test**: `TestCatalog_ManagedRowsNotOverwritable` — provides a snapshot with spoofed rows colliding with both codex and claude IDs, verifies spoofed rows are dropped and catalog-derived rows carry the correct state. Control test verifies nil catalog passes through.
- **Classification**: production-wired.

### Invariant 7: Stale generation/native updates remain rejected by accepted provider registry

- **Production**: `ManagedSessionRegistry.UpdateNativeStatus` at `managed_registry.go:116-135` — rejects wrong epoch, exited records, and unknown status values. `MarkExited` at `managed_registry.go:139-154` — rejects wrong epoch and already-exited records.
- **Test**: `TestCatalog_StaleGenerationRejected` — verifies wrong-epoch update is rejected and record is unchanged, correct-epoch update is accepted.
- **Classification**: production-wired (provider registry unchanged by PA1; test validates that the catalog's underlying store preserves this contract).

### Invariant 8: RuntimeOf preserves exact provider, version, generation and launch certification checks

- **Production**: `managedRuntimeCatalog.RuntimeOf()` at `managed_catalog.go:150-166` — dispatches by adapter prefix to the accepted provider-specific `RuntimeOf` methods. Those methods (`ManagedCodexService.RuntimeOf` at `managed_approval_activation.go:97-110`, `ManagedClaudeService.RuntimeOf` at `managed_claude_activation.go:139-167`) remain byte-for-byte unchanged.
- **Test**: `TestCatalog_RuntimeOf_PreservesExactProviderBinding` — installs approval execution before creating a runtime, verifies `RuntimeOf` returns correct `Adapter`, `Version`, and `LaunchGen`; verifies unknown adapters fail closed; verifies exited session rejects resolution.
- **Classification**: production-wired.

### Invariant 9: Public DTOs contain no PID, process token, hook directory, certified digest, raw provider event, prompt, command, path, token, or approval payload

- **Production**: `managedNativeStatusDTO` at `managed_api.go:50-61` — projects only 8 fields: `id`, `provider`, `version`, `nativeStatus`, `launchGen`, `createdAt`, `statusChangedAt`, `exited`. `appendCatalogRows` at `managed_catalog.go:204-215` builds `SessionTelemetry` rows with no raw provider data.
- **Test**: `TestCatalog_PublicDTOPrivacy` — verifies exact 8 allowed fields, no forbidden fields (PID, ProcessID, HookDir, CertifiedDigest, AttestorKind, CertResult, CertReason, OS, Arch, prompt, command, path, token, approval, payload, event).
- **Classification**: production-wired.

### Invariant 10: Catalog reads return defensive copies and expose no mutation authority

- **Production**: `ManagedSessionRegistry.Get()` at `managed_registry.go:157-162` returns a copy (value receiver on a map lookup of a struct). `List()` at `managed_registry.go:165-179` builds a new slice. The `ManagedRuntimeCatalog` interface declares only `Get`, `List`, `RuntimeOf` — no mutation methods.
- **Test**: `TestCatalog_DefensiveCopies` — mutates a Get return value, verifies subsequent Get is unaffected; mutates a List element, verifies subsequent List is unaffected.
- **Classification**: production-wired.

## 4. Production-wired vs. test-only

| Path | Classification |
| --- | --- |
| `managedRuntimeCatalog.Get/List/RuntimeOf` | production-wired |
| `appendCatalogRows` | production-wired |
| `HandleManagedNativeStatus` (catalog path) | production-wired |
| `HandleManagedSessions` (catalog path) | production-wired |
| `HandleSessionsV2` (catalog path) | production-wired |
| `h.Catalog` field on `Handlers` | production-wired |
| `h.RuntimeOf = h.Catalog.RuntimeOf` in app.go | production-wired |
| `NewManagedRuntimeCatalog` constructor call in app.go | production-wired |
| `appendManagedRows` / `appendClaudeManagedRows` (retained, uncalled from production) | legacy-only (PA2 deletion target) |
| `NewCombinedRuntimeResolver` (retained, uncalled from production) | legacy-only (PA2 deletion target) |
| Catalog test helpers (`catalogWithCodex`, `catalogWithClaude`, `catalogWithBoth`, `catalogForAPI`) | test-only |
| `TestCatalog_*` tests | test-only |

## 5. Gate results (frozen HEAD 5d7d4ab)

| Gate | Result |
| --- | --- |
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `go test -race ./internal/term ./cmd/devremote -count=1` | PASS |
| `gofmt -d` (changed files) | CLEAN |
| `git diff --check` | PASS |
| Invariant: vendor branch scan | CLEAN |
| Invariant: ID inference scan | CLEAN |
| Secret scan | CLEAN |

## 6. Non-goal confirmation

Verified NO modifications to:
- `mux.Registry` construction or adapter registration
- tmux, cmux, localpty, controlled-PTY implementations
- LinkStore, linker, `/api/v2/links`
- IPC create/attach protocol or constructor signatures
- `HandleWS`, Recorder, VT bytes, input, resize, replay, subscribers
- Stop/Kill/Delete routing or `LifecycleService`
- Observer telemetry polling, transcript/activity source selection, mobile branches
- Install flags, fixtures, legacy documentation cleanup
- Codex/Claude provider protocol, approval delivery, consumption witnesses, Approval Store, action digest, DTO, mobile approval behavior
- `ApprovalAuthority`, `RuntimeRef`, either provider's `RuntimeOf` implementation

## 7. Stop condition

This report marks the PA1 completion boundary. The implementation HEAD `5d7d4ab` is frozen and gated. PA2 is prohibited until PA1 receives an independent ACCEPT.

## 8. Skips

- Mobile gate: not run (no mobile source changes — catalog is backend-only)
- Live model turns: not run (no provider protocol changes)
- Android native gate: not run (no Android source changes)
- Claude runtime/approval composition tests: not explicitly re-run (no changes to `managed_claude_activation.go`, `managed_claude.go`, `claude_approval_delivery.go`, or any Claude provider path)
- Codex runtime/approval composition tests: implicitly covered by `go test ./internal/term` PASS (includes all managed approval, delivery, and runtime tests)
