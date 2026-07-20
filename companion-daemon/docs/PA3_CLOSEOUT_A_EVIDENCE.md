# PA3 Closeout A Evidence — Recorder Instance-Safe Termination

Status: **EVIDENCE**
Date: 2026-07-20
Implementation SHA: `d5d7fb4217e03d1a7da65993da61163200d04663`

## Root defect

Stale Recorder readLoop can EOF-write `terminated=true` into the global
recorder registry, blocking same-ID replacement. The `StartRecorder`
function checks `recorderRegistry.terminated[sessionID]` and returns nil
if true — a stale Recorder from a deleted/replaced session can set this
flag, preventing the replacement from starting.

## Fix

Two instance-safe guards in `internal/term/recorder.go`:

### 1. readLoop EOF termination (line 258-266)

```go
// PA3 Closeout A: before marking terminated, atomically prove
// the Registry still points to THIS exact Recorder instance.
recorderRegistry.mu.Lock()
if existing, ok := recorderRegistry.recorders[r.sessionID]; ok && existing == r {
    recorderRegistry.terminated[r.sessionID] = true
}
recorderRegistry.mu.Unlock()
```

### 2. Stop() registry removal (line 165-171)

```go
// PA3 Closeout A: instance-safe — only remove from registry if
// this Recorder is still the current one.
recorderRegistry.mu.Lock()
if existing, ok := recorderRegistry.recorders[r.sessionID]; ok && existing == r {
    delete(recorderRegistry.recorders, r.sessionID)
}
recorderRegistry.mu.Unlock()
```

## Tests (5 new in recorder_test.go)

| Test | What it proves |
|------|---------------|
| `TestRecorder_CloseoutA_DeleteRecreateSameID` | Same-ID delete+recreate produces distinct instances, B alive |
| `TestRecorder_CloseoutA_StaleEOFCannotMarkReplacement` | Stale A's EOF does not set terminated=true — B creates successfully |
| `TestRecorder_CloseoutA_StaleStopCannotAffectReplacement` | Stale A's Stop() does not remove B from registry |
| `TestRecorder_CloseoutA_MatchingRecordsTermination` | Current Recorder still records termination correctly |
| `TestRecorder_CloseoutA_SameIDRace` | 20 rapid delete+recreate iterations — no deadlock, no panic, final create succeeds |

## Gate results

```
$ cd companion-daemon && go build ./... && go vet ./...
(no output - success)

$ go test -race ./... -count=1
ok  devremote/companion-daemon/cmd/devremote  33.468s
ok  devremote/companion-daemon/internal/agent  2.898s
ok  devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202  4.555s
ok  devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1  8.985s
ok  devremote/companion-daemon/internal/agent/contract  4.728s
ok  devremote/companion-daemon/internal/agent/doctor  126.000s
ok  devremote/companion-daemon/internal/devicetrust  5.873s
ok  devremote/companion-daemon/internal/mux  8.459s
ok  devremote/companion-daemon/internal/sessionid  2.258s
ok  devremote/companion-daemon/internal/term  19.765s
ok  devremote/companion-daemon/internal/transcript  2.572s
ok  devremote/companion-daemon/internal/watcher  2.924s
(ALL 12 packages pass)

$ git diff --check
(no output - clean)
```
