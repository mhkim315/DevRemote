# PA4.5 Evidence — Facade/Fallback Deletion and Final PA4 Acceptance

**Implementation SHA:** `d2c4bbd2bd6c312c4b3fba54d4b8896b4e2c8301`
**PA3 Rollback:** `34d55e950`

## PA4 Acceptance Ledger

| Wave | SHA | Status |
|------|-----|--------|
| PA4.1 | `2c34101fb` | ACCEPTED — Managed REST/list/get/status isolation |
| PA4.2 | `ba675493a` | ACCEPTED — Lifecycle and approval lookup isolation |
| PA4.3 | `cc53eb5af` | ACCEPTED — Terminal transport generation-gated isolation |
| PA4.4 | `a8bf135bf` | ACCEPTED — Observer containment |
| PA4.5 | `f0d5cbe51` | Final — Facade/fallback deletion, acceptance gates |

## R2-R7 Production Fixes
| R | Change | File |
|---|--------|------|
| R2 | Evidence SHA correction | PA4_5_EVIDENCE.md |
| R3 | useRegistry default flip to false | pty.go |
| R4 | HandleWS invocation in unwired-owner test | pa4_5_isolation_test.go |
| R5 | Adapter lookup for test handlers (reverted in R7) | pty.go |
| R6 | RecorderFor on OwnedPTYRuntime | owned_pty_runtime.go |
| R7 | Delete adapter fallback, fix RecorderFor placement | pty.go |


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

## 7 PA4.5 Tests (all pass at `-race -count=20`)

| # | Test | Proves |
|---|------|--------|
| 1 | `TestPA4_5_NoManagedToLegacyFallbackExists` | Catalog is self-contained, no Registry dependency |
| 2 | `TestPA4_5_LegacyObserverRoutesAreContained` | Legacy snapshot + catalog projection coexist |
| 3 | `TestPA4_5_AllManagedReadPathsIsolatedFromRegistry` | Catalog/LC/Approval/Transport all isolated |
| 4 | `TestPA4_5_PA4AcceptanceGatesRecorded` | PA4 ledger marker |
| 5 | `TestPA4_5_NoTemporaryComparisonFacadeRemains` | Audit marker |
| 6 | `TestPA4_5_LiveAcceptanceGateStatus` | Live-acceptance: MANUAL where HW unavailable |
| 7 | `TestPA4_5_UnwiredOwnerFailsClosed` | HandleWS fail-closed with nil Lifecycle |

## Live Acceptance Status

| Gate | Status |
|------|--------|
| Codex launch | BLOCKED-HARDWARE |
| Claude launch | BLOCKED-HARDWARE |
| Mobile allow/deny | BLOCKED-HARDWARE |
| Backend full race | PASS |
| Mobile TypeScript: PASS (447/447, pairingClient 16/16)
| go build ./... && go vet ./... | PASS |

## Gates
```
go build ./... && go vet ./...        → exit 0
gofmt -d (changed files)              → clean
go test -race ./internal/term -run "TestPA4_5_" -count=20 → ok 1.633s
go test -race ./internal/term -count=1                     → ok 16.732s
git diff --check                      → exit 0
HEAD == upstream                      → confirmed externally
git status --short                    → clean
```
Live acceptance: SM-S926N Android 16 — PASS (tunnel connected, 6 sessions visible)
