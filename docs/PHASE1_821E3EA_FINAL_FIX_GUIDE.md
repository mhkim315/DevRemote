# Phase 1 `821e3ea` 최종 수정 가이드

- 대상 커밋: `821e3ea`
- 목표: Phase 1 acceptance criteria 최종 충족
- 허용 범위: 안전한 Registry 추출, CRUD 연결, context, 테스트, formatting 정리
- 목표 커밋 메시지: `fix: finalize Registry safety and acceptance tests`

## 1. 현재 상태

완료:

- Registry package singleton 제거
- `Registry.FindSession(ctx, id)`
- `Registry.CreateSession/TerminateSession`
- cmux health dependency 저장 및 stream 전달
- cmux polling failure refresh
- tmux adapter의 Registry 결합 제거
- vet/unit/race 통과

미완료:

- Registry context error를 여러 caller가 무시
- CRUD가 안전 검사 전에 Registry를 역참조
- IPC 시작 오류 무시
- acceptance test 누락
- formatting/trailing whitespace
- tracked `devremote_bin` diff

새 architecture를 만들지 않는다. 아래 작업만 순서대로 수행한다.

## 2. 공통 Registry 추출 helper 추가

파일:

- `companion-daemon/internal/term/runtime.go`

현재 `RegistryFromContext(ctx) (*mux.Registry, error)`는 유지한다.

HTTP handler에서 중복되는 오류 처리를 줄이기 위해 다음 helper를 추가해도 된다.

```go
func requireRegistry(
    w http.ResponseWriter,
    r *http.Request,
) (*mux.Registry, bool) {
    reg, err := RegistryFromContext(r.Context())
    if err != nil {
        log.Printf("request registry unavailable: %v", err)
        http.Error(
            w,
            "server configuration error",
            http.StatusInternalServerError,
        )
        return nil, false
    }
    return reg, true
}
```

`requireRegistry`를 어느 파일에 둘지는 자유지만 `term` package 내부의 unexported
함수여야 한다.

금지:

```go
reg, _ := RegistryFromContext(...)
```

최종 검색 결과가 0건이어야 한다.

```bash
rg -n 'RegistryFromContext\(.*\).*_' companion-daemon/internal/term
```

## 3. `HandleSessionCRUD`를 단순화

파일:

- `companion-daemon/internal/term/pty.go`

handler 진입 후 한 번만 Registry를 추출한다.

```go
func HandleSessionCRUD(w http.ResponseWriter, r *http.Request) {
    reg, ok := requireRegistry(w, r)
    if !ok {
        return
    }

    // 기존 method 분기
}
```

다음 임시 코드를 전부 삭제한다.

```go
reg, _ := RegistryFromContext(r.Context())
_, _ = reg.Adapter(adapterName)
```

POST:

```go
opts := mux.CreateOptions{
    Name:        ref.RawID,
    WorkspaceID: req.WorkspaceID,
}

createdID, err := reg.CreateSession(
    r.Context(),
    adapterName,
    opts,
)
if err != nil {
    http.Error(
        w,
        fmt.Sprintf("failed to create session: %v", err),
        http.StatusInternalServerError,
    )
    return
}
```

DELETE:

```go
if err := reg.TerminateSession(
    r.Context(),
    adapterName,
    ref.RawID,
); err != nil {
    http.Error(
        w,
        fmt.Sprintf("failed to terminate session: %v", err),
        http.StatusInternalServerError,
    )
    return
}
```

다음 direct capability 호출은 `pty.go`에서 0건이어야 한다.

```bash
rg -n 'creator\.CreateSession|terminator\.TerminateSession' \
  companion-daemon/internal/term/pty.go
```

## 4. 나머지 HTTP handler 안전 처리

다음 함수는 시작 부분에서 `requireRegistry`를 한 번 호출한다.

- `HandleWS`
- `HandleSessionsV2`
- `HandleLinksAPI`

### HandleWS

```go
reg, ok := requireRegistry(w, r)
if !ok {
    return
}

s, err := reg.FindSession(r.Context(), session)
```

### HandleSessionsV2

```go
reg, ok := requireRegistry(w, r)
if !ok {
    return
}
```

이후 동일한 `reg`를 history와 session 목록 전체에서 재사용한다.

