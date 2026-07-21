# PB.7 Evidence — Automated Closeout and Device Gate

**Pre-Device Candidate SHA:** `9b75c1e4a`
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

### Production (non-test Go)

```
$ grep -rnE "mux\.Registry|mux\.Adapter|mux\.Session|tmux|cmux|localpty|GetRecorder|EnsureRecorder|RegistryFromContext|WithRegistry|v1Bridge|NewV1FromOld|v1Handle|ManualLink|ResolveAgentLog|snapshotEndMarker|deltaMarker|drainSnapshot|observedAdapter|ObservedAdapter" --include='*.go' . | grep -v "_test.go"
scripts/archgate.go:47 — archgate enforcement script (the gate itself, not a violation)
```

### Tests

```
(empty — all prohibited symbols removed)
```

### Mobile

```
(empty — no prohibited symbols, no vendor branching, no ID inference)
```

### Scripts & Packaging

```
(empty)
```

## Pre-Device Packet Accepted SHAs

| Packet | IMPL SHA | EVID SHA | Description |
|--------|----------|----------|-------------|
| QR | `b55780c7f` | `0ce53d9ad` | Half-block ANSI renderer + secure PNG fallback |
| Input-A | `e28964875` | — | Effective permission + read-only UX |
| Input-B | `06c5b2a81` | `9b75c1e4a` | Versioned control-request protocol + delivery semantics |

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
