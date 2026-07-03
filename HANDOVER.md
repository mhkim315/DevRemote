# DevRemote 인수인계 (Phase 4-2 완료 → Phase 4-3 준비)

## 🌟 현재 상태 (2026-07-03 오전)

Phase 4-1(Quick Actions Bar) 검증 완료, **Phase 4-2 (다중 에이전트 동적 대시보드 & 멀티플렉서 추상화)** 까지 구현 및 검증 완료.

### 완료된 작업

1. **Multiplexer 추상화 아키텍처** ✅
   - `internal/term/mux.go` 생성하여 `Multiplexer` 인터페이스 정의
   - `TmuxMux` 구조체: `ListSessions()`, `AttachCmd(sessionName)` 구현
   - `DefaultMux` 글로벌 변수로 DI (향후 Zellij, Cumx 등 확장 가능)

2. **다중 에이전트 동적 대시보드** ✅
   - `GET /api/sessions` 엔드포인트 — 활성 tmux 세션 목록 JSON 반환
   - `DashboardScreen.tsx` — API 호출하여 세션 카드 동적 렌더링
   - `App.tsx` — `currentSession` state, FeedScreen에 `session` prop 전달

3. **동적 세션 라우팅** ✅
   - `FeedScreen.tsx` — `?session=<name>` 쿼리 파라미터로 터미널 URL 구성
   - `HandleWS` — 쿼리 파라미터 읽어 `DefaultMux.AttachCmd(session)` 호출
   - 존재하지 않는 세션은 `tmux new-session -A -s` 로 자동 생성

4. **Quick Actions Bar (Phase 4-1)** ✅
   - 11개 매크로 버튼: `[Ctrl+C] [Esc] [Tab] [↑] [↓] [Y] [N] [1] [2] [3] [Enter]`

5. **SPYCODE 크로스 플랫폼 지원** ✅
   - Android WebView Blob/ArrayBuffer 대응 (`ws.binaryType='arraybuffer'` + `TextDecoder`)
   - ANSI/제어문자 필터링으로 화면 텍스트 추출

## 🔍 검증 결과 (2026-07-03)

### 통과 항목

| 테스트 | 상태 | 증거 |
|--------|------|------|
| `GET /api/sessions` | ✅ | `["aider","frontend"]` 실제 세션 반환 |
| `DashboardScreen` 동적 렌더링 | ✅ | API → 카드 목록 생성, 로딩 스피너 |
| `FeedScreen ?session=` 라우팅 | ✅ | `https://term.fullcount.kr/term/?session=aider` HTTP 200 |
| `HandleWS` 멀티플렉서 연동 | ✅ | `DefaultMux.AttachCmd(session)` → tmux 세션 attach |
| SPYCODE 화면 덤프 | ✅ | `mhk@mhkair devremote %` 프롬프트 확인 |
| WebSocket 세션별 연결 | ✅ | `WS connected: [::1]:59527` |
| `mux.go` 아키텍처 | ✅ | 인터페이스 기반, Tmux/Zellij/Cumx 확장 가능 |
| 터널 (`term.fullcount.kr`) | ✅ | HTTP 200 정상 프록시 |
| APK 빌드 (release) | ✅ | 113MB, 설치 성공 |

### 발견된 이슈

| # | 심각도 | 내용 | 권장 조치 |
|---|--------|------|-----------|
| 1 | 🟡 중간 | **`/debug/cmd` 세션 무관함** — 모든 클라이언트가 같은 엔드포인트 공유. 여러 기기 연결 시 명령어가 엉뚱한 세션에서 실행됨. | `?session=` 파라미터 추가 또는 WebSocket으로 명령어 라우팅 |
| 2 | 🟢 낮음 | **ANSI 필터링 불완전** — ESC(`\x1b`) 제거 후 `[?1049h` 등 잔여 시퀀스가 SPYCODE 출력에 남음. | 정규식을 `\x1b\[[0-9;?]*[a-zA-Z]` 로 확장 |
| 3 | 🟢 낮음 | **`HandleSessions` 수동 JSON 빌드** — `[]byte` 조합으로 JSON 생성 중. 세션명에 `"` 포함 시 깨짐. | `encoding/json` 패키지로 교체 |
| 4 | 🟢 낮음 | **PHONE 덤프에 Mac 브라우저 데이터 혼재** — 동일 IP(`127.0.0.1`)로 여러 클라이언트 접속 시 구분 불가. | User-Agent 기반 필터링 또는 세션별 dump 엔드포인트 |

