# DevRemote 인수인계 (Phase 4-1 완료)

## 🌟 현재 상태 (2026-07-03 오전)

Phase 3 완료 후 **Phase 4-1 (Quick Actions Bar + SPYCODE 안정화)** 까지 달성.

### 완료된 작업

1. **WebRTC 100% 제거** ✅
   - `internal/webrtc/`, `internal/signal/` 패키지 삭제
   - `main.go`에서 WebRTC import 제거 (111줄 → 71줄)
   - `StartPTY` 함수 제거, `HandleWS` 하나로 PTY 단일화

2. **PTY 파이프라인 단일화** ✅
   - `HandleWS` → `tmux new-session -A -s devremote` (없으면 bash fallback)
   - 접속 끊겨도 tmux 세션 유지 (`cmd.Process.Kill()` 제거)
   - Approval 감지 통합: `isApprovalPrompt()` + `OnApproval` 콜백

3. **Quick Actions Bar (모바일 매크로 키보드)** ✅
   - `FeedScreen.tsx` 상단에 가로 스크롤 매크로 버튼 11개
   - `[Ctrl+C] [Esc] [Tab] [↑] [↓] [Y] [N] [1] [2] [3] [Enter]`
   - `injectJavaScript`로 WebSocket에 직접 바이트코드 주입

4. **SPYCODE 안정화 (Android WebView 대응)** ✅
   - Android WebView가 WebSocket 데이터를 Blob으로 수신하는 문제 해결
   - `ws.binaryType='arraybuffer'` + `TextDecoder` 크로스 플랫폼 대응
   - ANSI/제어문자 필터링으로 깔끔한 텍스트 추출
   - 검증됨: `curl -X POST :9171/debug/cmd` → bash PTY → PHONE 덤프 확인

5. **Cloudflare 터널 복구** ✅
   - `~/.cloudflared/config.yml` ingress rules 설정
   - `cloudflared tunnel cleanup devremote` → 재시작으로 복구
   - `term.fullcount.kr` → `:9171` 정상 프록시 (HTTP 200)

## 📁 파일 구조 (현재)

```
devremote/
├── companion-daemon/
│   ├── cmd/devremote/main.go       (HTTP 서버 + Push + cloudflared 자동실행)
│   ├── internal/term/pty.go        (HandleWS: tmux PTY + Approval + SPYCODE HTML)
│   ├── internal/push/expo.go       (Expo Push API)
│   ├── internal/watcher/tailer.go  (Claude JSONL 로그 tailing)
│   └── devremote                   (빌드된 바이너리, gitignore)
├── cloudflared                     (터널 바이너리, gitignore)
├── mobile/
│   ├── App.tsx                     (Dashboard + FeedScreen 라우팅 + Push 등록)
│   ├── src/screens/FeedScreen.tsx  (WebView + Quick Actions Bar + Send 입력)
│   └── src/screens/dashboard/DashboardScreen.tsx
└── HANDOVER.md
```

## 📡 API

| Endpoint | Method | 설명 |
|---|---|---|
| `/term/` | GET | xterm.js HTML + SPYCODE |
| `/term/ws` | WebSocket | tmux PTY (메인 통신) |
| `/debug/dump` | POST | SPYCODE 화면 덤프 (2초 간격) |
| `/debug/cmd` | POST | AI 명령어 주입 (phone 터미널에서 실행) |
| `/debug/cmd` | GET | Phone이 pending 명령어 폴링 |
| `/push/register` | GET | Expo Push Token 등록 |

## 🚀 실행 방법

```bash
# 1. Daemon 켜기 (터널 자동 실행됨)
cd companion-daemon
go build -o devremote ./cmd/devremote/
./devremote
# → 로컬 9171 + term.fullcount.kr 서빙

# 2. APK 빌드
cd mobile/android
JAVA_HOME="/Applications/Android Studio.app/Contents/jbr/Contents/Home" \
ANDROID_HOME="$HOME/Library/Android/sdk" \
./gradlew assembleRelease
# APK: mobile/android/app/build/outputs/apk/release/app-release.apk
```

## 🕵️ SPYCODE 사용법

```bash
# 폰 화면 읽기
grep 'PHONE:' /tmp/gz.log | tail -5

# 폰 터미널에 명령어 원격 주입
curl -X POST http://127.0.0.1:9171/debug/cmd -d 'echo hello'
```

## ⚠️ 알려진 이슈

1. **Cloudflare 터널 간헐적 불안정**
   - 잦은 kill/restart 시 "unknown error registering the connection" 발생
   - 해결: `cloudflared tunnel cleanup devremote` 실행 후 재시작
   - 터널이 죽으면 `term.fullcount.kr` 접속 불가 → FeedScreen URL을 `10.0.2.2:9171`로 임시 변경

2. **에뮬레이터 vs 실폰 URL**
   - 에뮬레이터: `http://10.0.2.2:9171/term/`
   - 실폰: `https://term.fullcount.kr/term/`
   - 환경변수나 빌드 플레이버로 분리 필요

3. **tmux 미설치 환경**
   - cmux 등 tmux 없는 환경에서는 자동 bash fallback
   - bash는 세션 영속성 없음 (WebSocket 끊기면 히스토리 날아감)

## 🚀 Phase 4-2 (다음 작업)

1. **다중 에이전트 동적 대시보드**
   - `tmux list-sessions` 스캔하여 활성 세션 목록 반환
   - `GET /api/sessions` 엔드포인트 추가
   - `DashboardScreen.tsx`에서 API 호출하여 하드코딩된 리스트 대체

2. **Claude Shell Hook 고도화**
   - 단순 문자열 매칭 → CLI 래퍼(`devremote hook`) 개발
   - AI 질문을 폰에 모달로 띄우고 Yes/No 응답을 터미널에 자동 주입

3. **모바일 코더 전용 키보드 매크로** ← Phase 4-1 완료됨
