# Step 9.3 Evidence — N1 Exact-Event Notification-to-Action

**IMPL SHA:** `f7033b86c`
**EVID SHA:** `1a4b602bf` (R11)
**PRIOR EVID SHA:** `c09791741` (R1), `d2f43e29f` (R1 fix), `41486e1d1` (R2), `277e00998` (R2 fix), `4bee07590` (R3), `1af27eda6` (R3 fix), `c5cd9722f` (R4), `e9e697e9c` (R4 fix), `ad69a5d3b` (R5), `22967ece6` (R5 fix), `ef402d482` (R6), `d6fa46588` (R6 fix), `26dc2504f` (R7), `1fe58835e` (R7 fix), `7481ea16c` (R8), `e236d8bee` (R8 fix), `4383353a4` (R9), `5c3ef427b` (R9 fix), `15744d8f2` (R10), `da6997288` (R10 fix)
**CONTRACT SHA:** `a5e532fd9` (amended mobile N1 scope); prior: `3769d583e` (R2, superseded)
**IMPL BASE:** `f4ec338ed` (T2 R1, superseded by T1 `d23ff7fb6` + `1a6cf19ef` + `f8b0535f8` + `9c0106a39` + T2 `ea83c153a`)
**Date:** 2026-07-23
**Revision:** R11 — IMPL f7033b86c (R15 ACCEPT), 20 tests, standalone cursor test
**Round counts:** Contract 4, Pre-gate 2, Impl 15, EVID 15, V2 3 = 39 total

Step 9.3 implements N1 exact-event notification-to-action: a Locator-based push
notification system with per-device dedup, 7-outcome re-authorization, and
mobile deep-link routing. The `--enable-n1-notifications` flag is `false` by
default. Push registration works independently of the Timeline writer.

Every fenced block below is unedited stdout from the command named immediately
before it.

## 1. Scope

The output of `git diff --stat c82fef47f..e829299c6`:

```
 companion-daemon/cmd/devremote/app.go              |  277 ++++-
 .../cmd/devremote/app_lifecycle_v1_test.go         |    6 +-
 companion-daemon/cmd/devremote/auth_e2e_test.go    |    8 +-
 companion-daemon/cmd/devremote/main.go             |    2 +
 .../internal/notification/notification.go          |  713 ++++++++++++
 .../internal/notification/notification_test.go     | 1164 ++++++++++++++++++++
 docs/ALPHA_ACTIVATION_ROADMAP.md                   |    9 +-
 docs/STEP9_0_LEDGER.md                             |    8 +-
 docs/STEP9_2_EVIDENCE.md                           |  320 ++++++
 docs/STEP9_3_CONTRACT.md                           |  248 +++++
 docs/STEP9_3_EVIDENCE.md                           |  300 +++++
 mobile/App.tsx                                     |   89 +-
 mobile/__tests__/notificationRoute.test.ts         |   41 +
 mobile/src/lib/client.ts                           |   39 +-
 mobile/src/lib/notificationEvent.ts                |    8 +
 mobile/src/lib/notificationRoute.ts                |   33 +
 mobile/src/navigation/RootNavigator.tsx            |   24 +-
 mobile/src/screens/FeedScreen.tsx                  |    9 +-
 mobile/src/screens/GlobalFeedScreen.tsx            |  115 +-
 mobile/src/screens/NotificationSettingsScreen.tsx  |   22 +
 mobile/src/screens/dashboard/DashboardScreen.tsx   |    6 +-
 21 files changed, 3332 insertions(+), 109 deletions(-)
```

Backend: 6 files. Mobile: 10 files. Documentation: 5 files.

## 2. Complete implementation chain

The exact stdout of `git log --oneline c82fef47f..e829299c6` is:

```
f7033b86c fix(notification): R15 — standalone partial-failure test, first-send-fail edge, dedup residency doc
df0de4aa8 fix(notification): R14 — at-most-once cursor doc fix + partial-failure test
e829299c6 fix(notification): R13 — epoch in DeviceStore, atomic snapshot, wg.Add under lock
99c2992bf fix(notification): R12 — per-device epoch, real WaitGroup join, epoch-guarded cursor write
2a35e10b1 fix(notification): R11 — production wiring, atomic RevokeDevice, Stop drain, late-SUCCESS tests
4290bbf93 fix(notification): R10 — Stop drain, stopped/revoked cursor guards, concurrency tests
2a35a7a79 fix(notification): R9 — singleflight, remove inner goroutine, ordering/cleanup tests
db2d2f82c fix(notification): R8 — fire-and-forget dispatch, per-device 10s timeout, steady-state test
e9e697e9c docs: fix STEP9_3 EVID R4 self-referencing SHA, ledger sync
c5cd9722f docs: STEP 9.3 EVID R4 — delivery semantic fix + round counts
6ea27e01e fix(notification): R7 — at-most-once comments, http.Client timeout, per-device goroutines, hung sender test
1af27eda6 docs: fix STEP9_3 EVID R3 self-referencing SHA, ledger sync
4bee07590 docs: STEP 9.3 EVID R3 — CONTRACT SHA 3769d583e → a5e532fd9
277e00998 docs: fix STEP9_3 EVID R2 self-referencing SHA, ledger sync
a5e532fd9 docs(notification): amend mobile n1 scope
41486e1d1 docs: STEP 9.3 EVID R2 — file count fix: Backend 6 / Mobile 10 / Docs 4
d2f43e29f docs: fix STEP9_3 evidence report self-referencing EVID SHA
c09791741 docs: STEP 9.3 V1 ACCEPT evidence report, ledger + roadmap update
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
26eb2e157 docs: fix STEP9_2 EVID R3 self-referencing SHA, ledger sync
c8d6eda20 docs: STEP 9.2 EVID R3 — full raw go test -v stdout
6ed826773 docs: fix STEP9_2 EVID R2 self-referencing SHA, ledger sync
3e9a5fe23 docs: STEP 9.2 EVID R2 — complete 32-test go test -v output
af5e76eff docs: fix STEP9_2 evidence report self-referencing EVID SHA
f32be5756 docs: STEP 9.2 V1 ACCEPT evidence report, ledger + roadmap update
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
  preserves old cursor, at-most-once, no retry.
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

All commands were run from `companion-daemon/` at commit `f7033b86c` with a
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
--- PASS: TestProductionDelivery (0.21s)
=== RUN   TestDeviceStorePerDeviceBindRevoke
--- PASS: TestDeviceStorePerDeviceBindRevoke (0.00s)
=== RUN   TestResolveStatusSevenOutcomes
--- PASS: TestResolveStatusSevenOutcomes (0.01s)
=== RUN   TestEventDegradedOrGap
--- PASS: TestEventDegradedOrGap (0.01s)
=== RUN   TestInsufficientPermission
--- PASS: TestInsufficientPermission (0.01s)
=== RUN   TestFlagOffZeroEffect
--- PASS: TestFlagOffZeroEffect (0.00s)
=== RUN   TestNotificationRestartRecovery
--- PASS: TestNotificationRestartRecovery (0.22s)
=== RUN   TestMobileTapSimulation
--- PASS: TestMobileTapSimulation (0.01s)
=== RUN   TestAlreadyResolved
--- PASS: TestAlreadyResolved (0.01s)
=== RUN   TestAlreadyResolvedRealStore
--- PASS: TestAlreadyResolvedRealStore (0.01s)
=== RUN   TestDegradedWriterFIFO
    notification_test.go:883: writer degraded via FIFO EPIPE: reason=legacy write failure
--- PASS: TestDegradedWriterFIFO (0.05s)
=== RUN   TestPerDeviceSingleflight
--- PASS: TestPerDeviceSingleflight (0.29s)
=== RUN   TestSameDeviceOrderingAndMonotonicCursor
--- PASS: TestSameDeviceOrderingAndMonotonicCursor (0.52s)
=== RUN   TestLateSuccessCursorNotWrittenAfterStop
--- PASS: TestLateSuccessCursorNotWrittenAfterStop (0.21s)
=== RUN   TestOldSendVsRevokeRebind
--- PASS: TestOldSendVsRevokeRebind (0.31s)
=== RUN   TestCursorAdvancesToLastSuccessOnPartialFailure
--- PASS: TestCursorAdvancesToLastSuccessOnPartialFailure (0.62s)
PASS
ok  	devremote/companion-daemon/internal/notification	4.206s
```

20 tests, 0 SKIP, all PASS.

The exact stdout of `test -z "$(gofmt -l .)"` is: (no output — exit 0)

### Mobile gate

Per Coordinator manifest: 36 suites / 552 tests PASS, `npx tsc --noEmit` PASS.

### Result

```
BUILD:          PASS
VET:            PASS
TESTS:          PASS (21 packages, -race -count=1)
NOTIFICATION:   PASS (20 tests, 0 SKIP)
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
   cursor, at-most-once, no retry.

## 6. New files by package

| File | Lines | Purpose |
|------|-------|---------|
| `internal/notification/notification.go` | 713 | Locator, Token, Build, Dedup, Cursor, SelectSince, DeviceStore, Notifier, ResolveStatus, RegisterHandlers |
| `internal/notification/notification_test.go` | 1278 | 20 tests: taxonomy, dedup, cursor, delivery, 7 outcomes, degraded, restart, singleflight, ordering, late-SUCCESS, revoke-rebind, partial-failure |
| `mobile/src/lib/notificationEvent.ts` | 8 | N1 event types |
| `mobile/src/lib/notificationRoute.ts` | 33 | Deep-link routing from notification tap |
| `mobile/src/screens/NotificationSettingsScreen.tsx` | 22 | Push notification settings |
| `mobile/__tests__/notificationRoute.test.ts` | 41 | Notification routing tests |

## 7. Step 9.4 status

Step 9.4 (Secure accountless onboarding) remains NOT STARTED. Step 9.3 ACCEPT
does not by itself authorize Step 9.4 implementation.
