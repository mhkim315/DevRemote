# PB.6 Evidence — Re-Verify at Terminal Remediation ACCEPT

**Re-Verify Candidate SHA:** `25636ec9c`
**PB.6 Evidence HEAD:** `25636ec9c`

**Accepted Terminal Remediation:**
- TERM-G1 ACCEPT: `2d13020ae` (IMPL `aba206add`) — managed-PTY geometry authority
- TERM-C1 ACCEPT: `ba78b617a` (IMPL `262d38b88`) — single control bridge
- Scroll Comparison: IDENTICAL to baseline, no action (`docs/SCROLL_COMPARISON.md`)

**Prior PB Wave:**
- PB.5 ACCEPT: `abe4df1d6`
- PA4 ACCEPT: `74560edd`

## Automated Gate Results

```
go build ./...                                    exit 0
go vet ./...                                      exit 0
gofmt -l .                                        0 files (clean)
go test -race ./... -count=1                       ALL PASS (12 packages)
cd mobile && npx tsc --noEmit                      clean
cd mobile && npx jest --no-coverage                519/519 pass, 35 suites
```

### Test Package Details

```
cmd/devremote                                     PASS (33.2s)
internal/agent                                    PASS
internal/agent/adapters/claude/v2_1_202           PASS
internal/agent/adapters/codex/v0_144_1            PASS
internal/agent/contract                           PASS
internal/agent/doctor                             PASS (101.9s)
internal/devicetrust                              PASS
internal/sessionid                                PASS
internal/term                                     PASS (20.7s)
internal/transcript                               PASS
internal/watcher                                  PASS
```

### TERM-G1 Test Detail

```
TestTERM_G1_NativeLauncherDefaultsZeroConfig       PASS
TestTERM_G1_HandleGetSizeAfterResize               PASS
TestTERM_G1_NativeLauncherHonorsExplicitConfig     PASS
TestTERM_G1_NativeLauncherRejectsZeroNegative      PASS (3 subtests)
TestTERM_G1_RecorderGetSizeChain                   PASS
TestTERM_G1_TerminalTransportGeomChain             PASS
TestTERM_G1_ResizeReflectedThroughChain            PASS
TestTERM_G1_StaleGenerationCannotObserve           PASS
TestTERM_G1_StaleResizeIsNoOp                      PASS
TestTERM_G1_HandleImplementsPTYHandle              PASS
TestTERM_G1_PTYReadDoesNotResolveWrongGeneration   PASS
TestTERM_G1_ANSIFixtureDefault100Cols              PASS
TestTERM_G1_ANSIFixtureResizeReflectedInTTY        PASS
TestTERM_G1_ANSIFixtureWideLineNoKernelWrap        PASS
TestTERM_G1_ANSIFixtureAlternateScreenSequences    PASS
TestTERM_G1_ANSIFixtureCursorMovement              PASS
TestTERM_G1_ANSIFixtureColorSGR                    PASS
```

### TERM-C1 Test Detail

```
Go bridge E2E tests (12):
  TestTERM_C1_DaemonPageServesControlBridge            PASS
  TestTERM_C1_HelloFrameDeliveredExactlyOnce           PASS
  TestTERM_C1_CtrlCWritesExactByteToPTYAndReceives...  PASS
  TestTERM_C1_PasteWritesAllBytesToPTYAndReceives...   PASS
  TestTERM_C1_TextPlusEnterIsTwoAcceptedWrites         PASS
  TestTERM_C1_WrongGenerationNoPTYWrite                PASS
  TestTERM_C1_WrongSessionNoPTYWrite                   PASS
  TestTERM_C1_ViewerDeniedZeroPTYWrite                 PASS
  TestTERM_C1_ViewerDeniedPasteThroughAcknowledged...  PASS
  TestTERM_C1_ViewerDeniedCtrlCThroughAcknowledged...  PASS
  TestTERM_C1_PendingInputACKBeforeReconnect           PASS
  TestTERM_C1_DuplicateHelloDoesNotClearPending...     PASS

Goja tests (6):
  TestTERM_C1_ServedPageGojaRealWebSocketToNative...   PASS
  TestTERM_C1_ServedPageGojaRealDirectCtrlC            PASS
  TestTERM_C1_ServedPageGojaRealEveryMacroDenied...    PASS
  TestTERM_C1_ServedPageGojaPendingDuplicateAnd...     PASS
  TestTERM_C1_ServedPageGojaInputSurfacesAnd...        PASS
  TestTERM_C1_ServedPageGojaFailsClosedAndReconnects   PASS

Mobile TERM-G1 tests (15):                            all PASS
Mobile TERM-C1 FeedScreen tests:                       all PASS
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
```

### 2. Tests

```
$ grep -rnE "mux\.Registry|mux\.Adapter|mux\.Session|tmux|cmux|localpty|v1Bridge|NewV1FromOld" --include='*_test.go' .
(empty)
```

### 3. Mobile (TypeScript)

```
$ grep -rnE "tmux|cmux|localpty|mux\.Registry|mux\.Adapter|mux\.Session" ../mobile/src/
(empty)

$ grep -rn "agentKind.*===" ../mobile/src/
(empty — no vendor branching)

$ grep -rn "opt\.id === 'approve'|opt\.id === 'reject'" ../mobile/src/
(empty — no ID inference)

$ grep -rn "/term/size" ../mobile/src/screens/FeedScreen.tsx | grep -v "//\|comment\|401"
(empty — no unauthenticated /term/size poll)
```

### 4. Scripts

```
$ grep -rnE "tmux|cmux|localpty|mux\.Registry|mux\.Adapter" scripts/
(empty)
```

### 5. Security

```
Secrets scan: CLEAN (pre-existing test fixtures only — deadbeef, xyz in auth test files, doctor tests)
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
| PB.6 | `25636ec9c` | RE-VERIFIED — All 5 surfaces clean, 519/519 Jest, 12/12 Go packages, TERM-G1 + TERM-C1 ACCEPT |
