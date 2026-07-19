# PA3 Step 6b-4 Evidence — Physical Deletion

Status: **EVIDENCE**
Date: 2026-07-20
Implementation SHA: `34f48d71bc4faec0ce26916aebf338a3f0c96d47`

## Scope

Final Step 6b: physically delete `activity_stub.go` and `eventstore_stub.go`,
remove `ActivityBuffer` and `EventStore` types from all production signatures,
and clean up all remaining wiring in `app.go`, `runtime.go`, and handlers.

## Deleted files

- `internal/term/activity_stub.go` — DELETED
- `internal/term/eventstore_stub.go` — DELETED

## Production signature changes

| File | Before | After |
|------|--------|-------|
| recorder.go | `Recorder{activity *ActivityBuffer}` | field removed |
| recorder.go | `StartRecorder(sessionID, stream, activity)` | `StartRecorder(sessionID, stream)` |
| recorder.go | `EnsureRecorder(sessionID, openStream, activity)` | `EnsureRecorder(sessionID, openStream)` |
| owned_pty_runtime.go | `NewOwnedPTYRuntime(spawn, activity, transcriptSvc)` | `NewOwnedPTYRuntime(spawn, transcriptSvc)` |
| lifecycle_service.go | `NewLifecycleService(ownedPTY, activity, transcriptSvc)` | `NewLifecycleService(ownedPTY, transcriptSvc)` |
| telemetry_service.go | `TelemetryService{events, activity}` | fields removed |
| telemetry_service.go | `NewTelemetryService(reg, events, notifier, detector, approvals, activity, transcriptSvc)` | `NewTelemetryService(reg, notifier, detector, approvals, transcriptSvc)` |
| runtime.go | `Handlers{Events, Activity}` | fields removed |
| ipc.go | `StartIPCServer(path, reg, events, telemetry, activity, ...)` | `StartIPCServer(path, reg, telemetry, ...)` |
| ipc.go | `handleIPCConnection(conn, reg, events, telemetry, activity, ...)` | `handleIPCConnection(conn, reg, telemetry, ...)` |
| create.go | `createControlledSession(ctx, reg, activity, opts)` | `createControlledSession(ctx, reg, opts)` |
| create.go | `startRecorder(ctx, reg, activity, canonicalID)` | `startRecorder(ctx, reg, canonicalID)` |
| terminal_transport.go | `EnsureRecorderTransport(sessionID, activity, openStream)` | `EnsureRecorderTransport(sessionID, openStream)` |
| app.go | `Dependencies{Events}` | removed |
| app.go | `App{events, activity}` | removed |
| app.go | `NewActivityBuffer(2000)` call | removed |
| app.go | `NewMemoryEventStore()` call | removed |
| app.go | `startWatcherProd(events)` | `startWatcherProd()` |

## Static gate

```sh
$ grep -rn "ActivityBuffer\|EventStore\|activity_stub\|eventstore_stub" \
  companion-daemon/internal/ companion-daemon/cmd/ --include="*.go" | \
  grep -v "_test.go"
# ZERO matches — types fully removed from production code.
```

## Files modified

47 files changed: 175 insertions, 260 deletions (net -85 lines across production + tests).

## Gate results

```sh
$ cd companion-daemon && go build ./... && go vet ./...
(no output — success)

$ git diff --check
(no output — clean)

$ cd ../mobile && npx tsc --noEmit
(no output — success, zero diff)
```
