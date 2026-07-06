# Phase 2 `146eeb3e8` 검증 결과와 수정 가이드

## 1. 판정

- 검증 대상: `146eeb3e8779968e8e5ba78a3bab12a696fb0dba`
- 기준 커밋: `ffe44e8`
- 판정: **REJECT — 구조는 진전됐지만 기존 기능과 lifecycle 보장이 훼손됨**
- 원격 상태: 검증 시점에 `146eeb3e8`은 실행 에이전트의 별도 checkout에만 있고
  `origin/feature/phase10-multi-adapter`에는 push되지 않았다.

자동 검증 결과:

```text
git diff --check ffe44e8..146eeb3e8  PASS
go vet ./...                         PASS
go test ./...                        PASS
go test -race ./...                  PASS
```

테스트 통과만으로 Phase 2를 승인할 수는 없다. 현재 테스트가 삭제된 watcher/tunnel
동작과 실제 IPC listener 종료를 관찰하지 않기 때문이다.

## 2. 유지할 구현

다음 변경은 Phase 2 목표에 부합하므로 되돌리지 않는다.

- `term.Handlers{Registry: reg}`를 통한 handler dependency 명시
- `InjectRegistry`, `requireRegistry` 및 context 기반 registry 전달 제거
- daemon 전용 `http.NewServeMux()` 사용
- 명시적인 `http.Server`
- `signal.NotifyContext` 기반 종료 신호 전달
- `main`을 flag parsing과 실행 진입점 중심으로 축소
- HTTP handler를 `Handlers` receiver method로 연결
- REST/WebSocket endpoint 및 response contract 유지

수정은 이 구조를 기반으로 하되 아래 회귀만 제거해야 한다. Phase 1의
`Registry` ownership을 다시 singleton이나 package global로 되돌리면 안 된다.

## 3. 차단 결함

### 3.1 watcher 기능 삭제

`146eeb3e8`은 기존 `startWatcher() *watcher.Tailer` 구현을 다음 no-op으로 바꿨다.

```go
func startWatcher() {
    // no-op
}
```

기존 구현은 `~/.claude` JSONL을 감시하고 agent event를 `models.EmitEvent`로
전달했다. 이것은 향후 Phase 4 작업이 아니라 이미 제공되던 runtime 기능이다.
Phase 2는 composition 변경 단계이므로 이 동작을 삭제할 권한이 없다.

### 3.2 tunnel 기능 삭제

기존 cloudflared named tunnel 실행도 no-op으로 교체됐다. 계획의
“cloudflared lifecycle 재설계는 비목표”는 tunnel을 없애라는 의미가 아니다.
기존 실행 방식과 외부 동작을 그대로 보존해야 한다.

`--insecure-local-only` 테스트에서는 tunnel 경로가 실행되지 않으므로 현재
테스트 성공이 이 회귀를 탐지하지 못한다.

### 3.3 IPC가 실제로 종료되지 않음

현재 `term.StartIPCServer`는 listener를 내부 goroutine에 숨기고 `error`만
반환한다. 따라서 `App`은 listener를 닫을 수 없다.

`App.Shutdown`의 다음 코드는 IPC 종료가 아니다.

```go
os.Remove(a.ipcPath)
```

Unix socket pathname을 삭제해도 이미 열린 listener와 accept goroutine은
종료되지 않는다. 또한 현재 accept loop는 `Accept()` 오류 시 무조건
`continue`하므로 listener가 닫혀도 오류를 반복할 수 있다.

### 3.4 telemetry 종료 완료를 기다리지 않음

`cancelTelemetry()`는 종료 요청일 뿐 종료 완료 보장이 아니다. Phase 2가
HTTP → telemetry → IPC 순서를 주장하려면 telemetry goroutine이 종료됐음을
확인한 뒤 IPC를 닫아야 한다. 이 대기는 반드시 shutdown context의 deadline을
준수해야 한다.

### 3.5 lifecycle 회귀 테스트 부재

현재 추가된 테스트는 handler injection 변경에 집중돼 있다. 다음을 검증하는
테스트가 없어 위 결함들이 모두 통과했다.

- 서로 다른 App/Server의 registry 및 route 격리
- HTTP graceful shutdown
- telemetry goroutine 종료
- IPC listener 종료와 같은 경로로 재-bind 가능 여부
- watcher 시작 및 shutdown 시 close
- 기존 tunnel 시작 조건 보존

## 4. 수정 설계 계약

### 4.1 App이 runtime resource를 소유한다

권장 구조는 다음과 같다. 정확한 타입 이름은 달라도 ownership과 종료 가능성은
동일해야 한다.

```go
type App struct {
    config    Config
    registry  *mux.Registry
    server    *http.Server
    telemetry *TelemetryService
    ipc       *term.IPCServer
    watcher   *watcher.Tailer
}
```

테스트 가능성을 위해 OS/network 의존 생성 함수를 작은 dependency struct나
interface로 주입할 수 있다. 단, 범용 DI framework를 도입하지 않는다.

