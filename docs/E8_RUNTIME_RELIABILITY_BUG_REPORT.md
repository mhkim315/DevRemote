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
- Pixel 9 (LTE): Cannot reach local daemon directly; LTE requires a tunnel or relay URL
- Cloudflare tunnel: not running (no launchd agent)
- WiFi/LAN: `--insecure-local-only` intentionally binds to 127.0.0.1, so another device cannot reach it without a tunnel or an explicit future listen-address option

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
3. Temporarily changed daemon bind from 127.0.0.1 to all interfaces, then reverted because it broke the `--insecure-local-only` security boundary
4. Rebuilt APK with EXPO_PUBLIC_POKIT_NO_LOGIN_LOCAL_TEST=1
5. Re-established adb reverse port forwarding multiple times

## Connectivity recovery plan

Do not change `--insecure-local-only` to bind all interfaces. That flag must remain
local-only. The current LTE regression should be treated as a transport setup
problem, not as a reason to weaken the daemon bind policy.

Use one of these routes:

1. Emulator route
   - Keep daemon on `127.0.0.1:9171`.
   - Use `adb reverse tcp:9171 tcp:9171`.
   - Use the local no-login app flow against `http://127.0.0.1:9171` or the existing emulator-local URL expected by the app.
   - This route is execution-agent-verifiable.

2. LTE physical-device route
   - Keep daemon on `127.0.0.1:9171`.
   - Start a tunnel from a public HTTPS URL to `127.0.0.1:9171`.
   - Point the phone app to the tunnel URL.
   - Verify `/api/sessions` through the same URL before testing Terminal.
   - This route is partly manual because it depends on the owner device/network.

3. Future LAN route
   - Add an explicit dangerous option such as `--listen-addr 0.0.0.0:9171` or `--dev-bind-all-interfaces`.
   - Do not overload `--insecure-local-only`.
   - Require clear warning logs and documentation.
   - This should be a separate implementation slice, not an E8 hotfix.

Before collecting E8DIAG, ensure exactly one daemon owns port 9171:

```sh
lsof -nP -iTCP:9171 -sTCP:LISTEN
```

If more than one candidate appears, stop LaunchAgent and kill stale manual
processes before restarting the intended daemon with log capture.

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

# Confirm only one daemon owns 9171
lsof -nP -iTCP:9171 -sTCP:LISTEN
```

## Build gate

ALL 8 GATES PASSED (verified multiple times)
