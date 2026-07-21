# QR Renderer/Security Packet — Evidence

**QR IMPL SHA:** `bad7e83eb`
**QA EVIDENCE SHA:** (this commit)
**PA4 ACCEPT SHA:** `74560edd`
**PB Ancestry Baseline SHA:** `abe4df1d6`

## Ancestry

```
$ git merge-base --is-ancestor 74560edd8 HEAD && echo "PA4 ACCEPT: ANCESTOR OK"
PA4 ACCEPT: ANCESTOR OK

$ git merge-base --is-ancestor abe4df1d6 HEAD && echo "PB BASELINE: ANCESTOR OK"
PB BASELINE: ANCESTOR OK
```

## Diff Scope

```
companion-daemon/cmd/devremote/pair.go      | 315 ++++----
companion-daemon/cmd/devremote/pair_test.go | 580 +++++++
 2 files changed, 779 insertions(+), 116 deletions(-)
```

Production change: `cmd/devremote/pair.go` only. No other package touched.

## Gate Results

```
go build ./...                                    exit 0
go vet ./...                                      exit 0
gofmt -l .                                        0 files (clean)
go test -race ./... -count=1                       ALL PASS (11 packages)
cd mobile && npx tsc --noEmit                      clean
cd mobile && npx jest --runInBand                  451/451 pass, 34 suites
```

## Focused Test Results (13 QR-specific tests)

```
TestQRPayloadByteEquality                  PASS
TestQRANSIDimensionBoundary                PASS (6 sub-cases)
TestQRHalfBlockMapping                     PASS
TestQRQuietZoneFourModulesEverySide        PASS
TestQRANSIColorsAndReset                   PASS
TestQRPNGSecureCreation                    PASS
TestQRPNGSymlinkRejection                  PASS
TestQROpenerDirectArgv                     PASS
TestQROpenerFailureNonFatal                PASS
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
| Payload byte equality, decoder round-trip | `TestQRPayloadByteEquality` | PASS |
| Width/height boundary tables | `TestQRANSIDimensionBoundary` (6 cases) | PASS |
| Half-block mapping (even + odd heights) | `TestQRHalfBlockMapping` | PASS |
| 4-module quiet zone all sides, no QR data copy | `TestQRQuietZoneFourModulesEverySide` | PASS |
| Explicit FG/BG per cell, per-row + final reset | `TestQRANSIColorsAndReset` | PASS |
| PNG round-trip → exact payload bytes | `TestQRPayloadByteEquality` (PNG path) | PASS |
| Atomic unpredictable creation, 0600, ownership | `TestQRPNGSecureCreation` | PASS |
| Symlink rejection | `TestQRPNGSymlinkRejection` | PASS |
| Permissive mode rejection | `TestVerifySecureFileRejectsPermissiveMode` | PASS |
| Direct-argv opener, no shell | `TestQROpenerDirectArgv` | PASS |
| Opener failure non-fatal | `TestQROpenerFailureNonFatal` | PASS |
| Secure creation failure fatal | `renderQRPNG` returns error → `log.Fatalf` | IMPL |
| Cleanup lifecycle | `TestQRCleanupRemovesPNG`, `TestQRCleanupEmptyPathNoOp` | PASS |
| No raw payload/token on output surfaces | `TestQRNoPayloadLeakedToOutput` | PASS |
| TTY/term size query → fallback to PNG | `ansiOK()` + `ansiOKWith()` logic | IMPL |

## Security Fix — Payload Leak Removed

The previous `pair.go:82` emitted the raw QR payload (including `bootstrapToken`)
to stdout:

```go
fmt.Printf("\nQR payload: %s\n", string(qrPayload))  // REMOVED
```

This line was deleted. `TestQRNoPayloadLeakedToOutput` proves the ANSI output
and PNG path contain no raw JSON payload, `bootstrapToken` field name, or token
value.

## Worktree

```
$ git status
On branch feature/phase10-multi-adapter
nothing to commit, working tree clean
```
