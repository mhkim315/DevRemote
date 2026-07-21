# PB.7 Evidence — Automated Closeout and Device Gate

**Pre-Device Candidate SHA:** `9b75c1e4a`
**PB.7 Evidence HEAD:** `780f4bc66`
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
- Physical Android device (Samsung SM-S926N) with Orca mobile app installed
- Physical macOS device running companion-daemon
- Cloudflare tunnel connectivity between daemon and mobile app
- Manual verification of session CRUD, WebSocket streaming, and agent display

This is the sole remaining gate before PB ACCEPT SHA can be assigned.

## Automated Gate Results

```
go build ./...                                    exit 0
go vet ./...                                      exit 0
gofmt -l .                                        0 files (clean)
git diff --check                                    exit 0
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

### Control
```
$ printf "tmux" | grep -E "tmux|Tmux|TMUX"
tmux
(exit 0 — pattern matches)
```

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

$ grep -rnE "ManualLink|ManualEvidence|ResolveLink|ResolveAgentLog|snapshotEndMarker|deltaMarker|drainSnapshot|observedAdapter" --include='*.go' . | grep -v "_test.go"
(empty)
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

## Pre-Device Packet ACCEPT SHAs

| Packet | ACCEPT SHA | Description |
|--------|-----------|-------------|
| QR | `b55780c7f` | Half-block ANSI renderer + secure PNG fallback |
| Input-A | `e28964875` | Effective permission + read-only UX |
| Input-B | `9b75c1e4a` | Versioned control-request protocol + delivery semantics |

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
| PB.5b-T2 | `0f0d57f30` | Consumer migration | VERIFIED |
| PB.5b-T3 | `c2c0f542a` | Physical deletion | VERIFIED |
| PB.6 | `9b75c1e4a` | Re-verify — all surfaces clean | VERIFIED |
