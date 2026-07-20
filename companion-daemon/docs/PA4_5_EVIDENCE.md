# PA4.5 Evidence — Facade/Fallback Deletion and Final PA4 Acceptance

**Implementation SHA:** `4aaf3b76a` (PA4-Final-R21: SetSubscriberFanOutHook deleted, same-package test access)
**Prior EVID SHAs:** `7c2d63b7c` (R20), `d2294af67` (R19), `ce2425406` (R18)
**PA3 Rollback:** `34d55e950`

## PA4 Acceptance Ledger

| Wave | SHA | Status |
|------|-----|--------|
| PA4.1 | `2c34101fb` | ACCEPTED — Managed REST/list/get/status isolation |
| PA4.2 | `ba675493a` | ACCEPTED — Lifecycle and approval lookup isolation |
| PA4.3 | `cc53eb5af` | ACCEPTED — Terminal transport generation-gated isolation |
| PA4.4 | `a8bf135bf` | ACCEPTED — Observer containment |
| PA4.5 | `f0d5cbe51` | ACCEPTED — Facade/fallback deletion, acceptance gates (R2-R14 applied) |

## R2-R14 Production Fixes
| R | Change | File |
|---|--------|------|
| R2 | Evidence SHA correction | PA4_5_EVIDENCE.md |
| R3 | useRegistry default flip to false | pty.go |
| R4 | HandleWS invocation in unwired-owner test | pa4_5_isolation_test.go |
| R5 | Adapter lookup for test handlers (reverted in R7) | pty.go |
| R6 | RecorderFor on OwnedPTYRuntime | owned_pty_runtime.go |
| R7 | Delete adapter fallback, fix RecorderFor placement | pty.go |
| R8 | Evidence regeneration | PA4_5_EVIDENCE.md |
| R9 | Evidence SHA + TypeScript status | PA4_5_EVIDENCE.md |
| R10 | Evidence with live acceptance | PA4_5_EVIDENCE.md |
| R11 | Empty URL rejection, iOS test fixes, 451/451 Jest | mobile/ |
| R12 | Evidence at 926d2bfaa, live acceptance complete | PA4_5_EVIDENCE.md |
| R13 | Fix pairingClient test count 16/16 | PA4_5_EVIDENCE.md |
| R14 | Remove RecorderFor generation gate bypass | pty.go, terminal_transport.go, owned_pty_runtime.go |
| R15 | Remove GetRecorder global lookup, retired→500 test, gofmt | pty.go, terminal_transport.go, pa4_5_isolation_test.go |
| R16 | Eliminate ALL managed-path GetRecorder: IPC, /term/size, awaitExit, TOCTOU | 8 files |
| R17 | True atomic SubscriberFanOut + exact-generation awaitExit | terminal_transport.go, owned_pty_runtime.go |
| R18 | Real goroutine race + lifecycle-path test proofs | pa4_5_isolation_test.go |
| R19 | Test hook proves SubscriberFanOut atomicity (RLock→Lock blocking) | terminal_transport.go, pa4_5_isolation_test.go |
| R20 | Unexport hook, delete Recorder() accessor, evidence sync | terminal_transport.go, docs |
| R21 | Delete exported SetSubscriberFanOutHook, PB doc SHA fix | terminal_transport.go, docs |


## PA4.5 Audit Results

| Audit | Result |
|-------|--------|
| Managed-to-legacy comparison facades | ZERO found |
| Temporary fallback bridges | ZERO found |
| Registry calls from managed catalog | ZERO (doc-enforced) |
| Registry calls from AgentStatusStore | ZERO (structural) |
| Registry calls from ApprovalStore | ZERO (structural) |
| Registry calls from LifecycleService | ZERO (structural) |
| Legacy observer routes | CONTAINED (pending PB removal) |

## PA4.5 Tests (10 tests, all pass at `-race -count=20`)

| # | Test | Proves |
|---|------|--------|
| 1 | `TestPA4_5_NoManagedToLegacyFallbackExists` | Catalog is self-contained, no Registry dependency |
| 2 | `TestPA4_5_LegacyObserverRoutesAreContained` | Legacy snapshot + catalog projection coexist |
| 3 | `TestPA4_5_AllManagedReadPathsIsolatedFromRegistry` | Catalog/LC/Approval/Transport all isolated |
| 4 | `TestPA4_5_PA4AcceptanceGatesRecorded` | PA4 ledger marker |
| 5 | `TestPA4_5_NoTemporaryComparisonFacadeRemains` | Audit marker |
| 6 | `TestPA4_5_LiveAcceptanceGateStatus` | Live-acceptance: MANUAL where HW unavailable |
| 7 | `TestPA4_5_UnwiredOwnerFailsClosed` | HandleWS fail-closed with nil Lifecycle |
| 8 | `TestPA4_Final_R14_SubscriberFanOut_DirectRecorder_NoGlobalLookup` | Transport uses direct recorder, not global GetRecorder |
| 9 | `TestPA4_Final_R14_SubscriberFanOut_RetiredTransport_FailClosed` | Retired transport SubscriberFanOut denied |
| 10 | `TestPA4_Final_R14_SubscriberFanOut_StaleGeneration_Denied` | Stale gen transport cannot access replacement recorder |

## QR Items — DEFERRED (not fixed in PA4)

| QR | Item | Status |
|----|------|--------|
| QR1 | Quiet zone padding | DEFERRED — cosmetic, no isolation impact |
| QR2 | Terminal width negotiation | DEFERRED — existing PTY default adequate |
| QR3 | PNG opener capability | DEFERRED — not a managed-path concern |
| QR4 | Cleartext log redaction | DEFERRED — existing redaction in place |

## Live Acceptance Status

| Gate | Status |
|------|--------|
| Codex launch | PASS — SM-S926N production mode |
| Claude launch | PASS — SM-S926N production mode |
| Mobile allow/deny | PASS — SM-S926N production mode |
| Backend full race | PASS |
| Mobile Jest: PASS (451/451, pairingClient 16/16, authMode + authPairing 34/34)
| Mobile TypeScript: PASS (tsc --noEmit clean)
| go build ./... && go vet ./... | PASS |
| gofmt -d (changed files) | PASS — clean |
| Production mode confirmed | PASS — no --insecure-local-only |
| Tunnel connected | PASS — SM-S926N Android 16 |
| Device paired | PASS — SM-S926N |
| Mobile keyboard input | DEFERRED — post-PB (WebView/xterm interaction layer, not managed transport) |

## Gates (at IMPL `4aaf3b76a`)
```
go build ./... && go vet ./...        → exit 0
gofmt -d (changed files)              → clean
go test -race ./... -count=1          → ok (all packages)
go test -race ./internal/term -run "TestPA4_" -count=1 → 42 tests PASS
go test -race ./internal/term -count=1                  → ok 16.710s
git diff --check                      → exit 0
HEAD == upstream                      → confirmed
git status --short                    → clean
```
Live acceptance: SM-S926N Android 16 — PASS (production mode, tunnel connected, device paired, 6 sessions visible)
Jest: 451/451 pass, pairingClient: 16/16 pass, authMode + authPairing: 34/34 pass, tsc --noEmit: clean
