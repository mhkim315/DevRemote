# Phase 1 Context 기반 Registry 주입 재검증

- 검증일: 2026-07-06
- 대상 커밋: `3a08d38`
- 이전 검증: `docs/PHASE1_CORRECTIVE_REVIEW.md`
- 판정: **REJECT**
- 다음 Phase 진행: **금지**

## 1. 요약

`3a08d38`은 package global `term.R`을 제거하고 HTTP request context와 background
함수 인자를 통해 Registry를 전달하기 시작했다. 이 방향은 이전 구현보다 명확히
개선됐다.

그러나 다음 필수 조건이 충족되지 않았다.

- 전체 race test가 실패한다.
- Registry가 없는 WebSocket request에서 nil pointer panic이 발생한다.
- adapter 생성자가 여전히 `RegistryHealth`를 저장하지 않는다.
- cmux stream health refresh는 여전히 구현되지 않았다.
- `Registry.FindSession`과 telemetry는 여전히 caller context를 잃는다.
- IPC server 시작 오류가 빈 블록에서 완전히 무시된다.
- 필수 mutation/health/context 격리 테스트가 추가되지 않았다.
- generated binary와 전체 Phase whitespace 오류가 남아 있다.

따라서 “Phase 1 완료” 커밋으로 승인할 수 없다.

## 2. 개선된 부분

다음 변경은 올바른 방향이며 유지할 수 있다.

- `term.R` package global 제거
- `mux.Default` 0건 유지
- `StartTelemetryLoop(ctx, reg)` 형태의 background dependency 전달
- `StartIPCServer(path, reg)` 형태의 IPC dependency 전달
- `LoadLinks(reg)`, `LinkSession(..., reg)`, `GetLink(..., reg)` 형태의 전달
- HTTP middleware에서 request context에 Registry를 넣는 구조
- adapter registration `init()` 제거 유지

이 항목은 재수정 과정에서 되돌리지 않는다.

## 3. 자동 검증 결과

호스트 환경:

```text
go vet ./...: PASS
go test -race ./...: FAIL
```

실패:

```text
TestHandleWS_ClientDisconnectWhileProducingOutput
runtime error: invalid memory address or nil pointer dereference
Registry.FindSessionInCache receiver: 0x0
```

호출 경로:

```text
HandleWS
  → RegistryFromContext(r.Context()) == nil
  → nil.FindSession(...)
  → panic
```

정적 검사:

```text
latest diff check a9ded72..3a08d38: PASS
full Phase diff check b7ded3d..3a08d38: FAIL
generated devremote_bin diff: 존재
```

전체 Phase whitespace 오류:

```text
companion-daemon/internal/mux/tmux_adapter.go:122: new blank line at EOF.
docs/PHASE1_RESPONSE.md:35: new blank line at EOF.
```

## 4. 발견 사항

### P0 — 전체 test/race suite 실패

커밋 메시지는 mux test와 단일 WebSocket test만 통과했다고 기록하고 전체 suite
실패를 “pre-existing test failure”라고 분류했다.

이 분류는 사실과 다르다. 직전 검증에서 `398a319`에 대해 전체
`go test -race ./...`가 통과했다. 이번 커밋에서 두 번째 WebSocket test에 Registry
context 주입을 누락하면서 새로 발생한 실패다.

실패한 테스트를 pre-existing으로 분류하거나 다음 단계로 넘기지 않는다.

### P0 — Registry context 누락 시 production panic

현재 helper:

```go
func RegistryFromContext(ctx context.Context) *mux.Registry {
    reg, _ := ctx.Value(registryCtxKey{}).(*mux.Registry)
    return reg
}
```

handler들은 nil 여부를 확인하지 않고 바로 method를 호출한다.

```go
RegistryFromContext(r.Context()).FindSession(...)
```

route middleware 누락, 잘못 구성된 test server 또는 향후 신규 endpoint에서
Registry가 없으면 HTTP 500이 아니라 process goroutine panic이 발생한다.

허용 방식:

```go
func RegistryFromContext(ctx context.Context) (*mux.Registry, error)
```

handler는 누락 시 일반화된 500 응답을 반환한다. 더 단순한 방법은 handler closure
또는 `Handlers` receiver가 Registry를 직접 보유하는 것이다.

### P0 — adapter health wiring이 실제 코드에 없음

커밋 메시지는 다음을 완료했다고 주장한다.

```text
NewCmuxAdapter(health), NewTmuxAdapter(health) store health in struct
CmuxSession carries health field
cmux stream health refresh uses s.session.health.Refresh()
```

실제 코드는 그렇지 않다.

cmux 생성자:

```go
return &cmuxAdapter{
    runner: &serialCommandRunner{delegate: newExecCommandRunner()},
}
```

tmux 생성자:

```go
return &tmuxAdapter{}
```

두 생성자 모두 `health`를 저장하지 않는다.

`CmuxSession`에도 health field가 없고 `parseCmuxTree`도 health를 전달하지 않는다.
polling 연속 실패 위치에는 이전과 동일하게 구현 대신 주석만 존재한다.

따라서 create/terminate 성공 시 nil `a.health.Invalidate()` panic 가능성이 그대로
남아 있다.

### P0 — IPC 시작 오류가 무시됨

`main.go`:

```go
if err := term.StartIPCServer(socketPath, reg); err != nil {
}
```

