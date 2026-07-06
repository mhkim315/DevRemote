# Phase 1 최종 보완 실행 지시서

- 대상 기준: `3a08d38`
- 목표 커밋: `fix: complete safe Registry dependency injection`
- 작업 성격: 기존 Phase 1 결함 수정만 수행
- 다른 설계 선택: 금지

## 0. 실행 원칙

이 문서는 제안서가 아니라 실행 명세다. 아래 순서와 설계를 그대로 따른다.

작업 중 금지:

- `var R`, `Default`, `GlobalRegistry` 등 package singleton 재도입
- 인증, EventStore, TelemetryService 객체화
- 모바일 코드 변경
- REST/WS path 또는 JSON 변경
- polling 주기 변경
- generated `devremote_bin` 빌드·커밋
- 실패 테스트를 skip하거나 pre-existing으로 분류
- 여러 설계 중 새로운 대안 고안

한 단계가 compile/test에 실패하면 다음 단계로 넘어가지 않는다.

## 1. 먼저 현재 오류를 재현

```bash
cd companion-daemon
GOCACHE=/tmp/devremote-go-cache go test -race ./...
```

반드시 현재 실패를 확인한다.

```text
TestHandleWS_ClientDisconnectWhileProducingOutput
nil Registry panic
```

실패를 확인하지 못하면 작업을 중단하고 환경과 commit을 보고한다.

## 2. Registry API에 context 전달

파일:

- `companion-daemon/internal/mux/registry.go`

현재:

```go
func (r *Registry) FindSession(id string) (Session, error)
```

변경:

```go
func (r *Registry) FindSession(
    ctx context.Context,
    id string,
) (Session, error) {
    id = MigrateLegacyID(id)

    if s, err := r.FindSessionInCache(id); err == nil {
        return s, nil
    }

    parts := strings.SplitN(id, ":", 2)
    if len(parts) == 2 {
        _, _ = r.Refresh(ctx, parts[0], true)
    } else {
        _ = r.Sessions(ctx)
    }

    if s, err := r.FindSessionInCache(id); err == nil {
        return s, nil
    }
    return nil, fmt.Errorf("session %s not found in any adapter", id)
}
```

모든 call site를 compile error를 따라 수정한다.

context 선택:

- HTTP handler: `r.Context()`
- telemetry: daemon `ctx`
- cmux stream: stream `ctx`
- IPC: 현재는 `context.Background()` 허용
- startup link load: startup context 또는 `context.Background()` 허용
- test: test context 또는 `context.Background()`

이 단계 후:

```bash
gofmt -w internal/mux/registry.go
GOCACHE=/tmp/devremote-go-cache go test ./internal/mux/...
```

## 3. mutation invalidation은 Registry가 소유

adapter가 Registry cache invalidation을 담당하지 않게 한다.

파일:

- `companion-daemon/internal/mux/registry.go`
- `companion-daemon/internal/mux/tmux_adapter.go`
- `companion-daemon/internal/mux/cmux_adapter.go`
- `companion-daemon/internal/term/pty.go`

Registry에 다음 method를 추가한다.

```go
func (r *Registry) CreateSession(
    ctx context.Context,
    adapterName string,
    opts CreateOptions,
) (string, error) {
    adapter, ok := r.Adapter(adapterName)
    if !ok {
        return "", fmt.Errorf("adapter %s not found", adapterName)
    }
    creator, ok := adapter.(SessionCreator)
    if !ok {
        return "", fmt.Errorf("adapter %s does not support session creation", adapterName)
    }

    id, err := creator.CreateSession(ctx, opts)
    if err != nil {
        return "", err
    }
    r.Invalidate()
    return id, nil
}

func (r *Registry) TerminateSession(
    ctx context.Context,
    adapterName string,
    id string,
) error {
    adapter, ok := r.Adapter(adapterName)
    if !ok {
        return fmt.Errorf("adapter %s not found", adapterName)
    }
    terminator, ok := adapter.(SessionTerminator)
    if !ok {
        return fmt.Errorf("adapter %s does not support session termination", adapterName)
    }

    if err := terminator.TerminateSession(ctx, id); err != nil {
        return err
    }
    r.Invalidate()
    return nil
}
```

tmux/cmux adapter의 create/terminate에서 아래 코드를 모두 제거한다.