```go
sess, err := reg.FindSession(
    r.Context(),
    mux.MigrateLegacyID(historyID),
)

sessions := reg.Sessions(r.Context())
snap, _ := reg.Snapshot(s.AdapterName())
```

history/screen:

```go
out, err = hr.ReadHistory(r.Context(), 10000)
out, err = sr.ReadScreen(r.Context())
```

### HandleLinksAPI

GET은 Registry가 필요하지 않더라도 route wiring 일관성을 위해 handler 시작 시
검증해도 된다. 최소한 POST/DELETE에서 error를 무시하면 안 된다.

```go
reg, ok := requireRegistry(w, r)
if !ok {
    return
}
```

## 5. background context 수정

파일:

- `companion-daemon/internal/term/telemetry.go`

현재:

```go
sessions := reg.Sessions(context.Background())
```

변경:

```go
sessions := reg.Sessions(ctx)
```

HTTP handler의 `Sessions`, `FindSession`, `ReadHistory`, `ReadScreen`은
`r.Context()`를 사용한다.

허용되는 `context.Background()`:

- startup link load
- 현재 IPC connection
- test

그 외 production HTTP/telemetry 경로의 background context는 제거한다.

## 6. IPC 오류 처리 복구

파일:

- `companion-daemon/cmd/devremote/main.go`

현재:

```go
if err := term.StartIPCServer(socketPath, reg); err != nil {
}
```

변경:

```go
if err := term.StartIPCServer(socketPath, reg); err != nil {
    log.Printf("Failed to start IPC server: %v", err)
}
```

빈 error block은 허용하지 않는다.

## 7. Registry mutation 테스트

파일:

- `companion-daemon/internal/mux/registry_test.go`

fake adapter에 create/terminate call count와 반환 오류를 기록한다.

필수 테스트:

```go
func TestRegistryCreateSessionInvalidatesOnSuccess(t *testing.T)
func TestRegistryCreateSessionPreservesCacheOnFailure(t *testing.T)
func TestRegistryTerminateSessionInvalidatesOnSuccess(t *testing.T)
func TestRegistryTerminateSessionPreservesCacheOnFailure(t *testing.T)
```

검증 방법:

1. Registry session snapshot을 먼저 채운다.
2. 성공 mutation을 호출한다.
3. snapshot의 `LastAttemptAt`이 zero인지 확인한다.
4. 실패 mutation 후 기존 `LastAttemptAt`이 유지되는지 확인한다.
5. 올바른 adapter와 인자가 호출됐는지 확인한다.

adapter 없음과 capability 없음도 table test로 검증한다.

## 8. context cancellation 테스트

파일:

- `companion-daemon/internal/mux/registry_test.go`

cancel을 기다리는 fake adapter를 사용한다.

```go
func TestRegistryFindSessionPropagatesCancellation(t *testing.T)
```

요구:

- `FindSession(ctx, missingID)`가 refresh를 실행
- fake adapter가 전달받은 context의 cancel을 관찰
- timeout 내 종료
- 내부 `context.Background()`을 사용하면 test 실패

`Adapter.ListSessions()`는 현재 context를 받지 않으므로 이 테스트가 구조적으로
불가능하다면 그 사실을 숨기지 않는다. 이 경우 Phase 1에서는 `Refresh` context가
singleflight 결과 대기와 이후 작업에 반영되는 범위를 테스트하고, Adapter
interface의 context 지원은 별도 명시적 기술 부채로 기록한다.

테스트를 억지로 통과시키기 위해 sleep을 사용하지 않는다.

## 9. cmux health 테스트 강화

파일:

- `companion-daemon/internal/mux/cmux_adapter_mock_test.go`

현재 `mockRegistryHealth`는 호출을 기록하지 않는다. 다음 필드를 추가한다.

```go
type mockRegistryHealth struct {
    mu       sync.Mutex
    calls    int
    name     string
    force    bool
    refreshErr error
}
```

`Refresh`에서 기록하고 `refreshErr`를 반환한다.

필수 테스트:

```go
func TestCmuxStreamFailureRefreshesOwningRegistry(t *testing.T)
```

검증:

