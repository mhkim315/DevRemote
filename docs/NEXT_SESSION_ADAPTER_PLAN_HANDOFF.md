# Next Session Handoff — Adapter Expansion Plan Verification

## 역할 분담 — 반드시 유지

이 작업에는 두 역할이 존재한다.

- **실행 에이전트**: 승인된 계획에 따라 각 Phase를 구현하고 커밋을 제출한다.
- **검증 에이전트**: 현재 문서를 읽는 다음 세션의 에이전트다. 구현을 대신 진행하지
  않고 실행 에이전트의 결과를 독립적으로 검증한다.

다음 세션의 너는 **검증 에이전트**다. 사용자가 별도로 역할 변경을 지시하지 않는 한
실행 에이전트 역할로 전환하면 안 된다.

검증 에이전트의 책임:

1. 실행 에이전트가 제시한 커밋과 그 부모 커밋의 diff를 모두 읽는다.
2. 계획의 해당 Phase 범위, 금지 사항, 합격 기준을 항목별로 대조한다.
3. 구현 주장이나 자체 테스트 결과를 그대로 신뢰하지 않고 직접 재현한다.
4. `go vet`, 전체 test, race test와 Phase별 targeted test를 직접 실행한다.
5. 실제 tmux/cmux 동작에 영향이 있으면 host integration과 모바일 smoke 필요 여부를
   명시한다.
6. 하드코딩, silent fallback, 오류 은폐, stale cache 파괴, canonical ID 회귀,
   reconnect storm 가능성을 꼼꼼히 검색한다.
7. 발견 사항에는 심각도, 코드 파일/line, 재현 방법, 기대 동작, 수정 방향을 적는다.
8. 검증 결과와 실행 에이전트에게 전달할 구체적인 수정 지시를 **문서 코멘트로 남긴다**.
9. 검증 문서도 커밋하고 원격 브랜치에 푸시해 실행 에이전트가 확인할 수 있게 한다.
10. REJECT이면 다음 Phase 진행을 허용하지 않는다. ACCEPT일 때만 다음 Phase를 안내한다.

검증 에이전트는 단순히 테스트 통과 여부만 확인하지 않는다. 구조가 확장 목표에 실제로
가까워졌는지, 세 번째 adapter 추가 시 기존 파일을 수정하게 만드는 새 결합이 생기지
않았는지까지 판단한다.

## 요청

새 대화에서는 구현하지 말고 `docs/ADAPTER_EXPANSION_PLAN.md`를 먼저 객관적으로
검증한다. 사용자는 계획을 세운 뒤 계획 자체를 검증하면서 보완하려 한다.

## 현재 상태

- 브랜치: `feature/phase10-multi-adapter`
- 계획 작성 전 동작 기준: `dc026d5`
- 최근 실기기 결과:
  - cmux 3개, tmux 9개가 모바일에 정상 표시
  - 양쪽 모두 모바일 입력과 Enter 동작
  - tmux Ctrl+C, 색상, activity/history 동작
  - 카드 순서 안정화
  - 죽은 WebSocket reconnect loop 해결
  - `tmux:aider`처럼 콜론이 포함된 tmux 이름도 내부 session ID target으로 정상 접속
- 실행 데몬: macOS LaunchAgent, local `127.0.0.1:9171`
- 현재 smoke 용도는 `--insecure-local-only`; production 보안 판정과 혼동하지 말 것
- 최신 tmux 누락 원인은 탭 기반 list parser였고 고정 delimiter 및 오류 보존으로 해결됨

## 반드시 먼저 읽을 파일

1. `docs/ADAPTER_EXPANSION_PLAN.md`
2. `docs/ARCHITECTURE.md`
3. `companion-daemon/internal/mux/adapter.go`
4. `companion-daemon/internal/mux/registry.go`
5. `companion-daemon/internal/mux/tmux_adapter.go`
6. `companion-daemon/internal/mux/cmux_adapter.go`
7. `companion-daemon/internal/term/pty.go`
8. `companion-daemon/cmd/devremote/app.go`
9. `mobile/src/components/AgentCard.tsx`

