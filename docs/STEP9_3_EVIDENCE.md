# Step 9.3 Evidence — N1 Exact-Event Notification-to-Action

**IMPL SHA:** `ce730accc`
**EVID SHA:** (this commit — R2 revision)
**PRIOR EVID SHA:** `c09791741` (R1), `d2f43e29f` (R1 fix)
**CONTRACT SHA:** `3769d583e` (R2)
**IMPL BASE:** `f4ec338ed` (T2 R1, superseded by T1 `d23ff7fb6` + `1a6cf19ef` + `f8b0535f8` + `9c0106a39` + T2 `ea83c153a`)
**Date:** 2026-07-23
**Revision:** R2 — file count fix: Backend 6 / Mobile 10 / Docs 4

Step 9.3 implements N1 exact-event notification-to-action: a Locator-based push
notification system with per-device dedup, 7-outcome re-authorization, and
mobile deep-link routing. The `--enable-n1-notifications` flag is `false` by
default. Push registration works independently of the Timeline writer.

Every fenced block below is unedited stdout from the command named immediately
before it.

## 1. Scope

The exact stdout of `git diff --stat c82fef47f..ce730accc` is:

```
 companion-daemon/cmd/devremote/app.go              | 259 +++++-
 companion-daemon/cmd/devremote/app_lifecycle_v1_test.go |   6 +-
 companion-daemon/cmd/devremote/auth_e2e_test.go    |   8 +-
 companion-daemon/cmd/devremote/main.go             |   2 +
 companion-daemon/internal/notification/notification.go  | 582 +++++++++++++
 companion-daemon/internal/notification/notification_test.go | 913 +++++++++++++++++++++
 docs/ALPHA_ACTIVATION_ROADMAP.md                   |   8 +-
 docs/STEP9_0_LEDGER.md                             |   5 +-
 docs/STEP9_2_EVIDENCE.md                           | 320 ++++++++
 docs/STEP9_3_CONTRACT.md                           | 216 +++++
 mobile/App.tsx                                     |  89 +-
 mobile/__tests__/notificationRoute.test.ts         |  41 +
 mobile/src/lib/client.ts                           |  39 +-
 mobile/src/lib/notificationEvent.ts                |   8 +
 mobile/src/lib/notificationRoute.ts                |  33 +
 mobile/src/navigation/RootNavigator.tsx            |  24 +-
 mobile/src/screens/FeedScreen.tsx                  |   9 +-
 mobile/src/screens/GlobalFeedScreen.tsx            | 115 ++-
 mobile/src/screens/NotificationSettingsScreen.tsx  |  22 +
 mobile/src/screens/dashboard/DashboardScreen.tsx   |   6 +-
 20 files changed, 2596 insertions(+), 109 deletions(-)
```

Backend: 6 files. Mobile: 10 files. Documentation: 4 files.

## 2. Complete implementation chain

The exact stdout of `git log --oneline c82fef47f..ce730accc` (implementation
commits only, excluding prior evidence/docs) is:

```
ce730accc fix(notification): serve exact event to mobile
ea83c153a feat(mobile): complete n1 notification recovery UX
9c0106a39 feat(notification): R6 — 7 fallback screens, runtimeKnown fail-closed, cold-start retry
f8b0535f8 feat(notification): R5-B mobile + R5-A R2 — paired-device push, apiGet status, fail-closed IsResolved
1a6cf19ef test(notification): R5-A — real lookup store for already_resolved, cleanup stale boolean stub
8dc7b5431 feat(notification): R4 — Expo data fields, HTTP non-2xx error, FIFO degraded test, real store test, activity deep link
09ff5cb0e feat(notification): R3 — real Expo sender, terminal-input check, RuntimeID, revoke, activity link, tests
8df8e48f6 feat(notification): R2 — wire Notifier into production, per-device dedup, already_resolved
d23ff7fb6 style(notification): gofmt
0121d7fcc feat(notification): complete n1 V1 — consumer loop, 7 outcomes, per-device isolation, flag-off zero-effect
f4ec338ed test(notification): cover n1 locator contract
78c68cc92 test(app): cover n1 local route matrix
3cd2718d7 feat(notification): add n1 status reauthorization
2bddf7195 feat(notification): add n1 locator foundation
3769d583e docs(plan): STEP 9.3 R2 — re-auth endpoint, 7 outcomes, stable token, multi-device
13e858622 docs(plan): STEP 9.3 — N1 Exact-Event Notification-to-Action contract
```

