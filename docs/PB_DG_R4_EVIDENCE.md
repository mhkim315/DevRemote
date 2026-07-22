# PB-DG-R4 R4.4 — Physical Device Smoke Evidence

**Date**: 2026-07-23 01:37–01:43 KST
**Device**: SM-S926N (Android 16, SDK 36)
**Tester**: Claude (automated evidence) + human operator (touch)
**PB_ACCEPT_SHA**: UNSET (pending independent verification)

## Candidate Identity

| Item | Value |
|------|-------|
| Source SHA | `059bef181c6c2ef312eee421dbf10f12b15b0326` |
| Daemon SHA-256 | `a2753537801536b94d725d274ec94a7f5b0d1b6b7409a2f2e5f5cbbef08e93ea` |
| APK SHA-256 | `c57d7cc023cdc8d25e84f99a1b9bae4d55228d3bb0852db973f3e2e816fb4c31` |
| APK size | 153M |
| vcs.revision | `059bef181...` |
| vcs.modified | false |
| Cleartext | `usesCleartextTraffic=true` (Pairing V1 LAN HTTP required) |
| Daemon path | `/tmp/pokit-daemon` |
| APK path | `/tmp/pokit-pb-device-artifacts/pokit-app-release.apk` |

## R4.4 Smoke Matrix

### 1. LAN HTTP QR pairing — PASS
- Endpoint: private IPv4 with explicit port, `/pair` path
- Operator approval: `y/N` prompt
- Device: `8a68e0ab...` (first 16)
- Role: **owner**
- Daemon log: `pairing: device 8a68e0ab... approved and registered`

### 2. DeviceAuth / Keystore — PASS
- Android Keystore identity available (post `pm clear`, new key generated)
- DeviceAuth challenge/verify succeeds
- Effective permissions include `terminal:input` (owner role)

### 3. HTTPS/WSS operational switch — PASS
- Post-pairing: app uses `https://term.fullcount.kr` (tunnel)
- Terminal: WSS via cloudflared tunnel → `127.0.0.1:9171`
- Daemon log: `WS [controlled_pty:shell-...]: 127.0.0.1:64626 connected`

### 4. Managed shell — PASS
- Session: `controlled_pty:shell-1784738301371267000`
- State: running
- Daemon log: `RECORDER start session=controlled_pty:shell-1784738301371267000`

### 5. TERM-C1 hello — PASS
- WebSocket connected, hello frame delivered
- No "View only" displayed
- Terminal input enabled (deviceCanInput=true)

### 6. Acknowledged input — PASS
- `printf 'POKIT_R4_DEVICE_OK'` executed
- Output visible in terminal
- Operator confirmed "전부 작동"

### 7. Ctrl+C / control macro — PASS
- `cat` started, Ctrl+C delivered
- Process interrupted
- Operator confirmed "전부 작동"

### 8. Cleartext-negative — PASS
- LAN HTTP: used only for `/pair`, `/pair/confirm`, `/pair/result`
- Operational traffic: HTTPS/WSS via cloudflared tunnel
- Daemon WS connections come from `127.0.0.1` (tunnel), not LAN IP
- No bearer/session/terminal/transcript/approval/input over cleartext LAN

## Known Issues (not part of R4.4)

- Send button (React Native path): keyboard Enter works
- Reconnect garbage characters: separate issue
- Transcript byte-stream suppression: T3 contract behavior

## Evidence Files

- Screenshot: `/tmp/r4.4-final.png` (335KB)
- Daemon log: `/tmp/daemon-r4.4.log`
- Android logcat: `/tmp/r4.4-final.log`
- APK binary: `/tmp/pokit-pb-device-artifacts/pokit-app-release.apk`
- Daemon binary: `/tmp/pokit-pb-device-artifacts/pokit-daemon`
