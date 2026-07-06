# Adapter Expansion Phase 1 Verification

- Phase: 1 — Identity와 discovery 계약 확정
- Executor commit: `587cfb859786a346993cb52e15e87b59f82f319d`
- Verifier decision: **REJECT**
- Verified on: 2026-07-06

## Scope verification

`SessionRef`, sentinel errors, duplicate `Register`, POST canonical response와 단위 테스트를
추가했다. Phase 2 capability 정리나 backend 추가는 섞이지 않았다.

그러나 Phase 1 계획의 discovery/context, snapshot lookup, typed error integration,
stale session race가 구현되지 않았다. 새 identity helper도 실제 handler 입력 경로에서
검증되지 않으며 cmux create ID를 이중 canonicalize한다.

## Contract verification

다음 항목은 부분적으로 충족됐다.

- 첫 콜론 기준 parser
- colon/Unicode local ID formatter test
- `Register` duplicate error
- tmux POST create의 canonical response
- sentinel error 값의 상호 구분

다음 핵심 계약은 미충족이다.

- `ListSessions(ctx)`와 취소/timeout
- snapshot refresh 기반 lookup 및 stale session liveness
- not-found/unavailable/unsupported/timeout의 실제 error wrapping과 HTTP mapping
- 모든 create adapter의 local-ID 반환
- ID validation의 실제 API/Registry 적용
- 종료 race, URL round-trip, duplicate local ID tests

## Backward compatibility

tmux POST 응답을 local ID에서 canonical ID로 변경한 것은 계획된 변경이며 golden test로
고정됐다. 그러나 cmux POST는 기존 creator 반환값과 새 handler canonicalization이
충돌해 잘못된 ID를 반환한다.

## Automated verification

- `git diff --check efbde27..587cfb859`: PASS
- Phase 1 identity/API targeted tests: PASS
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- `npx tsc --noEmit`: PASS

## Findings

### P1 — cmux create 결과가 이중 canonicalize된다

- handler: `companion-daemon/internal/term/pty.go:54-64`
- cmux creator: `companion-daemon/internal/mux/cmux_adapter.go:453-477`

handler는 모든 creator 결과를 local ID로 가정하고
`SessionRef{Adapter: adapterName, LocalID: createdID}.Canonical()`을 호출한다. 하지만
cmux creator는 `"cmux:surface:<N>"` 또는 `"cmux:unknown"`을 반환한다.

결과:

```text
cmux creator: cmux:surface:42
API response: cmux:cmux:surface:42
```

요구 수정:

- 모든 `SessionCreator` 반환 계약을 local ID로 통일한다.
- cmux는 `surface:42`를 반환하고 parse 실패를 `cmux:unknown` 성공으로 숨기지 말고
  error로 반환한다.
- tmux와 cmux 각각 create 결과가 API 경계에서 정확히 한 번 canonicalize되는 test를
  추가한다.

### P1 — Phase 1 discovery 계약이 구현되지 않았다

- interface: `companion-daemon/internal/mux/adapter.go:115-123`
- registry: `companion-daemon/internal/mux/registry.go:146-200`
- plan: `docs/ADAPTER_EXPANSION_PLAN.md:138-159`

`Adapter`는 여전히 context 없는 `ListSessions()`와 `GetSession(id)`를 필수로 요구한다.
Registry refresh도 `adapter.ListSessions()`를 호출하므로 caller 취소가 backend command에
전달되지 않는다. Phase 1이 정한 snapshot refresh 기반 lookup과 선택 direct lookup
경계도 반영되지 않았다.

요구 수정:

- `ListSessions(ctx context.Context)`로 변경하고 tmux/cmux runner까지 ctx를 전달한다.
- 필수 `GetSession`을 제거하고 Registry snapshot lookup을 기본 경로로 만든다.
- context cancellation/deadline test가 adapter 작업 종료까지 검증하도록 한다.

### P1 — stale cache session을 live I/O 전에 검증하지 않는다

- 파일: `companion-daemon/internal/mux/registry.go:104-126`

`FindSession`은 cache hit를 즉시 반환한다. 따라서 이미 종료된 session이 stale snapshot에
남아 있으면 refresh 없이 반환되고, WebSocket은 이후 `OpenStream` 실패를 500으로
응답한다. 계획이 요구한 “강제 refresh 성공 후 not-found / refresh 실패 시 unavailable”
구분과 반대다.

요구 수정:

- live lookup은 대상 adapter를 강제 refresh한 뒤 snapshot에서 찾는다.
- refresh 성공 후 사라졌으면 `ErrSessionNotFound`
- refresh 실패면 stale session을 live target으로 반환하지 않고
  `ErrAdapterUnavailable`로 원인을 wrapping
- session 종료 race와 transient refresh failure test를 추가한다.

### P1 — sentinel errors가 실제 경로에 연결되지 않았다

- declarations: `companion-daemon/internal/mux/adapter.go:11-20`
- Registry errors: `companion-daemon/internal/mux/registry.go:51-83`, `:104-126`,
  `:146-200`
