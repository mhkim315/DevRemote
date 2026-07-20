# PA4.5 Evidence — Facade/Fallback Deletion and Final PA4 Acceptance

**Implementation SHA:** `f0d5cbe5123a1a1e498f75209d2c0aae1b92a37b`
**PA3 Rollback:** `34d55e950`

## PA4 Acceptance Ledger

| Wave | SHA | Status |
|------|-----|--------|
| PA4.1 | `2c34101fb` | ACCEPTED — Managed REST/list/get/status isolation |
| PA4.2 | `ba675493a` | ACCEPTED — Lifecycle and approval lookup isolation |
| PA4.3 | `cc53eb5af` | ACCEPTED — Terminal transport generation-gated isolation |
| PA4.4 | `a8bf135bf` | ACCEPTED — Observer containment |
| PA4.5 | `f0d5cbe51` | Final — Facade/fallback deletion, acceptance gates |

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

## 6 PA4.5 Tests (all pass at `-race -count=20`)

| # | Test | Proves |
|---|------|--------|
| 1 | `TestPA4_5_NoManagedToLegacyFallbackExists` | Catalog is self-contained, no Registry dependency |
| 2 | `TestPA4_5_LegacyObserverRoutesAreContained` | Legacy snapshot + catalog projection coexist |
| 3 | `TestPA4_5_AllManagedReadPathsIsolatedFromRegistry` | Catalog/LC/Approval/Transport all isolated |
| 4 | `TestPA4_5_PA4AcceptanceGatesRecorded` | PA4 ledger marker |
| 5 | `TestPA4_5_NoTemporaryComparisonFacadeRemains` | Audit marker |
| 6 | `TestPA4_5_LiveAcceptanceGateStatus` | Live-acceptance: MANUAL where HW unavailable |

## Live Acceptance Status

| Gate | Status |
|------|--------|
| Codex launch | MANUAL (requires live Codex CLI + PTY) |
| Claude launch | MANUAL (requires live Claude CLI + PTY) |
| Mobile allow/deny | MANUAL (requires device farm) |
| Backend full race | PASS |
| Mobile TypeScript | NOT RUN |
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
