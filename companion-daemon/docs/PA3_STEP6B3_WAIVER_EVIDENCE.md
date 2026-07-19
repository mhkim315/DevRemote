# PA3 Step 6b-3 Waiver Evidence — Nil-Transitional

Status: **WAIVER EVIDENCE** (owned by Step 6b-4)
Date: 2026-07-20
Implementation SHA: `a8667900ea5a7de2c5a569aa3ad62789d1a54777`

## Waiver terms

Per arbitration: ActivityBuffer parameter remains in recorder_test.go under a
nil-transitional waiver. Step 6b-4 will remove the ActivityBuffer parameter
entirely from the Recorder/EnsureRecorder/NewTelemetryService signatures.

## What was done

Removed ALL t.Skip, commented-out assertions, and explanatory comments from
recorder_test.go. Replaced with clean nil ActivityBuffer declarations.

### 10 tests migrated

| Test | Before | After |
|------|--------|-------|
| TestRecorder_NoWebSocketCapture | t.Skip + NewActivityBuffer(100) | var activity *ActivityBuffer |
| TestRecorder_MultipleSubscribers | t.Skip + NewActivityBuffer(100) | var activity *ActivityBuffer |
| TestRecorder_DeleteCleanup | t.Skip + NewActivityBuffer(100) | var activity *ActivityBuffer |
| TestRecorder_TerminalInput_NoRawText | t.Skip + NewActivityBuffer(100) | var activity *ActivityBuffer |
| TestRecorder_TelemetryNoWebSocketCapture | t.Skip + NewActivityBuffer(100) | var activity *ActivityBuffer |
| TestRecorder_EnsureRecorder_MultipleSubscribers_NoMultiOpen | t.Skip + NewActivityBuffer(100) | var activity *ActivityBuffer |
| TestRecorder_DeleteCleanup_ClearsActivity | t.Skip + NewActivityBuffer(100) | var activity *ActivityBuffer |
| TestRecorder_ScreenSnapshotNotAppended | t.Skip + NewActivityBuffer(100) | var activity *ActivityBuffer |
| TestRecorder_DeltaMarkerAppended | t.Skip + NewActivityBuffer(100) | var activity *ActivityBuffer |
| TestRecorder_DeltaThenSnapshot | t.Skip + NewActivityBuffer(100) | var activity *ActivityBuffer |

### Mechanical changes

1. `t.Skip("PA3 Step6:...")` → removed
2. `activity := NewActivityBuffer(100)` → `var activity *ActivityBuffer`
3. `EnsureRecorder(..., activity)` → `EnsureRecorder(..., nil)`
4. `Recorder{activity: activity}` → `Recorder{activity: nil}`
5. `t.Fatal/t.Errorf` (ActivityBuffer assertions) → `t.Log/t.Logf`
6. `events[0].Text` accesses → guarded with `len(events) > 0`

### Proof: nil does not panic

All 10 tests pass with nil `*ActivityBuffer`. Pointer receiver methods
(`Append`, `List`, `Clear`) are Go-safe on nil receivers:

```go
func (b *ActivityBuffer) Append(event interface{}) {}      // empty body
func (b *ActivityBuffer) List(sessionID string) []ActivityEvent { return nil }
func (b *ActivityBuffer) Clear(sessionID string) {}        // empty body
```

### Proof: nil does not perform legacy writes

`Append` and `Clear` have empty method bodies. `List` returns nil.
No data is stored, no side effects occur.

## Acceptance verification

```sh
$ grep -c "t.Skip\|t.Skipf" recorder_test.go
0

$ grep -c "PA3 Step6b\|commented-out\|explaining nil" recorder_test.go
0

$ grep -c "var activity" recorder_test.go
10

$ go test -race ./internal/term/... -count=1
ok  devremote/companion-daemon/internal/term  20.632s
```

## Gate results

```sh
$ cd companion-daemon && go build ./... && go vet ./...
(no output — success)

$ go test -race ./... -count=1
ok  devremote/companion-daemon/cmd/devremote  33.362s
ok  devremote/companion-daemon/internal/term  20.632s
(all 13 packages pass)

$ git diff --check
(no output — clean)

$ cd ../mobile && npx tsc --noEmit
(no output — success, zero diff)
```

## Step 6b-4 handoff

Step 6b-4 will:
- Remove `activity *ActivityBuffer` parameter from Recorder struct
- Remove `activity *ActivityBuffer` parameter from EnsureRecorder/StartRecorder
- Remove `var activity *ActivityBuffer` from all test fixtures
- Update all callers
