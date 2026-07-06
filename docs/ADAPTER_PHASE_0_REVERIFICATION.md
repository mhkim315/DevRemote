# Adapter Expansion Phase 0 Reverification

- Phase: 0 — 기준선과 회귀 행렬 고정
- Corrective commit: `b2cb23d73`
- Previous verification: `3927c2a`
- Verifier decision: **REJECT**
- Verified on: 2026-07-06

## Scope verification

`b2cb23d73`은 canonical ID golden test를 보강하고 API handler golden test를 추가했다.
production code 변경은 없어 Phase 0 범위는 지켰다.

그러나 커밋 메시지의 “capability matrix corrected” 주장과 달리
`docs/ADAPTER_BEHAVIOR_MATRIX.md`는 변경되지 않았다.

```text
git diff --name-status 3927c2a..b2cb23d73
M companion-daemon/internal/mux/adapter_golden_test.go
A companion-daemon/internal/term/api_golden_test.go
```

## Corrective action status

1. capability matrix 전수 교정: **미해결**
2. command/timeout 교정: **미해결**
3. POST local-ID 기준선 및 API handler test: **부분 해결**
4. colon/Unicode canonical ID test: **해결**
5. code/device/effect/contract 상태 분리: **미해결**
6. 자동 검증 재실행: **해결**

## Automated verification

- `git diff --check 3927c2a..b2cb23d73`: PASS
- canonical ID 및 API golden targeted tests: PASS
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- `npx tsc --noEmit`: PASS

## Findings

### P1 — behavior matrix 사실 오류가 수정되지 않았다

- 파일: `docs/ADAPTER_BEHAVIOR_MATRIX.md:11-25`, `:50-70`
- 이전 corrective guide:
  `docs/ADAPTER_PHASE_0_CORRECTIVE_GUIDE.md`

문서는 여전히 다음과 같이 현재 코드와 다르다.

- cmux discovery를 `cmux top`으로 기록하지만 코드는 `tree --all`을 사용한다.
- cmux resize를 PASS로 기록하지만 코드는 성공을 반환하는 no-op이다.
- cmux create/terminate/batch process를 미지원으로 기록하지만 해당 capability 구현이 있다.
- tmux batch process를 PASS로 기록하지만 `ProcessSnapshotProvider` 구현이 없다.
- screen/history/process command가 현재 `read-screen` 및 `top --all --processes` 경로와
  다르다.
- 모든 cmux command timeout을 5초로 기록하지만 discovery는 3초, stream poll은
  1.5초다.

재현:

```bash
git diff --name-status 3927c2a..b2cb23d73
rg -n 'ListSessions|Resize|CreateSession|TerminateSession|ProcessSnapshot|read-screen' \
  companion-daemon/internal/mux
```

요구 수정:

- `ADAPTER_BEHAVIOR_MATRIX.md`를 실제 코드에 맞게 수정한다.
- capability interface 존재, 실제 효과, 실기기 검증, 보장 계약을 별도 열 또는 명확한
  상태값으로 구분한다.
- 커밋 전 실제 diff에 문서 변경이 포함됐는지 확인한다.

### P2 — API golden tests가 전체 schema를 고정하지 않는다

- 파일: `companion-daemon/internal/term/api_golden_test.go:15-49`

GET test는 응답을 `[]SessionTelemetry`로 decode한 뒤 `id`와 `adapter`만 검사한다.
JSON field가 누락되거나 이름이 바뀌어도 Go zero value로 decode되어 통과할 수 있다.
POST/DELETE test도 status와 일부 값만 검사한다.

요구 수정:

- 최소한 GET session object의 현재 JSON key 집합과 주요 값 타입을 검사한다.
- `stale`, `lastSuccessAt`, `lastError`처럼 `omitempty`인 조건부 필드는 포함/미포함
  조건을 명시한다.
- history 404 test와 문서의 성공 history fixture를 구분하고, 성공 schema도 실행 가능한
  test로 고정한다.

## Host/mobile verification

production code 변경이 없으므로 새 host integration 및 모바일 smoke는 요구하지 않는다.

## Required executor actions

1. 이전 corrective guide의 matrix 및 command/timeout P1을 실제 문서 diff로 반영한다.
2. code support, device verification, effective behavior, contract를 구분한다.
3. GET/history API golden test가 JSON schema 자체를 검사하도록 보강한다.
4. targeted test, vet, 전체 test, race test, TypeScript 검사를 재실행한다.

## Next phase permission

**BLOCKED** — Phase 0 ACCEPT 전 Phase 1 진행 금지.