## 📁 파일 구조

```
devremote/
├── companion-daemon/
│   ├── cmd/devremote/main.go       (HTTP 서버 + /api/sessions + cloudflared)
│   ├── internal/term/
│   │   ├── pty.go                  (HandleWS, HandleSessions, HandleHTML+SPYCODE, HandleCmd, HandleDump)
│   │   └── mux.go                  (Multiplexer 인터페이스 + TmuxMux + DefaultMux)
│   ├── internal/push/expo.go       (Expo Push API)
│   └── devremote                   (빌드된 바이너리, gitignore)
├── mobile/
│   ├── App.tsx                     (Dashboard↔FeedScreen 라우팅 + Push + session state)
│   ├── src/screens/FeedScreen.tsx  (WebView + Quick Actions Bar + session prop)
│   └── src/screens/dashboard/DashboardScreen.tsx (API 기반 세션 카드 동적 렌더링)
└── HANDOVER.md
```

## 📡 API

| Endpoint | Method | 설명 |
|---|---|---|
| `/term/` | GET | xterm.js HTML + SPYCODE (Query: `?session=...`) |
| `/term/ws` | WebSocket | tmux PTY (Query: `?session=...`) |
| `/api/sessions` | GET | 활성화된 터미널 세션 목록 (JSON Array) |
| `/debug/dump` | POST | SPYCODE 화면 덤프 (2초 간격) |
| `/debug/cmd` | POST/GET | AI 명령어 주입/폴링 (⚠️ 세션 구분 없음) |
| `/push/register` | GET | Expo Push Token 등록 |

## 🚀 실행 방법

```bash
# 1. Daemon 켜기 (tmux 필요)
brew install tmux       # tmux가 없다면 필수 설치
cd companion-daemon
go build -o devremote ./cmd/devremote/
./devremote
# → 로컬 9171 + Cloudflare 터널 자동 실행

# 2. 테스트용 tmux 세션 만들기 (Mac에서)
tmux new -s aider -d
tmux new -s frontend -d
# 대시보드에 aider, frontend 카드가 표시됨

# 3. APK 빌드
cd mobile/android
JAVA_HOME="/Applications/Android Studio.app/Contents/jbr/Contents/Home" \
ANDROID_HOME="$HOME/Library/Android/sdk" \
./gradlew assembleRelease
# APK: mobile/android/app/build/outputs/apk/release/app-release.apk
```

## 🕵️ SPYCODE 사용법

```bash
# 폰 화면 읽기 (실시간)
grep 'PHONE:' /tmp/gz.log | tail -5

# 폰 터미널에 명령어 원격 주입
# ⚠️ 여러 클라이언트 연결 시 엉뚱한 세션에서 실행될 수 있음 (이슈 #1)
curl -X POST http://127.0.0.1:9171/debug/cmd -d 'echo hello'
```

## ⚠️ 알려진 이슈

1. **Cloudflare 터널 간헐적 불안정**
   - 잦은 kill/restart 시 "unknown error registering the connection" 발생
   - 해결: `cloudflared tunnel cleanup devremote` → 재시작

2. **에뮬레이터 vs 실폰 URL**
   - 에뮬레이터: `http://10.0.2.2:9171/term/`
   - 실폰: `https://term.fullcount.kr/term/`
   - 환경변수나 빌드 플레이버로 분리 필요

3. **tmux 미설치 환경**
   - tmux 없으면 자동 bash fallback (세션 영속성 없음)

## 🚀 Phase 4-3 (다음 에이전트 작업)

### 우선순위 1: `/debug/cmd` 세션 인식 (이슈 #1)
- `POST /debug/cmd?session=aider` 로 세션별 명령어 라우팅
- 또는 WebSocket 메시지로 명령어 직접 전송

### 우선순위 2: Claude Shell Hook 고도화
- 현재 `[Approval Required]` 정규식 매칭 → 구체적인 질문 텍스트 파싱
- Push 메시지 Body에 AI 질문 내용 포함

### 우선순위 3: 모바일 UX 다듬기
- 터미널 스크롤 제스처
- ANSI 필터링 정규식 개선 (이슈 #2 포함)
- `HandleSessions` JSON `encoding/json` 전환 (이슈 #3)

### 우선순위 4: Phase 5 준비
- Supabase Auth 연동 설계
- 동적 터널링 (Multi-Tenant)
