# DevRemote 인수인계 (Phase 3 완료 - 최종 아키텍처 확정)

## 🌟 현재 상태 (Phase 3 달성)

WebRTC의 모순을 버리고 **"Cloudflare 고정 터널 + WebSocket"** 으로 최종 진화했습니다.

1. **영구 고정 터널 (`term.fullcount.kr`)** ✅
   - 사용자 소유 도메인(`fullcount.kr`)으로 Cloudflare 터널 `devremote`를 영구 생성.
   - 앱 소스코드(`FeedScreen.tsx`)에 주소를 고정으로 박아넣어 "1초 자동 연결" UX 완성.
2. **데몬 완전 자동화 (`main.go`)** ✅
   - 데몬 실행 시 `cloudflared tunnel run devremote` 서브프로세스를 알아서 백그라운드에 띄움.
3. **Omnara Dashboard 통합** ✅
   - `App.tsx`에서 정적 대시보드 렌더링. 에이전트 터치 시 즉각 터미널(`FeedScreen.tsx`) 진입.
4. **Push 알림 인프라 (FCM/Expo)** ✅
   - 폰에서 Expo Push Token을 발급받아 Mac 데몬(`/push/register`)에 등록.
   - PTY에서 `[Approval Required]` 출력 시, 데몬이 감지하여 Expo API로 푸시 발송.
5. **PTY 영속성 확보 (`tmux`)** ✅
   - 터널이 끊기더라도 `tmux`를 사용하여 이전 세션이 날아가지 않고 그대로 유지됨.

## 📁 실행 방법

```bash
# 1. Daemon 켜기 (터널 자동 실행됨)
cd companion-daemon
go build -o devremote ./cmd/devremote/
./devremote
# → 로컬 9171 포트 및 term.fullcount.kr 동시 서빙 시작

# 2. 모바일 앱 빌드 및 실행
cd mobile
npx expo start
```

## 🏗 파일 구조 변화 (Phase 3 핵심)

```
devremote/
├── companion-daemon/
│   ├── cmd/devremote/main.go       (HTTP 서버 + Push 토큰 등록 + cloudflared 자동실행)
│   ├── internal/term/pty.go        (tmux PTY + [Approval Required] 감지 푸시 후킹 + WebSocket)
│   └── cloudflared                 (터널 바이너리, main.go가 호출함)
├── mobile/
│   ├── App.tsx                     (Expo Push Token 발급 및 등록 + Dashboard 라우팅)
│   ├── src/screens/FeedScreen.tsx  (고정 주소 term.fullcount.kr/term/ WebView)
│   ├── assets/terminal.html        (local emulator spy 로직 포함)
└── HANDOVER.md
```

## 📡 API (최종 확정)

| Endpoint | Method | 설명 |
|---|---|---|
| `/term/` | GET | xterm.js HTML + spy (WebSocket auto-connect) |
| `/term/ws` | WebSocket | tmux PTY bash (메인 통신 채널) |
| `/debug/dump` | POST | Phone screen dump (에뮬레이터 디버깅 전용) |
| `/debug/cmd` | POST, GET | Command queue (AI가 명령어 주입 시 사용) |
| `/push/register` | GET | 모바일 앱에서 발급받은 Expo Push Token 등록 |

## 🚀 다음 에이전트가 할 일 (Phase 4 우선순위)

1. **Claude Shell Hook 고도화**
   - 현재 데몬에서 `[Approval Required]` 문자열만 감지하고 있음.
   - `devremote hook` 명령어를 만들어 `.zshrc`에 등록하고, Aider/Claude 등 실제 AI 에이전트의 프롬프트 입력을 가로채서 더 정교하게 푸시를 쏘는 기능 구현 필요.
2. **Dashboard 동적 데이터 연동**
   - 현재 `DashboardScreen.tsx`의 에이전트 목록이 하드코딩되어 있음.
   - 데몬이 실행 중인 `tmux` 세션들이나 AI 프로세스들을 스캔해서 앱의 대시보드 리스트에 동적으로 띄우는 기능.
3. **앱 배포 파이프라인 (EAS Build)**
   - iOS/Android 스토어 정식 배포 준비.