- handlers: `companion-daemon/internal/term/pty.go:54-64`, `:109-129`

커밋 설명은 handlers가 `errors.Is`로 조건을 구분한다고 하지만 production handler에는
해당 코드가 없다. Registry도 adapter missing, unsupported, session missing, timeout을
새 sentinel로 wrapping하지 않는다.

요구 수정:

- Registry와 adapter 경계에서 sentinel을 일관되게 wrapping한다.
- `context.Canceled`와 `context.DeadlineExceeded` 원인도 보존한다.
- handler가 invalid/not-found/unsupported/unavailable/timeout을 고정된 HTTP status와
  JSON error 형식으로 변환하도록 한다. Phase 2에서 최종 unsupported schema를 정할
  예정이면 Phase 1에서는 최소 typed propagation을 완료하고 handler mapping은 명시적으로
  defer한다. 현재처럼 구현했다고 주장하면 안 된다.

### P1 — `SessionRef.Validate`가 불완전하고 사용되지 않는다

- 파일: `companion-daemon/internal/mux/id_parser.go:38-53`
- handler: `companion-daemon/internal/term/pty.go:47-55`, `:76-83`

`strings.ContainsAny(value, "\x00\x1f\x7f")`는 NUL, US, DEL 세 문자만 검사한다. newline,
tab 등 나머지 C0 control characters를 허용한다. 또한 handler는 parse 후 `Validate()`를
호출하지 않아 빈 ID와 invalid ID가 adapter로 전달된다.

요구 수정:

- `unicode.IsControl` 또는 명시적 rune 범위로 모든 control character를 거부한다.
- adapter name 허용 문법을 고정한다.
- create/delete/lookup API 경계에서 validation을 적용하고 `ErrInvalidSessionID`를
  보존한다.
- newline, tab, Unicode, colon, empty component, URL encode/decode tests를 추가한다.

### P1 — duplicate registration 계약이 모든 등록 경로에서 유지되지 않는다

- constructor: `companion-daemon/internal/mux/registry.go:25-35`
- composition root: `companion-daemon/cmd/devremote/app.go:99-109`

`Register`는 duplicate를 거부하지만 `NewRegistry(adapters...)`는 map assignment로
중복을 조용히 덮어쓴다. composition root도 `Register` error를 `_ =`로 무시한다.

요구 수정:

- constructor와 runtime registration 모두 동일한 duplicate/name validation을 사용한다.
- composition root는 registration error를 반환한다.
- duplicate constructor input과 invalid/empty adapter name tests를 추가한다.

### P2 — `LocalID`와 deprecated `RawID`가 두 개의 source of truth다

- 파일: `companion-daemon/internal/mux/id_parser.go:11-15`
- 사용처: `companion-daemon/internal/term/pty.go:55`, `:83`

exported field 두 개가 서로 다른 값을 가질 수 있고 `Canonical`은 `LocalID`, 기존
handler와 golden tests는 `RawID`를 사용한다. 이는 “canonical ID의 유일한
parser/formatter” 목표와 충돌한다.

요구 수정:

- 내부 호출부와 tests를 `LocalID`로 전환하고 `RawID`를 제거한다.
- 호환이 정말 필요하면 exported duplicate field 대신 명시적 migration helper를 둔다.

## Test strategy gaps

다음 Phase 1 acceptance tests가 필요하다.

- tmux/cmux create → exactly-once canonical API ID
- list context cancellation/deadline
- empty list와 unavailable error 구분
- fresh not-found, stale-cache unavailable, ended-session race
- duplicate local ID deduplication 정책
- colon/Unicode/URL round-trip
- full control character rejection
- constructor/runtime duplicate adapter rejection

## Host/mobile verification

identity와 WebSocket lookup 동작이 바뀌므로 수정 후 tmux/cmux host integration이
필요하다. ACCEPT 전 최소 smoke:

- colon 포함 tmux local ID 연결
- tmux/cmux 기존 session WebSocket 연결
- 종료된 session 404/not-found
- adapter unavailable 시 unavailable 처리와 reconnect storm 부재

모바일 schema는 additive 변경이 없지만 create response와 connection routing이
변경되므로 tmux/cmux create/connect 경로가 있는 경우 모바일 smoke가 필요하다.

## Required executor actions

1. cmux creator를 local-ID 반환 계약으로 수정하고 double-prefix test를 추가한다.
2. `ListSessions(ctx)`와 snapshot-only lookup 계약을 구현한다.
3. stale live lookup의 not-found/unavailable 의미와 종료 race를 구현·검증한다.
4. sentinel errors를 Registry/adapter 실제 경로에 연결한다.
5. ID 및 adapter-name validation을 실제 API/registration 경계에 적용한다.
6. constructor duplicate와 composition root registration error를 처리한다.
7. `RawID` 이중 source를 제거한다.
8. 전체 Phase 1 acceptance test와 host smoke를 수행한다.

## Next phase permission

**BLOCKED** — Phase 1 ACCEPT 전 Phase 2 진행 금지.
