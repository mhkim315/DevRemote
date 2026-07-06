# Adapter Expansion Phase 1 Reverification — Round 5

- Phase: 1 — Identity와 discovery 계약 확정
- Corrective commit: `71f6457b3f689ff74234b3870b234ed9192151e9`
- Previous verification: `f0350c1`
- Verifier decision: **REJECT**
- Verified on: 2026-07-07

## Corrective action status

해결:

- `Refresh` snapshot과 return error에 동일한 `ErrAdapterUnavailable` wrapping
- `FindSession`이 `ParseSessionID` + `Validate` 사용
- adapter name 문법 `[a-z][a-z0-9_-]*` 추가 및 `Register` 적용
- `NewRegistry(...)(*Registry,error)`가 `Register`를 사용해 duplicate/invalid name 거부
- handler validation 중복 제거

미해결:

- `ErrTimeout` mapping
- 새 production behavior에 대한 regression tests
- production composition root의 panic helper 사용 정리
- singleflight waiter context 정책의 명시적 defer 기록

## Automated verification

- `git diff --check f0350c1..71f6457b3`: PASS
- Registry/identity/API targeted tests: PASS
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- `npx tsc --noEmit`: PASS

## Findings

### P1 — timeout sentinel 계약이 구현되지 않았다

- 선언: `companion-daemon/internal/mux/adapter.go:17`
- refresh: `companion-daemon/internal/mux/registry.go:183-216`

adapter가 `context.DeadlineExceeded`를 반환하면 `Refresh`는
`ErrAdapterUnavailable + context.DeadlineExceeded`로 wrapping한다. `ErrTimeout`은
production 어디에서도 wrapping되지 않는다.

따라서 계획이 요구한 unavailable과 timeout 구분을 할 수 없다.

요구 수정:

- `errors.Is(adErr, context.DeadlineExceeded)`이면
  `ErrTimeout`, `ErrAdapterUnavailable`, `context.DeadlineExceeded`를 모두 식별 가능하게
  보존한다.
- explicit cancel은 `context.Canceled`를 보존하되 `ErrTimeout`으로 분류하지 않는다.
- `Refresh`와 `FindSession` 양쪽에서 timeout/cancel taxonomy tests를 추가한다.

### P1 — 핵심 Phase 1 regression tests가 실제로 추가되지 않았다

커밋 설명은 Registry tests가 error taxonomy와 session lookup을 검증한다고 주장하지만
diff는 기존 `NewRegistry` 호출을 `MustNewRegistry`로 치환했을 뿐 새 test/assertion을
추가하지 않았다.

누락된 필수 tests:

- cache에 있던 session이 successful forced refresh 후 사라지면 `ErrSessionNotFound`
- failed forced refresh가 stale session을 반환하지 않고 `ErrAdapterUnavailable`
- invalid lookup이 adapter를 호출하지 않고 `ErrInvalidSessionID`
- direct `Refresh` unavailable/timeout/cancel taxonomy
- `NewRegistry` duplicate input → `ErrDuplicateAdapter`
- invalid adapter names 전체 표
- `Register` duplicate/invalid name
- tmux `ListSessions` cancellation/deadline
- tmux와 cmux create 결과의 exactly-once canonical ID
- URL encoded colon/Unicode round-trip

Phase 1은 이후 모든 adapter가 의존하는 identity/error 계약이므로 production 구현만 있고
contract tests가 없는 상태로 ACCEPT할 수 없다.

### P2 — production composition root가 test용 panic helper를 사용한다

- helper: `companion-daemon/internal/mux/registry.go:39-46`
- production: `companion-daemon/cmd/devremote/app.go:99-103`

`MustNewRegistry`는 주석상 tests용이지만 production `NewAppWithDeps`도 호출한다. 현재는
empty constructor라 panic이 발생하지 않지만 API 의도와 production error handling이
불일치한다.

요구 수정:

- production에서는 `reg, err := mux.NewRegistry()`를 사용하고 error를 반환하거나,
- empty constructor를 별도 non-error `NewRegistry()`로 두고 adapter variadic
  constructor만 error-returning 이름으로 분리한다.

### P2 — singleflight context 정책이 아직 기록되지 않았다

- 파일: `companion-daemon/internal/mux/registry.go:170-216`

첫 waiter context가 공유 refresh command에 캡처되는 동작은 그대로다. 이 문제는 Phase 3
lifecycle/concurrency 범위로 defer할 수 있지만, Phase 1 완료 문서의 deferred item에
명시하고 Phase 3 acceptance criterion으로 이동해야 한다.

## Host/mobile verification

forced lookup 구현은 코드상 완성에 가까우나 필수 regression tests가 없어 host smoke를
승인 근거로 대체하지 않는다. 위 P1 수정 후 다음을 확인한다.

- colon 포함 tmux session WebSocket
- 기존 tmux/cmux session WebSocket
- 종료된 session not-found
- adapter unavailable/timeout과 reconnect storm 부재

## Required executor actions

1. deadline에 `ErrTimeout` taxonomy를 적용하고 cancel과 구분한다.
2. 위에 열거한 Phase 1 contract regression tests를 추가한다.
3. production에서 test용 panic constructor 사용을 제거한다.
4. singleflight context 문제를 Phase 3 deferred item으로 문서화한다.
5. 자동 검증 후 필요한 host smoke 결과를 제출한다.

## Next phase permission

**BLOCKED** — Phase 1 ACCEPT 전 Phase 2 진행 금지.
