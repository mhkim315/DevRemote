# DevRemote 인수인계 (Phase 4 완료 → Phase 5 준비)

## 🌟 현재 상태 (2026-07-03 오후)

Phase 4 전체(Quick Actions Bar, 다중 에이전트 대시보드, 세션 격리, Claude Hook) 구현 및 **실기기(LTE) 테스트 완료**. 모바일 매크로 시스템과 터미널 UI가 Claude Code를 포함한 TUI 앱에서도 안정적으로 동작함을 확인했습니다.

---

## 🔧 실기기 테스트 중 발견 및 해결된 크리티컬 버그 (7개)

| # | 문제 | 근본 원인 | 해결 |
|---|------|-----------|------|
| 1 | **WebSocket 3초 disconnect 루프** | `/debug/dump` POST의 ANSI 데이터를 통신사 WAF가 쉘 공격으로 오인해 TCP RST → WebSocket 끊김 | `term.html`에서 `/debug/dump` POST 제거 |
| 2 | **Enter/Y/N이 Claude에서 안 먹힘** | TUI 앱은 Raw 모드로 `\r`(CR)을 기대하는데 `\n`(LF)을 보내고 있었음 | 모든 전송을 `\n`(10) → `\r`(13)로 변경 |
| 3 | **tmux 즉시 종료 (Crash)** | `pty.Start` 호출 시 `TERM` 환경변수 미설정 → tmux가 "terminal does not support clear" 오류로 종료 | `cmd.Env = append(os.Environ(), "TERM=xterm-256color")` |
| 4 | **화살표 키(↑↓←→) 무시됨** | ANSI 시퀀스(`ESC [ A`)를 3개 메시지로 분할 전송 → tmux가 첫 `ESC`만 단독 인식 | `String.fromCharCode.apply(null, arr)`로 1개 메시지로 묶어 전송 |
| 5 | **화면 절반만 사용** | PTY 기본 사이즈 80×24 고정 | `xterm-addon-fit` + `/term/resize` API → 100×60 최적 사이즈 |
| 6 | **WebView 무한 리로드** | `source={{uri: termUrl}}`이 매 렌더 새 객체 생성 | `useMemo`로 `source` 객체 고정 |
| 7 | **PTY 중복 attach** | 재연결마다 새 `tmux attach` → N개 PTY가 같은 세션에 붙어 TUI 깨짐 | `sessionConns` 맵으로 세션당 1연결만 허용 |

---

## 📁 파일 구조

```
devremote/
├── companion-daemon/
│   ├── cmd/devremote/main.go       (HTTP 서버 + /api/sessions + cloudflared)
│   ├── internal/term/
│   │   ├── pty.go                  (HandleWS, HandleHTML, sessionConns, TERM fix)
│   │   └── mux.go                  (Multiplexer 인터페이스 + TmuxMux)
│   ├── internal/push/expo.go       (Expo Push API)
│   └── devremote                   (빌드된 바이너리, gitignore)
├── mobile/
│   ├── App.tsx                     (Dashboard↔FeedScreen 라우팅 + Push)
│   ├── src/screens/FeedScreen.tsx  (WebView + 매크로바 + \r 전송 + 키보드)
│   └── src/screens/dashboard/DashboardScreen.tsx (API 기반 세션 카드)
├── HANDOVER.md
└── TESTING.md                      (실기기 테스트 가이드)
```

## 📡 API

| Endpoint | Method | 설명 |
|---|---|---|
| `/term/` | GET | xterm.js HTML + SPYCODE (Query: `?session=...`) |
| `/term/ws` | WebSocket | tmux PTY (Query: `?session=...`) |
| `/term/resize` | POST | PTY 리사이즈 (Query: `?session=&cols=&rows=`) |
| `/api/sessions` | GET | 활성화된 터미널 세션 목록 (JSON Array) |
| `/debug/cmd` | POST/GET | AI 명령어 주입/폴링 (Query: `?session=...`) |
| `/push/register` | GET | Expo Push Token 등록 |

> ⚠️ `/debug/dump` 엔드포인트는 WAF 이슈로 SPYCODE에서 제거됨. 필요시 재활성화 전에 payload sanitize 필요.

## 🚀 실행 방법

```bash
# 1. Daemon 켜기 (tmux + cloudflared 필요)
brew install tmux
cd companion-daemon
go build -o devremote ./cmd/devremote/
./devremote

# 2. 테스트용 tmux 세션
tmux new -s aider -d
tmux new -s frontend -d

# 3. APK 빌드
cd mobile/android
JAVA_HOME="/Applications/Android Studio.app/Contents/jbr/Contents/Home" \
ANDROID_HOME="$HOME/Library/Android/sdk" \
./gradlew assembleRelease
```

## 🕵️ SPYCODE 사용법

```bash
# 특정 세션 명령어 주입
curl -X POST "http://127.0.0.1:9171/debug/cmd?session=frontend" -d 'echo hello'

# 세션 목록
curl http://127.0.0.1:9171/api/sessions

# PTY 리사이즈
curl -X POST "http://127.0.0.1:9171/term/resize?session=frontend&cols=100&rows=40"
```

## ⚠️ 알려진 이슈

1. **`/debug/dump` 비활성화** — WAF 트리거로 인해 SPYCODE의 화면 읽기 기능이 제거됨. 복구하려면 payload sanitize 필요.
2. **Cloudflare 터널 간헐적 불안정** — `cloudflared tunnel cleanup devremote` 후 재시작 필요.
3. **터미널 사이즈 100×60 고정** — 현재 PC/모바일 절충 사이즈. 완전 동적 리사이즈는 `/term/resize` API로 가능하나 모바일에서 가로 100은 살짝 넘침.

## 🚀 Phase 5 (다음 작업 - SaaS 마이그레이션)

1. **인증 (Supabase Auth)** — 로그인/회원가입, JWT 토큰, 데몬 401 미인증 차단
2. **동적 터널링** — 유저별 랜덤 터널 URL 할당, `term.fullcount.kr` 고정 주소 폐기
3. **Push Relay 서버 분리** — 중앙 Push 서버로 Expo 알림 중계
