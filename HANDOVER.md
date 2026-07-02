# DevRemote 인수인계 (2026-07-03 오전)

## 현재 상태

**WebSocket 터미널: 완벽 작동** ✅
- Daemon `:9171` → `/term/` (xterm.js) + `/term/ws` (PTY bash)
- Phone WebView → Daemon URL → WebSocket → 터미널 렌더링
- Spy: `/debug/dump` (화면 캡처) + `/debug/cmd` (명령어 주입)
- FeedScreen: WebView + TextInput + `injectJavaScript` → `window.ws.send()`

## 아키텍처 결정: WebSocket + Cloudflare 터널 (WebRTC 폐기)

**이유**: 
- Local signaling = 같은 네트워크에서만 WebRTC 작동 (터널 불필요면 WebSocket으로 충분)
- WebRTC 디버깅 3일 실패 vs WebSocket 2일 검증 완료
- Cloudflare 터널로 원격 접속 해결 (무료, `./cloudflared tunnel --url http://localhost:9171`)

## 브랜치: `ground-zero`

## 실행

```bash
# 1. Daemon
cd companion-daemon
go build -o devremote ./cmd/devremote/
./devremote
# → http://localhost:9171/term/

# 2. Cloudflare tunnel (원격용)
./cloudflared tunnel --url http://localhost:9171
# → https://xxxx.trycloudflare.com URL

# 3. APK 빌드
cd mobile/android
JAVA_HOME="/Applications/Android Studio.app/Contents/jbr/Contents/Home" \
ANDROID_HOME="$HOME/Library/Android/sdk" \
./gradlew assembleRelease
# FeedScreen.tsx의 TERM_URL을 Wi-Fi IP 또는 Cloudflare URL로 변경 후 빌드
```

## 파일 구조

```
devremote/
├── companion-daemon/
│   ├── cmd/devremote/main.go       (60줄, HTTP + WebRTC + PTY)
│   ├── internal/term/pty.go        (HandleWS + HandleHTML + HandleDump/Cmd + StartPTY)
│   ├── internal/signal/local.go    (local signaling for WebRTC - 미사용)
│   ├── internal/webrtc/session.go  (WebRTC session - 미사용)
│   ├── internal/push/expo.go       (Expo push notifications - 미사용)
│   └── internal/watcher/           (JSONL watcher - 미사용)
├── mobile/
│   ├── App.tsx                     (Dashboard ↔ FeedScreen)
│   ├── src/screens/FeedScreen.tsx  (WebView → daemon URL, TextInput)
│   ├── src/screens/terminalHtml.ts (WebRTC inline HTML - 미사용)
│   ├── src/screens/dashboard/DashboardScreen.tsx
│   └── assets/terminal.html
├── cloudflared                     (Cloudflare tunnel binary)
└── HANDOVER.md
```

## API

| Endpoint | Method | 설명 |
|---|---|---|
| `/term/` | GET | xterm.js HTML + spy (WebSocket auto-connect) |
| `/term/ws` | WebSocket | PTY bash |
| `/debug/dump` | POST | Phone screen dump 수신 |
| `/debug/cmd` | POST | Command queue (AI 주입) |
| `/debug/cmd` | GET | Poll pending command (spy가 2초마다 호출) |
| `/join`, `/poll`, `/send` | HTTP | Local signaling (WebRTC용, 미사용) |

## Spy 활용

```bash
# 폰 화면 보기
grep 'PHONE:' /tmp/gz.log | tail -3

# 명령어 주입 (spy가 2초 내 polling)
curl -X POST http://127.0.0.1:9171/debug/cmd -d 'echo hello'

# Daemon 상태
curl http://127.0.0.1:9171/term/  # 200 = 정상
```

## 앞으로 할 일 (우선순위)

1. **Cloudflare 터널 자동화** — daemon 시작 시 자동 터널, 고정 URL
2. **Omnara Dashboard 통합** — DashboardScreen과 FeedScreen 자연스럽게 연결
3. **푸시 알림 (FCM)** — Expo Push Token 등록 → Claude 승인 시 알림
4. **Claude Shell Hook** — `devremote hook` → `.zshrc`에 alias
5. **iOS 빌드** — EAS Build
6. **Play Store 배포**

## WebRTC 폐기 근거

- Local signaling(`127.0.0.1:9171`): 같은 네트워크에서만 작동 → WebSocket이 더 빠르고 간단
- Public signaling(Oracle 168.107.59.177): 추가 인프라 필요 → Cloudflare 터널이 더 간단
- 3일 디버깅해도 연결 실패 (SDP 교환, CORS, inline HTML origin 문제)
- 터미널 텍스트 전송에서는 WebRTC P2P의 저레이턴시 이점이 거의 없음