기존 `docs/GLOBAL_STATE_REFACTOR_PLAN.md`의 Phase 번호와 새 계획의 Phase 번호는 별도다.
기존 전역 상태 제거 계획은 완료됐고, 새 문서는 adapter 확장 기반 전용 계획이다.

## 계획 검증 질문

다음 질문에 코드 근거와 함께 답한다.

1. 계획이 실제 남은 결합을 빠짐없이 다루는가?
2. 필수 `Adapter` 계약이 너무 크거나 너무 작은가?
3. `GetSession`/`LookupSession`이 필요한가, list snapshot lookup으로 충분한가?
4. capability interface 중 중복되거나 잘못된 경계가 있는가?
5. canonical ID를 바꾸지 않고 local ID의 콜론/Unicode를 안전하게 지원하는가?
6. cmux의 Registry 역참조를 제거하는 방향이 실제 reconnect 요구와 양립하는가?
7. fixture adapter가 확장성을 증명하기에 충분한가?
8. 실제 세 번째 backend 전에 더 필요한 contract test가 있는가?
9. API capability metadata 추가가 하위 호환 가능한가?
10. Phase 순서가 회귀 위험을 최소화하는가?

## 검증 방식

- 문서 주장만 읽지 말고 `rg`로 tmux/cmux 하드코딩을 다시 조사한다.
- 각 finding에 파일과 line을 제시한다.
- 우선순위를 `P0/P1/P2`로 분류한다.
- 계획 수정이 필요하면 구현 코드가 아니라 계획 문서만 수정한다.
- 사소한 naming 선호로 REJECT하지 않는다. 확장성, 정확성, 회귀 위험에 집중한다.

권장 판정 형식:

```text
Plan decision: ACCEPT | ACCEPT WITH CHANGES | REJECT

P0:
P1:
P2:

Phase ordering review:
Contract review:
Test strategy review:
Compatibility review:
Required document edits:
```

실행 커밋 검증 시에는 별도 검증 코멘트를 아래 형식으로 작성한다.

```text
Phase:
Executor commit:
Verifier decision: ACCEPT | REJECT

Scope verification:
Contract verification:
Backward compatibility:
Automated verification:
Host/mobile verification:

Findings:
- [P0|P1|P2] file:line — 문제, 근거, 재현, 요구 수정

Required executor actions:
1. ...

Deferred items:
- ...

Next phase permission: ALLOWED | BLOCKED
```

검증 코멘트 권장 파일명:

```text
docs/ADAPTER_PHASE_<N>_VERIFICATION.md
docs/ADAPTER_PHASE_<N>_CORRECTIVE_GUIDE.md
```

## 주의할 현재 코드 사실

- `term/pty.go`에는 tmux fallback과 tmux 이름 기반 initial snapshot 분기가 남아 있다.
- `mux/session.go`, `mux/tracker.go`, `term/gemini_resolver.go`에 tmux 직접 실행이 남아 있다.
- 모바일 `AgentCard`가 `tmux|cmux` 정규식으로 표시 ID를 제거한다.
- `Adapter.ListSessions()`에는 context가 없다.
- tmux adapter는 external display name과 internal `$session_id` target을 분리해야 한다.
  이 수정은 되돌리면 안 된다.
- Registry는 adapter 오류 시 stale snapshot을 보존한다. 이 성질도 유지해야 한다.
- cmux는 GUI socket과 reconnect 특성이 있으므로 단순 generic polling으로 치환하면 안 된다.

## 새 대화 첫 작업

1. 원격 최신 커밋과 clean worktree 확인
2. 위 필수 파일 재검토
3. 계획에 대한 판정 보고
4. 필요한 경우 `ADAPTER_EXPANSION_PLAN.md`만 보완
5. 사용자가 명시적으로 승인하기 전 Phase 0 구현 금지