```go
a.health.Invalidate()
```

HTTP CRUD handler는 adapter capability를 직접 호출하지 않고 context의 Registry
method를 호출한다.

이 설계를 사용하면 tmux adapter는 health dependency가 필요 없다.

```go
func NewTmuxAdapter() Adapter {
    return &tmuxAdapter{}
}
```

`tmuxAdapter.health` field와 `NewTmuxAdapter(health)` 인자를 제거한다.

이 단계 후:

```bash
gofmt -w internal/mux/registry.go \
  internal/mux/tmux_adapter.go \
  internal/mux/cmux_adapter.go \
  internal/term/pty.go

GOCACHE=/tmp/devremote-go-cache go test ./internal/mux/...
```

## 4. cmux health는 stream failure에만 주입

`RegistryHealth`는 다음 기능만 가진다.

```go
type RegistryHealth interface {
    Refresh(
        context.Context,
        string,
        bool,
    ) (AdapterSnapshot, error)
}
```

`Invalidate()`는 제거한다. mutation invalidation은 앞 단계에서 Registry가
소유하기 때문이다.

cmux 생성자:

```go
func NewCmuxAdapter(health RegistryHealth) (Adapter, error) {
    if health == nil {
        return nil, fmt.Errorf("cmux adapter requires RegistryHealth")
    }
    return &cmuxAdapter{
        runner: &serialCommandRunner{
            delegate: newExecCommandRunner(),
        },
        health: health,
    }, nil
}
```

필수 전달 경로:

```text
cmuxAdapter.health
  → parseCmuxTree(out, runner, health)
  → CmuxSession.health
  → CmuxStream.pollScreen()
```

구조체:

```go
type CmuxSession struct {
    // 기존 필드
    health RegistryHealth
}
```

`GetSession()`이 생성하는 `CmuxSession`에도 health를 넣는다.

polling 3회 실패:

```go
_, refreshErr := s.session.health.Refresh(
    s.ctx,
    "cmux",
    true,
)
if refreshErr != nil {
    log.Printf("cmux health refresh failed: %v", refreshErr)
}
```

원래 stream error 종료는 그대로 수행한다.

main:

```go
reg := mux.NewRegistry()

cmuxAdapter, err := mux.NewCmuxAdapter(reg)
if err != nil {
    log.Fatalf("failed to create cmux adapter: %v", err)
}

reg.Register(cmuxAdapter)
reg.Register(mux.NewTmuxAdapter())
```

nil health를 사용하는 기존 test는 fake health를 전달하거나, cmux adapter 생성이
필요 없는 parser test라면 parser에 명시적인 fake를 전달한다.

이 단계 후:

```bash
gofmt -w internal/mux/adapter.go \
  internal/mux/cmux_adapter.go \
  cmd/devremote/main.go

GOCACHE=/tmp/devremote-go-cache go test ./internal/mux/...
```

## 5. HTTP Registry context를 안전하게 처리

파일:

- `companion-daemon/internal/term/runtime.go`

변경:

```go
var ErrRegistryMissing = errors.New("registry missing from request context")

func RegistryFromContext(
    ctx context.Context,
) (*mux.Registry, error) {
    reg, ok := ctx.Value(registryCtxKey{}).(*mux.Registry)
    if !ok || reg == nil {
        return nil, ErrRegistryMissing
    }
    return reg, nil
}
```

모든 handler:

```go
reg, err := RegistryFromContext(r.Context())
if err != nil {
    log.Printf("request registry unavailable: %v", err)
    http.Error(w, "server configuration error", http.StatusInternalServerError)
    return
}
```

handler에서 `RegistryFromContext(...).Method()` 형태의 연쇄 호출을 남기지 않는다.

`InjectRegistry`도 nil Registry를 받으면 생성 시 panic하지 말고 명확히 거부한다.
함수 signature를 error 반환으로 바꿔도 된다. 최소한 request 처리 중 nil method
panic은 없어야 한다.

이 단계 후:

```bash
gofmt -w internal/term/runtime.go \
  internal/term/pty.go \
  internal/term/telemetry.go \
  internal/term/linker.go

GOCACHE=/tmp/devremote-go-cache go test ./internal/term/...
```

## 6. background dependency와 context 정리

