# PB-DG-R4 R4.4 — Physical Device Smoke Evidence (Final)

**Date**: 2026-07-23 01:52–01:55 KST
**Device**: SM-S926N (Android 16, SDK 36)
**Status**: 8/8 PASS
**PB_ACCEPT_SHA**: UNSET (pending independent verification)

## Candidate Identity

| Item | Value |
|------|-------|
| Source SHA | `059bef181c6c2ef312eee421dbf10f12b15b0326` |
| Daemon SHA-256 | `a2753537801536b94d725d274ec94a7f5b0d1b6b7409a2f2e5f5cbbef08e93ea` |
| APK SHA-256 | `9ee0c4763151e080cb50b67b393bae525b4ab79463b24708f3a1cfc57bf97128` |
| APK embedded candidate | `059bef181...` (matches) |
| APK embedded daemon | `a2753537...` (matches) |
| vcs.revision | `059bef181...` |
| vcs.modified | `false` |
| Cleartext | `usesCleartextTraffic=true` (Pairing V1 /pair LAN HTTP) |
| Daemon path | `/tmp/pokit-daemon` |
| APK path | `/tmp/pokit-pb-device-artifacts/pokit-app-release.apk` |

## Test Fixture

- `pm clear` executed: app data cleared, prior pairing removed
- Android Keystore key regenerated: `26e6b2a3...`
- Daemon registry: all prior devices revoked, 0 active before test
- Fresh QR scan required (no auto-connect)

## R4.4 Smoke Matrix

### 1. LAN HTTP QR pairing — PASS
- Endpoint: private IPv4, explicit port, `/pair` path
- Operator approval: `y/N` prompt on CLI
- Device: `26e6b2a3...` (owner)
- Daemon: `pairing: device 26e6b2a3... approved and registered`

### 2. DeviceAuth / Keystore — PASS
- Android Keystore identity available (new key post `pm clear`)
- DeviceAuth challenge/verify succeeds
- Owner role → `terminal:input` permission

### 3. HTTPS/WSS operational switch — PASS
- Post-pairing: app uses `https://term.fullcount.kr` (cloudflared tunnel)
- Terminal WS: `127.0.0.1` (tunnel endpoint, not LAN IP)
- Daemon: `WS [controlled_pty:shell-...]: 127.0.0.1:65226 connected`

### 4. Managed shell — PASS
- Session: `controlled_pty:shell-1784739181814305000`
- State: running (`RECORDER start`)
- adapterCapabilities includes `input`

### 5. TERM-C1 hello — PASS
- WebSocket connected via tunnel
- No "View only" displayed
- `deviceCanInput=true` (terminal writable, input accepted)

### 6. Acknowledged input — PASS
- `printf 'POKIT_R4_DEVICE_OK'` entered
- "Delivered to terminal" displayed
- Output visible in terminal

### 7. Ctrl+C / control macro — PASS
- `cat` started, Ctrl+C delivered
- Process interrupted correctly

### 8. Cleartext-negative — PASS
- LAN HTTP: restricted to `/pair`, `/pair/confirm`, `/pair/result` (pairing only)
- Operational traffic: HTTPS/WSS via cloudflared tunnel → `127.0.0.1:9171`
- No bearer/session/terminal/transcript/approval/input over cleartext LAN HTTP
- Daemon WS connections sourced from `127.0.0.1` (tunnel), not LAN IP

## Known Issues (pre-existing)

- Send button (React Native path): keyboard Enter works
- Reconnect garbage characters in PTY
- Transcript byte-stream suppression (T3 contract)

## Evidence Files

| File | Path |
|------|------|
| Screenshot | `/tmp/r4.4-r3-terminal.png` |
| Daemon log | `/tmp/daemon-r4.4.log` |
| Android logcat | `/tmp/r4.4-r3-smoke.log` |
| APK binary | `/tmp/pokit-pb-device-artifacts/pokit-app-release.apk` |
| Daemon binary | `/tmp/pokit-daemon` |