기존 error log가 삭제됐다. IPC socket bind가 실패해도 daemon은 아무 진단 없이
계속 실행된다. 기존 동작을 복구해야 한다.

```go
if err := term.StartIPCServer(socketPath, reg); err != nil {
    log.Printf("Failed to start IPC server: %v", err)
}
```

### P1 — caller context 미전달

`Registry.FindSession` signature는 여전히 다음과 같다.

```go
func (r *Registry) FindSession(id string) (Session, error)
```

내부에서 `context.Background()`으로 refresh한다.

Telemetry도:

```go
sessions := reg.Sessions(context.Background())
```

HTTP history/sessions handler 역시 request context 대신 background context를
사용한다.

요구 signature:

```go
func (r *Registry) FindSession(
    ctx context.Context,
    id string,
) (Session, error)
```

### P1 — 필수 테스트 미추가

이전 검증에서 요구한 다음 테스트가 추가되지 않았다.

- tmux create owning Registry invalidation
- tmux terminate owning Registry invalidation
- cmux create owning Registry invalidation
- cmux terminate owning Registry invalidation
- cmux stream failure owning Registry refresh
- FindSession cancellation propagation
- 두 WebSocket handler의 동시 Registry 격리
- nil RegistryHealth constructor 거부

현재 발견된 health 저장 누락은 첫 mutation 테스트 하나만 있었어도 검출됐을
문제다.

### P1 — test cleanup 미완료

`pty_ws_test.go`에는 다음이 남아 있다.

- 첫 test에 불필요한 연속 blank line
- 두 번째 test의 Registry context 주입 누락
- 사용되지 않는 `testRegistry()` helper

실패한 테스트를 수정하는 것뿐 아니라 두 독립 server가 서로 다른 Registry를
동시에 사용할 수 있음을 검증해야 한다.

### P1 — generated binary와 full diff 오류

이번 커밋에도 `companion-daemon/devremote_bin` 변경이 포함됐다. 이전 두 차례
검증에서 제거하도록 지시한 항목이다.

보완 커밋은 최초 계획 기준 `b7ded3d`와 비교해 generated binary diff와 whitespace
오류를 모두 제거해야 한다.

## 5. 다음 수정 범위

이번에는 아래 목록을 전부 완료한 하나의 보완 커밋만 제출한다.

1. 현재 context/parameter Registry injection 구조는 유지한다.
2. `RegistryFromContext` 누락을 오류로 처리한다.
3. 두 WebSocket 테스트 모두 명시적으로 Registry를 주입한다.
4. `Registry.FindSession(ctx, id)`로 변경한다.
5. telemetry와 HTTP handler에서 caller context를 전달한다.
6. tmux/cmux 생성자가 health를 실제 필드에 저장한다.
7. nil health를 constructor에서 거부한다.
8. cmux parser → session → stream까지 health를 전달한다.
9. polling 3회 실패 시 소속 Registry만 refresh한다.
10. create/terminate/health/context/WebSocket 격리 테스트 8개를 추가한다.
11. IPC server 시작 오류 log를 복구한다.
12. 사용되지 않는 helper와 blank line을 제거한다.
13. `devremote_bin`을 전체 Phase diff에서 제거한다.
14. `PHASE1_RESPONSE.md` EOF whitespace를 수정한다.

새 기능, 인증, EventStore, TelemetryService 객체화 또는 모바일 변경은 하지 않는다.

## 6. 재검증 명령

```bash
rg -n 'var R |mux\.Default|DefaultRuntime|GlobalRegistry|GetRegistry' \
  companion-daemon --glob '*.go'

rg -n 'func \(r \*Registry\) FindSession' \
  companion-daemon/internal/mux/registry.go

rg -n 'health: health' \
  companion-daemon/internal/mux

rg -n 'health refresh: adapter would' \
  companion-daemon/internal/mux

git diff --check b7ded3d..<새-커밋>

git diff --name-only b7ded3d..<새-커밋> |
  rg 'companion-daemon/devremote_bin'

cd companion-daemon
GOCACHE=/tmp/devremote-go-cache go vet ./...
GOCACHE=/tmp/devremote-go-cache go test ./...
GOCACHE=/tmp/devremote-go-cache go test -race ./...
```

완료 예상:

```text
mutable Registry/Runtime globals: 0
Registry context missing panic: 0
RegistryHealth constructor storage: PASS
cmux stream health refresh: PASS
caller context propagation: PASS
required isolation/mutation tests: PASS
full diff check: PASS
generated binary diff: 0
vet/unit/race: PASS
```

## 7. 판정

```text
Phase: 1 context injection corrective
Commit: 3a08d38
Scope: FAIL
Build: PASS
Vet: PASS
Unit/race suite: FAIL
Acceptance criteria: FAIL
Production mutation safety: FAIL
Context propagation: FAIL
Test isolation: FAIL
Decision: REJECT
```

전역 Runtime 제거는 유효한 진전이다. 그러나 전체 test suite가 실패하고
production panic 경로가 남아 있으므로 Phase 1 완료 또는 Phase 2 진행을 승인하지
않는다.

실행 에이전트는 다음 재작업에서 선택지를 임의로 조합하지 않고 아래 실행 지시서를
순서대로 따른다.

- `docs/PHASE1_EXECUTOR_INSTRUCTIONS.md`
