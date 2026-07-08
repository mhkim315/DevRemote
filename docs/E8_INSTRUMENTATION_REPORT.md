# E8 Instrumentation Report

Date: 2026-07-08
Commit: 5110340bc

## Instrumentation path (verified in code)

```
pty.go:325  onopen     → e8diag.connectCount++
pty.go:336  onmessage  → e8diag.msgCount++, totalBytes+=, lastMsgSize=, rawLen=
pty.go:348  onclose    → e8diag.closeCount++, wasReconnect=true
pty.go:400  setInterval → postMessage({type:"e8diag", ...}) every 5s
FeedScreen.tsx:216  onMessage → console.log('E8DIAG', ...)
```

## How to capture evidence

1. Start instrumented daemon
2. Launch app on device/emulator, connect to daemon
3. Open a session → Terminal tab
4. Watch Metro console output for `E8DIAG` lines
5. Scroll terminal, switch tabs, refresh
6. Observe counter changes:
   - If `connectCount` increases during scroll → reconnect is happening
   - If `closeCount` increases → WebSocket is closing
   - If `msgCount`/`totalBytes` increase without backend output → replay/duplication
   - If `wasReconnect: true` and counters spike → reconnect replay is the cause

## Current verification

| Check | Status |
|-------|--------|
| e8diag object in script | ✅ |
| connectCount increment | ✅ |
| closeCount increment | ✅ |
| msgCount/totalBytes/lastMsgSize/rawLen increment | ✅ |
| setInterval postMessage | ✅ |
| React Native onMessage handler | ✅ |
| Served HTML contains type:"e8diag" | ✅ |
| build-gate.sh | ✅ ALL 8 PASSED |

## Runtime evidence attempt

Emulator test (2026-07-08):
- Daemon with e8diag running on port 9171 ✅
- App launched, connected to daemon ✅
- Dashboard renders with session cards ✅
- Metro running, serving on port 8081 ✅
- Console.log output goes to Metro stdout, not adb logcat
- Metro stdout captured to /tmp/metro.log — no E8DIAG lines appeared
- Likely cause: Expo dev client routes console.log through Metro's internal
  WebSocket, not to stdout. Or Terminal tab WebView wasn't opened during capture.

## How to capture E8DIAG reliably

Option 1: Open Chrome DevTools on Metro (http://localhost:8081/debugger-ui)
Option 2: Use `react-native log-android` while app is running
Option 3: Watch Metro terminal output directly during active Terminal session
Option 4: Add native logcat side-channel to the terminal HTML instrumentation
