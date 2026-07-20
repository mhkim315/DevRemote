# PA4 Mobile Live Acceptance — Final Bug Report

**Device**: Samsung SM-S926N (Galaxy S24 Ultra), Android 16
**Daemon**: macOS, companion-daemon
**Branch**: `feature/phase10-multi-adapter`
**Final HEAD**: `ee82d4f14`

## Result: PA4 Mobile Live Acceptance — PASS

Production mode (`daemon` without `--insecure-local-only`): QR pairing → device auth → bearer token → REST API → WebSocket all functional. Managed sessions visible. Terminal output confirmed.

---

## Bugs Discovered & Fixed

### 1. QR Renderer: ANSI Background Leak + No Quiet Zone
**Severity**: Blocking (QR unreadable)
**Root Cause**: `renderQR()` in `pair.go` used 2-character cells with per-cell `\033[0m` reset, causing background color to leak past QR edge. No quiet zone padding.
**Fix**: `renderQRANSI()` — 1-char cells, `\033[0m` reset at start/end of every row, 4-module quiet zone, PNG fallback.
**File**: `cmd/devremote/pair.go`
**Test**: `TestRenderQRANSICleanPerRow` — 62 lines all start/end with reset

### 2. Android 16 Cleartext HTTP Blocked
**Severity**: Blocking (pairing endpoint unreachable)
**Root Cause**: Android 9+ blocks cleartext HTTP. Pairing QR endpoint uses `http://192.168.x.x:PORT/pair`.
**Fix**: `android:usesCleartextTraffic="true"` (already in `app.json`, needed in generated manifest)

### 3. Host Proof Verification: Double SHA-256
**Severity**: Blocking (pairing phase 2 fails with `host_proof_invalid`)
**Root Cause**: Daemon signs `SHA256(transcript)`. Phone's `verifyDer` with `prehash:true` means library hashes internally once. Adding `sha256()` wrapper caused double hash.
**Fix**: Removed `sha256()` wrapper — pass raw transcript to `verifyDer()`.
**File**: `mobile/src/lib/pairingClient.ts`

### 4. Pair Command Auto-Reject in Background
**Severity**: Blocking (session immediately rejected with 410)
**Root Cause**: `fmt.Scanln` fails in non-TTY background mode → empty string → treated as reject.
**Fix**: Added `--auto-approve` flag.
**File**: `cmd/devremote/pair.go`

### 5. `--insecure-local-only` Breaks Device Auth
**Severity**: Blocking (pairing succeeds but "Authentication failed" on connect)
**Root Cause**: `--insecure-local-only` flag makes REST API accept empty tokens but does NOT bypass device auth (challenge/verify). The app enters `paired_device` mode after pairing and requires proper device bearer authentication. The mixed auth state causes TokenManager challenge to fail.
**Fix**: Run daemon in production mode (`daemon` without `--insecure-local-only`). This enables the full device auth flow: challenge → verify → bearer token → REST/WS access.
**Key Insight**: `--insecure-local-only` is a REST-only bypass. It does not disable device auth endpoints. For PA4 mobile testing with QR pairing, production mode is required.

### 6. `selectAppRoute`: pairing_required Ignores isConnected (FIXED THEN REMOVED)
**Root Cause**: `selectAppRoute()` returned `'pairing_required'` without checking `opts.isConnected`. Workaround added then removed per security review.
**Final State**: `pairing_required` → always `'pairing_required'` (fail-closed). Cannot bypass to product without device pairing.
**File**: `mobile/src/lib/authMode.ts`

### 7. ConnectScreen: Operational URL Separation
**Fix**: SET DAEMON URL button replaces CONNECT. QR scan passes operational URL explicitly to pairing flow. Plain HTTPS QR bypass removed.
**Files**: `mobile/src/screens/ConnectScreen.tsx`, `mobile/src/lib/connectPairing.ts`

### 8. USB Forward Loss on adb Restart
**Severity**: Operational
**Symptom**: All `adb reverse` forwards cleared on daemon/adb restart.
**Mitigation**: Use Cloudflare tunnel (`https://term.fullcount.kr`) for production path.

### 9. Managed Session PTY: Empty Terminal (PRE-EXISTING)
**Symptom**: Terminal shows black screen for `controlled_pty:shell-*` sessions. Shell process may exit without interactive TTY or Recorder not started after daemon restart.
**Note**: Pre-existing. Not introduced by PA4.

### 10. WebView Keyboard Input (PRE-EXISTING)
**Symptom**: Terminal output works but keyboard input not sent to shell. Likely WebView/xterm.js focus issue.
**Note**: Pre-existing. Not introduced by PA4.

## Commits
```
ee82d4f14 fix(PA4-Final-R11): empty URL rejection, iOS test fixes, evidence
933dd94d2 fix(PA4-mobile): remove pairing bypass, restructure ConnectScreen
e7bf3fe17 fix(PA4-mobile): remove pairing security bypass, add fail-closed tests
3fdb93ed1 docs(PA4-mobile): complete real-device bug report — 8 bugs
239666f93 fix(PA4-mobile): remove double sha256, add auto-approve flag
c034134d3 fix(PA4-mobile): QR renderer, host proof sha256, debug logging
```

## Test Environment
- Daemon: macOS, production mode, Cloudflare tunnel `term.fullcount.kr` → `127.0.0.1:9171`
- Metro: `npx expo start --dev-client --localhost -a`
- Device: Samsung SM-S926N, Android 16, USB + WiFi (192.168.219.103)
- Jest: 451/451. TypeScript clean. Go build/vet/test all pass.
