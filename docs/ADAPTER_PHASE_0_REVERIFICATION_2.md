# Adapter Expansion Phase 0 Reverification — Round 2

- Phase: 0 — 기준선과 회귀 행렬 고정
- Corrective commit: `801008624`
- Previous verification: `6844fb7`
- Verifier decision: **REJECT**
- Verified on: 2026-07-06

## Scope verification

behavior matrix와 API golden test만 수정했으며 production code 변경은 없다. Phase 0
범위는 지켰다.

이전 재검증에서 지적한 “문서가 실제 diff에 포함되지 않음” 문제는 해결됐다. API
golden test도 raw key와 history screen fallback을 추가해 개선됐다. 그러나 기준선
문서에 capability 경계와 실제 command가 다른 P1 오류가 남아 있다.

## Corrective action status

1. capability matrix 전수 교정: **부분 해결**
2. command/timeout 교정: **부분 해결**
3. POST local-ID 및 API schema golden: **해결**
4. colon/Unicode canonical ID: **해결**
5. code/device/effect/contract 상태 분리: **형식 해결, 내용 일부 오류**
6. 자동 검증: **해결**

## Automated verification

- `git diff --check 6844fb7..801008624`: PASS
- canonical ID 및 API golden targeted tests: PASS
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- `npx tsc --noEmit`: PASS

## Findings

### P1 — session capability와 stream capability가 혼합돼 있다

- 문서: `docs/ADAPTER_BEHAVIOR_MATRIX.md:17-22`
- 코드:
  - `companion-daemon/internal/mux/adapter.go:20-48`
  - `companion-daemon/internal/mux/tmux_adapter.go:24-67`

문서는 tmux session이 `InputWriter`와 `Resizer`를 구현한다고 기록한다. 실제
`tmuxSession`은 `OpenStream`, `ReadScreen`, `ReadHistory`, `ProcessInfo`를 구현하고,
write/resize는 `OpenStream`이 반환하는 `TerminalStream`이 제공한다.

현재 Phase 2 계획은 중복 capability를 정리해야 하므로 이 차이를 Phase 0에서 정확히
고정해야 한다.

요구 수정:

- tmux `InputWriter`: session code `NO`; live input effect는 `TerminalStream.Write`로
  별도 기록한다.
- tmux `Resizer`: session `Resizer` code `NO`; stream `TerminalStream.Resize`는 `YES`로
  별도 기록한다.
- cmux도 `CmuxStream.Resize` no-op과 session `Resizer` 미구현을 구분한다.

### P1 — 실제 backend command와 인자가 여전히 다르다

- 문서: `docs/ADAPTER_BEHAVIOR_MATRIX.md:22`, `:36-42`, `:68-71`
- 코드:
  - `companion-daemon/internal/mux/tmux_adapter.go:35-37`
  - `companion-daemon/internal/mux/cmux_adapter.go:388-399`
  - `companion-daemon/internal/mux/cmux_adapter.go:453-485`

남은 불일치:

- tmux process: 문서 `list-panes`, 코드 `display-message -p -t ...`
- cmux screen/history: 문서 `--name`, 코드 `--surface`; history는 `--scrollback`도 사용
- cmux create: 문서 `surface create --name`, 코드
  `new-surface --type terminal --workspace`
- cmux terminate: 문서 `surface delete --name`, 코드
  `close-surface --surface`

요구 수정:

- 기준 커밋에서 실행되는 exact subcommand와 주요 target 인자를 기록한다.

### P1 — cmux Ctrl+C의 code와 effect 구분이 잘못됐다

- 문서: `docs/ADAPTER_BEHAVIOR_MATRIX.md:44`, `:61`
- 코드: `companion-daemon/internal/mux/cmux_adapter.go:401-405`,
  `:448-450`

문서는 code `NO`로 기록하지만 `WriteInput("\x03")`은
`WriteKey("ctrl-c")` → `send-key --surface ... ctrl-c`를 실행한다. 실기기에서 효과가
없었다면 `Code=YES`, `Effect=NOT SUPPORTED`, `Verified=FAIL/미지원 확인`으로 기록해야
한다.

### P1 — Registry ordering은 adapter별 alphabetic/numeric이 아니다

- 문서: `docs/ADAPTER_BEHAVIOR_MATRIX.md:53`
- 코드: `companion-daemon/internal/mux/registry.go:213-217`

Registry는 모든 canonical ID를 단순 문자열 비교한다. 따라서 adapter별로 각각
alphabetic/numeric 정렬하는 것이 아니며 `surface:10`은 `surface:9`보다 먼저 올 수 있다.

요구 수정:

- `canonical ID lexical ascending`으로 기록한다.

### P2 — history success golden이 event schema 전체를 고정하지 않는다

- 테스트: `companion-daemon/internal/term/api_golden_test.go:143-166`
- API DTO: `companion-daemon/internal/models/events.go`

screen fallback test가 검사하는 key는 `id`, `type`, `summary`, `detail`뿐이다.
응답에 포함되는 `session`, `agent`, `timestamp`, `toolCallId`가 rename/누락돼도 통과한다.

요구 수정:

- history success fixture에서 `AgentEvent`의 전체 JSON key와 주요 값 타입도 검사한다.

## Host/mobile verification

production code 변경이 없으므로 새 host integration 및 모바일 smoke는 요구하지 않는다.

## Required executor actions

1. session capability와 returned stream capability를 분리해 행렬을 수정한다.
2. tmux/cmux command 및 target 인자를 실제 코드와 일치시킨다.
3. cmux Ctrl+C를 code path와 verified effect로 분리한다.
4. Registry ordering을 canonical ID lexical sort로 수정한다.
5. history success golden에 `AgentEvent` 전체 schema 검사를 추가한다.
6. targeted test, vet, 전체 test, race test, TypeScript 검사를 재실행한다.

## Next phase permission

**BLOCKED** — Phase 0 ACCEPT 전 Phase 1 진행 금지.
