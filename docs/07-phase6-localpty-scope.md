# Phase 6 — LocalPTYAdapter Scope

Date: 2026-07-07

## 선택 근거

Phase 6는 실제 제3 backend vertical slice. LocalPTYAdapter를 선택한 이유:

1. **외부 의존성 최소화** — zellij, wezterm 등 별도 daemon 불필요. shell과 OS PTY만 필요.
2. **tmux/cmux와 다른 계열** — tmux/cmux는 외부 session manager, LocalPTY는 앱이 session
   owner. 이 차이가 adapter abstraction의 완전성을 검증한다.
3. **architecture 검증력** — session lifecycle ownership, create-only discovery, PTY
   lifecycle 동시성 처리가 tmux/cmux와 완전히 다른 패턴.
4. **SSH보다 단순** — SSH는 인증/네트워크/host key/권한 이슈가 adapter 검증 전에
   튀어나오므로 Phase 6 목적에 맞지 않음.

## Scope

### Backend 이름

`localpty`

### Session Lifecycle Ownership

- **앱이 생성한 local PTY session만 registry에 보관**
- 기존 OS terminal, arbitrary process는 discover하지 않음
- daemon restart 시 session 복구는 초기 미지원 (Phase 7 검토)
- `ListSessions`는 현재 active/in-memory session만 반환
- `CreateSession`으로 생성 → app lifetime 동안 관리 → `TerminateSession`으로 정리

### Supported Capabilities (초기)

| Capability | 지원 | 비고 |
|-----------|------|------|
| `Adapter.Name` | ✅ `"localpty"` | |
| `ListSessions` | ✅ | 생성된 세션만 |
| `CreateSession` | ✅ | child process PTY |
| `TerminateSession` | ✅ | process kill + cleanup |
| `StreamOpener` | ✅ | PTY read/write |
| `ScreenReader` | ❌ | live stream 기반, 별도 screen capture 미지원 |
| `HistoryReader` | ❌ | Phase 6 초기 미지원 |
| `ProcessProvider` | ❌ | Phase 7 검토 |
| `InputWriter` | ✅ | PTY stdin write |

### Production Registration

```go
// app.go: feature flag 뒤에서만 등록
if cfg.EnableLocalPTY {
    reg.Register(mux.NewLocalPTYAdapter())
}
```

- 기본값 `false`
- `POKIT_ENABLE_LOCALPTY=1` 또는 config flag로 활성화

### Hard Constraints

1. tmux/cmux production 파일 0줄 변경 (등록 지점 app.go +1 conditional line 제외)
2. localpty 실패가 registry 전체에 영향 없어야 함
3. history/screen 미지원 → capability로 명시 → API/UI가 안전히 처리

## 구현 단계

### Step 2a: Scope 문서 (this file)

✅ 완료

### Step 2b: LocalPTYAdapter core + mock tests

- `localpty_adapter.go` (신규 production file)
- `localpty_adapter_test.go` (신규 test file)
- `localptyExecRunner` — production PTY runner
- Mock runner 기반 contract test

### Step 2c: Create → discover → terminate E2E

- HTTP API 경계에서 create/delete 검증
- Session lifecycle: create → list → terminate → verify gone

### Step 2d: WebSocket live output/input E2E

- `/term/ws?session=localpty:<id>` → 실제 PTY 연결
- Shell output 수신
- WebSocket input → PTY stdin 도달

### Step 2e: Production registration + feature flag

- `app.go`에 conditional registration 추가
- Config flag `EnableLocalPTY`
