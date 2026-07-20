# PB.4 Evidence — Cmux and Recorder Snapshot Compatibility Removal

**PB.4 IMPL SHA:** `70c1dff3a` (R5: zero cmux across all Go files)
**PA4 ACCEPT SHA:** `74560edd`

## Deleted Components

| Component | Files | Verified |
|-----------|-------|----------|
| cmux adapter | cmux_adapter.go + cmux_delta.go | ZERO refs |
| cmux tests | 6 test files + test data | ZERO refs |
| cmux registration | app.go | ZERO refs |
| cmux ID migration | registry.go (no-op) | ZERO refs |
| snapshotEndMarker | recorder.go | ZERO refs |
| deltaMarker | recorder.go | ZERO refs |
| drainSnapshot | recorder.go | ZERO refs |
| CaptureModeScreenSnapshotDelta | transcript_capture.go | ZERO refs |

## Preserved Recorder Paths

| Path | Status |
|------|--------|
| Byte-stream capture | PASS |
| ANSI clear-screen detection | PASS |
| Transcript projection | PASS |
| Atomic bootstrap/live fan-out | PASS |
| Subscriber management | PASS |
| All non-cmux recorder tests | PASS |

## Zero Consumers Proof — All 5 Surfaces

### Control
```
$ printf "cmux" | grep -E "cmux|Cmux|CMUX"
cmux
(exit 0 — pattern matches)
```

### Production
```
$ grep -rnE "cmux|Cmux|CMUX" --include='*.go' . | grep -v "_test.go"
(empty — zero)
```

### Tests
```
$ grep -rnE "cmux|Cmux|CMUX" --include='*_test.go' .
(empty — zero)
```

### Mobile
```
$ grep -rnE "cmux|Cmux|CMUX" ../mobile/src/ ../mobile/__tests__/
(empty — zero)
```

### Scripts
```
$ grep -rnE "cmux|Cmux|CMUX" scripts/ ../scripts/
(empty — zero)
```

### Packaging
```
$ grep -rnE "cmux|Cmux|CMUX" ../Makefile ../Dockerfile ../*.nix 2>/dev/null
(empty — zero — no packaging files exist)
```

## Snapshot Marker Audit

```
$ grep -rn "snapshotEndMarker\|deltaMarker\|isDeltaMarker\|drainSnapshot\|CaptureModeScreenSnapshotDelta" --include='*.go' .
(empty — all five symbols verified zero across entire repo)
```

## Doc Exclusion List

The following docs contain historical cmux references classified as SUPERSEDED:

| Document | Classification |
|----------|---------------|
| PB_PA4_CONTRACT.md | Historical — pre-PB.4 plan |
| PB_0_INVENTORY.md | Historical — pre-deletion inventory |
| PB_4_EVIDENCE.md | This evidence — tracks removal |
| root docs/PA4_MANAGED_ISOLATION_CONTRACT.md | Historical — frozen PA4 contract |
| root docs/PB_LEGACY_REMOVAL_CONTRACT.md | Historical — PB plan |

These are not blockers — they document the state before/during removal.

## Gates

```
go build ./...                     exit 0
go vet ./...                       exit 0
gofmt -d .                         clean
go test -race ./... -count=1       ALL PASS (11 packages)
cmux/cmux grep across .go files    ZERO
```
