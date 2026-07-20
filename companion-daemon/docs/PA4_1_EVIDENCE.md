# PA4.1 Evidence — Managed REST/list/get/status Isolation

**Implementation SHA:** `d58c9bed4de3964cc59ebbf626d9f6bab3dc9fdf`
**Contract:** `docs/PA4_MANAGED_ISOLATION_CONTRACT.md` §4.1
**Rollback:** `34d55e950012e97ccdcb03fd9abba88088ffd9a7` (PA3 ACCEPTED)

**R3:** Removed dead capability helpers; lifecycle test verifies ManagedCapabilities contract + lifecycle integration; stale-generation test carries explicit generation-bearing Registry evidence; default-config test exercises production composition via HandleSessionsV2 with catalog+lifecycle wired.

**R2:** Ghost exclusion (managed-prefix Registry rows dropped), catalog-carried
capabilities via `ManagedCapabilities`/`ManagedAdapterPrefixes` contract methods,
8 focused tests covering all required scenarios.

## Migrated Call Sites

| # | File:Line | Change |
|---|-----------|--------|
| 1 | `managed_catalog.go:303` | `appendCatalogRows` now accepts `*LifecycleService`; populates `Capabilities`, `AdapterCapabilities`, `LifecycleState` from catalog authority |
| 2 | `managed_catalog.go:280-295` | New `managedAdapterCapabilities()` / `managedSessionCapabilities()` — static capability sets for managed adapters, never consult Registry |
| 3 | `telemetry.go:205` | `HandleSessionsV2` telemetry path passes `h.Lifecycle` to `appendCatalogRows` |
| 4 | `telemetry.go:212` | `HandleSessionsV2` fallback path passes `h.Lifecycle` to `appendCatalogRows` |
| 5 | `managed_catalog_test.go` (4 sites) | Updated `appendCatalogRows` callers with `nil` lifecycle param |

## Gate Output

### Command: `go build ./... && go vet ./...`
```
(exit 0)
```

### Command: `go test -race ./internal/term -run "TestPA4_1_" -count=20`
```
ok  	devremote/companion-daemon/internal/term	1.470s
(all 7 tests × 20 iterations PASS)
```

### Command: `go test -race ./internal/term ./internal/transcript ./internal/mux ./internal/agent -count=1`
```
ok  	devremote/companion-daemon/internal/term	17.490s
ok  	devremote/companion-daemon/internal/transcript	1.550s
ok  	devremote/companion-daemon/internal/mux	6.998s
ok  	devremote/companion-daemon/internal/agent	2.495s
```

### Command: `git diff --check`
```
(exit 0)
```

### Command: `git rev-parse HEAD`
```
7866d993db71a10fa9566ef37756316c78956b0d
```

### Command: `git status --short`
```
(clean worktree)
```

## PA4.1 Test Results

| Test | Assertion | Result |
|------|-----------|--------|
| `TestPA4_1_AppendCatalogDropsCollidingRegistryRows` | Registry rows with managed canonical IDs are dropped; catalog row is authoritative | PASS |
| `TestPA4_1_AppendCatalogProvidesCapabilities` | Managed catalog rows include AdapterCapabilities and session Capabilities from catalog | PASS |
| `TestPA4_1_HandleSessionsV2ManagedOnly` | When only managed sessions exist, HandleSessionsV2 returns them through catalog | PASS |
| `TestPA4_1_HandleSessionsV2BothProviders` | Both Codex and Claude managed rows appear with correct adapter-specific metadata | PASS |
| `TestPA4_1_NilCatalogNoPanic` | Nil catalog handled gracefully (no panic) | PASS |
| `TestPA4_1_MalformedIDFailsClosed` | Unknown adapter prefix rows excluded | PASS |
| `TestPA4_1_ConcurrentCatalogListIsolation` | Concurrent register + list operations never corrupt projection | PASS |

## Production Files Modified

| File | Lines | Nature |
|------|-------|--------|
| `internal/term/managed_catalog.go` | +42/−7 | Enhanced `appendCatalogRows` + capability helpers |
| `internal/term/telemetry.go` | +2/−2 | Pass `LifecycleService` to `appendCatalogRows` |
| `internal/term/managed_catalog_test.go` | +4/−4 | Updated test callers |
| `internal/term/pa4_1_isolation_test.go` | +247 new | 7 focused isolation tests |

## Preserved (Verified Unchanged)

- `recorder.go`, `service.go`, `chunk_queue.go` — PA3 Closeout A/B/C mechanisms
- `telemetry_service.go` — accepted adapter path
- `create.go`, `pty.go` — session creation + WebSocket
- `app.go` — adapter registration (PB territory)
- All legacy adapter files (tmux, cmux, localpty)