유지할 signature:

```go
StartTelemetryLoop(ctx, reg)
StartIPCServer(path, reg)
LoadLinks(reg)
LinkSession(link, reg)
GetLink(sessionID, reg)
```

Telemetry:

```go
sessions := reg.Sessions(ctx)
```

HTTP handler:

```go
sessions := reg.Sessions(r.Context())
session, err := reg.FindSession(r.Context(), id)
```

IPC는 Registry를 이미 인자로 받으므로 global helper를 만들지 않는다.

`main.go`의 IPC 오류 처리를 복구한다.

```go
if err := term.StartIPCServer(socketPath, reg); err != nil {
    log.Printf("Failed to start IPC server: %v", err)
}
```

## 7. 필수 테스트를 먼저 작성

다음 test 이름 또는 동등한 coverage가 반드시 존재해야 한다.

```text
TestRegistryCreateInvalidatesOwningRegistry
TestRegistryTerminateInvalidatesOwningRegistry
TestCmuxStreamFailureRefreshesOwningRegistry
TestRegistryFindSessionPropagatesCancellation
TestRegistryFromContextMissingReturnsError
TestWebSocketHandlersUseIndependentRegistries
TestNewCmuxAdapterRejectsNilHealth
TestNewCmuxAdapterStoresHealth
```

세부 요구:

- create/terminate는 fake adapter를 Registry에 등록해 실제 command 실행 없이 검증
- 두 Registry 중 소속 Registry snapshot만 만료되는지 확인
- cmux runner는 3회 실패하도록 fake 사용
- fake health는 refresh call count와 adapter name을 기록
- cancellation test는 adapter refresh가 `ctx.Done()`을 관찰하는지 확인
- WebSocket 독립성 test는 두 server를 동시에 실행
- context 없는 request는 panic 대신 HTTP 500

기존 실패 test 수정:

```go
r = r.WithContext(WithRegistry(r.Context(), reg))
```

두 번째 test에도 반드시 넣는다. 불필요한 blank line과 `testRegistry()` helper를
삭제한다.

이 단계 후:

```bash
GOCACHE=/tmp/devremote-go-cache go test ./...
GOCACHE=/tmp/devremote-go-cache go test -race ./...
```

하나라도 실패하면 commit하지 않는다.

## 8. generated binary와 whitespace 정리

`companion-daemon/devremote_bin`은 이번 source refactor 결과에 포함하면 안 된다.

기준 `b7ded3d`와 동일한 blob으로 복구하되 다른 source 파일은 되돌리지 않는다.

`tmux_adapter.go`와 `docs/PHASE1_RESPONSE.md`의 EOF blank line도 수정한다.

검사:

```bash
git diff --check b7ded3d..HEAD
git diff --name-only b7ded3d..HEAD |
  rg 'companion-daemon/devremote_bin'
```

두 번째 명령은 출력이 없어야 한다.

## 9. 최종 검증

아래 명령 결과를 그대로 작업 보고서에 붙인다.

```bash
rg -n 'var R |mux\.Default|DefaultRuntime|GlobalRegistry|GetRegistry' \
  companion-daemon --glob '*.go'

rg -n 'health refresh: adapter would' \
  companion-daemon/internal/mux

git diff --check b7ded3d..HEAD

git diff --name-only b7ded3d..HEAD |
  rg 'companion-daemon/devremote_bin'

cd companion-daemon
GOCACHE=/tmp/devremote-go-cache go vet ./...
GOCACHE=/tmp/devremote-go-cache go test ./...
GOCACHE=/tmp/devremote-go-cache go test -race ./...
```

필수 결과:

```text
mutable Registry/Runtime global search: 0
placeholder health comment search: 0
full diff check: PASS
generated binary diff: 0
vet: PASS
unit: PASS
race: PASS
```

## 10. 제출 형식

하나의 보완 commit:

```text
fix: complete safe Registry dependency injection
```

작업 보고:

```text
Commit:
Files changed:
Global search:
Context propagation tests:
Mutation invalidation tests:
Cmux health test:
WebSocket isolation test:
Full diff check:
Generated binary diff:
Go vet:
Go test:
Go race:
Known failures: none
```

`Known failures`가 하나라도 있으면 완료라고 보고하지 않는다.
