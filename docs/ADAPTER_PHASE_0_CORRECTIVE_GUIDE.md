# Adapter Expansion Phase 0 Verification

- Phase: 0 — 기준선과 회귀 행렬 고정
- Executor commits: `05924e5`, `a5a74e048`
- Verifier decision: **REJECT**
- Verified on: 2026-07-06

## Scope verification

제출 범위는 golden test와 기준선 문서뿐이며 production code 변경은 없다. Phase 0의
구현 금지 범위는 지켰다.

그러나 기준선 문서가 현재 구현과 다른 capability 및 command를 다수 기록하고, API
schema는 실행 가능한 golden test로 고정하지 않았다. 이 상태로는 이후 Phase의 회귀
판정 기준으로 사용할 수 없다.

## Contract verification

canonical ID parser와 legacy cmux migration의 기존 동작은 새 targeted test에서
통과한다. `<adapter>:<local-id>`를 첫 번째 콜론에서 분리하는 현재 동작도 보존된다.

반면 behavior matrix의 cmux capability, command, timeout과 create API 응답은 코드와
일치하지 않는다. 아래 P1 finding을 모두 수정해야 한다.

## Backward compatibility

production code와 외부 schema를 변경하지 않았으므로 직접적인 호환성 회귀는 없다.
다만 잘못된 API 예제를 golden baseline으로 승인하면 Phase 1에서 현재 응답을 회귀로
오판할 수 있다.

## Automated verification

- `git diff --check a6d39ba..a5a74e0`: PASS
- `go test ./internal/mux -run 'TestCanonicalID_' -count=1 -v`: PASS
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- `npx tsc --noEmit`: PASS

최초 sandbox 실행의 전체 test는 `httptest` localhost bind가 차단되어 실패했다. 동일
vet/test/race 명령을 host 권한으로 재실행해 모두 통과했다.

## Host/mobile verification

production 동작 변경이 없으므로 새로운 host integration 및 모바일 smoke는 요구하지
않는다. 기존 실기기 결과를 문서화할 때는 코드 capability와 실기기 관찰을 구분하고,
관찰 일시·환경·명령 또는 체크리스트 근거를 남겨야 한다.

## Findings

### P1 — cmux capability 기준선이 현재 구현과 반대다

- 문서: `docs/ADAPTER_BEHAVIOR_MATRIX.md:18-25`, `:50-54`
- 코드:
  - `companion-daemon/internal/mux/cmux_adapter.go:360-362`
  - `companion-daemon/internal/mux/cmux_adapter.go:401-450`
  - `companion-daemon/internal/mux/cmux_adapter.go:453-485`
  - `companion-daemon/internal/mux/cmux_adapter.go:704-727`

문서는 cmux resize를 PASS로 기록하지만 구현은 성공을 반환하는 no-op이다. Ctrl+C,
create, terminate, batch process는 미지원 또는 실패로 기록했지만 각각 `WriteKey`,
`SessionCreator`, `SessionTerminator`, `ProcessSnapshotProvider` 구현이 존재한다.
tmux에는 batch provider 구현이 없는데 PASS로 기록됐다.

재현:

```bash
rg -n 'func .*Resize|WriteInput|CreateSession|TerminateSession|ProcessSnapshot' \
  companion-daemon/internal/mux
```

기대 동작:

- interface 구현 여부, 실제 효과, 실기기 검증 결과를 별도 열로 구분한다.
- cmux resize는 `NO-OP / 효과 없음`으로 기록한다.
- cmux Ctrl+C/create/terminate/batch process와 tmux batch process를 현재 코드 기준으로
  수정한다.

### P1 — cmux command 및 timeout 기준선이 실제 실행 경로와 다르다

- 문서: `docs/ADAPTER_BEHAVIOR_MATRIX.md:11`, `:20-23`, `:58-61`, `:70`
- 코드:
  - `companion-daemon/internal/mux/cmux_adapter.go:155-165`
  - `companion-daemon/internal/mux/cmux_adapter.go:388-399`
  - `companion-daemon/internal/mux/cmux_adapter.go:646-650`