```go
type Dependencies struct {
    StartWatcher func() (*watcher.Tailer, error)
    StartTunnel  func(context.Context) error
    ListenIPC    func(string, *mux.Registry) (*term.IPCServer, error)
}
```

production default는 기존 동작을 사용하고 테스트에서만 fake를 주입한다.
함수 주입을 사용하더라도 registry handler wiring을 다시 closure/context
injection 방식으로 되돌리지 않는다.

### 4.2 watcher 동작을 보존한다

기존 `startWatcher`의 경로, callback, event payload 및 logging을 복원한다.
반환된 `*watcher.Tailer`를 `App`이 보관하고 shutdown에서 `Close()`한다.

- watcher 초기화 실패 정책은 기존과 동일하게 유지한다.
- `~/.claude` 감시와 `models.EmitEvent` 호출을 유지한다.
- Phase 2에서 watcher/store protocol을 재설계하지 않는다.
- `Close()` 오류는 수집해 shutdown 결과에 반영하거나 명확히 log한다.

### 4.3 tunnel 외부 동작을 보존한다

기존 `startTunnel` 구현을 복원한다.

- `InsecureLocalOnly == true`이면 시작하지 않는다.
- production mode에서는 기존 cloudflared arguments와 named tunnel 동작을
  유지한다.
- Phase 2에서는 tunnel supervisor, restart policy, 새로운 process manager를
  만들지 않는다.
- App 구조상 context 전달이 쉽게 가능하면 적용할 수 있지만 기존 remote access를
  깨뜨리는 대규모 재설계는 다음 단계로 미룬다.

### 4.4 종료 가능한 IPCServer를 만든다

`StartIPCServer`가 listener를 숨기지 않도록 resource type을 도입한다.

```go
type IPCServer struct {
    listener net.Listener
    done     chan struct{}
    closeOnce sync.Once
}

func StartIPCServer(path string, reg *mux.Registry) (*IPCServer, error)
func (s *IPCServer) Close() error
func (s *IPCServer) Wait(ctx context.Context) error
```

필수 동작:

1. listen 성공 후에만 `*IPCServer`를 반환한다.
2. `Close()`는 idempotent해야 한다.
3. listener close로 발생한 `net.ErrClosed`는 정상 종료로 처리한다.
4. accept loop는 close 이후 `continue`하지 않고 반환한다.
5. goroutine 종료 시 `done`을 닫는다.
6. socket pathname 정리는 listener 종료와 goroutine 종료 뒤에 수행한다.
7. shutdown 뒤 동일 socket path로 새 server를 시작할 수 있어야 한다.
8. protocol framing과 `handleIPCConnection` 동작은 변경하지 않는다.

동시 connection까지 강제로 끊을지는 이번 단계에서 최소 범위로 결정할 수 있다.
다만 `Wait(ctx)`가 무한 대기해서는 안 되고 shutdown deadline은 반드시 지켜야 한다.

### 4.5 telemetry에 완료 신호를 제공한다

다음 중 한 방식으로 종료 완료를 관찰할 수 있게 한다.

```go
func StartTelemetryLoop(ctx context.Context, reg *mux.Registry) <-chan struct{}
```

또는:

```go
type TelemetryService struct {
    cancel context.CancelFunc
    done   <-chan struct{}
}

func (s *TelemetryService) Stop(ctx context.Context) error
```

루프의 기존 sampling, adapter 호출 및 interval은 변경하지 않는다. 종료 API만
추가한다. cancel 후 `done`을 기다리되 deadline 초과 시 적절한 오류를 반환한다.

### 4.6 Shutdown의 단일 소유권과 순서를 보장한다

`Shutdown` 호출자가 내부 cancel function을 인자로 전달하면 안 된다. telemetry
cancel과 resource handle은 `App` 필드가 소유해야 한다.

```go
func (a *App) Shutdown(ctx context.Context) error
```

최소 종료 순서:

1. HTTP server shutdown
2. telemetry cancel 및 종료 대기
3. watcher close
4. IPC listener close 및 accept loop 종료 대기
5. IPC socket pathname 정리

각 오류를 log만 하고 버리지 않는다. `errors.Join` 등을 이용해 가능한 resource를
끝까지 정리한 뒤 오류를 반환한다. 같은 App에 대한 중복 shutdown 가능성이 있으면
`sync.Once` 또는 각 resource의 idempotent close로 안전하게 처리한다.

`Run`에서 HTTP server가 `http.ErrServerClosed`로 종료되는 것은 정상 종료다.
HTTP가 예상치 못한 오류로 먼저 끝난 경우에도 background resource 정리를 수행하고
원래 오류를 보존한다.

## 5. 테스트 요구사항

실행 에이전트는 production 코드 수정과 함께 아래 테스트를 추가해야 한다.

### 5.1 handler와 mux 격리

- 서로 다른 registry를 가진 두 handler/App을 생성한다.
- 한쪽 registry의 session이 다른 쪽 API 결과에 노출되지 않음을 검증한다.
- 두 App을 같은 process에서 생성해도 route duplicate panic이 없음을 검증한다.
- 존재하지 않는 session의 기존 404 contract를 유지한다.

### 5.2 IPC lifecycle