- `read-screen`이 연속 3회 실패
- health refresh 정확히 1회
- name은 `cmux`
- force는 `true`
- refresh error가 있어도 stream은 정해진 오류로 종료

nil health constructor:

```go
func TestNewCmuxAdapterRejectsNilHealth(t *testing.T)
```

생성된 adapter가 전달된 health를 실제 session에 연결하는 테스트도 추가한다.

## 10. HTTP context 및 WebSocket 격리 테스트

파일:

- `companion-daemon/internal/term/pty_ws_test.go`
- 필요하면 `runtime_test.go`

필수 테스트:

```go
func TestRegistryFromContextMissingReturnsError(t *testing.T)
func TestHandleWSMissingRegistryReturns500(t *testing.T)
func TestWebSocketHandlersUseIndependentRegistries(t *testing.T)
```

독립성 테스트:

- Registry A에는 session A만 등록
- Registry B에는 session B만 등록
- 서로 다른 두 test server를 동시에 실행
- A server에서 B session을 찾지 못함
- B server에서 A session을 찾지 못함
- package global 대입 없음

기존 두 WebSocket 테스트의 불필요한 blank line과 한 줄 두 statement를 제거한다.

현재:

```go
r = r.WithContext(...); r.URL.RawQuery = ...
```

변경:

```go
r = r.WithContext(WithRegistry(r.Context(), reg))
r.URL.RawQuery = "session=mock:test"
```

## 11. formatting 정리

다음 명령을 실행한다.

```bash
cd companion-daemon
gofmt -w \
  cmd/devremote/main.go \
  internal/mux/*.go \
  internal/term/*.go
```

직접 공백만 수정하지 않는다. gofmt 결과를 사용한다.

cmux terminate의 빈 block을 제거한다.

현재:

```go
_, err := a.runner.Run(...)
if err == nil {
}
return err
```

변경:

```go
_, err := a.runner.Run(...)
return err
```

`linker.go` import는 standard library와 internal package를 분리한다.

```go
import (
    "context"
    "encoding/json"
    "net/http"
    // ...

    "devremote/companion-daemon/internal/mux"
)
```

## 12. `.gitignore`와 binary 정리

유지:

```text
companion-daemon/.gitignore
  devremote_bin
```

삭제:

- root `.gitignore`의 중복 `companion-daemon/devremote_bin`
- `companion-daemon/internal/.gitignore`

이미 tracked된 binary는 ignore만으로 복구되지 않는다. source refactor 기준
`b7ded3d`의 blob과 동일하게 되돌린다.

주의:

- 다른 source 파일을 revert하지 않는다.
- binary를 새로 build해 다시 stage하지 않는다.

검증:

```bash
git diff --name-only b7ded3d..HEAD |
  rg 'companion-daemon/devremote_bin'
```

출력 0건이 목표다.

## 13. 단계별 실행 순서

아래 순서를 변경하지 않는다.

1. handler Registry error 처리
2. CRUD Registry mutation API 연결 정리
3. telemetry/HTTP context 수정
4. IPC log 복구
5. Registry mutation tests
6. cmux health tests
7. HTTP context/WebSocket isolation tests
8. gofmt
9. `.gitignore`/binary 정리
10. 전체 검증

각 1~7 단계 후 관련 package test를 실행한다.

## 14. 최종 검증

```bash
rg -n 'RegistryFromContext\(.*\).*_' \
  companion-daemon/internal/term

rg -n 'RegistryFromContext\(.*\)\.' \
  companion-daemon/internal/term

rg -n 'creator\.CreateSession|terminator\.TerminateSession' \
  companion-daemon/internal/term/pty.go

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
ignored Registry context errors: 0
unchecked Registry context chains: 0
direct adapter mutation in handler: 0
full diff check: PASS
generated binary diff: 0
vet: PASS
unit: PASS
race: PASS
known failures: none
```

## 15. 제출 보고 형식

```text
Commit:
Registry context errors handled:
CRUD mutation API:
Caller context:
IPC error logging:
Mutation tests:
Cmux health tests:
WebSocket isolation:
Gofmt:
Full diff check:
Generated binary diff:
Vet:
Unit:
Race:
Known failures:
```

`Known failures`가 비어 있지 않으면 완료라고 보고하지 않는다.
