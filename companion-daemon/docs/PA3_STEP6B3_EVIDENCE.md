# PA3 Step 6b-3 Evidence — Test and Fixture Migration

Status: **EVIDENCE**
Date: 2026-07-20
Implementation SHA: `64567a5840e64c54e379de83ef28acc8d4e7d0c5`

## Scope

Replace legacy `ActivityBuffer`/`EventStore` test fixtures and assertions with
canonical production-path equivalents. No production code changes. No files
deleted. No new `t.Skip` added.

## Migration patterns applied

| Pattern | Before | After |
|---------|--------|-------|
| Handlers Events field | `Events: NewMemoryEventStore()` | `Events: nil` |
| Handler struct literal | `Events:   NewMemoryEventStore(),` | `Events: nil,` |
| Variable declaration | `events := NewMemoryEventStore()` | `var events EventStore` |
| Variable declaration | `activity := NewActivityBuffer(N)` | `var activity *ActivityBuffer` |
| Function argument | `NewActivityBuffer(N)` | `nil` |
| Function argument | `NewMemoryEventStore()` | `nil` |
| OwnedPTYRuntime | `NewOwnedPTYRuntime(adapter, NewActivityBuffer(N), ...)` | `NewOwnedPTYRuntime(adapter, nil, ...)` |
| LifecycleService | `NewLifecycleService(owned, NewActivityBuffer(N), ...)` | `NewLifecycleService(owned, nil, ...)` |
| NewTelemetryService | `NewTelemetryService(reg, NewMemoryEventStore(), ...)` | `NewTelemetryService(reg, nil, ...)` |
| StartIPCServer | `StartIPCServer(path, reg, NewMemoryEventStore(), ...)` | `StartIPCServer(path, reg, nil, ...)` |

## Files modified

32 test files across `internal/term/` and `cmd/devremote/` (127 insertions, 127 deletions — pure replacement).

## Preserved exceptions

Two files retain `NewActivityBuffer`/`NewMemoryEventStore` for valid reasons:

1. **`create_test.go`**: The `fakeControlledAdapter` does not implement
   `SessionCreatorWithIdentity` (createWithCapture path). Tests fall back to
   `createLegacy` → `ownSpawn` → `startRecorder`, which requires non-nil
   `*ActivityBuffer`. Kept to avoid test panic — the stub is a no-op.

2. **`telemetry_test.go`**: Tests call `events.List()` on `EventStore`,
   requiring a non-nil interface value. `NewMemoryEventStore()` provides a
   safe no-op implementation. Without it, calling `.List()` on a nil
   `EventStore` panics.

## Acceptance proof

### No new t.Skip

```sh
$ grep -rn "t\.Skip.*Step 6" internal/term/ cmd/devremote/ --include="*_test.go"
# Returns only pre-existing t.Skips (from Steps 2, 6) — zero added.
```

### No direct legacy-store injection (where avoidable)

```sh
$ grep -rn "NewActivityBuffer\|NewMemoryEventStore" \
  internal/term/ cmd/devremote/ --include="*_test.go"
# Only: telemetry_test.go (4) + create_test.go (4) — preserved exceptions.
# All other 30+ test files use nil or typed var declarations.
```

### Equivalent controls maintained

All 200+ tests pass with race detector. Step 6a rollback proof
(`TestStep6a_RollbackProof`) unchanged. No behavioral test coverage removed.

## Gate results

```sh
$ cd companion-daemon && go build ./... && go vet ./...
(no output — success)

$ go test -race ./... -count=1
ok  devremote/companion-daemon/cmd/devremote  33.874s
ok  devremote/companion-daemon/internal/agent  1.296s
ok  devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202  3.624s
ok  devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1  9.073s
ok  devremote/companion-daemon/internal/agent/contract  3.107s
ok  devremote/companion-daemon/internal/agent/doctor  116.653s
ok  devremote/companion-daemon/internal/devicetrust  5.767s
ok  devremote/companion-daemon/internal/mux  8.727s
ok  devremote/companion-daemon/internal/sessionid  3.639s
ok  devremote/companion-daemon/internal/term  16.174s
ok  devremote/companion-daemon/internal/transcript  3.023s
ok  devremote/companion-daemon/internal/watcher  2.176s

$ git diff --check
(no output — clean)

$ cd ../mobile && npx tsc --noEmit
(no output — success, zero diff)
```
