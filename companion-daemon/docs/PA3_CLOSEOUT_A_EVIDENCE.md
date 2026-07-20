# PA3 Closeout A Evidence — Recorder Instance-Safe Termination

Status: **EVIDENCE**
Date: 2026-07-20
Implementation SHA: `2419735677d3112c39159420e46b89957388befc`
R3: arbitration-aligned test design — split observation/release barrier, stale-EOF and stale-Stop replacement guards

## Recovery classification

**Implementation recovered after worker disconnect.** The original Executor
(`term_8825de59`) committed the IMPL and disconnected before sending
`worker_done`. The implementation was recovered as-is — no amendments,
rebases, or resets. The dispatch `ctx_d10f306e7c0d` remains failed/lost;
this evidence-only task completes the record with fresh gate results.

## Root defect

Stale Recorder readLoop can EOF-write `terminated=true` into the global
recorder registry, blocking same-ID replacement. The `StartRecorder`
function checks `recorderRegistry.terminated[sessionID]` and returns nil
if true — a stale Recorder from a deleted/replaced session can set this
flag, preventing the replacement from starting.

## Fix

Two instance-safe guards in `internal/term/recorder.go` (unchanged from R2):

### 1. readLoop EOF termination

```go
// PA3 Closeout A: before marking terminated, atomically prove
// the Registry still points to THIS exact Recorder instance.
recorderRegistry.mu.Lock()
if existing, ok := recorderRegistry.recorders[r.sessionID]; ok && existing == r {
    recorderRegistry.terminated[r.sessionID] = true
}
recorderRegistry.mu.Unlock()
```

### 2. Stop() registry removal

```go
// PA3 Closeout A: instance-safe — only remove from registry if
// this Recorder is still the current one.
recorderRegistry.mu.Lock()
if existing, ok := recorderRegistry.recorders[r.sessionID]; ok && existing == r {
    delete(recorderRegistry.recorders, r.sessionID)
}
recorderRegistry.mu.Unlock()
```

## R3 Arbitration — Test redesign

### Arbitration

`task_977aa917aff5` (ACCEPTED). R2 blockers:

1. `barrierStream.Close()` closes release, unblocking Read immediately —
   `DeleteRecorder` calls Stop→Close, A EOFs before B installed (vacuous stale-EOF)
2. `SameIDRace` launches 20 uncontrolled creators, flags expected nil as
   t.Error (inherently flaky)

### Removals (from R2)

- `TestRecorder_CloseoutA_SameIDRace` (lines 1003-1047)
- `TestRecorder_CloseoutA_BarrierControlledStream` (entire)
- `TestRecorder_CloseoutA_StaleFinalizer` (entire)
- Old `barrierStream` (lines 777-801)

### New barrierStream — split observation/release

```go
type barrierStream struct {
    release     chan struct{}
    closeCalled chan struct{}
    releaseOnce sync.Once
    closeOnce   sync.Once
}

func newBarrierStream() *barrierStream {
    return &barrierStream{release: make(chan struct{}), closeCalled: make(chan struct{})}
}
func (s *barrierStream) Read([]byte) (int, error)    { <-s.release; return 0, io.EOF }
func (s *barrierStream) Write(p []byte) (int, error) { return len(p), nil }
func (s *barrierStream) Close() error                { s.closeOnce.Do(func() { close(s.closeCalled) }); return nil }
func (s *barrierStream) Resize(int, int) error       { return nil }
func (s *barrierStream) Release()                    { s.releaseOnce.Do(func() { close(s.release) }) }
```

`Close()` no longer unblocks Read — it signals `closeCalled` only. `Release()`
controls when the barrier drops. This separates the observation of Close
(proof that `DeleteRecorder` called Stop→Close) from the release timing
(the test controls when the stale A actually reads EOF).

## Tests (3 in recorder_test.go, barrier-controlled concurrency)

| Test | What it proves |
|------|---------------|
| `TestRecorder_CloseoutA_StaleEOFCannotMarkReplacement` | `DeleteRecorder` in goroutine, observe `closeCalled` (not released yet), assert A removed from registry while readLoop still blocked, THEN install B, THEN release A. Assert B identity preserved + no terminated flag. |
| `TestRecorder_CloseoutA_StaleStopCannotAffectReplacement` | `recA.unregisterSelf()` for deterministic stale precondition, create B, then run stale `A.Stop()`, observe `closeCalled`, assert B untouched, release, assert B still current + no terminated flag. |
| `TestRecorder_CloseoutA_MatchingRecordsTermination` | Positive control: current Recorder correctly records its own termination (unchanged from R2). |

`TestRecorder_StreamOnlyInputFallback` retained unchanged per arbitration.

No production seam — tests use package-local `unregisterSelf` (same-package test).

## Gate results

### Build + vet

```
$ cd companion-daemon && go build ./... && go vet ./...
(no output — success)
```

### Focused stability: StaleEOFCannotMarkReplacement ×20

```
$ go test -race ./internal/term -run "TestRecorder_CloseoutA_StaleEOFCannotMarkReplacement" -count=20
ok  	devremote/companion-daemon/internal/term	1.401s
(20/20 PASS)
```

### Full CloseoutA suite

```
$ go test -race ./internal/term -run "TestRecorder_CloseoutA_" -count=1 -v
=== RUN   TestRecorder_CloseoutA_StaleEOFCannotMarkReplacement
--- PASS: TestRecorder_CloseoutA_StaleEOFCannotMarkReplacement (0.00s)
=== RUN   TestRecorder_CloseoutA_StaleStopCannotAffectReplacement
--- PASS: TestRecorder_CloseoutA_StaleStopCannotAffectReplacement (0.00s)
=== RUN   TestRecorder_CloseoutA_MatchingRecordsTermination
--- PASS: TestRecorder_CloseoutA_MatchingRecordsTermination (0.00s)
PASS
ok  	devremote/companion-daemon/internal/term	1.326s
```

### Full backend suite

```
$ go test -race ./... -count=1
ok  	devremote/companion-daemon/cmd/devremote	33.391s
?   	devremote/companion-daemon/cmd/signald	[no test files]
ok  	devremote/companion-daemon/internal/agent	2.330s
ok  	devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202	3.094s
ok  	devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1	10.028s
ok  	devremote/companion-daemon/internal/agent/contract	3.290s
ok  	devremote/companion-daemon/internal/agent/doctor	122.691s
ok  	devremote/companion-daemon/internal/devicetrust	6.002s
?   	devremote/companion-daemon/internal/models	[no test files]
ok  	devremote/companion-daemon/internal/mux	9.223s
ok  	devremote/companion-daemon/internal/sessionid	3.131s
ok  	devremote/companion-daemon/internal/term	20.499s
ok  	devremote/companion-daemon/internal/transcript	3.494s
ok  	devremote/companion-daemon/internal/watcher	3.413s
(ALL 12 packages PASS)
```

### Format check

```
$ git diff --check
(no output — clean)
```

### Ancestry

```
IMPL 241973567 → parent 4327ab1b8 (R2 EVID)
Diff: 1 file — companion-daemon/internal/term/recorder_test.go (+96/-198)
```
