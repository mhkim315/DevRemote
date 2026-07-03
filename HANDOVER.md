# DevRemote 인수인계 (Phase 4 완료 → Phase 5 준비)

## 🌟 현재 상태 (2026-07-03 오후)

Phase 4 전체(Quick Actions Bar, 다중 에이전트 대시보드, 세션 격리, Claude Hook, Copy/Paste, Scroll Mode) 구현 및 **실기기(LTE) 테스트 완료**.

---

## 🎯 오늘 실기기 테스트로 발견 & 해결된 크리티컬 버그 (7개)

| # | 문제 | 근본 원인 | 해결 |
|---|------|-----------|------|
| 1 | **WebSocket 3초 disconnect 루프** | `/debug/dump` POST의 ANSI 데이터를 통신사 WAF가 쉘 공격으로 오인 | `term.html`에서 dump POST 제거 |
| 2 | **Enter/Y/N이 TUI에서 안 먹힘** | Raw 모드 TUI는 `\r`(CR) 기대, `\n`(LF) 보내고 있었음 | 모든 전송 `\n`→`\r` |
| 3 | **tmux 즉시 Crash** | `TERM` 환경변수 미설정 | `TERM=xterm-256color` |
| 4 | **화살표 키 무시됨** | ANSI 시퀀스 3분할 전송 → tmux가 첫 ESC만 인식 | `String.fromCharCode.apply`로 1메시지 통합 |
| 5 | **화면 절반만 사용** | PTY 80×24 고정 | 100×60 + `/term/resize` API |
| 6 | **WebView 무한 리로드** | `source={{uri}}` 매 렌더 새 객체 | `useMemo` |
| 7 | **PTY 중복 attach** | N개 tmux 클라이언트 경쟁 | `sessionConns` 1:1 |

## 📱 Phase 4 최종 기능 목록

| 기능 | 설명 |
|------|------|
| **Quick Actions Bar** | 13개 매크로: Ctrl+C(x2), Ctrl+D, Esc, Tab, ↑↓←→, Y, N, Enter |
| **Contextual Scroll Mode** | 토글 시 매크로바가 PgUp/PgDn/LineUp/LineDown/Exit Scroll로 전환 |
| **Copy** | 터미널 텍스트를 Modal에 띄워 Long Press 선택 가능 |
| **Paste** | 클립보드 내용을 터미널로 즉시 전송 (멀티라인 \r 변환) |
| **동적 대시보드** | `GET /api/sessions` → tmux 세션 목록 카드 렌더링 |
| **세션 라우팅** | `?session=name` → 정확한 tmux 세션에 attach |
| **세션 격리** | `pendingCmds[ session ]` + `PHONE [ session ]` 로 완전 분리 |
| **Claude Hook** | Approval 감지 → 질문 원문 150자 추출 → Push 발송 |
| **WebSocket 안정화** | `reconnecting` 플래그, 세션당 1연결, `TERM` 환경변수 |
| **키보드 UX** | Android `Keyboard.addListener` 동적 패딩, `multiline={false}` |
| **Enter 키** | Raw TUI 호환 `\r` 전송, `handleChangeText` 폴백 감지 |
| **ANSI 화살표** | 1메시지 통합 전송으로 tmux escape-time 문제 해결 |

## 📁 파일 구조

```
devremote/
├── companion-daemon/
│   ├── cmd/devremote/main.go       (HTTP + /api/sessions + /push/register + cloudflared)
│   ├── internal/term/
│   │   ├── pty.go                  (HandleWS, HandleHTML, HandleCmd, HandleDump,
│   │   │                             HandleSessions, sessionConns, TERM fix, \r 지원)
│   │   └── mux.go                  (Multiplexer 인터페이스 + TmuxMux)
│   ├── internal/push/expo.go       (Expo Push API)
│   └── devremote                   (빌드 바이너리, gitignore)
├── mobile/
│   ├── App.tsx                     (Dashboard↔FeedScreen + Push + session state)
│   ├── src/screens/FeedScreen.tsx  (WebView + Contextual Macros + Copy/Paste Modal
│   │                                 + Scroll Mode + Keyboard + \r 전송)
│   └── src/screens/dashboard/DashboardScreen.tsx (API 기반 세션 카드)
├── HANDOVER.md
├── TESTING.md
└── BUG_WEBSOCKET_LOOP.md           (3초 루프 디버깅 기록)
```

## 📡 API

| Endpoint | Method | 설명 |
|---|---|---|
| `/term/` | GET | xterm.js HTML + SPYCODE (Query: `?session=...`) |
| `/term/ws` | WebSocket | tmux PTY (Query: `?session=...`) |
| `/term/resize` | POST | PTY 리사이즈 (`?session=&cols=&rows=`) |
| `/api/sessions` | GET | 활성 tmux 세션 목록 (JSON Array) |
| `/debug/cmd` | POST/GET | 명령어 주입/폴링 (`?session=...`) |
| `/push/register` | GET | Expo Push Token 등록 |

> ⚠️ `/debug/dump`는 WAF 이슈로 JS에서 제거됨. 필요시 payload sanitize 후 복구.

## 🚀 실행

```bash
# Daemon
cd companion-daemon && go build -o devremote ./cmd/devremote/ && ./devremote

# APK
cd mobile/android
JAVA_HOME="/Applications/Android Studio.app/Contents/jbr/Contents/Home" \
ANDROID_HOME="$HOME/Library/Android/sdk" \
./gradlew assembleRelease
```

## ⚠️ 알려진 이슈

1. **터널 간헐적 불안정** — `cloudflared tunnel cleanup devremote` 후 재시작
2. **`/debug/dump` 비활성화** — SPYCODE 화면 읽기 불가. WAF-safe 포맷으로 재구현 필요
3. **PTY 100×60 고정** — `/term/resize`로 동적 조절 가능하나 기본값 하드코딩

## 🚀 Phase 5 (다음)

1. **Supabase Auth** — JWT 인증, 데몬 401 미인증 차단
2. **동적 터널링** — 유저별 랜덤 URL, `term.fullcount.kr` 고정 폐기
3. **Push Relay 분리** — 중앙 Push 서버
