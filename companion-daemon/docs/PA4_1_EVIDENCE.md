# PA4.1 Evidence — Managed REST/list/get/status Isolation

**Implementation SHA:** `e0d831d99f68eff2192ed1e287aaf43944d0e5b5`
**Contract:** `docs/PA4_MANAGED_ISOLATION_CONTRACT.md` §4.1
**Rollback:** `34d55e950012e97ccdcb03fd9abba88088ffd9a7` (PA3 ACCEPTED)

## Changes Summary

| # | File | Change |
|---|------|--------|
| 1 | `managed_catalog.go` | `ManagedAdapterPrefixes()` — canonical managed prefixes for ghost exclusion |
| 2 | `managed_catalog.go` | `ManagedCapabilities(adapter)` — catalog-carried capability sets |
| 3 | `managed_catalog.go` | `appendCatalogRows` — drops managed-prefix ghosts, queries catalog for capabilities |
| 4 | `telemetry.go` | `HandleSessionsV2` passes `LifecycleService` (2 call sites) |
| 5 | `lifecycle_pa2c_test.go` | `fakeManagedCatalog` implements new interface methods |
| 6 | `pa4_1_isolation_test.go` | 12 focused term-level isolation tests |
| 7 | `app_test.go` | 1 production constructor test via `NewAppWithDeps` |

## 13 PA4.1 Tests

### Term-level (all pass at `-race -count=20`)
| # | Test | Proves |
|---|------|--------|
| 1 | `TestPA4_1_RegistryCodexPrefixGhostExcluded` | Codex-prefix Registry ghost excluded |
| 2 | `TestPA4_1_RegistryClaudePrefixGhostExcluded` | Claude-prefix Registry ghost excluded |
| 3 | `TestPA4_1_RegistryControlledPTYPrefixGhostExcluded` | Non-managed controlled_pty survives |
| 4 | `TestPA4_1_CatalogRowCarriesCapabilitiesAndLifecycle` | ManagedCapabilities + positive LifecycleState |
| 5 | `TestPA4_1_RegistryMetadataCannotOverrideCatalog` | Registry metadata cannot override catalog |
| 6 | `TestPA4_1_StaleGenerationNotResurrectedThroughRegistry` | Catalog Epoch gate authoritative |
| 7 | `TestPA4_1_DefaultConfigEnforcesIsolation` | HandleSessionsV2 with catalog+lifecycle+Registry |
| 8 | `TestPA4_1_LegacyNonManagedRegistryBehaviorUnchanged` | tmux/cmux pass through |
| 9 | `TestPA4_1_AppendCatalogDropsCollidingRegistryRows` | Managed collision → Registry dropped |
| 10 | `TestPA4_1_HandleSessionsV2BothProviders` | Codex + Claude via HandleSessionsV2 |
| 11 | `TestPA4_1_NilCatalogNoPanic` | Nil catalog handled gracefully |
| 12 | `TestPA4_1_ConcurrentCatalogListIsolation` | Concurrent ops never corrupt |

### App-level (passes at `-race -count=5`)
| # | Test | Proves |
|---|------|--------|
| 13 | `TestPA4_1_DefaultConfigEnforcesIsolation` | `NewAppWithDeps` with `EnableManagedCodex` wires non-nil Catalog, invokes `/api/sessions` successfully |

## Gate Output

### `go build ./... && go vet ./...`
```
(exit 0)
```

### `gofmt -d internal/term/pa4_1_isolation_test.go internal/term/managed_catalog.go cmd/devremote/app_test.go`
```
(exit 0 — clean)
```

### `go test -race ./internal/term -run "TestPA4_1_" -count=20`
```
ok  devremote/companion-daemon/internal/term  1.462s
```

### `go test -race ./cmd/devremote -run "TestPA4_1_" -count=5`
```
ok  devremote/companion-daemon/cmd/devremote  1.638s
```

### `go test -race ./internal/term -count=1`
```
ok  devremote/companion-daemon/internal/term  (PASS)
```

### `git diff --check`
```
(exit 0 — clean)
```

### `git rev-parse HEAD`
```
6ca3dceddd4bc532de23f377a042eaece088c407
```

### `git status --short`
```
(clean worktree)
```

## Preserved (Unchanged)
- `recorder.go`, `service.go`, `chunk_queue.go` — PA3 Closeout mechanisms
- `telemetry_service.go` — accepted adapter path
- `create.go`, `pty.go` — session creation + WebSocket
- All legacy adapter files (tmux, cmux, localpty)
- Closeout A-D contracts
