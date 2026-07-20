# PA3 Closeout A Evidence — Recorder Instance-Safe Termination

Status: **EVIDENCE**
Date: 2026-07-20
Implementation SHA: `a1e73da44bb4da60e7e5596e11a887b6f357fd65`
R2: coordinated concurrency — delete in goroutine, poll registry, install B, release A

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

## Tests (5 in recorder_test.go, barrier-controlled concurrency)

### barrierStream helper

Custom `ptyStream` that blocks `Read()` until `release` channel is closed
or `Close()` is called. Enables precise control over exactly when a stale
Recorder receives EOF — the replacement is installed BEFORE the stale EOF
fires.

### Test cases

| Test | What it proves |
|------|---------------|
| `TestRecorder_CloseoutA_BarrierControlledStream` | Install B BEFORE releasing A's EOF barrier. Prove B stays current, no terminated flag for stale A, stale EOF is discarded by instance guard. |
| `TestRecorder_CloseoutA_ConcurrentStaleOps/stale-EOF` | Coordinated goroutine: stale A's EOF fires concurrently with B alive. B remains current in registry. |
| `TestRecorder_CloseoutA_ConcurrentStaleOps/stale-Stop` | Coordinated goroutine: stale A's Stop() fires concurrently with B alive. B remains current — stale Stop discarded. |
| `TestRecorder_CloseoutA_MatchingRecordsTermination` | Positive control: current Recorder correctly records its own termination. |
| `TestRecorder_CloseoutA_SameIDRace` | 20 concurrent goroutines create+delete same ID. Final create must succeed — no deadlock, race, or state corruption. |

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