- 임시 디렉터리의 socket path로 IPC server를 시작한다.
- 실제로 연결 가능함을 확인한다.
- `Close`와 `Wait`가 deadline 내 성공하는지 확인한다.
- 종료 후 동일 path로 다시 listen할 수 있음을 확인한다.
- listener close가 accept error log storm이나 goroutine leak을 만들지 않음을
  검증 가능한 수준으로 확인한다.
- `Close()`를 두 번 호출해도 panic하거나 hang하지 않는지 확인한다.

### 5.3 telemetry lifecycle

- 짧은 test context로 loop를 시작한다.
- cancel 후 done signal이 deadline 내 도착하는지 확인한다.
- race test에서 goroutine이 공유 상태를 잘못 접근하지 않는지 확인한다.

### 5.4 App shutdown

fake resource 또는 test listener를 사용해 종료 호출 순서를 기록한다.

```text
HTTP -> telemetry -> watcher -> IPC
```

- shutdown deadline을 준수한다.
- 한 resource가 오류를 반환해도 나머지 resource를 정리한다.
- 반환 오류에 실패 원인이 보존된다.
- `Run`의 context cancel 경로와 HTTP unexpected-error 경로를 모두 테스트한다.

### 5.5 기능 보존

- watcher factory가 production Run에서 호출되고 반환 resource가 닫히는지 확인한다.
- insecure local mode에서는 tunnel을 시작하지 않는다.
- production mode에서는 tunnel start 경로가 유지되는지 fake로 확인한다.
- endpoint, JSON schema, WebSocket 경로를 변경하지 않는다.

## 6. 작업 순서

아래 순서로 진행하면 회귀 원인을 분리하기 쉽다.

1. `146eeb3e8`의 App/Handlers/private mux 구조는 유지한다.
2. `IPCServer` resource type과 lifecycle test를 먼저 구현한다.
3. telemetry done/stop contract와 test를 구현한다.
4. 기존 watcher 구현을 복원하고 App ownership에 연결한다.
5. 기존 tunnel 실행을 복원하고 mode별 start test를 추가한다.
6. `App.Shutdown(ctx)`를 인자 없는 내부 ownership 구조로 정리한다.
7. App/mux 격리와 전체 shutdown test를 추가한다.
8. 전체 정적 검사와 race test를 실행한다.
9. 실행 에이전트 checkout에서 commit한 뒤 원격 공유 전에 검증을 요청한다.

한 번에 auth, store, cloudflared supervisor 또는 모바일 코드를 함께 수정하지 않는다.

## 7. 검증 명령

`companion-daemon` 디렉터리에서 실행한다.

```bash
gofmt -w <변경한 go 파일>
git diff --check ffe44e8
go vet ./...
go test ./...
go test -race ./...
```

추가 확인:

```bash
rg -n 'http\\.DefaultServeMux|http\\.HandleFunc\\(' cmd/devremote internal
rg -n 'func startWatcher|func startTunnel|no-op|Implementation deferred' cmd/devremote
rg -n 'os\\.Remove\\(.*pokit\\.sock|StartIPCServer|IPCServer' cmd/devremote internal
```

첫 번째 검색에서 daemon route가 default mux에 등록되면 실패다. 두 번째 검색에서
watcher/tunnel이 no-op이면 실패다. 세 번째는 pathname 삭제만으로 IPC 종료를
대체하지 않았는지 사람이 확인해야 한다.

가능하면 실제 smoke test도 수행한다.

- insecure local daemon 시작 및 `/api/sessions` 확인
- tmux와 cmux session discovery 유지
- 휴대폰 WebSocket 입력과 carriage return 동작 유지
- daemon 종료 후 IPC socket 재-bind 확인
- production 설정에서 기존 tunnel start command가 유지되는지 확인

## 8. 재검증 제출물

실행 에이전트는 다음을 함께 전달한다.

- 수정 commit hash
- `git diff --stat ffe44e8..<commit>`
- 새 lifecycle test 목록
- `go vet`, `go test`, `go test -race` 결과
- watcher와 tunnel 동작을 보존한 코드 위치
- IPC listener가 실제 종료된다는 테스트 근거
- shutdown 순서와 deadline 처리 설명
- 의도적으로 다음 Phase로 미룬 항목 목록

“빌드와 기존 테스트 통과”만으로는 재검증 요청을 완료한 것으로 보지 않는다.

## 9. 최종 ACCEPT 기준

다음 조건이 모두 충족되어야 Phase 2를 승인한다.

- App composition root와 private mux 구조 유지
- handler dependency가 field/constructor로 명시됨
- watcher와 tunnel 기존 동작 보존
- App이 IPC listener를 실제로 닫고 goroutine 종료를 기다림
- telemetry 종료 완료를 deadline 내 관찰함
- shutdown 오류를 버리지 않고 resource 정리를 끝까지 수행함
- App/mux/IPC/telemetry lifecycle 테스트 추가
- endpoint와 external protocol 회귀 없음
- `go vet`, 전체 test, race test 통과
- 실제 tmux/cmux smoke test 통과

그 전에는 Phase 3을 시작하지 않는다.
