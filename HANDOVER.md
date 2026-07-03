# DevRemote 인수인계 (Phase 4-3 완료 → Phase 5 준비)

## 🌟 현재 상태 (2026-07-03 오전)

Phase 4-1(Quick Actions Bar), **Phase 4-2 (다중 에이전트 대시보드)**에 이어 **Phase 4-3 (세션 격리 및 Claude Hook 원문 추출 고도화)** 까지 완벽하게 구현 및 자체 검증 완료되었습니다.

### 완료된 작업 (최근 Phase 4-3)

1. **완벽한 멀티-세션 분리 (`/debug/cmd`, `/debug/dump`)** ✅
   - `pty.go`의 `pendingCmd`를 `map[string]string`으로 변경하여 명령어 혼선 방지.
   - API 호출 시 `?session=<name>` 파라미터를 받아 정확하게 타겟 세션으로 명령을 라우팅하고, 화면 덤프 역시 `PHONE [세션명]: ...` 형태로 독립 로깅되도록 수정.

2. **Claude Shell Hook 질문 원문 추출 고도화** ✅
   - 기존의 단순 `[Approval Required]` 정규식 매칭을 개선.
   - AI의 질문(예: `Do you want to proceed?`)을 감지하면 ANSI 색상 코드를 필터링하고 질문의 **실제 문맥 150자를 추출**하여 Push Notification의 Body에 담아 모바일로 전송.
   - `main.go`의 푸시 알림 Payload를 안전한 `encoding/json` 방식으로 직렬화.

3. **마이너 버그 픽스** ✅
   - `HandleSessions`의 수동 JSON 빌드를 `encoding/json` 패키지로 교체.
   - SPYCODE HTML의 잔여 ANSI 시퀀스(`[?1049h` 등)를 `\x1b\[[0-9;?]*[a-zA-Z]` 정규식으로 완벽히 필터링.

## 📁 파일 구조

```
devremote/
├── companion-daemon/
│   ├── cmd/devremote/main.go       (HTTP 서버 + Push JSON Payload화 + cloudflared)
│   ├── internal/term/
│   │   ├── pty.go                  (세션별 명령어 라우팅, 질문 원문 추출 알고리즘)
│   │   └── mux.go                  (Multiplexer 인터페이스)
│   ├── internal/push/expo.go       (Expo Push API)
│   └── devremote                   (빌드된 바이너리, gitignore)
├── mobile/
│   ├── App.tsx                     (Dashboard↔FeedScreen 라우팅 + Push)
│   ├── src/screens/FeedScreen.tsx  (WebView + Quick Actions Bar + session prop)
│   └── src/screens/dashboard/DashboardScreen.tsx 
└── HANDOVER.md
```

## 📡 API

| Endpoint | Method | 설명 |
|---|---|---|
| `/term/` | GET | xterm.js HTML + SPYCODE (Query: `?session=...`) |
| `/term/ws` | WebSocket | tmux PTY (Query: `?session=...`) |
| `/api/sessions` | GET | 활성화된 터미널 세션 목록 (JSON Array) |
| `/debug/dump` | POST | SPYCODE 화면 덤프 (Query: `?session=...`) |
| `/debug/cmd` | POST/GET | AI 명령어 주입/폴링 (Query: `?session=...`) |
| `/push/register` | GET | Expo Push Token 등록 |

## 🚀 실행 방법

```bash
# 1. Daemon 켜기 (tmux 필요)
brew install tmux
cd companion-daemon
go build -o devremote ./cmd/devremote/
./devremote
# → 로컬 9171 + Cloudflare 터널 자동 실행

# 2. 테스트용 tmux 세션 만들기 (Mac에서)
tmux new -s aider -d
tmux new -s claude -d
# 폰에서 /debug/cmd?session=aider 로 독립 제어 가능!

# 3. APK 빌드
cd mobile/android
JAVA_HOME="/Applications/Android Studio.app/Contents/jbr/Contents/Home" \
ANDROID_HOME="$HOME/Library/Android/sdk" \
./gradlew assembleRelease
```

## 🕵️ SPYCODE 사용법

```bash
# 특정 세션 폰 화면 읽기 (실시간)
grep 'PHONE \[aider\]:' /tmp/gz.log | tail -5

# 특정 폰 터미널(세션)에 명령어 원격 주입 (이슈 해결됨!)
curl -X POST "http://127.0.0.1:9171/debug/cmd?session=aider" -d 'echo hello aider'
```

## 🚀 Phase 5 (다음 에이전트 작업 - SaaS 마이그레이션)

이제 단일 기기(Single-Tenant)용으로 구현된 모든 UX와 파이프라인이 완벽하게 동작합니다. 본격적인 **Multi-Tenant SaaS 화**를 시작합니다.

1. **Supabase Auth 연동**
   - 모바일 앱에 Supabase 로그인/회원가입 화면 추가.
   - 로그인 시 발급받은 JWT 토큰을 데몬 및 앱에서 관리.
2. **동적 터널링 할당 (Cloudflare / Tailscale)**
   - 유저가 가입하면 자동으로 `유저명.devremote.io` 와 같은 동적 터널 주소를 할당받아, 유저 본인의 Mac과 1:1로 매핑되는 구조 설계.
3. **Push Relay 서버 분리**
   - 현재는 `main.go`가 직접 Expo에 Push를 쏘고 있지만, SaaS 환경에서는 중앙 릴레이 서버가 필요할 수 있음.
