# R1a Connectivity Baseline — Validation Report

Date: 2026-07-09
Commit: 62a53a5d0

## E-track validation (executor-verifiable)

### 1. Build & Typecheck

| Check | Result |
|-------|--------|
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `go test -race ./... -count=1` | PASS |
| `npx tsc --noEmit` | PASS |
| `sh scripts/build-gate.sh` | ALL GATES PASSED |
| `npx expo run:ios` (iPhone 17 Pro) | BUILD SUCCEEDED |

### 2. API Connectivity Matrix

| URL | HTTP | Classification | Sessions | Notes |
|-----|------|---------------|----------|-------|
| `http://localhost:9171` | 200 | OK | 6 loaded | Local daemon, healthy |
| `http://localhost:19999` | connection refused | NetworkUnreachable | N/A | Port not open |
| `https://term.fullcount.kr` | 530 | APIError | N/A | Tunnel up, origin down |

### 3. Endpoint Reachability

| Endpoint | Session | Result |
|----------|---------|--------|
| `/api/sessions` | — | 200, 6 sessions (tmux:ai, cmux:surface:1-3, etc.) |
| `/api/sessions?activity=tmux:ai` | tmux:ai | 200, 0 events (no recent output captured) |
| `/api/sessions?activity=cmux:surface:2` | cmux:surface:2 | 200, 1 event (recorder capture working) |
| `/api/sessions?history=tmux:ai` | tmux:ai | 200, 2 events (history endpoint working) |
| `/term/?session=tmux:ai` | tmux:ai | 200 (terminal HTML served) |
| `/term/ws?session=tmux:ai` | tmux:ai | WebSocket upgrade available |

### 4. Code Verification — Connectivity Diagnostics

| Feature | File | Status |
|---------|------|--------|
| Typed errors (PokitError, ConnectivityFailure) | `client.ts` | ✅ Implemented |
| probeDaemon() structured diagnostics | `client.ts` | ✅ Implemented, uses checkedFetch |
| ConnectionProvider: daemonReachable, sessionsLoaded, sessionsEmpty, failure | `connection.tsx` | ✅ Exposed |
| refreshDiagnostics() for stale state | `connection.tsx` | ✅ Exposed via context |
| Dashboard: error classification UI | `DashboardScreen.tsx` | ✅ Specific messages per failure type |
| Dashboard: empty sessions state | `DashboardScreen.tsx` | ✅ "DAEMON CONNECTED" + "No sessions found" |
| Dashboard: stale diagnostics refresh | `DashboardScreen.tsx` | ✅ Calls refreshDiagnostics on fetch failure |
| FeedScreen: terminalError state | `FeedScreen.tsx` | ✅ onError + onHttpError handlers |
| FeedScreen: terminal failure UI | `FeedScreen.tsx` | ✅ "TERMINAL CONNECTION FAILED" + RETRY |
| FeedScreen: activityError display | `FeedScreen.tsx` | ✅ Error banner on transcript tab |
| FeedScreen: historyError display | `FeedScreen.tsx` | ✅ Error banner on activity tab |

### 5. Simulator Verification

| Check | Result |
|-------|--------|
| App launches on iPhone 17 Pro simulator | ✅ ConnectScreen rendered |
| Camera permission request | ✅ QR scanner visible |
| Manual URL entry field | ✅ Visible with placeholder |
| Tunnel URL quick-connect button | ✅ "Connect to term.fullcount.kr" visible |

### 6. Failure Classification Coverage

| Failure Mode | Classified As | UI Message |
|-------------|--------------|------------|
| DNS/connection refused | NetworkUnreachable | "Daemon unreachable. Check that the daemon is running." |
| Connection timeout | Timeout | "Connection timed out. Check your network or daemon URL." |
| HTTP 401/403 | AuthError | "Auth failed. Re-scan the QR code or re-enter the daemon URL." |
| HTTP 4xx/5xx (non-auth) | APIError | "Sessions API error (N). Daemon may need restart." |
| WebView load failure | onError | "TERMINAL CONNECTION FAILED" |
| WebView HTTP error | onHttpError | "Terminal HTTP N. Check daemon URL." |
| Activity endpoint failure | activityError | "Transcript endpoint unreachable." |
| History endpoint failure | historyError | "Activity endpoint unreachable." |

## Known Gaps (M-track / manual)

The following R1a behaviors require a human with a physical device on LTE/significant
network change to validate:

1. **Real tunnel connectivity**: term.fullcount.kr origin daemon is currently down (HTTP 530).
   When the origin is running, verify the app connects through the tunnel with live sessions,
   terminal, and transcript tabs working.

2. **LTE fallback behavior**: Verify that switching from WiFi to LTE correctly triggers
   the "daemon unreachable" state rather than showing stale/empty sessions.

3. **WebSocket internal failure**: The RN-level WebView onError/onHttpError covers page-load
   failures. Internal WebSocket failures after page-load rely on the terminal HTML's existing
   reconnect/session-ended display. This is adequate for R1a but could be made more explicit
   in R1b.

4. **First-time URL entry UX**: The manual URL entry text field exists but the quality of the
   typing/autocomplete/paste experience on a real device was not validated by the executor.

## Conclusion

R1a E-track acceptance criteria are satisfied:
- Daemon reachability is verified before claiming connected ✅
- Empty sessions are distinguished from daemon unreachable ✅
- Terminal WebView failure is visible ✅
- Transcript/activity endpoint failure is visible ✅
- Tunnel/manual URL entry is supported ✅
- Stale diagnostics are refreshed after failure ✅

M-track validation items (real device, LTE switch, tunnel with live origin) are documented
as known gaps for the product owner.
