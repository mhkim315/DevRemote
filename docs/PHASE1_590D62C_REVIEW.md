# Phase 1 커밋 `590d62c` 검증 결과

- 검증일: 2026-07-06
- 대상 커밋: `590d62c`
- 실행 지시서: `docs/PHASE1_EXECUTOR_INSTRUCTIONS.md`
- 판정: **REJECT — 최종 마무리 보완 필요**
- 다음 Phase 진행: **아직 금지**

## 1. 총평

이번 커밋은 이전 시도보다 크게 진전됐다.

완료된 핵심 항목:

- `mux.Default`, `term.R` 전역 singleton 0건
- `Registry.FindSession(ctx, id)` 전환
- Registry의 `CreateSession`, `TerminateSession` 추가
- tmux adapter의 Registry 의존 제거
- cmux adapter가 `RegistryHealth`를 실제 field에 저장
- cmux parser → session → stream health 전달
- cmux polling 실패 시 소속 Registry refresh 구현
- nil `RegistryHealth` 생성 거부
- 두 WebSocket 기존 테스트 정상화
- 전체 vet, unit, race test 통과

그러나 새 Registry mutation API가 실제 HTTP CRUD 경로에서 사용되지 않고,
Registry context 누락 panic과 IPC 오류 무시가 남았다. 필수 테스트도 대부분
추가되지 않았으며 전체 Phase diff와 formatting 검사가 실패한다.

기능 기반은 거의 완성됐지만 acceptance criteria를 충족하지 않아 아직 ACCEPT할 수
없다.

## 2. 자동 검증

호스트 환경:

```text
go vet ./...: PASS
go test ./...: PASS
go test -race ./...: PASS
```

정적 검사:

```text
mutable Registry/Runtime singleton: 0
placeholder health comment: 0
git diff --check b7ded3d..590d62c: FAIL
generated devremote_bin diff: 존재
gofmt diff: 존재
```

`git diff --check` 실패:

```text
companion-daemon/internal/mux/cmux_adapter_mock_test.go:111: trailing whitespace
companion-daemon/internal/mux/cmux_adapter_mock_test.go:164: trailing whitespace
companion-daemon/internal/mux/cmux_adapter_mock_test.go:219: trailing whitespace
```

## 3. 발견 사항

### P0 — HTTP CRUD가 Registry mutation API를 사용하지 않음

Registry에 올바른 method가 추가됐다.

```go
reg.CreateSession(ctx, adapterName, opts)
reg.TerminateSession(ctx, adapterName, id)
```

하지만 `HandleSessionCRUD`는 여전히 adapter를 직접 꺼내 capability를 호출한다.

```go
adapter, _ := RegistryFromContext(...).Adapter(adapterName)
creator.CreateSession(...)
terminator.TerminateSession(...)
```

결과:

- create 성공 후 Registry cache가 invalidate되지 않는다.
- terminate 성공 후 Registry cache가 invalidate되지 않는다.
- 새 Registry method는 production에서 사용되지 않는 dead API다.

수정:

```go
reg, err := RegistryFromContext(r.Context())
// error 처리

createdID, err := reg.CreateSession(
    r.Context(),
    adapterName,
    opts,
)
```

DELETE도 `reg.TerminateSession()`을 직접 사용한다.

### P0 — Registry context 누락 시 nil panic 유지

`RegistryFromContext`는 아직 다음 signature다.

```go
func RegistryFromContext(ctx context.Context) *mux.Registry
```

context에 Registry가 없으면 nil을 반환한다. handler는 nil을 확인하지 않고 즉시
method를 호출한다.

이전 커밋에서 실제 panic으로 확인된 경로가 구조적으로 남아 있다. 기존 테스트에
context를 넣어 통과시킨 것은 production 안전 처리를 추가한 것이 아니다.

요구:

```go
func RegistryFromContext(
    ctx context.Context,
) (*mux.Registry, error)
```

모든 handler에서 누락 시 panic이 아니라 일반화된 HTTP 500을 반환한다.

### P1 — IPC 시작 오류가 계속 무시됨

`main.go`:

```go
if err := term.StartIPCServer(socketPath, reg); err != nil {
}
```

기존 log를 복구한다.

```go
if err := term.StartIPCServer(socketPath, reg); err != nil {
    log.Printf("Failed to start IPC server: %v", err)
}
```

### P1 — HTTP와 telemetry 일부 context 손실

개선된 `FindSession(ctx, id)`는 올바르다. 그러나 다음 호출은 여전히
`context.Background()`을 사용한다.

```go
sessions := reg.Sessions(context.Background())
sessions := RegistryFromContext(r.Context()).Sessions(context.Background())
```

