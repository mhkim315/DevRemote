# PA4.1 Evidence — Managed REST/list/get/status Isolation

**Implementation SHA:** `e0d831d99f68ab5199c886f8555a495e330e25d9
**Contract:** `docs/PA4_MANAGED_ISOLATION_CONTRACT.md` §4.1
**Rollback:** `34d55e950012e97ccdcb03fd9abba88088ffd9a7` (PA3 ACCEPTED)

## Changes Summary

| # | File | Change |
|---|------|--------|
| 1 | `managed_catalog.go` | `ManagedAdapterPrefixes()` — always returns canonical managed prefixes for ghost exclusion |
| 2 | `managed_catalog.go` | `ManagedCapabilities(adapter)` — catalog-carried authoritative capability sets |
| 3 | `managed_catalog.go` | `appendCatalogRows` — drops managed-prefix Registry ghosts, queries catalog for capabilities |
| 4 | `telemetry.go` | `HandleSessionsV2` passes `LifecycleService` to `appendCatalogRows` (2 call sites) |
| 5 | `lifecycle_pa2c_test.go` | `fakeManagedCatalog` implements new interface methods |
| 6 | `pa4_1_isolation_test.go` | 12 focused isolation tests |

## 12 PA4.1 Tests (all pass at `-race -count=20`)

| # | Test | Proves |
|---|------|--------|
| 1 | `TestPA4_1_RegistryCodexPrefixGhostExcluded` | Registry-only Codex-prefix row excluded without catalog record |
| 2 | `TestPA4_1_RegistryClaudePrefixGhostExcluded` | Registry-only Claude-prefix row excluded without catalog record |
| 3 | `TestPA4_1_RegistryControlledPTYPrefixGhostExcluded` | Legacy controlled_pty rows without managed prefix survive |
| 4 | `TestPA4_1_CatalogRowCarriesCapabilitiesAndLifecycle` | ManagedCapabilities contract + nil/non-nil lifecycle integration |
| 5 | `TestPA4_1_RegistryMetadataCannotOverrideCatalog` | Registry screen/tmux metadata cannot override catalog |
| 6 | `TestPA4_1_StaleGenerationNotResurrectedThroughRegistry` | Catalog Epoch gate: stale Epoch 0 rejected, current Epoch 2 authoritative |
| 7 | `TestPA4_1_DefaultConfigEnforcesIsolation` | Production composition via HandleSessionsV2 with catalog+lifecycle+Registry |
| 8 | `TestPA4_1_LegacyNonManagedRegistryBehaviorUnchanged` | tmux/cmux legacy rows pass through unmodified |
| 9 | `TestPA4_1_AppendCatalogDropsCollidingRegistryRows` | Managed ID collision → Registry dropped, catalog wins |
| 10 | `TestPA4_1_HandleSessionsV2BothProviders` | Both Codex and Claude rows appear via HandleSessionsV2 |
| 11 | `TestPA4_1_NilCatalogNoPanic` | Nil catalog handled gracefully |
| 12 | `TestPA4_1_ConcurrentCatalogListIsolation` | Concurrent register + list never corrupts projection |

## Gate Output

### `go build ./... && go vet ./...`
```
(exit 0)
```

### `gofmt -d internal/term/pa4_1_isolation_test.go internal/term/managed_catalog.go`
```
(exit 0 — clean)
```

### `go test -race ./internal/term -run "TestPA4_1_" -count=20`
```
ok  devremote/companion-daemon/internal/term  1.726s
```

### `go test -race ./internal/term -count=1`
```
ok  devremote/companion-daemon/internal/term  16.692s
```

### `go test -race ./... -count=1`
```
ok  devremote/companion-daemon/cmd/devremote
ok  devremote/companion-daemon/internal/agent
ok  devremote/companion-daemon/internal/mux
ok  devremote/companion-daemon/internal/term
ok  devremote/companion-daemon/internal/transcript
(all packages pass)
```

### `git diff --check`
```
(exit 0 — clean)
```

### `git rev-parse HEAD`
```
e0d831d99f68ab5199c886f8555a495e330e25d9
```

### `git status --short`
```
(clean worktree)
```

## Preserved (Unchanged)

- `recorder.go`, `service.go`, `chunk_queue.go` — PA3 Closeout mechanisms
- `telemetry_service.go` — accepted adapter path
- `create.go`, `pty.go` — session creation + WebSocket
- `app.go` — adapter registration (PB territory)
- All legacy adapter files (tmux, cmux, localpty)
- Closeout A-D contracts
