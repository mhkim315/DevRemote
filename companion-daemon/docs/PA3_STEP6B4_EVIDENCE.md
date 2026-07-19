# PA3 Step 6b-4 R1 Evidence — Physical Deletion

Status: **EVIDENCE**
Date: 2026-07-20
Implementation SHA: `3f8a2061b1ca5ea512e2d8ec38827a2b4cbe2fc6`

## Scope

Physically delete `activity_stub.go` and `eventstore_stub.go` from the
repository. Remove ALL `ActivityBuffer`, `ActivityEvent`, `EventStore`,
and `memoryEventStore` types from production code.

## Deleted files

```sh
$ git rm companion-daemon/internal/term/activity_stub.go
$ git rm companion-daemon/internal/term/eventstore_stub.go
```

## Production signature changes

| File | Before | After |
|------|--------|-------|
| ipc.go | `IPCServer{events EventStore, activity *ActivityBuffer}` | fields removed |
| recorder.go | `Recorder{activity *ActivityBuffer}` | field removed |
| recorder.go | `StartRecorder(sID, stream, activity)` | `StartRecorder(sID, stream)` |
| recorder.go | `EnsureRecorder(sID, fn, activity)` | `EnsureRecorder(sID, fn)` |
| owned_pty_runtime.go | `NewOwnedPTYRuntime(spawn, activity, ts)` | `NewOwnedPTYRuntime(spawn, ts)` |
| lifecycle_service.go | `LifecycleService{activity}` | field removed |
| lifecycle_service.go | `NewLifecycleService(owned, activity, ts)` | `NewLifecycleService(owned, ts)` |
| telemetry_service.go | `TelemetryService{events, activity}` | fields removed |
| telemetry_service.go | `NewTelemetryService(reg, events, n, d, a, activity, ts)` | `NewTelemetryService(reg, n, d, a, ts)` |
| runtime.go | `Handlers{Events, Activity}` | fields removed |
| create.go | `createControlledSession(ctx, reg, activity, opts)` | `createControlledSession(ctx, reg, opts)` |
| create.go | `startRecorder(ctx, reg, activity, id)` | `startRecorder(ctx, reg, id)` |
| terminal_transport.go | `EnsureRecorderTransport(id, activity, fn)` | `EnsureRecorderTransport(id, fn)` |
| app.go | `Dependencies{Events}`, `App{events, activity}` | fields removed |
| app.go | `NewActivityBuffer(2000)`, `NewMemoryEventStore()` | removed |
| app.go | `startWatcherProd(events)` | `startWatcherProd()` |
| pty.go | All Activity references | removed |

## Static gate

```sh
$ grep -rn "ActivityBuffer\|ActivityEvent\|EventStore\|memoryEventStore" \
  companion-daemon/internal/ companion-daemon/cmd/ --include="*.go" | \
  grep -v "_test.go" | grep -v "managedEventStore"
# ZERO matches
```

`managedEventStore` (managed_codex.go, managed_claude.go) is a separate
internal type for managed runtimes, not the legacy EventStore.

## Comments updated

All production comments referencing forbidden identifiers rewritten:
- recorder.go: "ActivityBuffer" → "transcript" / "output capture"
- telemetry.go: "EventStore" → "Transcript"
- runtime.go: "EventStore" → "Transcript"
- telemetry_service.go: stale comment removed
- lifecycle_service.go: stale comment removed
- owned_pty_runtime.go: stale comment removed
- app.go: "EventStore" → "Transcript"
- mux/transcript_capture.go: "ActivityBuffer" → removed
- mux/cmux_delta.go: "ActivityBuffer" → "transcript"
- transcript/contract.go: "ActivityBuffer" → removed

## Gate results

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)

$ git diff --check
(no output — clean)

$ cd ../mobile && npx tsc --noEmit
(no output — success, zero diff)
```
