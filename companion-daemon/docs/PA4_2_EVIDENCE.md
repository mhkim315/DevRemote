# PA4.2 Evidence — Managed Lifecycle and Approval Lookup Isolation

**Implementation SHA:** `ba675493a52ef0a3801a16e3be72f7aed94833c8`
**PA4.1 Baseline:** `2c34101fb` (ACCEPTED)
**PA3 Rollback:** `34d55e950`

## Scope
PA4.2 verifies that managed lifecycle (Stop/Kill/Delete) routes exclusively
through OwnedPTYRuntime and ManagedRuntimeCatalog, and approval resolution
uses exact provider/session identity — never Registry, screen text, process
discovery, or raw JSONL.

## Verified Call Sites (zero production changes needed)

| # | File:Line | Path | Isolation |
|---|-----------|------|-----------|
| 1 | `lifecycle_service.go:146` | `Stop()` → `ownedPTY.Stop()` for controlled_pty | OwnedPTYRuntime |
| 2 | `lifecycle_service.go:155` | `Stop()` → `catalog.Get()` → `owner.Stop()` | ManagedRuntimeCatalog |
| 3 | `lifecycle_service.go:171` | `Kill()` → `ownedPTY.Kill()` or catalog+owner | No Registry |
| 4 | `lifecycle_service.go:197` | `Delete()` → `ownedPTY.Delete()` or catalog+owner | No Registry |
| 5 | `lifecycle_service.go:58` | Comment: "has no *mux.Registry dependency" | Structural |
| 6 | `lifecycle_handlers.go:32-60` | HTTP handlers → `LifecycleService` methods | No direct Registry |
| 7 | `approval_ingest.go` | Approval ingest uses explicit provider/session identity | No Registry |
| 8 | `approval_handler.go` | Approval resolution via `AuthoritativeApprovalStore` | No Registry |

## 7 PA4.2 Tests (all pass at `-race -count=20`)

| # | Test | Proves |
|---|------|--------|
| 1 | `TestPA4_2_LifecycleServiceHasNoRegistryDependency` | Stop/Kill/Delete route through OwnedPTYRuntime |
| 2 | `TestPA4_2_LifecycleStopRoutesThroughProviderOwner` | Provider owner receives catalog-derived Epoch |
| 3 | `TestPA4_2_LifecycleDeleteClearsTranscript` | Delete routes to provider owner via catalog |
| 4 | `TestPA4_2_ManagedLifecycleNeverUsesRegistryAdapter` | Nil OwnedPTYRuntime → lifecycle still works via catalog |
| 5 | `TestPA4_2_UnknownSessionFailsClosed` | tmux/cmux sessions fail closed |
| 6 | `TestPA4_2_ApprovalStoreHasNoRegistryDependency` | ApprovalStore has zero Registry fields |
| 7 | (implicit) All lifecycle paths verified through existing lifecycle_pa2c_test.go | Provider dispatch, generation gating |

## Gates
```
go build ./... && go vet ./...        → exit 0
gofmt -d (changed files)              → clean
go test -race ./internal/term -run "TestPA4_2_" -count=20 → ok 1.611s
go test -race ./internal/term -count=1                     → ok 16.842s
git diff --check                      → exit 0
HEAD == upstream                      → confirmed by release-verification agent
git status --short                    → clean
```

## Production Files (Unchanged)
- `lifecycle_service.go` — already architecturally clean (no Registry dependency)
- `lifecycle_handlers.go` — routes through LifecycleService
- `owned_pty_runtime.go` — controlled_pty lifecycle authority
- `approval_ingest.go`, `approval_handler.go`, `approval_delivery.go` — exact provider/session identity
