# PA4.4 Evidence — Observer Containment

**Implementation SHA:** `a8bf135bf44c2ed389e8c34ee1db5d84d5369823`
**PA4.3 Baseline:** `cc53eb5af` (ACCEPTED)
**PA3 Rollback:** `34d55e950`

## Scope
PA4.4 proves observer telemetry, screen parsing, raw JSONL readers, and
process discovery cannot reach managed catalog, status, approval, or
lifecycle. Legacy observer paths are contained within legacy routes.

## Contained Paths (zero production changes)

| # | Boundary | Mechanism |
|---|----------|-----------|
| 1 | `ManagedRuntimeCatalog` | Doc: "never probes Registry, discovery, process names, panes, screen text, PTY bytes, or JSONL" |
| 2 | `processSession` | `isAcceptedAdapter` gate — non-codex/claude rejected |
| 3 | `AgentStatusStore` | `Update/Revoke/Invalidate` accept explicit provider-identity params |
| 4 | `ApprovalStore` | `Ingest/ListSafe` use exact sessionID + provider identity |
| 5 | `appendCatalogRows` | Drops colliding + managed-prefix Registry rows (PA4.1) |
| 6 | `ManagedRuntimeCatalog.Get` | Unknown/non-managed prefix returns (zero, false) |

## 6 PA4.4 Tests (all pass at `-race -count=20`)

| # | Test | Proves |
|---|------|--------|
| 1 | `TestPA4_4_ManagedRuntimeCatalogNeverProbesObserver` | Catalog Get fails closed for tmux/cmux |
| 2 | `TestPA4_4_AgentStatusStoreNoObserverDependency` | Status store has no Registry/discovery fields |
| 3 | `TestPA4_4_ProcessSessionGatesOnAcceptedAdapter` | Non-accepted adapters rejected |
| 4 | `TestPA4_4_ObserverCannotAlterManagedCatalog` | Observer snapshot dropped, catalog wins |
| 5 | `TestPA4_4_ScreenTextCannotBecomeManagedStatus` | Status projection uses provider-native API |
| 6 | `TestPA4_4_ApprovalDeliveryUsesExactProviderIdentity` | Approval uses explicit identity |

## Gates
```
go build ./... && go vet ./...        → exit 0
gofmt -d (changed files)              → clean
go test -race ./internal/term -run "TestPA4_4_" -count=20 → ok 1.666s
go test -race ./internal/term -count=1                     → ok 16.880s
git diff --check                      → exit 0
HEAD == upstream                      → confirmed externally
git status --short                    → clean
```
