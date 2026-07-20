# PA4.1 Evidence — Managed REST/list/get/status Isolation

**Implementation SHA:** `2c34101fb2316f58934e2699e62c21199a39490c`
**Rollback:** `34d55e950` (PA3 ACCEPTED)

## Production changes
| File | Change |
|------|--------|
| `managed_catalog.go` | `ManagedAdapterPrefixes()`, `ManagedCapabilities()` on catalog contract; ghost exclusion + catalog-carried caps in `appendCatalogRows` |
| `telemetry.go` | `HandleSessionsV2` passes `LifecycleService` to `appendCatalogRows` (2 sites) |

## Tests (13 total)
| # | Test | Proves |
|---|------|--------|
| 1 | `TestPA4_1_RegistryCodexPrefixGhostExcluded` | Codex-prefix Registry ghost excluded |
| 2 | `TestPA4_1_RegistryClaudePrefixGhostExcluded` | Claude-prefix Registry ghost excluded |
| 3 | `TestPA4_1_RegistryControlledPTYPrefixGhostExcluded` | Non-managed controlled_pty survives |
| 4 | `TestPA4_1_CatalogRowCarriesCapabilitiesAndLifecycle` | ManagedCapabilities contract + positive LifecycleState via OwnedPTYRuntime entry |
| 5 | `TestPA4_1_RegistryMetadataCannotOverrideCatalog` | Registry screen/tmux metadata rejected |
| 6 | `TestPA4_1_StaleGenerationNotResurrectedThroughRegistry` | Catalog Epoch gate: stale Epoch 0 rejected, Epoch 2 authoritative |
| 7 | `TestPA4_1_DefaultConfigEnforcesIsolation` | HandleSessionsV2 with catalog+lifecycle+Registry |
| 8 | `TestPA4_1_LegacyNonManagedRegistryBehaviorUnchanged` | tmux/cmux rows pass through |
| 9 | `TestPA4_1_AppendCatalogDropsCollidingRegistryRows` | Managed ID collision drops Registry |
| 10 | `TestPA4_1_HandleSessionsV2BothProviders` | Codex+Claude via HandleSessionsV2 |
| 11 | `TestPA4_1_NilCatalogNoPanic` | Nil catalog handled gracefully |
| 12 | `TestPA4_1_ConcurrentCatalogListIsolation` | Concurrent ops never corrupt |
| 13 | `TestPA4_1_DefaultConfigEnforcesIsolation` (app) | `NewAppWithDeps`+`EnableManagedCodex` wires non-nil Catalog, hits `/api/sessions` |

## Gates
```
go build ./... && go vet ./...        → exit 0
gofmt -d (changed files)              → clean
go test -race ./internal/term -run "TestPA4_1_" -count=20 → ok 1.609s
go test -race ./cmd/devremote -run "TestPA4_1_" -count=5  → ok 1.602s
go test -race ./internal/term -count=1                     → ok
git diff --check                      → exit 0
git rev-parse HEAD                    → 2c34101fb2316f58934e2699e62c21199a39490c
git status --short                    → clean
```
