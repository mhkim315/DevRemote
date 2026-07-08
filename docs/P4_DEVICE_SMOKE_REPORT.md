# P4 Device UX Smoke Report

Date: 2026-07-08
Targets: Android Emulator (sdk_gphone16k_arm64), iOS Simulator (iPhone 17 Pro)

## Build

| Step | Android | iOS |
|------|---------|-----|
| npm ci | OK | OK |
| npm run typecheck | OK | OK |
| Build | BUILD SUCCESSFUL (94s) | BUILD SUCCESSFUL (0 errors, 2 warnings) |
| Install | OK | OK |
| Launch | OK | OK |

## Smoke Results

### Dashboard (P1a)

| Check | Android | iOS | Notes |
|-------|---------|-----|-------|
| App launches | ✅ | ✅ | Dev client connects to Metro |
| POKIT AGENTS header | ✅ | ✅ | |
| Section headers visible | ✅ | ✅ | RECENTLY COMPLETED section confirmed on iOS |
| Agent cards render | ✅ | ✅ | Session name, SLEEPING state, totem |
| ScrollView scrolls | ✅ | - | |
| ConnectScreen → Dashboard | ✅ | ✅ | URL entry + connect flow works |

### FeedScreen (P2)

| Check | Android | iOS | Notes |
|-------|---------|-----|-------|
| Terminal tab renders | ✅ | - | WebView terminal with macros |
| Activity tab renders | ✅ | - | Empty (no agent events in test env) |
| Macro buttons visible | ✅ | - | Ctrl+C, Esc, Tab, arrows, Y, N, Enter |
| Text input visible | ✅ | - | "Send text..." field |
| Back navigation works | ✅ | - | Returns to Dashboard |

### Interaction (A9)

| Check | Android | iOS | Notes |
|-------|---------|-----|-------|
| Interaction card | - | - | Not tested (no pending approvals in test env) |

### Push (P1b)

| Check | Android | iOS | Notes |
|-------|---------|-----|-------|
| Push notification | - | - | Requires physical device with Expo push token |

### Unknown/degraded (A7)

| Check | Android | iOS | Notes |
|-------|---------|-----|-------|
| Agent kind displayed | - | ✅ | 🤖 with agent name visible on cards |
| Degraded fallback | - | - | No degraded sessions in test env |

### Build Gate (P3)

| Check | Result |
|-------|--------|
| sh scripts/build-gate.sh | ALL 8 GATES PASSED |

### Notes

- Daemon required `adb reverse tcp:9171 tcp:9171` for Android emulator access.
- Metro required explicit host IP (`192.168.219.100`) for emulator connectivity.
- Activity feed is empty because no live agent sessions are generating events — expected.
- Push notification smoke not possible on simulators (require physical device + Expo push token).
- Interaction card not visible because no sessions have pending approvals — expected.
- iOS build had 2 non-blocking warnings (ambiguous script dependencies).
- Android build had deprecated Gradle features warning (non-blocking).

### Known gaps

- EAS build authentication requires "minani" project owner; used `npx expo run:android/ios` instead.
- Physical device smoke not performed (emulator/simulator only).
- Push notification end-to-end not tested (simulator limitation).
- Interaction card end-to-end not tested (no pending approval data).
- 10 moderate npm audit vulnerabilities (dependency audit deferred to P5/P6).
- No automated UI tests exist.