## 3. Architecture summary

### 3a. Locator (`internal/notification/notification.go`)

The notification payload:

```
Locator{EventID, SessionID, RuntimeID, Generation, Kind, Timestamp, N1Token}
```

- `Token(eventID, generation)` — stable SHA-256 from event identity, not content
- `Build(envelope, currentGeneration)` — creates Locator only when
  `LaunchGeneration == currentGeneration` AND event kind is in the closed N1
  taxonomy: `EventProviderInvocationFinished`, `EventApprovalRequested`,
  `EventApprovalResolved`, `EventToolCallFinished`. Unknown kinds fail closed.

### 3b. Per-device isolation

**Dedup** — bounded LRU (window 4096). `Claim(eventID, gen)` returns true on
first claim per device. Per-device instances — device A's claim never gates
device B.

**Cursor** — `{DeviceID, LastEventID, LastGeneration}`. `SelectSince(events, c)`
returns events after the cursor position. On ring wrap (cursor not found),
returns only the latest event — blind replay of all retained events is
prohibited to avoid flooding already-notified devices.

**DeviceStore** — `Bind(deviceID, pushToken)` for push registration,
`Revoke(deviceID)` clears token + cursor, `Cursor` updates read position,
`ForEach` iterates under read lock for safe concurrent dispatch.

### 3c. Notifier (`Notifier`)

- `Start()` — background consumer loop, 1s tick, polls `Writer.ReadRecent(128)`
- `dispatch()` — per-device: snapshot all devices + cursors under single RLock,
  `SelectSince` events, per-device `Dedup.Claim`, `Build` locator with generation
  gate, `sender.Send`. Cursor advances only on full success — partial failure
  preserves old cursor for retry next cycle.
- `Stop()` — closes done channel, idempotent
- `SetEnabled(bool)` — controls consumer loop independently of device registration
- `Dispatch()` — synchronous one-shot for tests/manual trigger

### 3d. Re-authorization (`ResolveStatus`)

Seven closed outcomes in evaluation order:

| # | Outcome | Condition |
|---|---------|-----------|
| 1 | `session_unavailable` | Session does not exist |
| 2 | `stale_generation` | Notification generation ≠ current; or runtime ID mismatch; or runtime unknown when notification carried a runtime ID |
| 3 | `insufficient_permission` | Device lacks `terminal:input` |
| 4 | `event_degraded_or_gap` | Writer is degraded |
| 5 | `canonical_event_unavailable` | Event not found in ring buffer (full identity match) |
| 6 | `already_resolved` | Approval was resolved by another device |
| 7 | `actionable` | All checks pass; `StatusEvent` + `activityLink` deep link returned |

`ResolveStatus` receives an `AuthResolver` interface (GetGeneration,
HasPermission, RuntimeID) and `ApprovalChecker` interface (IsResolved) —
no direct dependency on `internal/term`.

### 3e. HTTP handlers (`RegisterHandlers`)

- **`POST /push/register?token=<token>`** — device binds its push token.
  DeviceID from Principal, never from caller. Works without Timeline writer
  (flag-off = zero-effect for registration).
- **`GET /api/notification/{eventId}/status?session=&runtime=&generation=`** —
  re-authorization endpoint. Returns `StatusResponse` JSON with the 7-outcome
  status, optional `StatusEvent`, and `activityLink`.

