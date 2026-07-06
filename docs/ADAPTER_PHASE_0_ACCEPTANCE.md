# Adapter Expansion Phase 0 Acceptance

- Phase: 0 — 기준선과 회귀 행렬 고정
- Final corrective commit: `d6166fd8a9553d804d8e4f5193750e4ac83fd40a`
- Previous verification: `bba32fc`
- Verifier decision: **ACCEPT**
- Verified on: 2026-07-06

## Scope verification

Phase 0 제출 전체는 behavior baseline 문서와 golden tests에 한정됐다. production code
변경은 없으며 다음 Phase 구현이 섞이지 않았다.

최종 수정은 직전 재검증의 세 잔여 항목만 처리했다.

- history `AgentEvent` typed decode를 정상 경로에서 항상 실행
- client disconnect와 fatal stream/input WS 1011 경로 분리
- cmux 500ms를 latency 상한이 아닌 poll interval로 기록

## Contract verification

behavior matrix가 adapter/session/stream capability 소유권을 분리하고, tmux/cmux에
동일한 functional dimensions를 적용한다.

다음 기준선이 코드와 일치한다.

- canonical ID `<adapter>:<local-id>` 및 legacy cmux migration
- colon/Unicode local ID
- tmux fixed discovery delimiter와 internal `$session_id` target
- Registry canonical lexical ordering과 stale snapshot 보존
- initial screen, live output, input, resize, lifecycle, reconnect, error behavior
- 현재 POST local-ID 응답과 API/history JSON schema
- code path, 실기기 effect, contract, accidental behavior 구분

## Backward compatibility

production behavior와 외부 API는 변경되지 않았다. 기존 동작을 golden baseline으로
기록하고 실행 가능한 tests로 고정했으므로 Phase 1 변경 전 비교 기준이 확보됐다.

## Automated verification

- `git diff --check bba32fc..d6166fd8a9553d804d8e4f5193750e4ac83fd40a`: PASS
- `go test ./internal/mux ./internal/term -run 'TestCanonicalID_|TestAPIGolden_' -count=1 -v`: PASS
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- `npx tsc --noEmit`: PASS

## Host/mobile verification

production code 변경이 없으므로 추가 host integration 및 모바일 smoke는 요구하지 않는다.
문서의 실기기 항목은 Phase 0 이전 2026-07-06 tmux/cmux smoke 결과를 기준으로 한다.

## Findings

- P0: 없음
- P1: 없음
- P2: 없음

## Deferred items

- POST create 결과 canonicalization: Phase 1
- adapter/session/stream capability 중복 정리: Phase 2
- cmux no-op resize 처리: Phase 2

## Next phase permission

**ALLOWED** — Phase 1 구현을 시작할 수 있다.
