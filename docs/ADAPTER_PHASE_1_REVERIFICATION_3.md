# Adapter Expansion Phase 1 Reverification — Round 3

- Phase: 1 — Identity와 discovery 계약 확정
- Corrective commit: `adab2d965b7853dc6970eb6c6a4e658f839ab9db`
- Previous verification: `db61396`
- Verifier decision: **REJECT**
- Verified on: 2026-07-07

## Corrective action status

해결:

- tmux `ListSessions(ctx)`가 `exec.CommandContext` 사용
- `GetSession`이 필수 `Adapter` interface에서 제거됨

미해결:

- live lookup의 stale cache 검증
- refresh unavailable/timeout/cancel sentinel
- lookup/registration validation
- constructor duplicate rejection
- 실제 production behavior regression tests

추가 정리 필요:

- POST/DELETE에 중복 추가된 `Validate()` 호출
- 존재하지 않는 optional `SessionLookup`을 언급하는 interface 주석

## Automated verification

- `git diff --check db61396..adab2d965`: PASS
- Phase 1 identity/API targeted tests: PASS
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- `npx tsc --noEmit`: PASS

## Findings

### P1 — stale live lookup 계약이 여전히 구현되지 않았다

- 파일: `companion-daemon/internal/mux/registry.go:106-129`

`FindSession`은 cache hit를 refresh 없이 반환한다. cache miss일 때도 forced refresh error를
무시하고 보존된 stale snapshot을 다시 검색한다. 따라서 종료된 session 또는 unavailable
adapter의 stale session이 live target으로 반환될 수 있다.

요구 수정:

- canonical ID를 parse/validate한 뒤 target adapter를 강제 refresh한다.
- refresh 성공 후 local ID가 없으면 `ErrSessionNotFound`
- refresh 실패면 stale session을 반환하지 않고 `ErrAdapterUnavailable`
- adapter가 없는 경우도 `ErrAdapterUnavailable`
- cache hit ended race와 refresh failure retaining stale snapshot tests 추가

### P1 — refresh error taxonomy가 미구현이다

- 파일: `companion-daemon/internal/mux/registry.go:150-204`

없는 adapter는 일반 `fmt.Errorf`, discovery 실패는 원본 error, deadline은 caller
context error 또는 adapter error로 흩어진다. `ErrTimeout`은 production refresh 경로에서
사용되지 않는다.

요구 수정:

- unavailable discovery를 `ErrAdapterUnavailable`로 wrapping
- deadline은 `ErrTimeout`과 `context.DeadlineExceeded`를 모두 판별 가능하게 보존
- cancel은 `context.Canceled` 보존
- empty successful list와 unavailable을 구분하는 tests 추가

### P1 — lookup 및 registration validation이 없다

- lookup: `companion-daemon/internal/mux/registry.go:109-129`
- constructor/register: `companion-daemon/internal/mux/registry.go:25-50`

POST/DELETE만 `SessionRef.Validate()`를 사용한다. WebSocket/history가 호출하는
`FindSession`은 직접 문자열 split을 사용하며 invalid canonical ID를 validation하지
않는다. adapter name도 empty/control/invalid syntax를 검사하지 않는다.

요구 수정:

- `FindSession`이 `ParseSessionID` + `Validate`만 사용하고 직접 `SplitN`을 제거
- `Register`와 constructor에 동일한 adapter-name validation 적용
- invalid lookup이 adapter를 호출하지 않고 `ErrInvalidSessionID`를 반환하는 test 추가

### P1 — constructor duplicate는 계획과 반대로 silent overwrite다

- 파일: `companion-daemon/internal/mux/registry.go:25-37`

새 주석은 last-write-wins를 명시했지만 Phase 1 계획은 duplicate adapter registration의
명시적 실패를 요구한다. 생성 경로에 따라 duplicate 의미가 달라지면 fixture/세 번째
adapter 등록 오류를 놓칠 수 있다.

요구 수정:

- error-returning constructor 또는 empty registry + `Register` 단일 경로 사용
- duplicate constructor input이 `ErrDuplicateAdapter`가 되는 test 추가

### P1 — 새 production 계약을 검증하는 test가 추가되지 않았다

이번 커밋은 production files 네 개만 변경했다. 다음 tests가 여전히 없다.

- tmux cancellation/deadline
- live lookup fresh hit / ended race / stale refresh failure
- refresh unavailable/timeout/cancel
- invalid lookup causes no adapter call
- constructor duplicate/invalid adapter name
- tmux/cmux exactly-once canonical create

### P2 — handler validation이 중복 실행된다

- 파일: `companion-daemon/internal/term/pty.go:47-56`, `:86-95`

POST/PUT와 DELETE에서 동일한 `ref.Validate()` block이 두 번 연속 존재한다. 동작 오류는
없지만 corrective commit이 기존 코드를 확인하지 않고 중복 삽입된 흔적이다.

요구 수정:

- 각 경로에서 한 번만 validation한다.
- adapter 없는 ID를 거부하므로 뒤의 tmux fallback dead code도 제거하거나 호환 정책을
  test로 명시한다.

## Host/mobile verification

lookup 의미가 아직 완성되지 않아 host/mobile smoke는 보류한다. 다음 수정 후 colon 포함
tmux ID, tmux/cmux WebSocket, ended session, unavailable adapter와 reconnect storm 부재를
검증해야 한다.

## Required executor actions

1. live lookup의 forced refresh 및 not-found/unavailable 계약을 구현한다.
2. refresh timeout/cancel/unavailable sentinel을 연결한다.
3. lookup과 registration validation을 적용한다.
4. constructor duplicate를 명시적으로 실패시킨다.
5. production behavior regression tests를 추가한다.
6. 중복 validation과 dead fallback을 정리한다.

## Next phase permission

**BLOCKED** — Phase 1 ACCEPT 전 Phase 2 진행 금지.
