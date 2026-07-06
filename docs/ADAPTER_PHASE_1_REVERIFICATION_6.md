# Adapter Expansion Phase 1 Reverification — Round 6

- Phase: 1 — Identity와 discovery 계약 확정
- Corrective commit: `5662d610a0885aa27f9a4e9ee8d49924e1910628`
- Previous verification: `4a3b2c7`
- Verifier decision: **REJECT**
- Verified on: 2026-07-07

## Corrective action status

해결:

- production composition root가 error-returning `NewRegistry` 사용
- singleflight first-waiter context 문제를 Phase 3 deferred로 기록
- adapter-name, duplicate constructor, invalid lookup tests 추가

부분 해결:

- `ErrTimeout` wrapping 코드 추가
- Phase 1 regression test 파일 보강

미해결:

- `Refresh` deadline 반환의 결정적 `ErrTimeout` taxonomy
- ended-session/stale-failure live lookup regression tests
- cancel과 timeout 구분 test
- exactly-once canonical create의 tmux/cmux 경계 test
- URL round-trip
- host smoke

## Automated verification

- `git diff --check 4a3b2c7..5662d610a`: PASS
- Phase 1 targeted tests `-count=20`: PASS
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- `npx tsc --noEmit`: PASS

## Findings

### P1 — `Refresh` deadline은 여전히 raw error로 반환될 수 있다

- wrapping: `companion-daemon/internal/mux/registry.go:197-205`
- select: `companion-daemon/internal/mux/registry.go:215-226`

adapter result가 먼저 선택되면 deadline error가
`ErrTimeout + ErrAdapterUnavailable + context.DeadlineExceeded`로 반환된다. 하지만
`ctx.Done()` case가 먼저 선택되면 `ctx.Err()`만 반환하므로 `ErrTimeout`과
`ErrAdapterUnavailable`을 잃는다.

동일한 timeout이 select scheduling에 따라 다른 taxonomy를 갖는 것은 계약으로 사용할
수 없다.

요구 수정:

- `ctx.Done()` branch도 deadline이면 동일한 timeout/unavailable wrapping을 사용한다.
- 공통 error classifier helper를 사용해 snapshot, result, ctx.Done 경로가 같은 의미를
  갖게 한다.
- explicit cancel은 `context.Canceled`를 보존하고 `ErrTimeout`은 포함하지 않는다.

### P1 — timeout test가 `ErrTimeout`을 검사하지 않고 wrapping 경로도 실행하지 않는다

- 파일: `companion-daemon/internal/mux/phase1_test.go:163-179`

`TestRefresh_TimeoutPreservation`은 `context.DeadlineExceeded`만 assertion한다.
`ErrTimeout`과 `ErrAdapterUnavailable`을 확인하지 않는다.

또한 context를 duration 0으로 생성하고 adapter는 context를 무시한 채 즉시 성공하므로
대부분 `Refresh`의 `ctx.Done()` branch가 raw deadline을 반환한다. 새 wrapping code가
없어도 test가 통과한다.

요구 수정:

- context가 해제될 때까지 block한 뒤 `ctx.Err()`를 반환하는 adapter fake 사용
- 다음 세 가지 모두 assertion:
  - `errors.Is(err, ErrTimeout)`
  - `errors.Is(err, ErrAdapterUnavailable)`
  - `errors.Is(err, context.DeadlineExceeded)`
- cancel test에서는 `context.Canceled == true`, `ErrTimeout == false`

### P1 — stale live lookup의 핵심 regression tests가 아직 없다

forced refresh 구현의 존재 이유인 다음 두 경우를 test하지 않는다.

1. cache에 있던 session이 successful refresh에서 사라짐
   → `ErrSessionNotFound`
2. cache에 있던 session을 보존한 채 refresh 실패
   → `ErrAdapterUnavailable`, session 반환 금지

현재 추가된 `TestFindSession_AdapterUnavailable`은 등록되지 않은 adapter만 검사하므로
stale cache 회귀를 검출하지 못한다.

요구 수정:

- initial successful refresh로 cache를 채운 뒤 adapter 상태를 변경하는 tests 추가
- 반환 session이 반드시 nil인지 검사
- snapshot 자체는 stale session을 계속 보존하는지도 함께 검사

### P1 — create test가 exactly-once canonical API 계약을 검증하지 않는다

- 파일: `companion-daemon/internal/mux/phase1_test.go:181-193`

`TestCreateSession_ReturnsLocalID`은 fake creator가 입력 name을 그대로 반환하게 만든 뒤
결과 문자열에 `"test:"`가 없는지만 검사한다. tmux/cmux creator나 handler
canonicalization을 검증하지 않는다.

요구 수정:

- cmux mock runner가 `surface:42` 출력 시 creator가 `surface:42` local ID를 반환하는 test
- handler fake로 tmux와 cmux 모두 API 응답이 정확히
  `tmux:<local>` / `cmux:surface:<N>`이고 prefix가 한 번만 존재하는 test
- cmux parse failure가 error이고 `unknown` ID를 반환하지 않는 test

### P2 — URL encoded colon/Unicode round-trip test가 없다

parser unit test는 colon/Unicode 문자열을 직접 전달한다. 실제 query path에서
`url.QueryEscape`/decode 후 동일 canonical ID가 Registry까지 도달하는 경계를 고정하지
않는다.

요구 수정:

- colon 포함 local ID와 Unicode ID를 HTTP query에 encode해 handler/lookup round-trip
  test 추가

## Host/mobile verification

코드 계약 tests가 완성된 후 host smoke가 필요하다.

- colon 포함 tmux local ID WebSocket 연결
- 기존 tmux/cmux session WebSocket 연결
- 종료된 session not-found
- unavailable/timeout에서 stale live session 미반환
- reconnect storm 부재

모바일 schema 변경은 없으므로 별도 전체 모바일 회귀보다 기존 session 접속 smoke면
충분하다.

## Required executor actions

1. 모든 `Refresh` 반환 경로의 deadline taxonomy를 일치시킨다.
2. timeout/cancel을 실제로 구분하는 deterministic tests를 추가한다.
3. ended-session과 retained-stale failure live lookup tests를 추가한다.
4. tmux/cmux exactly-once canonical create tests를 추가한다.
5. URL encoded colon/Unicode round-trip test를 추가한다.
6. 자동 검증 후 host smoke 결과를 제출한다.

## Next phase permission

**BLOCKED** — Phase 1 ACCEPT 전 Phase 2 진행 금지.
