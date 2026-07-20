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

### `go build ./... && go vet ./...`
```
(exit 0)
```

### `gofmt -d` (all changed files)
```
(clean)
```

### `go test -race ./internal/term -run "TestPA4_1_" -count=20`
```
ok  devremote/companion-daemon/internal/term  1.609s
```

### `go test -race ./cmd/devremote -run "TestPA4_1_" -count=20`
```
ok  devremote/companion-daemon/cmd/devremote  2.075s
```

### `go test -race ./... -count=1`
```
ok  devremote/companion-daemon/cmd/devremote  34.209s
?   devremote/companion-daemon/cmd/signald    [no test files]
ok  devremote/companion-daemon/internal/agent  3.353s
ok  devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202  4.655s
ok  devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1  8.246s
ok  devremote/companion-daemon/internal/agent/contract  3.254s
ok  devremote/companion-daemon/internal/devicetrust  4.137s
?   devremote/companion-daemon/internal/models  [no test files]
ok  devremote/companion-daemon/internal/mux  6.812s
ok  devremote/companion-daemon/internal/sessionid  3.633s
ok  devremote/companion-daemon/internal/term  19.499s
ok  devremote/companion-daemon/internal/transcript  1.639s
ok  devremote/companion-daemon/internal/watcher  2.083s
```

### `git diff --check`
```
(exit 0)
```

### `git rev-parse HEAD`
```
ef05e3582d2e4b2dc53f659290048db2855fdcec
```

### `git status --short`
```
(clean worktree)
```
