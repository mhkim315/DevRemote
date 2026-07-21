# PB.6 Evidence — Re-Verify at 9b75c1e4a

**Re-Verify Candidate SHA:** `9b75c1e4a`
**PB.6 Evidence HEAD:** `780f4bc66`
**PA4 ACCEPT SHA:** `74560edd`
**PB Baseline SHA:** `abe4df1d6`

## Automated Gate Results

```
go build ./...                                    exit 0
go vet ./...                                      exit 0
gofmt -l .                                        0 files (clean)
go test -race ./... -count=1                       ALL PASS (11 packages)
cd mobile && npx tsc --noEmit                      clean
cd mobile && npx jest --runInBand                  482/482 pass, 35 suites
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

### 1. Production (non-test Go)

```
$ grep -rnE "mux\.Registry|mux\.Adapter[^a-zA-Z]|mux\.Session[^I]" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "tmux|Tmux|TMUX|cmux|Cmux|CMUX|localpty|LocalPTY" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "GetRecorder|EnsureRecorder|RegistryFromContext|WithRegistry" --include='*.go' . | grep -v "_test.go"
(empty)

$ grep -rnE "ManagedPTYLauncher |v1Bridge|NewV1FromOld|v1Handle" --include='*.go' . | grep -v "_test.go"
scripts/archgate.go:47 — archgate enforcement script (the gate itself, not a violation)
```

### 2. Tests

```
$ grep -rnE "mux\.Registry|mux\.Adapter|mux\.Session|tmux|cmux|localpty|GetRecorder|v1Bridge|NewV1FromOld|ManualLink|ResolveAgentLog|snapshotEndMarker|deltaMarker|drainSnapshot" --include='*_test.go' .
(empty)
```

### 3. Mobile (TypeScript)

```
$ grep -rnE "tmux|cmux|localpty|mux\.Registry|mux\.Adapter|mux\.Session|ManualLink|observerAdapter|ObservedAdapter" mobile/src/
(empty)

$ grep -rn "agentKind.*===" mobile/src/
(empty — no vendor branching)

$ grep -rn "opt\.id === 'approve'|opt\.id === 'reject'" mobile/src/
(empty — no ID inference)
```

### 4. Scripts

```
$ grep -rnE "tmux|cmux|localpty|mux\.Registry|mux\.Adapter" scripts/
(empty)
```

### 5. Packaging

```
$ grep -rnE "tmux|cmux|localpty" Makefile
(empty)
```

## PB Wave Ledger

| Wave | SHA | Status |
|------|-----|--------|
| PB.0 | `321cd1a84` | ACCEPTED — Consumer inventory |
| PB.1 | `c0f5664d0` | ACCEPTED — Localpty removal |
| PB.2a | `359e3853e` | ACCEPTED — Manual link removal |
| PB.2b | `3c65b5990` | ACCEPTED — Discovery/observer removal |
| PB.3 | `2987b6fe1` | ACCEPTED — Tmux removal |
| PB.4 | `ed9bb5468` | ACCEPTED — Cmux/snapshot removal |
| PB.5a | `4ed0d3dc3` | ACCEPTED — V1 launcher cutover |
| PB.5b-T2 | `0f0d57f30` | ACCEPTED — Consumer migration |
| PB.5b-T3 | `c2c0f542a` | ACCEPTED — Physical deletion |
| PB.6 | `9b75c1e4a` | RE-VERIFIED — All surfaces clean |
