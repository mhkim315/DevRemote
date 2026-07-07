# P4 Device UX Smoke Report

Date: 2026-07-08
Target: Pixel 9 (Android)

## Build

| Step | Result |
|------|--------|
| npm ci | OK |
| npm run typecheck | OK |
| npx expo run:android | BUILD SUCCESSFUL (1m 34s) |
| APK installed | OK, app-debug.apk (90MB) on Pixel 9 |
| Dev client launched | OK, Metro connected |

## Automated verification (via build-gate.sh)

All 8 backend + invariant gates pass:
- go build, go vet, go test -race, git diff --check
- npm run typecheck
- vendor branch scan, ID inference scan, secret scan

## Manual smoke checklist

These require visual verification on the device. Mark [x] when verified by a human tester.

### Dashboard (P1a)
- [ ] Dashboard sections render (Needs Attention, Running, Recently Completed, Degraded)
- [ ] Section headers visible with emoji + color
- [ ] Agent cards show totem animation, agent kind, agent status
- [ ] VIEW ONLY badge on observe-only sessions
- [ ] ACTION badge on pending approval sessions
- [ ] ScrollView scrolls correctly

### Feed (P2)
- [ ] Tapping a session opens FeedScreen (Terminal tab + Activity tab)
- [ ] Terminal tab shows WebView terminal
- [ ] Activity tab shows event bubbles
- [ ] Event types render in correct categories:
  - [ ] user_message → blue right-aligned
  - [ ] assistant_message → agent bubble with totem
  - [ ] thinking → dimmed italic
  - [ ] tool_call_started → blue tool bubble with tool name
  - [ ] interaction → red attention border
  - [ ] error → red background
  - [ ] unknown → dimmed dashed fallback
- [ ] Tool results collapsed by default, expandable on tap
- [ ] Time stamps visible on each event

### Interaction (A9)
- [ ] Approval/Interaction card renders with options
- [ ] Approve/Reject/Open Terminal buttons visible based on capabilities
- [ ] Per-option input fields render when option has input schema
- [ ] Required input disables button until filled
- [ ] Tapping approve/reject resolves the interaction
- [ ] Error state shows "No remote actions available" for empty options

### Push (P1b)
- [ ] Push notification received (Interaction required)
- [ ] Notification body is redacted (no raw prompt/command)
- [ ] Tapping notification opens the correct session
- [ ] Cold-start notification tap works (app not running)

### Unknown/degraded states (A7)
- [ ] Unknown agent kind renders gracefully
- [ ] Unknown agent status renders gracefully
- [ ] Degraded/low-confidence agents are dimmed
- [ ] Observe-only sessions show VIEW ONLY, not action buttons

### General
- [ ] Back button from FeedScreen returns to Dashboard
- [ ] App background → foreground preserves state
- [ ] App restart loads sessions correctly
- [ ] No crashes on empty state (no sessions, no events)

## Known gaps

- EAS build requires "minani" account; local build used `npx expo run:android` instead.
- Metro was already running on port 8081 from another project; used existing instance.
- 10 moderate npm audit vulnerabilities (dependency audit deferred to P5/P6).
- No automated UI tests exist — all smoke tests are manual.
- iOS build not attempted (no macOS signing / Xcode context available).
