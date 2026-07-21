# PB.7 Evidence — Automated Closeout and Device Gate

**Document SHA (evidence HEAD):** `5ad609a72`
**Production Candidate SHA:** `82e550e9c`
**PA4 ACCEPT SHA:** `74560edd`
**PB Ancestry Baseline SHA:** `abe4df1d6`
**PB ACCEPT SHA:** UNSET
**Status:** ALL NON-DEVICE PB GATES ACCEPTED — AWAITING_DEVICE_GATE

## Ancestry Verification

```
$ git merge-base --is-ancestor 74560edd8 HEAD && echo "PA4 ACCEPT: ANCESTOR OK"
PA4 ACCEPT: ANCESTOR OK

$ git merge-base --is-ancestor abe4df1d6 HEAD && echo "PB BASELINE: ANCESTOR OK"
PB BASELINE: ANCESTOR OK
```

## Physical Device Matrix

**Physical device matrix was NOT executed.** This gate requires:
- Physical iOS device (iPhone/iPad) with Orca mobile app installed
- Physical macOS device running companion-daemon
- Cloudflare tunnel connectivity between daemon and mobile app
- Manual verification of session CRUD, WebSocket streaming, and agent display

This is the sole remaining gate before PB ACCEPT SHA can be assigned.

## Automated Gate Results

```
go build ./...                                    exit 0
go vet ./...                                      exit 0
gofmt -l .                                        0 files (clean)
git diff --check abe4df1d6..HEAD                   exit 0
go test -race ./... -count=1                       ALL PASS (11 packages)
cd mobile && npx tsc --noEmit                      clean
cd mobile && npx jest --runInBand                  451/451 pass, 34 suites
```

### Test Package Details

```
cmd/devremote                                     PASS
internal/agent                                    PASS
internal/agent/adapters/claude/v2_1_202           PASS
internal/agent/adapters/codex/v0_144_1            PASS
internal/agent/contract                           PASS
internal/agent/doctor                             PASS
internal/devicetrust                              PASS
internal/sessionid                                PASS
internal/term                                     PASS
internal/transcript                               PASS
internal/watcher                                  PASS
```

## Zero-Consumer Static Scans — All 5 Surfaces

### Production (non-test Go)

```
$ grep -rnE "mux\.Registry|mux\.Adapter[^a-zA-Z]|mux\.Session[^I]" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "tmux|Tmux|TMUX|cmux|Cmux|CMUX|localpty|LocalPTY" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "GetRecorder|EnsureRecorder|RegistryFromContext|WithRegistry" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "ManagedPTYLauncher |v1Bridge|NewV1FromOld|v1Handle" --include='*.go' . | grep -v "_test.go"
scripts/archgate.go:47 — archgate enforcement script (the gate itself, not a violation)

$ grep -rnE "ManualLink|ManualEvidence|SourceManualLink|LinkedLogResolver|ResolveLink" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "ResolveAgentLog|GeminiResolver|CodexResolver|ClaudeResolver|TermAgentDetector" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "snapshotEndMarker|deltaMarker|isDeltaMarker|drainSnapshot|CaptureModeScreenSnapshotDelta" --include='*.go' .
(empty)

$ grep -rnE "observedAdapter|ObservedAdapter" --include='*.go' . | grep -v "_test.go"
(empty)
```

### Tests

```
$ grep -rnE "mux\.Registry|mux\.Adapter|mux\.Session|tmux|cmux|localpty|GetRecorder|RegistryFromContext|v1Bridge|NewV1FromOld|ManualLink|ResolveAgentLog|snapshotEndMarker|deltaMarker|drainSnapshot" --include='*_test.go' .
(empty)
```

### Mobile

```
$ grep -rnE "tmux|cmux|localpty|mux\.Registry|mux\.Adapter|mux\.Session|ManualLink|manualLink|observerAdapter|ObservedAdapter" mobile/src/
(empty)

$ grep -rn "agentKind.*===" mobile/src/
(empty — no vendor branching)

$ grep -rn "opt\.id === 'approve'|opt\.id === 'reject'" mobile/src/
(empty — no ID inference)
```

### Scripts & Packaging

```
$ grep -rnE "tmux|cmux|localpty|mux\.Registry|mux\.Adapter" scripts/ Makefile
(empty)
```

## Deletion Gate

### Mux Package

```
$ ls internal/mux/
testdata/

$ ls internal/mux/testdata/
(empty directory — zero files)

$ grep -rn "devremote/companion-daemon/internal/mux" --include='*.go' .
(empty — zero import references)
```

The `internal/mux/` directory contains only an empty `testdata/` skeleton.
All `.go` source files and import references have been removed.

### Deleted Components

| Component | Removed In | SHA |
|-----------|-----------|-----|
| tmux adapter + tests | PB.3 | `2987b6fe1` |
| cmux adapter + tests + delta | PB.4 | `ed9bb5468` |
| localpty adapter + flag | PB.1 | `c0f5664d0` |
| ManualLink / ResolveLink | PB.2a | `359e3853e` |
| ObserverAdapter / agent resolvers | PB.2b | `3c65b5990` |
| Snapshot delta markers | PB.4 | `ed9bb5468` |
| mux.Registry (production) | PB.5b-T2 | `0f0d57f30` |
| mux package (.go files) | PB.5b-T3 | `c2c0f542a` |
| OwnedPTYRuntime + lifecycle legacy impl | PB.5b-T3 | `c2c0f542a` |

### Deleted-Test Inventory

Full deleted-test migration map in `docs/PB5_TASK3_EVIDENCE.md` (PB.5b Task 3 —
Physical Deletion). All removed tests have V1 successors in
`internal/term/pb5_v1_migration_test.go`, `internal/term/step6a_test.go`,
and related managed-test suites. No test coverage gap.

## Known Harmless Retained Patterns

| Pattern | Location | Rationale |
|---------|----------|-----------|
| `ManagedPTYLauncher ` | `scripts/archgate.go:47` | Archgate enforcement script |
| `github.com/gorilla/websocket` | `internal/term/pty.go:14` | WebSocket library for terminal WS handler |
| `adapter !== 'native'` | `mobile/src/components/AgentCard.tsx:104` | Harmless display filter |
| `observedAt` field | `mobile/src/lib/*.ts` | Valid telemetry timestamp field |
| Empty `internal/mux/testdata/` | `internal/mux/testdata/` | Directory skeleton, zero files |

## PB Wave Ledger

| Wave | SHA | Description | Status |
|------|-----|-------------|--------|
| PB.0 | `321cd1a84` | Consumer inventory | VERIFIED |
| PB.1 | `c0f5664d0` | Localpty removal | VERIFIED |
| PB.2a | `359e3853e` | Manual link removal | VERIFIED |
| PB.2b | `3c65b5990` | Discovery/observer removal | VERIFIED |
| PB.3 | `2987b6fe1` | Tmux removal | VERIFIED |
| PB.4 | `ed9bb5468` | Cmux/snapshot removal | VERIFIED |
| PB.5a | `4ed0d3dc3` | V1 launcher cutover | VERIFIED |
| PB.5b-T2 | `0f0d57f30` | Consumer migration (zero mux imports) | VERIFIED |
| PB.5b-T3 | `c2c0f542a` | Physical deletion (mux directory + legacy impl) | VERIFIED |
| PB.6 | `5ad609a72` | Re-verify — all surfaces + gates clean | VERIFIED |
