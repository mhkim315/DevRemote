# PA3 Step 6b-2 Evidence — Legacy Readers Migration

Status: **EVIDENCE**
Date: 2026-07-20
Implementation SHA: `a26cb680e65be6aea9dfb97e57004154ec0f21c7`

## Scope

Remove all production READERS from legacy `ActivityBuffer` and `EventStore`
stores. After Step 6b-1 removed all writers, this step neutralizes all
remaining read paths so legacy stores are no longer authoritative for
any production data flow. Transcript and AgentStatusStore are the
canonical read paths.

## Legacy readers neutralized

### telemetry.go — EventStore parameter removal

- **`buildSimpleSnapshot(reg, events EventStore)`** → `buildSimpleSnapshot(reg)`
  The `events` parameter was never used (no `.List()` call). Removed entirely.

- **`buildSimpleSnapshotWithDetector(reg, events EventStore, detector)`** →
  `buildSimpleSnapshotWithDetector(reg, detector)`. Updated internal call
  to `buildSimpleSnapshot(reg)`.

- **Fallback snapshot path** (line 230): Changed `h.Events` to removed param.
  Now passes no EventStore to the snapshot builder.

### telemetry_service.go — EventStore.List removed

- **`s.events.List(compoundID)`** replaced with `nil` in `Snapshot()`.
  `EventStore.List` is a no-op stub returning `nil`. The `Events` field
  on `SessionTelemetry` is `json:"-"` (never serialized), so this has
  zero API impact.

### lifecycle_service.go — ActivityBuffer.Clear removed

- **`s.activity.Clear(id)`** removed from `Delete()` path.
  `ActivityBuffer.Clear` is a no-op stub. Transcript cleanup
  (`s.transcript.ClearTranscript`) and status cleanup (`s.status.Clear`)
  continue to handle canonical deletion.

### owned_pty_runtime.go — ActivityBuffer.Clear removed

- **`o.activity.Clear(id)`** removed from `Delete()` path.
  Same reasoning — `ActivityBuffer.Clear` is a no-op stub.

## Acceptance proof

### Negative: zero legacy readers

```sh
$ grep -rn "events\.List\|activity\.Clear\|h\.Events" \
  companion-daemon/internal/term/ companion-daemon/cmd/devremote/ \
  --include="*.go" | grep -v "_test.go"
# ZERO active reader calls. Only:
# - Function signatures (ipc.go, telemetry_service.go) — params retained
#   for compilation compatibility
# - PA3 Step 6b migration comments documenting removal
```

### Positive: canonical read paths

| Data | Canonical reader | File:line |
|------|-----------------|-----------|
| Agent status | `s.statusStore.Current(id)` | telemetry_service.go:331 |
| Transcript segments | `transcript.Service.List(sessionID)` | transcript/service.go |
| Session lifecycle | `o.entries[id]` | owned_pty_runtime.go |
| PTY output (live) | `r.broadcast(payload)` | recorder.go:367 |
| Agent events (T3) | `s.transcript.ProjectAgentEvents(id, ...)` | telemetry_service.go:199 |

### Preserved invariants

- Step 6a lifecycle: createWithCapture + cap.Execute() rollback unchanged
- Steps 1-5: lock-free I/O, exact-instance cleanup, Session propagation
- No file deletion: activity_stub.go, eventstore_stub.go retained
- Function signatures retained for IPC/compatibility (ipc.go, telemetry_service.go)

## Files modified

| File | Change |
|------|--------|
| `internal/term/telemetry.go` | Remove `events EventStore` param from 2 functions; update callers |
| `internal/term/telemetry_service.go` | Replace `s.events.List()` with `nil` |
| `internal/term/lifecycle_service.go` | Remove `s.activity.Clear(id)` |
| `internal/term/owned_pty_runtime.go` | Remove `o.activity.Clear(id)` |

## Gate results

```sh
$ cd companion-daemon && go build ./... && go vet ./...
(no output — success)

$ cd companion-daemon && go test -race ./... -count=1
ok  	devremote/companion-daemon/cmd/devremote	35.611s
ok  	devremote/companion-daemon/internal/agent	1.554s
ok  	devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202	3.097s
ok  	devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1	9.473s
ok  	devremote/companion-daemon/internal/agent/contract	4.735s
ok  	devremote/companion-daemon/internal/agent/doctor	119.334s
ok  	devremote/companion-daemon/internal/devicetrust	5.508s
ok  	devremote/companion-daemon/internal/mux	7.483s
ok  	devremote/companion-daemon/internal/sessionid	5.078s
ok  	devremote/companion-daemon/internal/term	15.305s
ok  	devremote/companion-daemon/internal/transcript	4.254s
ok  	devremote/companion-daemon/internal/watcher	3.395s

$ git diff --check
(no output — clean)

$ cd ../mobile && npx tsc --noEmit
(no output — success, zero diff)
```
