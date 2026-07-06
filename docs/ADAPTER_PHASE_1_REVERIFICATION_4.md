# Adapter Expansion Phase 1 Reverification — Round 4

- Phase: 1 — Identity와 discovery 계약 확정
- Corrective commit: `0f13f92d3f72bb0d8a3df141337c1012d1c03ac8`
- Previous verification: `a26ceb8`
- Verifier decision: **REJECT**
- Verified on: 2026-07-07

## Corrective action status

해결:

- `FindSession` cache-hit 즉시 반환 제거
- target adapter forced refresh 후 snapshot lookup
- refresh 실패 시 stale session 반환 차단
- adapter 없는 `Refresh`에 `ErrAdapterUnavailable`
- dead tmux fallback 제거

미해결:

- `Refresh` caller error의 unavailable/timeout taxonomy
- lookup `SessionRef` validation
- registration adapter-name validation
- constructor duplicate rejection
- 새 live lookup/error behavior regression tests
- 중복 handler validation 정리

## Automated verification

- `git diff --check a26ceb8..0f13f92d3`: PASS
- Registry/identity/API targeted tests: PASS
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- `npx tsc --noEmit`: PASS

## Findings

### P1 — `Refresh`는 snapshot만 sentinel로 바꾸고 반환 error는 원본이다

- 파일: `companion-daemon/internal/mux/registry.go:173-204`

adapter discovery가 실패하면 `currentSnap.LastError`는 `ErrAdapterUnavailable`로 wrapping
되지만 singleflight closure는 `adErr` 원본을 반환한다. 따라서 `Refresh` caller가 받는
error는 `errors.Is(err, ErrAdapterUnavailable)`을 만족하지 않는다.

`FindSession`은 별도로 다시 wrapping하므로 그 경로만 unavailable을 식별할 수 있다.
Registry의 public `Refresh` 계약과 snapshot `LastError` 계약이 서로 다르다.

요구 수정:

- closure에서 wrapped error를 snapshot과 return 양쪽에 동일하게 사용한다.
- deadline이면 `ErrTimeout`과 `context.DeadlineExceeded`를 모두 보존한다.
- cancel이면 `context.Canceled`를 보존하고 timeout으로 오분류하지 않는다.
- direct `Refresh` unavailable/timeout/cancel tests를 추가한다.

### P1 — `FindSession`이 canonical parser/validation을 사용하지 않는다

- 파일: `companion-daemon/internal/mux/registry.go:109-128`

`FindSession`은 `strings.SplitN`으로 adapter를 추출하며 `SessionRef.Validate()`를 호출하지
않는다. adapter 없는 ID는 모든 adapter를 refresh한 뒤 `ErrSessionNotFound`가 되고,
control character가 포함된 ID도 adapter lookup까지 진행한다.

이는 `SessionRef`를 canonical ID의 유일한 parser/formatter로 사용한다는 Phase 1
합격 기준과 충돌한다.

요구 수정:

- `MigrateLegacyID` 후 `ParseSessionID` + `Validate` 사용
- invalid ID는 backend 호출 없이 `ErrInvalidSessionID`
- validated `ref.Adapter`와 `ref.Canonical()`만 lookup에 사용
- invalid/no-adapter/control/colon/Unicode/URL round-trip tests 추가

### P1 — adapter registration name validation이 없다

- 파일: `companion-daemon/internal/mux/registry.go:39-50`

`Register`는 duplicate만 확인하고 empty, colon 포함, whitespace/control adapter name을
허용한다. canonical ID의 첫 component 문법이 registration과 parser에서 통일되지 않았다.

요구 수정:

- adapter name 문법을 확정한다. 권장: non-empty ASCII identifier
  `[a-z][a-z0-9_-]*`
- `Register`와 constructor에 동일 validation 적용
- invalid name이 `ErrInvalidSessionID` 또는 별도 typed error가 되는 tests 추가

### P1 — constructor duplicate가 여전히 계획을 위반한다

- 파일: `companion-daemon/internal/mux/registry.go:25-37`

last-write-wins 주석은 현재 동작을 설명할 뿐, Phase 1의 “duplicate adapter name 등록은
명시적으로 실패” 요구를 충족하지 않는다. test compatibility를 이유로 production
constructor 계약을 약화하면 fixture/세 번째 adapter 등록 실수를 숨긴다.

요구 수정:

- `NewRegistry() *Registry`는 empty constructor로 제한하고 adapter 등록은 error-returning
  `Register`만 사용하거나,
- `NewRegistry(adapters...) (*Registry, error)`로 전환한다.
- duplicate variadic input silent overwrite를 제거하는 test 추가

### P1 — 핵심 live lookup 변경에 regression test가 없다

이번 커밋은 production files만 변경하고 test를 추가하지 않았다. 기존
`TestRegistryFindSessionPropagatesCancellation`은 cancellation만 검사하며 다음 새 동작을
검증하지 않는다.

- cache hit도 forced refresh함
- successful refresh에서 ended session → `ErrSessionNotFound`
- failed refresh에서 retained stale session 미반환 → `ErrAdapterUnavailable`
- missing adapter → `ErrAdapterUnavailable`
- invalid ID → `ErrInvalidSessionID`

이 tests 없이 stale-cache 회귀를 향후 발견할 수 없다.

### P2 — POST/DELETE validation이 두 번씩 실행된다

- 파일: `companion-daemon/internal/term/pty.go:47-56`, `:83-92`

각 경로에 동일한 `ref.Validate()` block이 연속 두 개 남아 있다.

요구 수정:

- 한 번만 실행하도록 중복 제거
- invalid request가 Registry/backend를 호출하지 않는 handler test 추가

## Host/mobile verification

forced live refresh는 WebSocket/history 연결 동작에 영향을 주지만 필수 unit regression
tests가 아직 없어 host smoke를 승인 근거로 대체하지 않는다. 위 P1 수정 후 tmux/cmux
WebSocket, colon ID, ended session, adapter unavailable과 reconnect storm 부재를
확인해야 한다.

## Required executor actions

1. `Refresh` return/snapshot error taxonomy를 일치시키고 timeout/cancel을 구분한다.
2. `FindSession`에 `SessionRef` parse/validation을 적용한다.
3. adapter-name validation을 registration 전체 경로에 적용한다.
4. constructor duplicate silent overwrite를 제거한다.
5. forced live lookup 및 error taxonomy regression tests를 추가한다.
6. handler validation 중복을 제거하고 invalid-request test를 추가한다.

## Next phase permission

**BLOCKED** — Phase 1 ACCEPT 전 Phase 2 진행 금지.
