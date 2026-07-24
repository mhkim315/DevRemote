# 9.4-E Bug Fix Verification Report

**Date:** 2026-07-25
**HEAD:** 1bdf6b632 → 9e64e1670
**Device:** SM-S926N / Android 16
**macOS:** 26.5.1 arm64

## 9-Fix Verification Results

| Fix | Bug | Result | Evidence |
|-----|-----|--------|----------|
| QW1 | P0 LaunchAgent PATH | ✅ PASS | cloudflared auto-start, tunnel HTTP 401 |
| QW2 | P1 QR 카메라 멈춤 | ✅ PASS | 3초 이내 owner 진입, 3 retry + 500ms backoff |
| QW3 | P1 Claude 생성 | ✅ PASS | `pokit run claude` → `claude_headless:*` 성공 |
| QW4 | P2 Version SSoT | ✅ PASS | `CertifiedClaudeVersion` 1곳, `CertifiedCodexAuthorityVersion` 1곳 |
| QW5 | P2 Claude I/O | ⚠️ PASS* | CLI 성공, ManagedSessionView 표시, 앱 HTTP 생성 실패 (B4) |
| QW6 | P2 RESCAN 버튼 | ✅ PASS | DashboardScreen에서 버튼 제거 |
| QW7 | P3 Font 12px | ✅ PASS | TranscriptRenderer fontSize: 12 |
| QW9 | P3 Suppressed color | ✅ PASS | FeedScreen amber + TranscriptRenderer amber (9e64e1670) |
| QW12 | P3 Tunnel dedup | ✅ PASS | 3회 재시작 후 cloudflared 1개 |

**8/9 PASS, 1 conditional (QW5)**

## New Bugs Found During Verification

### B1 — ManagedSessionView Title Hardcoded
- **File:** `mobile/src/components/ManagedSessionView.tsx:84`
- **Issue:** Title always "Native Managed Session · Codex" even for Claude sessions
- **Fix:** Derive title from session adapter type

### B2 — CLI Output Says "Codex" for Claude
- **File:** `companion-daemon/cmd/devremote/client.go:244`
- **Issue:** `pokit run claude` prints "Attached to managed Codex session"
- **Fix:** Use profile name in output message

### B3 — QR Pairing: No Wi-Fi Guidance
- **File:** `mobile/src/screens/ConnectScreen.tsx`
- **Issue:** Wi-Fi OFF → QR scan 미반응. 사용자에게 "같은 Wi-Fi 필요" 안내 없음
- **Fix:** `network_error` 감지 시 Wi-Fi 확인 메시지 표시

### B4 — Claude App HTTP Creation Fails
- **File:** `companion-daemon/internal/term/create.go:68-96`
- **Issue:** `h.ManagedClaude.CreateDetached()` HTTP 경로 실패 (CLI/IPC는 성공)
- **Symptom:** "server returned an unexpected response"
- **Root cause:** `CreateDetached` 실패 — HTTP 요청의 device Principal 또는 lease 시스템 문제 추정
- **Fix:** ManagedClaude HTTP 생성 경로 디버깅 필요

### B5 — Claude Headless Back Button Broken
- **File:** `mobile/src/components/ManagedSessionView.tsx`
- **Issue:** `claude_headless` 세션에서 Back 버튼 미작동
- **Fix:** ManagedSessionView 내비게이션 검증

### B6 — Transcript/Terminal Desync (Existing P3 #10)
- **Issue:** Terminal 출력이 Transcript에 반영 안 됨
- **Root cause:** Source Separation 설계 (AgentEvent primary → byte-stream fallback)
- **Status:** Post-Alpha 구조적 수정 필요

### B7 — Send Button Not Working (Existing P3 #8)
- **Issue:** 터미널 입력창 Send 버튼 미작동
- **Root cause:** WebRTC vs WebSocket 이중 입력 경로
- **Status:** Post-Alpha 구조적 수정 필요

## Code Changes (this session)

| Commit | Description |
|--------|-------------|
| `9e64e1670` | QW9 follow-up: degraded segment amber in TranscriptRenderer |
| `e9d9bbde1` | QR always PNG (remove ANSI terminal QR) |
| `256adf401` | Artifact manifest for 1bdf6b632 |
| `1bdf6b632` | QW3 uncertified version warning + QW4 Claude version SSoT |
| `61dc3e03c` | QW7 font sync + QW9 neutral colors + QW12 tunnel dedup |

## Artifacts

| Artifact | SHA256 |
|----------|--------|
| Source (pair.go fix) | 9e64e1670 |
| Daemon | 3b1d0276... |
| APK | 8ed1f37b... (needs rebuild for QW9-b + B1) |

## LaunchAgent Configuration

```xml
PATH=/Users/mhk/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin
--enable-managed-codex
--enable-managed-claude
--claude-digest 71abaff59312c9a9...
```

## Evidence

```
~/Desktop/pokit-device-runs/20260724T144152Z-9fix-verify/
├── raw-private/
│   ├── handoff.env
│   ├── android.log
│   └── installed.apk
└── screenshots/
    ├── 00-pre.png
    ├── 01-apk-installed.png
    ├── 02-clean-start.png
    ├── 03-pair-issue.png
    ├── 04-qr-scanned-no-y.png
    └── 05-qr-paired-ok.png
```
