# QR Renderer/Security Packet — Evidence (R1 — Rejection Fixes)

**QR IMPL SHA (R1):** `e6dc31b87`
**QR R0 IMPL SHA:** `bad7e83eb`
**QA EVIDENCE SHA:** (this commit)
**PA4 ACCEPT SHA:** `74560edd`
**PB Ancestry Baseline SHA:** `abe4df1d6`

## Rejection Changes (R0 → R1)

| Issue | Fix |
|-------|-----|
| Payload decode: never compared QR content to original | Re-encode same payload, compare all `code.Black(x,y)` modules; prove different payload produces different modules |
| Symlink: never exercised production rejection | Open file through symlink path, call `verifySecureFile`, assert error returned |
| Opener: never tested failure through `renderQR` | Added `openPNGFn` test seam; inject failing opener, call `renderQR`, assert non-fatal (returns cleanup, no panic) |
| Test count: evidence said 13, actual 15 | Fixed count to 15 |

## Gate Results

```
go build ./...                                    exit 0
go vet ./...                                      exit 0
gofmt -l .                                        0 files (clean)
go test -race ./... -count=1                       ALL PASS (11 packages)
cd mobile && npx tsc --noEmit                      clean
cd mobile && npx jest --runInBand                  451/451 pass, 34 suites
```

## Focused Test Results (15 QR-specific tests)

```
TestQRPayloadByteEquality                  PASS (now compares all QR modules)
TestQRANSIDimensionBoundary                PASS (6 sub-cases)
TestQRHalfBlockMapping                     PASS
TestQRQuietZoneFourModulesEverySide        PASS
TestQRANSIColorsAndReset                   PASS
TestQRPNGSecureCreation                    PASS
TestQRPNGSymlinkRejection                  PASS (now exercises verifySecureFile rejection)
TestQROpenerDirectArgv                     PASS
TestQROpenerFailureNonFatal                PASS (now exercises renderQR with failing opener)
TestQRNoPayloadLeakedToOutput              PASS
TestVerifySecureFileRegularMode            PASS
TestVerifySecureFileRejectsPermissiveMode  PASS
TestQRCleanupRemovesPNG                    PASS
TestQRCleanupEmptyPathNoOp                 PASS
TestAnsiOKFuncMatchesBoundary              PASS
```

## Requirement Trace

| §4 Requirement | Test(s) | Status |
|---|---|---|
| Payload byte equality, decode → compare | `TestQRPayloadByteEquality` (module matrix compare) | PASS |
| Width/height boundary tables | `TestQRANSIDimensionBoundary` (6 cases) | PASS |
| Half-block mapping (even + odd heights) | `TestQRHalfBlockMapping` | PASS |
| 4-module quiet zone all sides, no QR data copy | `TestQRQuietZoneFourModulesEverySide` | PASS |
| Explicit FG/BG per cell, per-row + final reset | `TestQRANSIColorsAndReset` | PASS |
| PNG round-trip → valid image | `TestQRPayloadByteEquality` (PNG decode path) | PASS |
| Atomic unpredictable creation, 0600, ownership | `TestQRPNGSecureCreation` | PASS |
| Symlink rejection | `TestQRPNGSymlinkRejection` (exercises verifySecureFile) | PASS |
| Permissive mode rejection | `TestVerifySecureFileRejectsPermissiveMode` | PASS |
| Direct-argv opener, no shell | `TestQROpenerDirectArgv` | PASS |
| Opener failure non-fatal through renderQR | `TestQROpenerFailureNonFatal` (injects failing opener) | PASS |
| Secure creation failure fatal | `renderQRPNG` returns error → `log.Fatalf` | IMPL |
| Cleanup lifecycle | `TestQRCleanupRemovesPNG`, `TestQRCleanupEmptyPathNoOp` | PASS |
| No raw payload/token on output surfaces | `TestQRNoPayloadLeakedToOutput` | PASS |
| TTY/term size query → fallback to PNG | `ansiOK()` + `ansiOKWith()` logic | IMPL |