문서는 discovery를 `cmux top`, screen/history를 `surface get`, process를 `surface info`,
timeout을 명령당 5초로 기록한다. 현재 코드는 discovery에 `tree --all`, 화면과 history에
`read-screen`, process에 `top --all --processes --format tsv`를 사용한다. discovery는
3초 timeout이고 stream polling은 별도 1.5초 timeout을 사용하므로 “5s per command”도
현재 계약이 아니다.

기대 동작:

- 기준 커밋의 실제 command와 timeout을 기록한다.
- 고정 계약이 아닌 숫자는 accidental behavior로 명시한다.

### P1 — POST API golden 응답이 실제 handler 응답과 다르고 자동 고정되지 않는다

- 문서: `docs/ADAPTER_BEHAVIOR_MATRIX.md:143-151`
- 코드:
  - `companion-daemon/internal/term/pty.go:47-63`
  - `companion-daemon/internal/mux/tmux_adapter.go:77-84`

요청 ID `tmux:test`는 handler에서 local ID `test`로 분리되고 tmux creator도 `test`를
반환한다. 따라서 현재 응답은 문서의 `{"id":"tmux:test"}`가 아니라 `{"id":"test"}`다.
또한 새 golden test는 ID parser만 검사하고 GET/POST/DELETE JSON schema를 검사하지
않으므로 “외부 API schema를 golden fixture로 고정”했다는 합격 기준을 충족하지 않는다.

재현:

```text
ParseSessionID("tmux:test").RawID == "test"
tmuxAdapter.CreateSession(...).return == opts.Name
HandleSessionCRUD response id == createdID
```

기대 동작:

- 현재 응답을 accidental behavior로 정확히 기록한다.
- handler에 fake Registry/adapter를 주입하는 API golden test로 status, content type,
  JSON 필드, canonical/local create ID의 현재 동작을 실행 가능하게 고정한다.
- Phase 1에서 create ID 계약을 변경할 예정임을 기준선과 명확히 구분한다.

### P2 — colon/Unicode와 API schema fixture 범위가 Phase 0 행렬을 충분히 고정하지 않는다

- 테스트: `companion-daemon/internal/mux/adapter_golden_test.go:65-95`
- 계획: `docs/ADAPTER_EXPANSION_PLAN.md:122-134`

`tmux:aider`, `tmux:cmux-38`은 canonical ID 기준 local ID가 각각 `aider`,
`cmux-38`이므로 local ID에 콜론이 있는 사례가 아니다. 실제 colon 사례는
`tmux:session:with:colons` 하나뿐이고 Unicode 사례가 없다. 또한 문서에 기록한 정렬,
stale snapshot, API schema는 새 golden test에서 검증하지 않는다.

기대 동작:

- local ID가 `tmux:aider`인 canonical ID `tmux:tmux:aider`와 Unicode local ID를
  명시적으로 추가한다.
- API schema, deterministic ordering, stale-on-error는 기존 테스트를 golden suite에서
  참조하거나 Phase 0 전용 test로 고정한다.

## Required executor actions

1. behavior matrix의 cmux/tmux capability를 현재 interface 구현과 실제 효과 기준으로
   전수 교정한다.
2. discovery/screen/history/process command와 timeout을 현재 코드에 맞게 수정한다.
3. API POST 예제를 현재 local-ID 응답으로 수정하고, GET/POST/DELETE schema를 실행 가능한
   golden handler test로 고정한다.
4. `tmux:tmux:aider` 및 Unicode canonical ID test를 추가한다.
5. 코드 지원, 실기기 검증, accidental behavior, guaranteed contract를 서로 다른 상태로
   표현한다.
6. 수정 커밋에서 targeted test, vet, 전체 test, race test, TypeScript 검사를 다시
   실행한다.

## Deferred items

- API create 결과를 canonical ID로 통일하는 구현은 Phase 1 범위다. Phase 0에서는 현재
  local-ID 응답을 정확히 고정하고 planned change로 표시한다.
- cmux resize를 실제 지원하거나 unsupported로 바꾸는 구현은 Phase 2 capability 정리
  범위다.

## Next phase permission

**BLOCKED** — 위 P1 수정 후 Phase 0 재검증에서 ACCEPT를 받기 전 Phase 1 진행 금지.