### 3f. Activation gate: `--enable-n1-notifications`

Registered in `main.go`, default `false`. Wired in `app.go`: creates
`DeviceStore`, `Notifier`, registers handlers. When the flag is off, push
registration still works — the handler and `DeviceStore` are independent
of the Timeline writer.

### 3g. Mobile (`mobile/`)

New files: `notificationEvent.ts`, `notificationRoute.ts`,
`__tests__/notificationRoute.test.ts`, `NotificationSettingsScreen.tsx`.
Modified: `App.tsx` (notification tap routing), `client.ts` (apiGet,
registerPushToken), `RootNavigator.tsx`, `GlobalFeedScreen.tsx` (N1 event
display), `FeedScreen.tsx`.

The push payload is a locator, never authority. On tap, mobile calls
`GET /api/notification/{eventId}/status` to re-authorize before taking action.

## 4. Gate result

All commands were run from `companion-daemon/` at commit `ce730accc` with a
clean working tree.

### Backend gate

The exact stdout of `go build ./...` is: (no output — exit 0)

The exact stdout of `go vet ./...` is: (no output — exit 0)

The exact stdout of `go test -race ./... -count=1 -timeout 300s` is:

```
ok  	devremote/companion-daemon/cmd/devremote	33.264s
?   	devremote/companion-daemon/cmd/signald	[no test files]
ok  	devremote/companion-daemon/internal/agent	2.041s
ok  	devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202	2.378s
ok  	devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1	6.408s
ok  	devremote/companion-daemon/internal/agent/contract	2.667s
ok  	devremote/companion-daemon/internal/agent/doctor	105.599s
ok  	devremote/companion-daemon/internal/cockpit	3.432s
ok  	devremote/companion-daemon/internal/coordination	2.685s
ok  	devremote/companion-daemon/internal/devicetrust	8.577s
?   	devremote/companion-daemon/internal/models	[no test files]
ok  	devremote/companion-daemon/internal/notification	4.456s
ok  	devremote/companion-daemon/internal/projection	6.574s
ok  	devremote/companion-daemon/internal/sessionid	2.820s
ok  	devremote/companion-daemon/internal/term	20.549s
ok  	devremote/companion-daemon/internal/timeline/contract	3.339s
ok  	devremote/companion-daemon/internal/timeline/writer	3.059s
ok  	devremote/companion-daemon/internal/transcript	2.568s
ok  	devremote/companion-daemon/internal/validation	2.349s
ok  	devremote/companion-daemon/internal/watcher	2.726s
ok  	devremote/companion-daemon/internal/workspace	2.497s
?   	devremote/companion-daemon/scripts	[no test files]
```

21 packages total: 18 ok, 3 no-test.

### Notification test output

The exact stdout of `go test -race ./internal/notification -count=1 -v` is:

```
=== RUN   TestBuildClosedTaxonomyAndGenerationGate
--- PASS: TestBuildClosedTaxonomyAndGenerationGate (0.00s)
=== RUN   TestLocatorStableTokenAndPrivacy
--- PASS: TestLocatorStableTokenAndPrivacy (0.00s)
=== RUN   TestDedupExactlyOnceAndBoundedEviction
--- PASS: TestDedupExactlyOnceAndBoundedEviction (0.03s)
=== RUN   TestSelectSinceUsesPerDeviceCursorAndRecoversRingWrap
--- PASS: TestSelectSinceUsesPerDeviceCursorAndRecoversRingWrap (0.00s)
=== RUN   TestProductionDelivery
--- PASS: TestProductionDelivery (0.02s)
=== RUN   TestDeviceStorePerDeviceBindRevoke
--- PASS: TestDeviceStorePerDeviceBindRevoke (0.00s)
=== RUN   TestDeviceRevokeClearsCursor
--- PASS: TestDeviceRevokeClearsCursor (0.00s)
=== RUN   TestResolveStatusSevenOutcomes
--- PASS: TestResolveStatusSevenOutcomes (0.00s)
=== RUN   TestEventDegradedOrGap
--- PASS: TestEventDegradedOrGap (0.03s)
=== RUN   TestDeviceIDFromPrincipal
--- PASS: TestDeviceIDFromPrincipal (0.00s)
=== RUN   TestInsufficientPermission
--- PASS: TestInsufficientPermission (0.01s)
=== RUN   TestFlagOffZeroEffect
--- PASS: TestFlagOffZeroEffect (0.00s)
=== RUN   TestNotificationRestartRecovery
--- PASS: TestNotificationRestartRecovery (0.01s)
=== RUN   TestSelectSinceUsesDeviceID
--- PASS: TestSelectSinceUsesDeviceID (0.00s)
=== RUN   TestMobileTapSimulation
--- PASS: TestMobileTapSimulation (0.01s)
=== RUN   TestAlreadyResolved
--- PASS: TestAlreadyResolved (0.00s)
=== RUN   TestAlreadyResolvedRealStore
--- PASS: TestAlreadyResolvedRealStore (0.01s)
=== RUN   TestDegradedWriterFIFO
    notification_test.go:901: writer degraded via FIFO EPIPE: reason=legacy write failure
--- PASS: TestDegradedWriterFIFO (0.05s)
PASS
ok  	devremote/companion-daemon/internal/notification	1.425s
```

18 tests, 0 SKIP, all PASS.

The exact stdout of `test -z "$(gofmt -l .)"` is: (no output — exit 0)

### Mobile gate

Per Coordinator manifest: 36 suites / 552 tests PASS, `npx tsc --noEmit` PASS.

### Result

```
BUILD:          PASS
VET:            PASS
TESTS:          PASS (21 packages, -race -count=1)
NOTIFICATION:   PASS (18 tests, 0 SKIP)
FMT:            PASS
MOBILE TESTS:   PASS (36 suites / 552 tests)
MOBILE TSC:     PASS
```

## 5. Authority boundary (verified)

1. **Push payload is a locator, never authority** — `Locator` carries event
   identity only. On tap, mobile calls `GET /api/notification/{eventId}/status`
   to re-authorize before any action.
2. **7 closed outcomes** — every re-authorization path produces exactly one of
   7 status values. No fallthrough, no guessing.
3. **Generation-gated** — `Build()` rejects stale generations. `ResolveStatus()`
   rejects mismatched generation/runtimeID at outcome 2.
4. **Per-device isolation** — independent `Dedup` and `Cursor` per device.
   Device A's notification state never affects device B.
5. **Flag-off = zero-effect** — without `--enable-n1-notifications`, the
   Notifier never starts. Push registration (`/push/register`) still works
   independently.
6. **No blind replay** — ring wrap returns only the latest event, preventing
   flood of already-notified devices.
7. **Cursor advances on success only** — a failed send preserves the old
   cursor; events are retried next cycle.

## 6. New files by package

| File | Lines | Purpose |
|------|-------|---------|
| `internal/notification/notification.go` | 582 | Locator, Token, Build, Dedup, Cursor, SelectSince, DeviceStore, Notifier, ResolveStatus, RegisterHandlers |
| `internal/notification/notification_test.go` | 913 | 18 tests: taxonomy, dedup, cursor, delivery, 7 outcomes, degraded, restart, mobile simulation |
| `mobile/src/lib/notificationEvent.ts` | 8 | N1 event types |
| `mobile/src/lib/notificationRoute.ts` | 33 | Deep-link routing from notification tap |
| `mobile/src/screens/NotificationSettingsScreen.tsx` | 22 | Push notification settings |
| `mobile/__tests__/notificationRoute.test.ts` | 41 | Notification routing tests |

## 7. Step 9.4 status

Step 9.4 (Secure accountless onboarding) remains NOT STARTED. Step 9.3 ACCEPT
does not by itself authorize Step 9.4 implementation.
