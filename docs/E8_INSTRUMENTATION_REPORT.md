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

## Pending: runtime evidence

Runtime log capture requires Metro console access during active device session.
Terminal-based emulator testing cannot reliably capture console.log output.
