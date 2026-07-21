# PB.7 Evidence — Automated Closeout and Device Gate

**PB Candidate SHA:** `82e550e9c`
**PA4 ACCEPT SHA:** `74560edd`
**PB Baseline SHA:** `abe4df1d6`
**PB ACCEPT SHA:** UNSET
**Status:** AWAITING_DEVICE_GATE

## Ancestry Verification

```
$ git merge-base --is-ancestor 74560edd8 HEAD && echo "PA4 ACCEPT: ANCESTOR OK"
PA4 ACCEPT: ANCESTOR OK

$ git merge-base --is-ancestor abe4df1d6 HEAD && echo "PB BASELINE: ANCESTOR OK"
PB BASELINE: ANCESTOR OK
```

## Automated Gate Results

```
go build ./...                                    exit 0
go vet ./...                                      exit 0
gofmt -d .                                        0 lines (clean)
git diff --check                                   exit 0
go test -race ./... -count=1                       ALL PASS (11 packages)
cd mobile && npx tsc --noEmit                      clean
cd mobile && npx jest                              451/451 pass, 34 suites
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

Note: `internal/mux` no longer exists — removed in PB.5b-T3 (`c2c0f542a`).

## Zero-Consumer Static Scans

### Control
```
$ printf "tmux" | grep -E "tmux|Tmux|TMUX"
tmux
(exit 0 — pattern matches)
```

### Production (non-test Go)
```
$ grep -rnE "tmux|Tmux|TMUX" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "cmux|Cmux|CMUX" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "localpty|LocalPTY|EnableLocalPTY" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "mux\.Registry|mux\.Adapter[^a-zA-Z]|mux\.Session[^I]" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "ManualLink|ManualEvidence|SourceManualLink|LinkedLogResolver|ResolveLink" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "ResolveAgentLog|GeminiResolver|CodexResolver|ClaudeResolver|TermAgentDetector" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "snapshotEndMarker|deltaMarker|isDeltaMarker|drainSnapshot|CaptureModeScreenSnapshotDelta" --include='*.go' .
(empty)

$ grep -rnE "GetRecorder|EnsureRecorder|observedAdapter|ObservedAdapter" --include='*.go' . | grep -v "_test.go"
(empty)
```

### Tests
```
$ grep -rnE "tmux|cmux|localpty|ManualLink|ResolveAgentLog|snapshotEndMarker|deltaMarker|drainSnapshot" --include='*_test.go' .
(empty — all prohibited symbols removed from test files)
```

### Mobile TypeScript
```
$ grep -rnE "tmux|cmux|localpty" mobile/src/
(empty)

$ grep -rnE "mux\.Registry|mux\.Adapter|mux\.Session" mobile/src/
(empty)

$ grep -rnE "observerAdapter|ObservedAdapter|ManualLink|manualLink" mobile/src/
(empty)

$ grep -rn "agentKind.*===" mobile/src/
(empty — no vendor branching)

$ grep -rn "opt\.id === 'approve'|opt\.id === 'reject'" mobile/src/
(empty — no ID inference)
```

### Scripts & Packaging
```
$ grep -rnE "tmux|cmux|localpty" scripts/ Makefile
(empty)
```

## Deleted Components

| Component | Removed In | Status |
|-----------|-----------|--------|
| tmux adapter + tests | PB.3 (`2987b6fe1`) | ZERO refs |
| cmux adapter + tests + delta | PB.4 (`ed9bb5468`) | ZERO refs |
| localpty adapter + flag | PB.1 (`c0f5664d0`) | ZERO refs |
| ManualLink / ResolveLink | PB.2a (`359e3853e`) | ZERO refs |
| ObserverAdapter / agent resolvers | PB.2b (`3c65b5990`) | ZERO refs |
| Snapshot delta markers | PB.4 (`ed9bb5468`) | ZERO refs |
| mux.Registry (production) | PB.5b-T2 (`0f0d57f30`) | ZERO refs |
| mux package (physical) | PB.5b-T3 (`c2c0f542a`) | Directory missing |
| V1 launcher wiring | PB.5a (`4ed0d3dc3`) | Complete |
| Mobile/daemon cleanup | PB.6 (`82e550e9c`) | Complete |

## Known Harmless Retained Patterns

| Pattern | Location | Rationale |
|---------|----------|-----------|
| `github.com/gorilla/websocket` | `internal/term/pty.go:14` | Standard WebSocket library for terminal WS handler |
| `adapter !== 'native'` | `mobile/src/components/AgentCard.tsx:104` | Harmless display filter, documented in CLAUDE.md |
| `observedAt` field | `mobile/src/lib/*.ts` | Valid telemetry timestamp field |

## PB Wave Ledger (FINAL — ACCEPTED)

| Wave | SHA | Description |
|------|-----|-------------|
| PB.0 | `321cd1a84` | Consumer inventory |
| PB.1 | `c0f5664d0` | Localpty removal (R2) |
| PB.2a | `359e3853e` | Manual link removal (R2) |
| PB.2b | `3c65b5990` | Discovery/observer removal (R3) |
| PB.3 | `2987b6fe1` | Tmux removal (R4) |
| PB.4 | `ed9bb5468` | Cmux/snapshot removal (R8) |
| PB.5a | `4ed0d3dc3` | V1 launcher cutover (R6) |
| PB.5b-T2 | `0f0d57f30` | Consumer migration (zero mux imports) |
| PB.5b-T3 | `c2c0f542a` | Physical deletion (mux directory gone) |
| PB.6 | `82e550e9c` | Mobile/daemon final cleanup |
