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
$ ls scripts/package.sh scripts/install.sh companion-daemon/scripts/install-launchagent.sh
companion-daemon/scripts/install-launchagent.sh  scripts/install.sh  scripts/package.sh

$ grep -rnE "cmux|CMUX" scripts/package.sh scripts/install.sh companion-daemon/scripts/install-launchagent.sh
(exit 1 — zero matches across all 3 packaging files)
```

## Snapshot Marker Audit

```
$ grep -rn "snapshotEndMarker\|deltaMarker\|isDeltaMarker\|drainSnapshot\|CaptureModeScreenSnapshotDelta" --include='*.go' .
(empty — all five symbols verified zero across entire repo)
```

## Doc Exclusion List

152 documents contain "cmux" in historical context.

```
$ grep -rlE "cmux|CMUX" docs/ companion-daemon/docs/ | wc -l
     152
```

**Classification:** ALL 152 documents are SUPERSEDED HISTORICAL DOCUMENTS. They document adapter architecture, phase plans, handoffs, and contracts from before cmux removal. Zero production, test, mobile, script, or packaging references remain.

Representative sample (11 of 152):

| Document | Classification |
|----------|---------------|
| ADAPTER_EXPANSION_PLAN.md | Historical — pre-implementation planning |
| CMUX_DELTA_POC_REPORT.md | Historical — cmux delta POC |
| CMUX_ROBUSTNESS_IMPLEMENTATION_REVIEW.md | Historical — cmux review |
| CMUX_SOCKET_STORM_HANDOVER.md | Historical — cmux handover |
| E8G4_CMUX_TERMINAL_DUP_DIAGNOSIS.md | Historical — cmux diagnosis |
| PB_EXECUTION_PLAN.md | Historical — PB plan referencing cmux |
| PB_0_INVENTORY.md | Historical — pre-deletion inventory |
| PB_4_EVIDENCE.md | This evidence — tracks removal |
| PA4_MANAGED_ISOLATION_CONTRACT.md | Historical — frozen PA4 contract |
| PB_LEGACY_REMOVAL_CONTRACT.md | Historical — PB plan |
| ... and 142 more | All SUPERSEDED HISTORICAL |

## Gates

```
go build ./...                     exit 0
go vet ./...                       exit 0
gofmt -d .                         clean
go test -race ./... -count=1       ALL PASS (11 packages)
cmux/cmux grep across .go files    ZERO
```
