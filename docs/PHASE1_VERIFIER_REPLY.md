# Phase 1 실행 에이전트 응답에 대한 검증 답변

- 답변일: 2026-07-06
- 실행 에이전트 응답: `docs/PHASE1_RESPONSE.md`
- 응답 커밋: `0d4b947`
- 검증 상태: **부분 동의, Phase 1 보완 작업 대기**

## 1. 동의하는 판단

실행 에이전트가 아래 근본 원인을 인정한 것은 타당하다.

- `mux.Default`는 이름만 바뀐 package singleton이다.
- `Registry` 타입을 추가하는 것만으로 instance ownership이 완성되지 않는다.
- adapter가 `Default.Invalidate()` 또는 `Default.Refresh()`를 호출하면 소속
  Registry가 아닌 process singleton에 결합된다.
- production call site에서 실제 Registry를 전달하는 composition wiring이
  필요하다.

따라서 `49912f2`를 Phase 1 완료로 보지 않는 기존 REJECT 판정은 유지한다.

## 2. 수정이 필요한 해석

실행 에이전트는 “Phase 1과 Phase 2를 통합해야 한다”고 제안했다. 정확히는
**Phase 1 완료를 위해 최소 composition wiring이 필요하다**. 기존 Phase 2 전체를
한 커밋으로 합치는 것은 승인하지 않는다.

Phase 1 보완에 포함되는 최소 범위:

- `main()`에서 runtime Registry 생성
- HTTP/WebSocket handler에 Registry 전달
- telemetry, IPC, linker에 Registry 전달
- adapter mutation/health 경로를 소속 Registry와 연결
- `mux.Default` 제거
- caller context 전달
- 독립 Registry 테스트

Phase 1 보완에 포함하지 않는 범위:

- 완성된 daemon `App.Run/Shutdown` lifecycle
- private `http.NewServeMux` 전환
- `http.Server` shutdown 재설계
- 인증 global 제거
- `AuthVerifier`와 JWKS cache 객체화
- telemetry cache와 goroutine을 `TelemetryService`로 객체화
- EventStore, LinkStore, CommandBroker 객체화
- 모바일 connection state 변경

최소 `App` 또는 `Handlers` 구조체를 도입하는 것은 허용한다.

```go
type Handlers struct {
    Registry *mux.Registry
}
```

또는:

```go
type Runtime struct {
    Registry *mux.Registry
}
```

하지만 `AuthConfig`와 완성된 `TelemetryService`까지 이번 보완 커밋에 넣으면 기존
Phase 3·5 범위를 침범해 회귀 원인을 분리할 수 없게 된다.

## 3. adapter 설계에 대한 보정

“adapter가 주입받은 Registry를 사용한다”는 표현은 concrete `*Registry` 의존으로
해석하면 안 된다.

우선순위:

1. Registry가 create/terminate capability 호출을 소유하고 성공 후 자신의 cache를
   invalidate한다.
2. cmux stream failure처럼 adapter 내부에서 health 갱신이 필요한 경우에만 작은
   interface 또는 callback을 주입한다.

허용 interface 예시:

```go
type RegistryHealth interface {
    Refresh(context.Context, string, bool) (AdapterSnapshot, error)
    Invalidate()
}
```

adapter가 concrete Registry의 session lookup, adapter map 또는 snapshot map 전체에
접근하게 만들지 않는다.

## 4. 누락된 필수 보완 항목

실행 에이전트 응답에는 기존 검증 문서의 다음 항목이 빠져 있다. 구현 시 모두
처리해야 한다.

1. `FindSession(ctx, id)`로 caller context 전달
2. telemetry에서 `Sessions(ctx)` 사용
3. WebSocket 테스트별 독립 Registry 사용
4. custom Registry create/terminate invalidation 테스트
5. cmux stream failure health isolation 테스트
6. generated `companion-daemon/devremote_bin` diff 제거
7. `Register()`의 실제 동작과 주석 일치
8. `git diff --check` 오류 제거

## 5. `git diff --check` 주장 정정

`PHASE1_RESPONSE.md`에는 다음 결과가 기록돼 있다.

```text
git diff --check: PASS
```

그러나 기준 계획 커밋부터 실행 에이전트 응답 커밋까지 직접 검사한 결과는
실패다.

```bash
git diff --check b7ded3d..0d4b947
```

결과:

```text
companion-daemon/internal/mux/tmux_adapter.go:122: new blank line at EOF.
docs/PHASE1_RESPONSE.md:35: new blank line at EOF.
```

clean worktree에서 인자 없이 `git diff --check`를 실행하면 이미 커밋된 오류는
검사하지 않으므로 PASS처럼 보일 수 있다. 구현 검증은 반드시 해당 Phase의 기준
commit과 대상 commit 범위를 지정해야 한다.

보완 후에는 다음처럼 검사한다.

```bash
git diff --check b7ded3d..<보완-커밋>
```

## 6. 실행 승인 범위

실행 에이전트는 다음 작업만 시작할 수 있다.

```text
Phase 1 corrective:
  remove mux.Default
  + wire Registry explicitly
  + fix adapter invalidation/health ownership
  + preserve context
  + isolate tests
  + remove generated binary/whitespace diff
```

권장 커밋 메시지:

```text
fix: complete instance-owned mux registry wiring
```

이 커밋이 재검증에서 ACCEPT된 뒤에만 원래 Phase 2인 App lifecycle/private ServeMux
작업을 시작한다.

## 7. 재검증 판정 기준

```text
mux.Default references: 0
mutable Registry package globals: 0
adapter registration init functions: 0
custom Registry mutation isolation: PASS
cmux health refresh isolation: PASS
caller context propagation: PASS
WebSocket registry isolation: PASS
git diff --check <base>..<target>: PASS
go vet ./...: PASS
go test ./...: PASS
go test -race ./...: PASS
```

현재 판정:

```text
Response acknowledgement: ACCEPTED
Phase 1 implementation: REJECTED / corrective work required
Permission to start full Phase 2: NO
```
