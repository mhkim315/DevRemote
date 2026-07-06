# Adapter Expansion Phase 1 Reverification — Round 2

- Phase: 1 — Identity와 discovery 계약 확정
- Corrective commits: `bc5f9d2`, `adcd9e5f7`
- Previous verification: `37f0165`
- Verifier decision: **REJECT**
- Verified on: 2026-07-06

## Scope and corrective status

이번 수정은 Registry 일부 sentinel wrapping과 POST/DELETE validation 호출을 추가했다.
production scope는 Phase 1 안에 있다.

해결:

- POST/DELETE가 invalid canonical ID를 400으로 거부
- missing adapter create/terminate가 `ErrAdapterUnavailable`
- unsupported create/terminate가 `ErrUnsupported`
- 최종 missing session error가 `ErrSessionNotFound`

미해결:

- tmux command context 전달
- 필수 `GetSession` 제거
- live lookup의 stale cache 검증
- refresh unavailable/timeout sentinel
- lookup validation
- constructor duplicate/name validation
- Phase 1 acceptance tests

## Automated verification

- `git diff --check 37f0165..adcd9e5f7`: PASS
- Phase 1 identity/API targeted tests: PASS
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- `npx tsc --noEmit`: PASS

## Findings

### P1 — tmux context 수정 주장이 실제 커밋에 없다

- 커밋 설명: `bc5f9d2`
- 코드: `companion-daemon/internal/mux/tmux_adapter.go:103-105`

`ListSessions(ctx)`는 여전히 `exec.Command`를 호출한다. `exec.CommandContext` 변경은
두 corrective commit의 diff에 존재하지 않는다.

요구 수정:

- `exec.CommandContext(ctx, ...)`로 실제 코드를 변경한다.
- canceled/deadline context가 tmux process와 반환 error까지 전달되는 test를 추가한다.

### P1 — stale cache contract를 구현하지 않고 기존 동작을 주석으로 유지했다

- 파일: `companion-daemon/internal/mux/registry.go:104-127`

추가된 주석은 cache hit를 즉시 반환하고 live caller가 별도로 `Refresh(true)`를 호출해야
한다고 적는다. 실제 `HandleWS`와 history handler는 `FindSession`만 호출하므로 별도
refresh를 하지 않는다.

더구나 cache miss 뒤 refresh가 실패해 stale snapshot을 보존하면 refresh error를
무시한 뒤 stale session을 찾아 반환할 수 있다. 이는 계획의 다음 계약과 반대다.

```text
live I/O 전 target adapter 강제 refresh
refresh 성공 + 없음 → ErrSessionNotFound
refresh 실패 → ErrAdapterUnavailable, stale session 반환 금지
```

요구 수정:

- `FindSession` 자체를 live lookup 계약으로 구현하거나 별도 `FindLiveSession`을 만들고
  모든 live/history 호출부가 이를 사용하게 한다.
- refresh error를 버리지 않는다.
- cache hit ended race와 cache miss + retained stale snapshot failure test를 추가한다.

### P1 — snapshot lookup 계약과 필수 interface 정리가 미완성이다

- interface: `companion-daemon/internal/mux/adapter.go:115-120`
- tmux: `companion-daemon/internal/mux/tmux_adapter.go:131-134`
- cmux: `companion-daemon/internal/mux/cmux_adapter.go:234-251`

`GetSession`이 여전히 필수 `Adapter` method다. Phase 1 계획은 list snapshot lookup을
기본으로 하고 direct lookup은 실제 필요가 있을 때만 선택 capability로 두도록 했다.

요구 수정:

- 필수 `GetSession`과 구현/test boilerplate를 제거한다.
- adapter별 refreshed snapshot에서 local ID를 찾는다.

### P1 — sentinel integration이 refresh/timeout 경로를 구분하지 않는다

- 파일: `companion-daemon/internal/mux/registry.go:149-203`

없는 adapter refresh는 일반 error이며 adapter discovery failure도 원본 error 그대로다.
`ErrAdapterUnavailable`과 `ErrTimeout`은 refresh 경로에서 사용되지 않는다.
`FindSession`은 refresh error 자체를 무시한다.

요구 수정:

- discovery failure를 `ErrAdapterUnavailable`로 wrapping한다.
- deadline이면 `context.DeadlineExceeded`와 `ErrTimeout`을 모두 식별 가능하게 보존한다.
- canceled context는 `context.Canceled`를 보존한다.
- empty success와 unavailable error를 구분하는 tests를 추가한다.

### P1 — validation이 lookup/registration에 적용되지 않는다

- API create/delete: `companion-daemon/internal/term/pty.go:47-49`, `:81-84`
- lookup: `companion-daemon/internal/mux/registry.go:107-127`
- registration: `companion-daemon/internal/mux/registry.go:25-48`

POST/DELETE만 validation한다. WebSocket/history가 사용하는 Registry lookup과 adapter
registration은 invalid ID/name을 허용한다.

또한 validation을 fallback보다 먼저 수행해 adapter 없는 legacy create/delete ID는 항상
400이 되므로 아래 tmux fallback은 dead code다. fallback 제거가 Phase 1 의도라면 코드를
제거하고 호환 변경을 test로 고정해야 한다.

요구 수정:

- Registry lookup과 registration에 validation/name 문법을 적용한다.
- adapter 없는 ID의 지원/거부 정책을 하나로 정하고 dead fallback을 제거한다.
- C1 control characters까지 거부할지 계약과 test를 명시한다.

### P1 — constructor duplicate 정책이 여전히 last-write-wins다

- 파일: `companion-daemon/internal/mux/registry.go:25-35`

커밋 설명은 이를 “map semantics”라고 문서화했지만 Phase 1 계획은 duplicate adapter
registration의 명시적 실패를 요구한다. constructor만 silent overwrite를 허용하면
등록 API에 따라 계약이 달라진다.

요구 수정:

- `NewRegistry`도 `Register`와 동일한 validation을 사용하거나 error-returning constructor로
  바꾼다.
- duplicate constructor input test를 추가한다.

### P2 — 추가된 validation/sentinel 동작을 검증하는 tests가 없다

두 corrective commit은 production files 두 개만 바꾸고 test를 추가하지 않았다. 기존
targeted tests는 helper sentinel의 상호 구분과 valid/invalid `SessionRef`만 검사한다.

필수 test:

- invalid POST/DELETE가 backend를 호출하지 않고 400
- lookup invalid ID
- create/terminate unavailable 및 unsupported `errors.Is`
- refresh unavailable/timeout/canceled
- constructor duplicate
- stale live lookup 종료 race

## Host/mobile verification

lookup 계약이 아직 구현되지 않았으므로 host/mobile smoke는 보류한다. 수정 완료 후
colon tmux, tmux/cmux WebSocket, ended session, adapter unavailable 및 reconnect storm
부재를 확인해야 한다.

## Required executor actions

1. tmux context 변경을 실제 코드와 test에 반영한다.
2. `GetSession`을 제거하고 refreshed snapshot lookup을 구현한다.
3. stale live lookup의 not-found/unavailable 계약을 구현한다.
4. refresh/timeout/cancel sentinel을 실제 경로에 연결한다.
5. lookup/registration validation과 adapter 없는 ID 정책을 완결한다.
6. constructor duplicate를 명시적으로 실패시킨다.
7. 모든 새 production behavior에 regression tests를 추가한다.

## Next phase permission

**BLOCKED** — Phase 1 ACCEPT 전 Phase 2 진행 금지.
