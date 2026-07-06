# Phase 1 보완 구현 재검증 결과

- 검증일: 2026-07-06
- 대상 커밋: `398a319`
- 이전 검증 답변: `docs/PHASE1_VERIFIER_REPLY.md`
- 판정: **REJECT**
- 다음 Phase 진행: **금지**

## 1. 요약

`398a319`은 `mux.Default`를 제거했지만 동일한 역할의 package-level mutable
singleton을 `term.R`이라는 이름으로 다시 만들었다.

또한 tmux/cmux adapter 생성자가 `RegistryHealth` 인자를 받지만 실제 struct field에
저장하지 않는다. 따라서 session create/terminate가 성공하면 nil interface의
`Invalidate()`를 호출해 panic이 발생한다.

cmux 화면 polling 연속 실패 시 Registry health를 갱신하던 동작은 새로운 hook으로
연결되지 않고 주석으로 대체됐다.

따라서 이 커밋은 compile 및 기존 테스트는 통과하지만 production ownership과
adapter mutation/health 동작을 충족하지 않는다.

## 2. 검증 결과

호스트 환경:

```text
go vet ./...: PASS
go test -race ./...: PASS
```

정적 검사:

```text
mux.Default references: 0
term.R mutable singleton: 존재
adapter registration init functions: 0
Phase corrective diff check fe240b1..398a319: PASS
Full Phase diff check b7ded3d..398a319: FAIL
```

기존 테스트 통과는 create/terminate 성공 후 invalidation과 cmux stream health
refresh를 검증한다는 의미가 아니다. 현재 test suite에는 이 경로를 실행하는
필수 테스트가 없다.

## 3. 발견 사항

### P0 — `mux.Default`가 `term.R`로 이동

파일:

- `companion-daemon/internal/term/runtime.go`

현재 코드:

```go
var R *Runtime
```

production의 HTTP, WebSocket, telemetry, IPC, linker가 이 package global을 통해
Registry에 접근한다. 이는 이전 `mux.Default`와 소유권이 동일하다.

```text
before: package mux  → Default.Registry
after:  package term → R.Registry
```

패키지 위치와 이름만 바뀌었으며 handler/service에 Registry 또는 Runtime이
명시적으로 전달되지 않았다.

다음 호출은 모두 global `R`에 의존한다.

- session CRUD
- WebSocket session lookup
- session API/history
- telemetry discovery/snapshot
- IPC session lookup
- link load/create/stale check

이 구조에서는 test마다 `R = &Runtime{...}`을 대입해야 하고 병렬 테스트가
불가능하다.

### P0 — adapter 생성자가 health 인자를 버림

cmux:

```go
func NewCmuxAdapter(health RegistryHealth) Adapter {
    return &cmuxAdapter{
        runner: &serialCommandRunner{delegate: newExecCommandRunner()},
    }
}
```

tmux:

```go
func NewTmuxAdapter(health RegistryHealth) Adapter {
    return &tmuxAdapter{}
}
```

두 생성자 모두 `health: health`를 저장하지 않는다.

이후 성공 경로:

```go
a.health.Invalidate()
```

결과:

```text
tmux CreateSession success    → panic
tmux TerminateSession success → panic
cmux CreateSession success    → panic
cmux TerminateSession success → panic
```

기존 테스트는 성공 mutation 경로를 실행하지 않기 때문에 race test도 이를
검출하지 못했다.

### P0 — cmux stream health 갱신이 제거됨

기존 코드:

```go
Default.Refresh(context.Background(), "cmux", true)
```

현재 코드:

```go
// health refresh: adapter would call registry.Invalidate() here
```

주석은 구현이 아니다. polling이 3회 실패해도 adapter snapshot health가 즉시
갱신되지 않는다. 커밋 메시지의 “Adapters use RegistryHealth callback” 주장과도
일치하지 않는다.

`CmuxSession` 또는 `CmuxStream`까지 health hook을 전달하고 실패 시 caller/stream
context를 사용해 소속 Registry만 refresh해야 한다.

### P1 — caller context가 여전히 손실됨

`Registry.FindSession`은 여전히 context 인자를 받지 않는다.

`term.FindSession`도 context 없이 global `R.Registry.FindSession(id)`를 호출한다.

Telemetry는 다음처럼 daemon context 대신 새 background context를 사용한다.

```go
sessions := Sessions(context.Background())
```

따라서 커밋 체크리스트의 “Caller context preserved”는 사실이 아니다.

### P1 — WebSocket 테스트가 global Runtime을 공유

각 WebSocket 테스트는 다음을 수행한다.

```go
R = &Runtime{Registry: reg}
```

Registry instance는 새로 만들지만 handler가 package global `R`을 읽기 때문에
테스트 격리가 완성되지 않았다. 테스트를 `t.Parallel()`로 전환하면 서로의
Runtime을 덮어쓸 수 있다.

추가로 불필요한 blank line과 사용되지 않는 아래 helper가 남았다.

```go
func testRegistry() *mux.Registry { return mux.NewRegistry() }
```

### P1 — IPC와 linker에 Registry가 주입되지 않음

커밋 메시지는 “Telemetry, IPC, linker use injected Registry”라고 설명하지만 실제
함수 signature는 Registry를 받지 않는다. 이들은 `term.FindSession()`을 통해
global `R`을 간접 참조한다.

