# Adapter Expansion Phase 0 Reverification — Round 4

- Phase: 0 — 기준선과 회귀 행렬 고정
- Corrective commit: `24d573cf7`
- Previous verification: `0ec66c3`
- Verifier decision: **REJECT**
- Verified on: 2026-07-06

## Scope and corrective status

production code 변경 없이 behavior matrix와 golden test만 수정했다. 다음 직전 요구사항은
반영됐다.

- adapter/session/stream capability 표 분리
- functional behavior dimensions 복원
- tmux `list-sessions -F` 고정 delimiter 및 internal target/title 기록
- `AgentEvent` 전체 key 검사 추가

다만 typed decode가 정상 경로에서 실행되지 않는 test control-flow 오류와 WebSocket
disconnect 기준선 오류가 남아 있다.

## Automated verification

- `git diff --check 0ec66c3..24d573cf7`: PASS
- canonical ID 및 API golden targeted tests: PASS
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- `npx tsc --noEmit`: PASS

테스트 통과는 아래 unreachable typed assertion을 검출하지 못하므로 ACCEPT 근거가 되지
않는다.

## Findings

### P1 — history typed decode가 정상 응답에서 실행되지 않는다

- 파일: `companion-daemon/internal/term/api_golden_test.go:159-178`

typed decode와 모든 value assertion이 다음 조건문 내부에 들어 있다.

```go
if !strings.Contains(raw, "GOLDEN_SCREEN") {
    // typed decode and assertions
    t.Errorf("history response missing screen content")
}
```

정상 fixture에는 `GOLDEN_SCREEN`이 있으므로 조건이 false이고 typed decode가 전혀
실행되지 않는다. 반대로 content가 없을 때만 decode한 뒤 결국 error를 기록한다.

요구 수정:

- missing-content assertion 직후 조건문을 닫는다.
- typed decode와 event assertions를 조건문 밖에서 항상 실행한다.
- 이 test가 잘못된 field type에서 실제 실패함을 확인한다.

### P1 — client disconnect의 WS close 기준선이 실제 handler와 다르다

- 문서: `docs/ADAPTER_BEHAVIOR_MATRIX.md:90-94`
- 코드: `companion-daemon/internal/term/pty.go:236-264`

문서는 client disconnect 시 양 adapter 모두 `stream.Close, WS 1011 close`라고 기록한다.
실제 handler는 client `ReadMessage` 오류로 loop를 빠져나오고 stream을 닫은 뒤
`stopWriter()`로 writer를 종료한다. 1011은 `fatalErr`에 전달되는 stream/input 오류
경로에서만 전송된다.

요구 수정:

- client disconnect: `stream.Close + writer shutdown`, 별도 1011 없음
- stream/input fatal error: WS 1011

두 동작을 별도 행으로 기록한다.

### P2 — cmux 500ms는 latency 상한이 아니라 poll interval이다

- 문서: `docs/ADAPTER_BEHAVIOR_MATRIX.md:75-79`
- 코드: `companion-daemon/internal/mux/cmux_adapter.go:290-342`

poll ticker가 500ms여도 serial runner 대기와 `read-screen` 실행 시간이 추가되므로 실제
latency가 항상 500ms 이하라고 보장할 수 없다.

요구 수정:

- `Latency ≤500ms`를 `poll interval 500ms + command/queue time`으로 수정한다.

## Host/mobile verification

production code 변경이 없어 새 host integration 및 모바일 smoke는 요구하지 않는다.

## Required executor actions

1. history typed decode를 정상 경로에서 항상 실행하도록 test block을 이동한다.
2. client disconnect와 fatal stream/input error의 close 동작을 분리해 기록한다.
3. cmux latency를 poll interval과 command/queue 시간을 포함하도록 수정한다.
4. targeted test, vet, 전체 test, race test, TypeScript 검사를 재실행한다.

## Next phase permission

**BLOCKED** — Phase 0 ACCEPT 전 Phase 1 진행 금지.