변경:

- telemetry: `reg.Sessions(ctx)`
- HTTP: `reg.Sessions(r.Context())`
- HTTP history screen/history read도 가능한 범위에서 `r.Context()` 사용

startup link load와 현재 IPC connection 경로의 background context는 이번 Phase에서
허용한다.

### P1 — 필수 테스트 대부분 누락

추가된 테스트는 nil health를 거부하는 생성자 테스트 수준이다. 다음 acceptance
test가 검색되지 않는다.

- Registry create invalidation
- Registry terminate invalidation
- cmux stream failure health refresh call 검증
- FindSession cancellation propagation
- Registry context 누락 시 HTTP 500
- 두 WebSocket handler의 동시 독립 Registry 검증
- cmux health 저장/호출 검증

현재 mock health는 refresh 호출 횟수·adapter name·force 값을 기록하지 않아 health
동작을 검증하지 않는다.

### P1 — formatting 및 불필요 코드

`gofmt -d` 결과 다음 문제가 있다.

- `cmux_adapter.go` struct field 들여쓰기
- health refresh error block 들여쓰기
- 연속 blank line
- `cmux_adapter_mock_test.go` trailing whitespace
- `pty_ws_test.go` 연속 blank line과 한 줄에 두 statement
- `linker.go` import order

cmux terminate에는 비어 있는 조건문도 남아 있다.

```go
if err == nil {
}
```

단순히 `return runner.Run(...)` 결과를 처리하도록 정리한다.

### P1 — generated binary가 여전히 tracked diff

`.gitignore`에 경로를 추가해도 이미 추적 중인 `devremote_bin`의 기존 변경은
사라지지 않는다.

현재 다음 세 ignore entry가 중복돼 있다.

- root `.gitignore`
- `companion-daemon/.gitignore`
- `companion-daemon/internal/.gitignore`

`internal/.gitignore`는 해당 binary 위치와도 맞지 않으므로 제거한다. 하나의 적절한
ignore rule만 유지하고, tracked binary blob은 기준 `b7ded3d`와 동일하게 복구한다.

## 4. 최종 보완 체크리스트

새 설계는 필요 없다. 다음 항목만 수정한다.

1. CRUD handler가 `Registry.CreateSession/TerminateSession`을 사용
2. `RegistryFromContext`가 `(*Registry, error)` 반환
3. 모든 HTTP handler가 context 누락을 500으로 처리
4. telemetry/HTTP에서 caller context 사용
5. IPC start error log 복구
6. 필수 mutation/health/cancellation/context/WebSocket 격리 테스트 추가
7. 전체 변경 파일 `gofmt`
8. trailing whitespace와 빈 조건문 제거
9. 중복 `.gitignore` 정리
10. tracked `devremote_bin`을 기준 blob으로 복구

금지:

- 새 architecture 도입
- 인증·store·mobile 변경
- 테스트 skip
- known failure 허용

## 5. 재검증 명령

```bash
gofmt -w \
  companion-daemon/cmd/devremote/main.go \
  companion-daemon/internal/mux/*.go \
  companion-daemon/internal/term/*.go

git diff --check b7ded3d..<새-커밋>

git diff --name-only b7ded3d..<새-커밋> |
  rg 'companion-daemon/devremote_bin'

rg -n 'creator\.CreateSession|terminator\.TerminateSession' \
  companion-daemon/internal/term/pty.go

rg -n 'RegistryFromContext\(.*\)\.' \
  companion-daemon/internal/term --glob '*.go'

cd companion-daemon
GOCACHE=/tmp/devremote-go-cache go vet ./...
GOCACHE=/tmp/devremote-go-cache go test ./...
GOCACHE=/tmp/devremote-go-cache go test -race ./...
```

예상:

```text
direct adapter mutation from handler: 0
unchecked RegistryFromContext chaining: 0
full diff check: PASS
generated binary diff: 0
vet/unit/race: PASS
known failures: none
```

## 6. 판정

```text
Phase: 1 final corrective
Commit: 590d62c
Scope: PARTIAL PASS
Architecture direction: PASS
Global ownership: PASS
Adapter health wiring: PASS
Build/vet/unit/race: PASS
Production mutation invalidation: FAIL
Missing-context safety: FAIL
Required acceptance tests: FAIL
Formatting/full diff: FAIL
Decision: REJECT
```

이번 REJECT는 이전처럼 구조를 다시 설계해야 한다는 뜻이 아니다. 핵심 구조는
올바른 방향에 도달했다. 위 10개 마무리 항목을 같은 설계 안에서 처리하면 Phase 1
최종 승인 가능성이 높다.
