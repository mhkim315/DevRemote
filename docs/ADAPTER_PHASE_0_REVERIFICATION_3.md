# Adapter Expansion Phase 0 Reverification — Round 3

- Phase: 0 — 기준선과 회귀 행렬 고정
- Corrective commits: `629174f`, `a7fd00f95`
- Previous verification: `4e8b6e8`
- Verifier decision: **REJECT**
- Verified on: 2026-07-06

## Scope verification

수정은 behavior matrix와 API golden test에 한정됐고 production code 변경은 없다.
중복 위치에 생성됐던 문서도 제거됐다.

직전 검증의 stream/session write·resize, exact cmux command, Ctrl+C code/effect,
canonical lexical ordering, 전체 `AgentEvent` key 요구는 반영됐다. 그러나 adapter-level
capability가 session 표에 포함됐고, 문서 축약 과정에서 Phase 0 필수 회귀 차원이
삭제됐다.

## Automated verification

- `git diff --check 4e8b6e8..a7fd00f95`: PASS
- canonical ID 및 API golden targeted tests: PASS
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- `npx tsc --noEmit`: PASS

## Findings

### P1 — adapter capability가 session capability 표에 포함돼 있다

- 문서: `docs/ADAPTER_BEHAVIOR_MATRIX.md:10-43`
- 코드:
  - `companion-daemon/internal/mux/tmux_adapter.go:77-92`
  - `companion-daemon/internal/mux/cmux_adapter.go:453-485`
  - `companion-daemon/internal/mux/cmux_adapter.go:704-727`

문서는 “session struct가 직접 구현한 capability” 표라고 정의하지만 다음 항목은 session이
아니라 adapter receiver에 구현돼 있다.

- tmux/cmux `SessionCreator`
- tmux/cmux `SessionTerminator`
- cmux `ProcessSnapshotProvider`

이는 Phase 2에서 create/delete/process capability를 어느 객체에서 검사해야 하는지
결정하는 기준선이므로 단순 표기 문제가 아니다.

요구 수정:

- `Adapter capabilities`, `Session capabilities`, `TerminalStream capabilities`를 세
  표로 분리한다.
- discovery `ListSessions`도 adapter 필수 계약 표에 포함한다.

### P1 — Phase 0 필수 회귀 행렬 차원이 재작성 중 삭제됐다

- 문서: `docs/ADAPTER_BEHAVIOR_MATRIX.md`
- 계획: `docs/ADAPTER_EXPANSION_PLAN.md:120-134`

Phase 0 계획은 discovery, history, initial screen, live output, text, Enter, Ctrl+C,
resize, disconnect/reconnect, transient adapter failure를 tmux/cmux에 동일하게 적용한
행렬을 요구한다.

현재 문서에는 capability와 일부 command는 있으나 다음 동작 비교가 사라졌다.

- WS 연결 직후 initial screen 경로
- live output 방식과 ANSI 품질
- printable text, Enter CR/LF/CRLF, arrow/Esc, Unicode/CJK
- client disconnect 후 새 stream 및 daemon restart 후 rediscovery
- stream read error와 unsupported input의 결과

요구 수정:

- 이전 문서의 functional dimensions를 삭제하지 말고, 정확한 code/effect/verified/status
  구분을 적용해 복원한다.
- tmux와 cmux에 동일한 행을 사용하고 미지원은 명시적으로 표시한다.

### P1 — tmux discovery command가 parser 회귀 기준을 고정하지 않는다

- 문서: `docs/ADAPTER_BEHAVIOR_MATRIX.md:67-72`
- 코드: `companion-daemon/internal/mux/tmux_adapter.go:12`, `:103-126`

문서는 `tmux ls`로 축약한다. 실제 discovery는
`tmux list-sessions -F "#{session_id}::POKIT::#{session_name}"`이며, 고정 delimiter와
internal session target 분리는 최근 콜론 포함 이름 회귀를 막는 핵심 기준선이다.

요구 수정:

- 실제 `list-sessions -F` format과 `$session_id` target / display name 분리를 기록한다.

### P2 — history golden은 key 존재만 검사하고 JSON value type을 검증하지 않는다

- 테스트: `companion-daemon/internal/term/api_golden_test.go:143-166`

전체 key는 검사하지만 raw string 검색만 사용한다. 응답을 `[]models.AgentEvent` 또는
명시적 golden DTO로 decode하지 않아 field value type 변경을 고정하지 못한다.

요구 수정:

- key 검사 후 typed decode를 수행하고 session/type/detail/timestamp의 주요 값을
  검증한다.

## Resolved from round 2

- tmux session write/resize와 returned stream capability 분리
- cmux no-op resize 명시
- cmux exact `--surface`, `new-surface`, `close-surface` command 반영
- cmux Ctrl+C code path와 실기기 미지원 효과 분리
- canonical ID lexical ordering 반영
- `AgentEvent` 전체 JSON key 검사 추가

## Host/mobile verification

production code 변경이 없으므로 새 host integration 및 모바일 smoke는 요구하지 않는다.

## Required executor actions

1. adapter/session/stream capability 표를 분리한다.
2. 계획이 요구하는 functional behavior dimensions를 tmux/cmux 공통 행으로 복원한다.
3. tmux discovery의 exact format과 internal target/display name 분리를 기록한다.
4. history success golden을 typed decode하고 주요 값을 검사한다.
5. targeted test, vet, 전체 test, race test, TypeScript 검사를 재실행한다.

## Next phase permission

**BLOCKED** — Phase 0 ACCEPT 전 Phase 1 진행 금지.
