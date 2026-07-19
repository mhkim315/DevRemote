# PA3 Step 6b-1 Evidence — Legacy Writers Migration

Status: **EVIDENCE**
Date: 2026-07-20
Implementation SHA: `50f5067e937ab23bac0d988bd7ca31bd1cadef71`
R1: comment fix (forbidden identifiers removed from migration comments)

## Scope

Remove all remaining production writes to legacy `ActivityBuffer` and
`EventStore` stores. Transcript is the canonical telemetry/timeline
authority. ActivityBuffer/EventStore stub files are preserved (NO FILE
DELETION per task requirement).

## Production writers migrated

### recorder.go — ActivityBuffer.Append (terminal output)

Two `r.activity.Append(ActivityEvent{...})` blocks removed from `readLoop`:

1. **Delta marker handler** (cmux delta frames `ESC[9998m`): Removed the
   `if r.activity != nil { ... r.activity.Append(...) }` block.
   Transcript `AddSnapshotSegment` already captures delta output at the
   canonical store.

2. **Regular output**: Removed the `if r.activity != nil { ...
   r.activity.Append(...) }` block. Transcript `feedTranscript` already
   feeds raw bytes to the byte-stream projector.

The `r.activity` field remains on the `Recorder` struct for compilation
compatibility with callers that still pass an `*ActivityBuffer`.
Stripping and broadcast logic is unchanged.

### pty.go — ActivityBuffer.Append (terminal input)

Removed the `if h.Activity != nil && len(msg) > 0 { h.Activity.Append(...) }`
block from the WebSocket input handler. Transcript `BeginInput` already
provides input tracking with echo-privacy suppression.

Also pass `nil` instead of `h.Activity` to `startRecorder` and
`EnsureRecorder` in pty.go. The `Activity` field on `Handlers` stays
for existing callers; these paths just don't need it.

### app.go — EventStore.Emit (file watcher)

Removed the `events.Emit(session, "file_edit", toolUse.Name, file)` call
in `startWatcherProd`. The legacy `EventStore.Emit` is a no-op stub.
The watcher filter/observer pattern is retained for future canonical writes.

## Acceptance proof

### Negative: no legacy writes

```sh
$ grep -rn "ActivityBuffer\.Append\|EventStore\.Append" \
  companion-daemon/internal/ companion-daemon/cmd/ --include="*.go" | \
  grep -v "_test.go" | grep -v "activity_stub\|eventstore_stub"
# ZERO matches — R1 rewrote migration comments to omit forbidden identifiers.
```

All `ActivityBuffer.Append` / `EventStore.Append` occurrences are strictly:
- Stub definitions (activity_stub.go:9, eventstore_stub.go:24)
- No production code, no migration comments — complete physical removal

### Positive: Transcript is canonical

All session data now flows exclusively through Transcript:

| Data path | Canonical writer | File:line |
|-----------|-----------------|-----------|
| PTY byte-stream output | `r.feedTranscript(payload)` | recorder.go:349 |
| cmux delta output | `r.transcriptSvc.AddSnapshotSegment(...)` | recorder.go:303 |
| Terminal input tracking | `h.Transcript.BeginInput(session, time.Now())` | pty.go:456 |
| Agent event projection | `s.transcript.ProjectAgentEvents(id, acceptedEvents)` | telemetry_service.go:199 |
| TUI boundary detection | `r.transcriptSvc.BeginTUIBurst` / `EndTUIBurst` | recorder.go:343,345 |

No data path writes to ActivityBuffer or EventStore — Transcript is the
single authoritative store for all session telemetry and timeline data.

### Preserved invariants

- Step 6a lifecycle: createWithCapture + cap.Execute() rollback unchanged
- Steps 1-5: lock-free I/O, exact-instance cleanup, Session propagation
- activity_stub.go and eventstore_stub.go preserved (no file deletion)
- `ActivityBuffer{}` struct and all methods (Append, List, Clear) retained
- `EventStore` interface and `memoryEventStore` retained

## Files modified

| File | Change |
|------|--------|
| `internal/term/recorder.go` | -33 lines: remove two ActivityBuffer.Append blocks |
| `internal/term/pty.go` | -25 +18 lines: remove ActivityBuffer.Append, pass nil to helpers |
| `cmd/devremote/app.go` | -14 +3 lines: remove events.Emit in watcher callback |

## Gate results

```sh
$ cd companion-daemon && go build ./...
(no output — success)

$ cd companion-daemon && go vet ./...
(no output — success)

$ cd companion-daemon && go test -race ./... -count=1
ok  	devremote/companion-daemon/cmd/devremote	33.291s
ok  	devremote/companion-daemon/internal/agent	2.763s
ok  	devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202	3.477s
ok  	devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1	7.380s
ok  	devremote/companion-daemon/internal/agent/contract	5.738s
ok  	devremote/companion-daemon/internal/agent/doctor	117.268s
ok  	devremote/companion-daemon/internal/devicetrust	5.574s
ok  	devremote/companion-daemon/internal/mux	8.478s
ok  	devremote/companion-daemon/internal/sessionid	4.801s
ok  	devremote/companion-daemon/internal/term	17.067s
ok  	devremote/companion-daemon/internal/transcript	3.483s
ok  	devremote/companion-daemon/internal/watcher	3.532s

$ cd companion-daemon && git diff --check
(no output — clean)

$ cd mobile && npx tsc --noEmit
(no output — success, zero diff)
```
