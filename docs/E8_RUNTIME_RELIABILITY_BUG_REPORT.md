# E8 Runtime Interaction Reliability Bug Report

Date: 2026-07-08
Status: BLOCKED — needs verifier assistance

## Goal

Capture E8DIAG runtime evidence from Terminal WebView to identify
root cause of terminal scroll duplication.

## What works

### Instrumentation code (verified)
- Terminal HTML has e8diag object with counters (connectCount, closeCount, msgCount, totalBytes, lastMsgSize, rawLen, wasReconnect)
- Counters increment in onopen/onclose/onmessage
- 5s setInterval POSTs to /debug/e8diag with auth header
- /debug/e8diag endpoint: AuthMiddleware, POST-only, safeNum/safeBool validation, log.Printf output
- Direct curl test: HTTP 200, E8DIAG log line appears in daemon stdout

### Daemon
- 6 sessions available from tmux/cmux adapters
- API /api/sessions returns data correctly
- build-gate.sh: ALL 8 GATES PASSED

## What doesn't work

### 1. Cannot capture E8DIAG from WebView
- E8DIAG POST to /debug/e8diag confirmed working via curl
- But when Terminal tab opened on emulator/device, no E8DIAG appears in daemon log
- Either WebView isn't loading the new HTML, or fetch() is silently failing

### 2. Device connectivity
- Emulator: AuthScreen appeared instead of ConnectScreen (EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST flag not taking effect?)
- Pixel 9 (LTE): Cannot reach local daemon (needs tunnel or WiFi)
- Cloudflare tunnel: not running (no launchd agent)
- WiFi: daemon bound to 127.0.0.1, not accessible from other devices

### 3. Two daemon processes
- After rebuild, both old (IPv4 localhost:9171) and new (IPv6 *:9171) daemons running simultaneously
- Causes confusion about which daemon is serving which client

### 4. Dashboard empty / "failed to save agent profile"
- App shows empty Dashboard with only "NEW AGENT" card
- Creating agent profile fails with "failed to save agent profile"
- API /api/sessions returns data (verified via curl)

## Attempted fixes

1. Added `wasReconnect` flag + `term.clear()` on reconnect (possible fix, not proven)
2. Added dual-path E8DIAG capture: postMessage → RN + fetch → daemon log
3. Changed daemon bind from 127.0.0.1 to all interfaces
4. Rebuilt APK with EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST=1
5. Re-established adb reverse port forwarding multiple times

## Environment

- Daemon: /Users/mhk/.local/bin/devremote (built from feature/phase10-multi-adapter)
- Daemon flags: --insecure-local-only --enable-localpty --enable-agent-detection
- Log output: /tmp/daemon.log, /tmp/daemon2.log, /tmp/daemon3.log
- Mobile APK: app-release.apk built with EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST=1
- Emulator: emulator-5554 (sdk_gphone16k_arm64)
- Physical device: Pixel 9 (LTE)
- Cloudflare tunnel config: term.fullcount.kr → 127.0.0.1:9171 (not running)
- Launchd: com.pokit.daemon.plist (unloaded to stop auto-restart)

## Commands tried

```sh
# E8DIAG direct test (WORKS)
curl -s -X POST "http://localhost:9171/debug/e8diag?connectCount=1&closeCount=0&msgCount=5" -H "Authorization: Bearer <DEV_TOKEN>"

# Daemon start with log capture
/Users/mhk/.local/bin/devremote daemon --insecure-local-only --enable-localpty --enable-agent-detection > /tmp/daemon.log 2>&1 &

# Emulator port forward
adb -s emulator-5554 reverse tcp:9171 tcp:9171
adb -s emulator-5554 reverse tcp:8081 tcp:8081

# Build preview APK
cd mobile && EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST=1 npx expo run:android --variant release

# Stop launchd auto-restart
launchctl unload ~/Library/LaunchAgents/com.pokit.daemon.plist
```

## Build gate

ALL 8 GATES PASSED (verified multiple times)
