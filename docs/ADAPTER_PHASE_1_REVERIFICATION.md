# Adapter Expansion Phase 1 Reverification

- Phase: 1 — Identity와 discovery 계약 확정
- Corrective commit: `7f0722ecbc709806f5f75c36e77479c0ad3a79a8`
- Previous verification: `fff1190`
- Verifier decision: **REJECT**
- Verified on: 2026-07-06

## Corrective action status

해결:

- cmux creator가 local `surface:<N>`을 반환하고 parse 실패를 error로 처리
- `RawID` 제거 및 `LocalID` 단일화
- cmux `ListSessions(ctx)`가 caller context를 3초 timeout의 parent로 사용
- composition root가 `Register` error를 반환
- tmux golden create canonicalization 유지

미해결:

- tmux command context 전달
- 필수 `GetSession` 제거와 snapshot-only lookup
- stale session의 not-found/unavailable 의미
- sentinel errors의 실제 Registry/adapter 연결
- API/lookup ID validation
- constructor duplicate/name validation
- Phase 1 acceptance tests

## Automated verification

- `git diff --check fff1190..7f0722ecb`: PASS
- Phase 1 identity/API targeted tests: PASS
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- `npx tsc --noEmit`: PASS

## Findings

### P1 — tmux `ListSessions(ctx)`가 context를 사용하지 않는다

- 파일: `companion-daemon/internal/mux/tmux_adapter.go:103-105`

시그니처만 `ListSessions(ctx context.Context)`로 바뀌었고 command는 여전히
`exec.Command`로 생성된다. caller cancellation/deadline이 tmux process에 전달되지
않는다.

요구 수정:

- `exec.CommandContext(ctx, ...)`를 사용한다.
- 취소된 context에서 command가 종료되고 `context.Canceled` 또는
  `context.DeadlineExceeded`가 `errors.Is`로 보존되는 test를 추가한다.

### P1 — snapshot-only lookup과 stale live lookup 계약이 여전히 구현되지 않았다

- adapter: `companion-daemon/internal/mux/adapter.go:115-120`
- Registry: `companion-daemon/internal/mux/registry.go:104-126`

필수 `GetSession(id)`가 남아 있고 tmux/cmux/test adapter가 계속 구현한다.
`FindSession`은 cache hit를 refresh 없이 즉시 반환하므로 종료된 stale session을 live
target으로 돌려준다.

요구 수정:

- 필수 `GetSession`을 제거한다.
- canonical ID를 `SessionRef`로 parse/validate하고 대상 adapter 하나를 강제 refresh한다.
- refresh 성공 후 session이 없으면 `ErrSessionNotFound`
- refresh 실패면 stale session을 반환하지 않고 `ErrAdapterUnavailable`에 원인을 wrapping
- fresh hit, ended-session race, transient failure with stale cache tests를 추가한다.

### P1 — sentinel errors가 선언만 있고 production 경로에서 사용되지 않는다

- 선언: `companion-daemon/internal/mux/adapter.go:11-20`
- Registry: `companion-daemon/internal/mux/registry.go:50-83`, `:104-126`,
  `:146-201`

Registry의 missing adapter, unsupported create/terminate, missing session, refresh failure는
일반 `fmt.Errorf` 또는 원본 error다. `ErrSessionNotFound`,
`ErrAdapterUnavailable`, `ErrUnsupported`, `ErrTimeout`을 production에서 wrapping하는
호출부가 없다.

요구 수정:

- Registry operation별 sentinel mapping을 구현한다.
- 원인 error와 context error를 `%w`/`errors.Join` 등으로 보존한다.
- empty list와 unavailable, not-found와 refresh failure가 `errors.Is`로 구분되는 tests를
  추가한다.

### P1 — `SessionRef.Validate()`가 API/lookup 경계에서 호출되지 않는다

- parser: `companion-daemon/internal/mux/id_parser.go:36-56`
- handler: `companion-daemon/internal/term/pty.go:47-56`, `:77-84`
- lookup: `companion-daemon/internal/mux/registry.go:104-126`

create/delete/lookup 모두 parse 후 validation 없이 진행한다. 빈 component, newline/tab,
invalid adapter name이 backend로 전달될 수 있다.

또한 현재 rune 검사는 C0와 DEL만 거부하며 `unicode.IsControl`이 포함하는 C1 control
characters는 허용한다. 계획의 “control character 거부”를 ASCII C0로 제한할 의도라면
계약 문서에 명시해야 한다.

요구 수정:

- API와 Registry lookup 경계에서 `Validate()`를 적용한다.
- adapter name 문법을 고정하고 registration에도 동일하게 적용한다.
- full control rejection을 의도하면 `unicode.IsControl`을 사용한다.
- invalid ID가 backend를 호출하지 않고 `ErrInvalidSessionID`가 되는 tests를 추가한다.

### P1 — constructor duplicate registration은 여전히 silent overwrite다

- 파일: `companion-daemon/internal/mux/registry.go:25-35`

커밋 설명은 `NewRegistry`가 duplicate를 검사한다고 하지만 실제 diff와 코드는
변경되지 않았다. 동일 이름 adapter를 variadic argument로 넘기면 마지막 값이 map에서
조용히 앞 값을 덮어쓴다.

요구 수정:

- `NewRegistry`가 error를 반환하거나 empty registry + `Register` 단일 경로로 생성되게
  한다.
- constructor/runtime registration 모두 duplicate와 invalid name을 동일하게 거부한다.
- constructor duplicate test를 추가한다.

### P2 — singleflight refresh가 첫 caller context를 공유 작업에 캡처한다

- 파일: `companion-daemon/internal/mux/registry.go:157-201`

`DoChan` closure가 refresh를 시작한 첫 caller의 `ctx`를 adapter command에 전달한다.
첫 caller만 취소돼도 같은 adapter refresh를 기다리는 다른 활성 caller의 공유 작업까지
취소될 수 있다.

요구 수정:

- 공유 refresh 작업 lifetime과 waiter cancellation 정책을 명시한다.
- 한 waiter cancellation이 다른 활성 waiter의 refresh 결과를 깨지 않는 concurrency
  test를 추가한다. 구현을 Phase 3 lifecycle 표준화로 defer한다면 Phase 1 문서에 명시한다.

## Missing tests

- tmux context cancellation/deadline
- cmux/tmux exactly-once canonical create
- invalid ID causes no backend call
- constructor duplicate/invalid adapter name
- fresh lookup / ended session / stale refresh failure
- empty list vs unavailable
- URL round-trip and full control character rejection

## Host/mobile verification

production identity/discovery 경로가 아직 완성되지 않아 host/mobile smoke는 이번 REJECT
판정의 선행 조건으로 실행하지 않았다. 수정 완료 후 colon 포함 tmux ID, tmux/cmux
WebSocket, ended session, adapter unavailable smoke가 필요하다.

## Required executor actions

1. tmux command에 context를 전달한다.
2. `GetSession`을 제거하고 target adapter snapshot refresh lookup을 구현한다.
3. stale session의 not-found/unavailable 계약을 구현한다.
4. sentinel errors를 실제 Registry/adapter 경로에 연결한다.
5. validation과 adapter-name 규칙을 API/Registry/registration에 적용한다.
6. constructor duplicate를 silent overwrite 없이 처리한다.
7. 누락된 Phase 1 tests를 추가한다.
8. singleflight context 정책을 구현하거나 명시적으로 defer한다.

## Next phase permission

**BLOCKED** — Phase 1 ACCEPT 전 Phase 2 진행 금지.
