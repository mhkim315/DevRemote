# PB.4 Evidence — Cmux and Recorder Snapshot Compatibility Removal

**PB.4 IMPL SHA:** `03736b0f1`
**PA4 ACCEPT SHA:** `74560edd`

## Deleted Components

| Component | Files |
|-----------|-------|
| cmux adapter | cmux_adapter.go + cmux_delta.go |
| cmux tests | 6 test files + test data |
| cmux registration | app.go |
| cmux ID migration | registry.go (MigrateLegacyID→no-op) |
| snapshotEndMarker | recorder.go |
| deltaMarker | recorder.go |
| drainSnapshot | recorder.go |
| CaptureModeScreenSnapshotDelta | transcript_capture.go |

## Preserved Recorder Paths

| Path | Status |
|------|--------|
| Byte-stream capture | PASS |
| ANSI clear-screen detection | PASS |
| Transcript projection | PASS |
| Atomic bootstrap/live fan-out | PASS |
| Subscriber management | PASS |
| All recorder tests | PASS (cmux-specific skipped) |

## Zero Consumers Proof

### Control
```
$ printf "cmux" | grep -E "cmux|Cmux|CMUX"
cmux
(exit 0)
```

### Production
```
$ grep -rnE "cmux|Cmux|CMUX" --include='*.go' . | grep -v "_test.go"
(only in adapter:id grammar examples + comments)
```

### Tests
```
$ grep -rnE "cmuxAdapter|CmuxError|snapshotEndMarker|deltaMarker|CaptureModeScreenSnapshotDelta" --include='*_test.go' .
(empty — zero)
```

## Gates
```
go build ./...                     exit 0
go vet ./...                       exit 0
go test -race ./... -count=1       ALL PASS (12 packages)
```