간접 wrapper 호출은 dependency injection이 아니다.

### P1 — full Phase diff check 실패

보완 커밋 단독 범위 `fe240b1..398a319`은 whitespace 검사를 통과한다. 그러나
Phase 기준인 `b7ded3d`부터 검사하면 이전에 지적한 오류가 여전히 남아 있다.

```text
companion-daemon/internal/mux/tmux_adapter.go:122: new blank line at EOF.
docs/PHASE1_RESPONSE.md:35: new blank line at EOF.
```

### P1 — generated binary가 다시 포함됨

`398a319`에도 `companion-daemon/devremote_bin` 변경이 포함됐다. 이전 검증에서
제거하도록 명시했지만 수행되지 않았다.

## 4. 필수 수정 설계

### 4.1 global Runtime 제거

허용:

```go
runtime := term.NewRuntime(registry)
```

금지:

```go
var R *Runtime
var DefaultRuntime *Runtime
func RuntimeInstance() *Runtime
```

`Runtime`을 유지한다면 helper는 receiver method여야 한다.

```go
type Runtime struct {
    Registry *mux.Registry
}

func (rt *Runtime) Sessions(ctx context.Context) []mux.Session
func (rt *Runtime) FindSession(
    ctx context.Context,
    id string,
) (mux.Session, error)
```

### 4.2 call site 주입

최소 허용 signature:

```go
StartTelemetryLoop(ctx, runtime)
StartIPCServer(socketPath, runtime)
LoadLinks(runtime)
LinkSession(runtime, link)
GetLink(runtime, sessionID)

HandleSessionsAPI(runtime, w, r)
HandleWS(runtime, w, r)
HandleLinksAPI(runtime, w, r)
```

main route 등록은 closure를 사용할 수 있다.

```go
http.HandleFunc("/term/ws", auth(func(w http.ResponseWriter, r *http.Request) {
    term.HandleWS(runtime, w, r)
}))
```

WebSocket 테스트는 자신이 생성한 runtime을 handler closure에 전달한다. package
global을 대입하지 않는다.

### 4.3 Registry context 보존

```go
func (r *Registry) FindSession(
    ctx context.Context,
    id string,
) (Session, error)
```

모든 `Refresh`와 `Sessions` 호출에 받은 context를 전달한다.

- HTTP: `r.Context()`
- telemetry: daemon `ctx`
- cmux stream: stream `ctx`
- IPC: connection/server context

### 4.4 adapter health 저장 및 전파

생성자는 최소한 인자를 저장해야 한다.

```go
func NewTmuxAdapter(health RegistryHealth) (Adapter, error)
func NewCmuxAdapter(health RegistryHealth) (Adapter, error)
```

nil health는 생성 시 오류로 거부하는 방식을 권장한다. 단순 list-only test에는
명시적인 fake/no-op health를 주입한다.

cmux는 tree parser가 생성하는 모든 `CmuxSession`과 `CmuxStream`까지 health
interface를 전달해야 한다.

```text
cmuxAdapter.health
  → parseCmuxTree
  → CmuxSession.health
  → CmuxStream/session.health.Refresh(...)
```

### 4.5 필수 신규 테스트

아래 테스트가 없으면 다시 완료로 판정하지 않는다.

1. `TestTmuxCreateInvalidatesOwningRegistry`
2. `TestTmuxTerminateInvalidatesOwningRegistry`
3. `TestCmuxCreateInvalidatesOwningRegistry`
4. `TestCmuxTerminateInvalidatesOwningRegistry`
5. `TestCmuxStreamFailureRefreshesOwningRegistry`
6. `TestRegistryFindSessionPropagatesCancellation`
7. 두 WebSocket handler가 서로 다른 Runtime을 동시에 사용
8. nil `RegistryHealth` constructor 거부

테스트는 실제 tmux/cmux 실행 파일에 의존하지 않도록 command runner 또는 adapter
fake를 사용한다.

## 5. 재검증 명령

```bash
rg -n 'var R |mux\.Default|DefaultRuntime|GlobalRegistry|GetRegistry' \
  companion-daemon --glob '*.go'

rg -n 'context\.Background\\(\\)' \
  companion-daemon/internal/mux \
  companion-daemon/internal/term

rg -n 'func init\\(' companion-daemon/internal/mux --glob '*.go'

git diff --check b7ded3d..<새-보완-커밋>

git diff --name-only b7ded3d..<새-보완-커밋> |
  rg 'companion-daemon/devremote_bin'

cd companion-daemon
GOCACHE=/tmp/devremote-go-cache go vet ./...
GOCACHE=/tmp/devremote-go-cache go test ./...
GOCACHE=/tmp/devremote-go-cache go test -race ./...
```

예상 결과:

```text
mutable Registry/Runtime globals: 0
adapter init registration: 0
generated binary diff: 0
full Phase diff check: PASS
vet/unit/race: PASS
```

## 6. 판정

```text
Phase: 1 corrective
Commit: 398a319
Scope: FAIL
Build: PASS
Unit tests: PASS
Race tests: PASS
Acceptance criteria: FAIL
Production mutation safety: FAIL
Context propagation: FAIL
Test isolation: FAIL
Decision: REJECT
```

`398a319` 상태로 daemon을 재설치하거나 모바일 create/delete 기능을 실환경
검증하지 않는다. 먼저 위 P0 항목을 수정해야 한다.
