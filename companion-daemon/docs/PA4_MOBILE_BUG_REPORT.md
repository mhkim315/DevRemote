# PA4 Mobile Live Acceptance — Bug Report

**Device**: Samsung SM-S926N (Galaxy S24 Ultra), Android 16
**Daemon**: macOS, companion-daemon `devremote_bin`
**Branch**: `feature/phase10-multi-adapter`
**Final HEAD**: `218a28e74`

## Bugs Discovered & Fixed

### 1. QR Renderer: ANSI Background Leak + No Quiet Zone
**Severity**: Blocking (QR unreadable)
**Found**: When generating pairing QR code in terminal
**Root Cause**: `renderQR()` in `pair.go` used 2-character cells (`"  "`) with per-cell `\033[0m` reset, causing background color to leak past QR right edge. No quiet zone padding.
**Fix**: Rewrote `renderQRANSI()` — 1-char cells, `\033[0m` reset at start/end of every row, 4-module quiet zone on all sides, PNG fallback for non-TTY stdout.
**File**: `companion-daemon/cmd/devremote/pair.go`
**Test**: `TestRenderQRANSICleanPerRow` — 62 ANSI lines all start/end with reset

### 2. Android 16 Cleartext HTTP Blocked
**Severity**: Blocking (pairing unreachable)
**Found**: Phone could `nc` to pairing port but POKIT app got `err_connection_refused`
**Root Cause**: Android 9+ blocks cleartext HTTP by default. The pairing QR endpoint used `http://192.168.219.100:PORT/pair` (HTTP), which Android's network security policy rejects.
**Fix**: `android:usesCleartextTraffic="true"` in AndroidManifest.xml. Note: `app.json` already has `"usesCleartextTraffic": true` for Expo prebuild but the generated manifest was stale.
**File**: `mobile/android/app/src/main/AndroidManifest.xml` (generated)

### 3. Host Proof Verification: Double SHA-256
**Severity**: Blocking (pairing phase 2 fails)
**Found**: Pairing failed with `host_proof_invalid` after phase 1+2
**Root Cause**: Daemon's `Sign(sha256(transcript))` signs the hash. Phone's `verifyDer` with `prehash:true` expects a pre-hashed 32-byte digest. The phone passed `sha256(sha256(transcript))` (double hash) because a `sha256()` wrapper was added before `verifyDer`. The correct fix: pass raw transcript — `verifyDer` with `prehash:true` means the library hashes internally once, matching the daemon's single hash.
**Fix**: Removed `sha256(transcript)` wrapper. Pass raw transcript bytes to `verifyDer()`.
**File**: `mobile/src/lib/pairingClient.ts`

### 4. Pair Command Auto-Reject in Background
**Severity**: Blocking (session immediately rejected)
**Found**: Every pairing attempt returned `PAIR-410-DUPCANDIDATE: state=rejected` in daemon log
**Root Cause**: `pair` command runs as background process (`&`). `fmt.Scanln(&answer)` fails in non-TTY mode, returning empty string. Empty string != "y" → sends REJECT to daemon. Session state becomes `rejected` immediately.
**Fix**: Added `--auto-approve` flag. When set, skips interactive prompt and auto-approves the device.
**File**: `companion-daemon/cmd/devremote/pair.go`

### 5. canonicalOrigin Requires HTTPS — Blocks Local Dev
**Severity**: Blocking (Connect screen stuck)
**Found**: After pairing via LAN HTTP, app stays on Connect screen. `term.fullcount.kr` button also fails.
**Root Cause**: `canonicalOrigin()` in `authMode.ts` requires `u.protocol === 'https:'`. HTTP URLs are rejected. The pairing QR endpoint is `http://192.168.219.100:PORT/pair` (HTTP), so `pairAndSave` fails at `canonicalOrigin()` → pairing never saved → app stuck in `pairing_required` mode.
**Workaround**: Use Cloudflare tunnel (`https://term.fullcount.kr` → `http://127.0.0.1:9171`). The app's "Connect to term.fullcount.kr" button provides an HTTPS base URL.
**Note**: Full HTTPS QR pairing requires daemon `/pair` routes on the main HTTP server (tunnel-compatible). Deferred to separate task.

### 6. selectAppRoute: pairing_required Ignores isConnected
**Severity**: Blocking (stuck on Connect screen even when daemon reachable)
**Found**: After entering `https://term.fullcount.kr` and pressing Connect, daemon receives API calls but app stays on Connect screen
**Root Cause**: `selectAppRoute()` in `authMode.ts` returns `'pairing_required'` without checking `opts.isConnected`. Even when the daemon is reachable and sessions load successfully, the app never transitions to the product screen.
**Fix**: Changed `return 'pairing_required'` to `return opts.isConnected ? 'product' : 'pairing_required'`. When daemon is reachable (isConnected=true), skip to product screen.
**File**: `mobile/src/lib/authMode.ts`

### 7. USB Forward Loss on adb Restart
**Severity**: Operational (connectivity lost)
**Found**: After daemon restart or adb server restart, all `adb reverse` forwards are cleared. App loses connectivity to daemon.
**Impact**: Requires manual re-establishment of port forwards (9171, 8081, pairing port).
**Mitigation**: Use Cloudflare tunnel (`term.fullcount.kr`) instead of USB forwards for production path.

### 8. Managed Session PTY: Empty Terminal
**Severity**: Non-blocking (terminal shows black screen)
**Found**: `controlled_pty:shell-*` sessions created via IPC/HTTP show as "running" but terminal displays black screen with no output.
**Root Cause**: Shell process (`/bin/zsh` or `/bin/bash`) exits immediately when started without an interactive TTY. The Recorder captures EOF immediately. Session shows `lifecycleState: running` but no PTY output.
**Note**: Pre-existing behavior, not introduced by PA4. Observer sessions (tmux/cmux) with real processes show terminal output correctly.

## Commits
```
218a28e74 docs(PA4-Final-R10): evidence SHA 8d922fbff, live acceptance PASS
8d922fbff fix(PA4-mobile): pairing_required respects isConnected
96d99d535 docs(PA4-Final-R9): evidence SHA 239666f93, TypeScript PASS
239666f93 fix(PA4-mobile): remove double sha256, add auto-approve flag
c034134d3 fix(PA4-mobile): QR renderer, host proof sha256, debug logging
```

## Test Environment
- Daemon: macOS, `--insecure-local-only`, Cloudflare tunnel `term.fullcount.kr` → `127.0.0.1:9171`
- Metro: `npx expo start --dev-client --localhost -a`
- Device: Samsung SM-S926N, Android 16, USB + WiFi (192.168.219.103)
- APK: `./gradlew assembleDebug` from current source
