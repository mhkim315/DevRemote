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

## 📱 앱 테스트 및 빌드 방법 (중요)

### 1. 개발 모드 (에뮬레이터)
```bash
cd mobile
npx expo start
```
- Android 에뮬레이터를 띄우고 `a`를 눌러 실행합니다.
- `FeedScreen.tsx`에는 `assets/terminal.html`이 로드되어 있으며, 데몬의 `https://term.fullcount.kr/term/ws`로 자동 연결됩니다.

### 2. APK 빌드 방법 (실제 폰 테스트용)
```bash
cd mobile/android
JAVA_HOME="/Applications/Android Studio.app/Contents/jbr/Contents/Home" \
ANDROID_HOME="$HOME/Library/Android/sdk" \
./gradlew assembleRelease
# 빌드된 APK 경로: mobile/android/app/build/outputs/apk/release/app-release.apk
```

## 🕵️‍♂️ Emulator Spy (AI 자동화 테스트 도구)

AI 에이전트는 폰 화면을 직접 볼 수 없으므로, **Spy 시스템**이 내장되어 있습니다. `FeedScreen` 안의 `terminal.html`이 2초마다 터미널 화면의 텍스트를 긁어서 데몬(`/debug/dump`)으로 보냅니다.

```bash
# 1. 폰(에뮬레이터) 화면 글자 읽기
grep 'PHONE:' /tmp/gz.log | tail -n 5

# 2. 폰의 터미널 뷰로 명령어 원격 주입 (자동 타이핑)
curl -X POST http://127.0.0.1:9171/debug/cmd -d 'echo hello'
```
*주의:* 위 명령을 실행하면 AI가 타이핑한 것처럼 폰 화면의 터미널에 `echo hello`가 실행됩니다.

## 🚀 다음 에이전트가 즉시 해야 할 일 (Phase 4 핵심 과제)

1. **🚨 PTY 아키텍처 대통합 (가장 시급함)**
   - 현재 `pty.go`의 `HandleWS` 함수는 접속할 때마다 새로운 `bash`를 띄우는 치명적 버그(?)가 남아 있습니다. (과거 WebRTC 시절의 잔재)
   - **조치:** `HandleWS`가 새 bash를 띄우지 말고, `tmux new-session -A -s devremote`에 붙도록(`attach`) 코드를 뜯어고쳐야 완벽한 세션 영속성이 보장됩니다.
2. **모바일 네이티브 모달 팝업 띄우기**
   - 현재 `OnApproval`이 트리거되면 데몬이 Push를 쏘기만 합니다.
   - **조치:** 모바일 앱(`FeedScreen.tsx`)에 상태 오버레이 UI를 만들어서, 푸시 알림이 오거나 `[Approval Required]` 상태일 때 화면 아래에 **[Yes / No]** 모달 팝업을 예쁘게 띄우고 터치 시 `y\n`을 전송하도록 만들어야 합니다.
3. **Claude Shell Hook 고도화**
   - 현재 단순 문자열 매칭(`Do you want` 등)만 사용 중이므로, 더 정교한 CLI 래퍼(Wrapper) 스크립트 개발이 필요합니다.
