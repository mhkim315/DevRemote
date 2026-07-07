# Next Session Handoff — Agent Adapter Layer

## 역할 분담 — 반드시 유지

이 작업에는 두 역할이 존재한다.

- **실행 에이전트**: 승인된 계획에 따라 각 Phase를 수행하고 커밋을 제출한다.
- **검증 에이전트**: 실행 에이전트의 결과를 독립적으로 검증한다.

다음 세션의 역할은 사용자의 지시에 따라 달라진다. 사용자가 "실행에이전트"라고 하면
이 문서를 implementation handoff로 사용한다. 사용자가 "검증에이전트"라고 하면 구현하지
말고 계획 또는 실행 커밋을 검증한다.

현재 사용자의 의도는 **실행에이전트에게 Agent Adapter Layer의 남은 후속 작업을 넘기는 것**이다.

## 현재 상태

- 브랜치: `feature/phase10-multi-adapter`
- Terminal Adapter Layer acceptance: Phase 7 accepted
- Phase 7 acceptance verifier commit: `b163c1b`
- Agent Adapter Layer:
  - Phase A0 accepted
  - Phase A1 accepted
  - Phase A2 accepted
  - Phase A3 accepted
  - Phase A4 accepted
  - Phase A5a scoped production detection bridge accepted
  - Phase A5 Agent Backend Foundation accepted
  - Phase A6 Codex Backend Slice accepted
  - Phase A7 Agent Agnostic UX / Product Gate accepted
  - Phase A8-prep Antigravity actual-log fixture collection accepted
- A5a scoped acceptance reviewed commit: `c15659568`
- A5a verifier document: `docs/AGENT_PHASE_A5_SCOPED_ACCEPTANCE.md`
- A6 backend acceptance verifier commit: `1fc12bf`
- A6 backend acceptance document: `docs/AGENT_PHASE_A6_BACKEND_ACCEPTANCE.md`
- A7 product gate acceptance verifier commit: `108525a`
- A7 product gate acceptance document: `docs/AGENT_PHASE_A7_ACCEPTANCE.md`
- A8-prep executor commit: `de3197dbc`
- A8-prep scope document: `docs/AGENT_PHASE_A8_PREP_SCOPE.md`
- A8 executor onboarding document: `docs/AGENT_PHASE_A8_EXECUTOR_ONBOARDING.md`
- 핵심 결론:
  - tmux/cmux production adapter를 깨지 않고 LocalPTY까지 붙였다.
  - mobile은 terminal backend 이름 목록이 아니라 capability로 동작한다.
  - Phase 7은 운영/UX/진단 정리까지 ACCEPT됐다.
  - Claude process evidence가 production detector를 거쳐 `/api/sessions` agent fields로 흐르는 것은 증명됐다.
  - Claude backend foundation과 Codex backend slice가 accepted 됐다.
  - A6 ACCEPT는 Backend Slice Acceptance이며 Product Completion을 의미하지 않는다.
  - A7 ACCEPT는 Agent Agnostic UX/Product Gate 통과이며 release-complete를 의미하지 않는다.
  - A8-prep는 Antigravity 실제 로그 fixture 수집으로 ACCEPT됐지만, A8 backend slice 완료는 아니다.
  - 다음 핵심은 Antigravity production detector/resolver/parser/telemetry backend slice다.

Phase 1~7의 의미:

```text
Terminal Adapter Layer proved:
tmux / cmux / LocalPTY can flow through API, WebSocket, telemetry, and mobile
without hardcoding backend-specific behavior at the product boundary.
```

다음 목표:

```text
Agent Adapter Layer:
Claude / Codex-GPT / Antigravity / unknown agent outputs should normalize into
Common AgentEvent, AgentStatus, AgentIdentity, and AgentApproval models.
```

## 반드시 먼저 읽을 파일

1. `docs/AGENT_PHASE_A8_EXECUTOR_ONBOARDING.md`
2. `docs/AGENT_ADAPTER_LAYER_PLAN.md`
3. `docs/AGENT_PHASE_A8_PREP_SCOPE.md`
4. `docs/AGENT_PHASE_A5_SCOPED_ACCEPTANCE.md`
5. `docs/AGENT_PHASE_A6_BACKEND_ACCEPTANCE.md`
6. `docs/AGENT_PHASE_A7_ACCEPTANCE.md`
7. `docs/AGENT_UX_RELEASE_GATE_FOLLOWUPS.md`
8. `docs/ADAPTER_PHASE_7_ACCEPTANCE.md`
9. `docs/08-phase7-adapter-ops.md`
10. `docs/09-phase7-mobile-smoke.md`
11. `docs/ADAPTER_EXPANSION_PLAN.md`
12. `docs/ARCHITECTURE.md`

코드 확인용:

1. `companion-daemon/internal/mux/adapter.go`
2. `companion-daemon/internal/mux/registry.go`
3. `companion-daemon/internal/term/pty.go`
4. `companion-daemon/cmd/devremote/app.go`
5. `mobile/src/components/AgentCard.tsx`
6. `mobile/src/screens/FeedScreen.tsx`

## 중요한 경계

Agent Adapter Layer는 Terminal Adapter Layer를 대체하지 않는다.

Terminal Adapter 담당:

- tmux/cmux/LocalPTY 같은 실행 위치
- live WebSocket I/O
- screen/history capability
- create/delete/resize
- terminal session lifecycle

Agent Adapter 담당:

- Claude/Codex/Antigravity 같은 실행 주체 감지
- log/process/screen evidence 수집
- agent-specific output parsing
- common AgentEvent/AgentStatus/AgentApproval 생성
- parser degradation/diagnostics

금지:

- Claude/Codex/Antigravity 실행 방식을 wrapper로 강제하지 않는다.
- agent parser 실패 때문에 terminal session을 숨기거나 죽이지 않는다.
- mobile behavior를 agent 이름으로 분기하지 않는다.
- raw prompt/token/path/code를 fixture, push, diagnostic에 그대로 넣지 않는다.
- native approval API를 초기 scope에 넣지 않는다.

## 실행에이전트가 시작할 작업

### 다음 작업: Phase A8 — Third Agent Backend Slice

A5는 **Agent Backend Foundation**으로 종료됐고, A6는 **Codex Backend Slice**로 accepted 됐으며,
A7은 **Agent Agnostic UX / Product Gate**로 accepted 됐다.

근거:

- `docs/AGENT_PHASE_A5_BACKEND_FOUNDATION_ACCEPTANCE.md`
- `docs/AGENT_PHASE_A5B_BACKEND_ACCEPTANCE.md`
- `docs/AGENT_PHASE_A5B_DEGRADED_FOLLOWUP_REVIEW.md`
- `docs/AGENT_PHASE_A6_BACKEND_ACCEPTANCE.md`
- `docs/AGENT_PHASE_A7_ACCEPTANCE.md`

A5 accepted backend contract:

1. production detection bridge;
2. production parser path;
3. common `AgentEvent` canonicalization;
4. `/api/sessions.Events` product boundary;
5. `user_message`;
6. `thinking`;
7. `tool_call_started`;
8. `approval_requested`;
9. parser-derived `agentStatus=waiting_approval`;
10. malformed log survival;
11. resolver context propagation;
12. backend regression pass.

A6 accepted backend contract:

1. Codex parser/detector/resolver contract;
2. Codex production telemetry path;
3. `/api/sessions.Events` common event boundary;
4. Codex `AgentKind == "codex"`;
5. Codex parser-derived `AgentStatus == "waiting_approval"`;
6. Codex `AgentConfidence >= 0.5`;
7. composite production detector bridge;
8. Claude A5 regression preservation;
9. mobile no-name-branch smoke.

A7 accepted product gate:

1. unknown `AgentKind` graceful rendering;
2. unknown `AgentStatus` graceful rendering;
3. degraded state visualization;
4. schema evolution compatibility;
5. mobile/common UX에 vendor-specific behavior branch 추가 금지;
6. backend contract 변경 없이 future agent 추가 가능성 검증.

A7 이후 불변조건:

1. mobile/common UX에 vendor-specific behavior branch를 추가하지 않는다.
2. unknown/future agent/event/status는 graceful fallback한다.
3. `agentKind`/`agentStatus`는 activity path 전체에서 보존한다.

A8 목표:

1. Claude/Codex 외에 실제 redacted fixture가 있는 제3 agent를 backend contract에 태운다.
2. parser/detector/resolver 확장이 기존 Claude/Codex production parser 수정 없이 가능한지 재검증한다.
3. output은 common `AgentEvent`, `AgentStatus`, `AgentIdentity` contract로 변환한다.
4. mobile은 A7에서 검증한 common status/event/capability UX를 그대로 사용한다.

해야 할 일:

1. 실제 third agent 후보 fixture inventory를 작성한다.
   - 후보: Gemini, Qwen, OpenAI Responses, Antigravity, OpenCode, Aider.
   - raw fixture는 커밋하지 않는다.
   - redacted fixture만 커밋한다.
2. 실제 redacted fixture가 충분하면 parser/detector/resolver를 추가한다.
3. 실제 fixture가 불충분하면 A8 implementation을 시작하지 말고 A8-prep fixture inventory/redaction만 커밋한다.
4. third agent events/status를 common contract로 canonicalize한다.
5. production telemetry boundary에 연결한다.
6. A5/A6/A7 regression을 유지한다.
7. unknown/future agent fallback이 깨지지 않았음을 검증한다.

금지:

- A5 common event/status path를 우회하지 않는다.
- mobile에 `if agent == "claude"` / `if agent == "codex"` 같은 behavior branch를 추가하지 않는다.
- mobile에 Gemini/Qwen/OpenAI Responses/Antigravity 같은 future vendor branch를 미리 추가하지 않는다.
- 실제 redacted fixture 없이 가짜 third agent나 상상 기반 parser를 만들지 않는다.
- status visualization을 agent 종류와 결합하지 않는다.
- unknown agent를 fatal/unsupported로 취급해 terminal session을 숨기지 않는다.
- parser failure를 terminal session failure로 전파하면 안 된다.
- raw prompt/token/path/code를 API, mobile, push, diagnostic에 노출하면 안 된다.
- backend common contract를 A8에서 변경하지 않는다. 필요하면 먼저 문서화하고 검증 에이전트 판정을 받아야 한다.

권장 커밋 메시지:

```text
feat: agent phase A8 third backend slice
```

Phase A8 완료 보고 형식:

```text
Phase A8 완료 — <commit>

주요 변경:
- ...

검증 포인트:
- 실제 redacted fixture 존재
- 가짜 third agent / 상상 기반 parser 없음
- Detector/Resolver/Parser 연결
- Common AgentEvent/AgentStatus/AgentIdentity contract 변환
- A5/A6/A7 regression 유지
- Mobile/common vendor-specific behavior branch 없음
- Unknown/future fallback 유지
```

### Mandatory release-gate follow-ups

A5/A6/A7 종료가 아래 항목의 완료를 의미하지 않는다.

반드시 별도 release gate로 추적한다:

- degraded diagnostics UX;
- approval UX;
- diagnostics / alpha release gate.

추적 문서:

- `docs/AGENT_UX_RELEASE_GATE_FOLLOWUPS.md`

A7에서 Agent Agnostic UX/Product Gate는 닫혔다.
Product/release-complete 상태는 A9 approval UX와 A10 diagnostics/alpha release gate가 닫히기 전까지 선언하지 않는다.

## Phase별 실행 요약

### Phase A0 — Scope 확정

- 문서만 수정.
- 구현 금지.
- Agent Adapter Layer의 목표, non-goals, phase acceptance 확정.

### Phase A1 — 로그 인벤토리 + fixture 수집

- 실제 Mac에서 Claude/Codex/Antigravity 로그 후보 조사.
- raw fixture는 repo에 넣지 않음.
- redacted fixture만 commit.
- 최소 2개 agent, agent별 최소 3개 scenario 확보.
- tool call 또는 approval fixture 최소 1개.

Phase A0에서 확인된 실제 경로 (2026-07-07):

```text
Claude:  ~/.claude/history.jsonl (1,099 entries)
         ~/.claude/projects/*/<uuid>.jsonl
Codex:   ~/.codex/history.jsonl (231 entries)
         ~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl  ← 실제 경로
Antigravity: ~/.gemini/antigravity/brain/<uuid>/.system_generated/logs/transcript.jsonl
         (50 sessions, 49 transcripts)
```
- malformed/unknown fixture 최소 1개.

중요: 민감정보 redaction이 불충분하면 다음 Phase로 가지 않는다.

### Phase A2 — Common Agent Model

- AgentIdentity, AgentStatus, AgentEvent, AgentApproval 확정.
- fixture 기반으로만 모델 확정.
- Claude JSONL field명을 common model에 그대로 노출하지 않음.

### Phase A3 — Parser Contract Harness

- production parser보다 먼저 common contract suite 작성.
- malformed record, unknown field, duplicate, cursor resume, approval/tool detection 검증.
- parser panic recover와 degraded diagnostic 검증.

### Phase A4 — Detector / Log Resolver 기반

- process/cwd/log path/screen/manual link evidence 수집.
- confidence 기반 detection.
- wrong positive보다 unknown fallback 선호.
- detector 실패가 terminal session을 숨기지 않음.

### Phase A5a — Claude Production Detection Bridge

- Production process evidence → `agent.NewTermAgentDetector()` → `/api/sessions` bridge.
- Claude true-positive와 non-agent false-positive product-boundary test.
- `c15659568` 기준 scoped accept 완료.
- 검증 문서: `docs/AGENT_PHASE_A5_SCOPED_ACCEPTANCE.md`

남은 follow-up:

- request-time `Snapshot()`에서 unbounded `ProcessInfo(context.Background())` 직접 호출을
  telemetry loop cache 또는 timeout context로 정리한다.

### Phase A5 — Agent Backend Foundation

- 완료.
- accepted scope는 backend foundation이다.
- UX/release-complete가 아니다.

검증 문서:

- `docs/AGENT_PHASE_A5_BACKEND_FOUNDATION_ACCEPTANCE.md`
- `docs/AGENT_PHASE_A5B_BACKEND_ACCEPTANCE.md`
- `docs/AGENT_PHASE_A5B_DEGRADED_FOLLOWUP_REVIEW.md`

완료 범위:

- Claude parser/resolver/event/status backend core.
- malformed log survival guard.
- `/api/sessions.Events` common product boundary.

이관된 mandatory follow-up 상태:

- A7에서 degraded/unknown/mobile schema rendering proof는 닫혔다.
- 남은 항목은 degraded diagnostics UX, approval UX, diagnostics / alpha release gate다.

추적 문서:

- `docs/AGENT_UX_RELEASE_GATE_FOLLOWUPS.md`

### Phase A5b — Historical Claude Parser/Event/UX Slice

- historical context로 남긴다.
- 실제 종료 판정은 `Phase A5 — Agent Backend Foundation`을 따른다.
- mobile/degraded UX는 A5 blocker가 아니라 mandatory release gate로 이관됐다.

### Phase A6 — Codex Backend Slice

- 완료.
- accepted scope는 backend slice다.
- product/mobile UX completion이 아니다.
- Claude parser 수정 없이 Codex backend 추가.
- `/api/sessions`에서 Codex events, `AgentKind`, `AgentStatus`, `AgentConfidence` 검증.

검증 문서:

- `docs/AGENT_PHASE_A6_BACKEND_ACCEPTANCE.md`

### Phase A7 — Agent Agnostic UX / Product Gate

- 완료.
- accepted scope는 Agent Agnostic UX/Product Gate다.
- Third Agent 실제 backend 완성, Approval UX 완성, release-complete를 의미하지 않는다.
- unknown `AgentKind`, unknown `AgentStatus`, degraded state, missing optional fields, unknown event type을 graceful하게 렌더링한다.
- status visualization은 agent 종류가 아니라 현재 상태를 표현한다.
- mobile vendor-specific branching 추가 금지를 regression으로 유지한다.

검증 문서:

- `docs/AGENT_PHASE_A7_ACCEPTANCE.md`

### Phase A8 — Third Agent Backend Slice

- 다음 작업.
- 실제 redacted fixture가 있는 제3 agent만 진행한다.
- Antigravity fixture가 충분하면 Antigravity, 아니면 Gemini/Qwen/OpenAI Responses/OpenCode/Aider 등 실제 fixture 확보 가능한 agent.
- 실제 fixture가 없으면 implementation 대신 A8-prep fixture inventory/redaction만 진행한다.
- structured log가 없으면 screen/process fallback과 low confidence UX를 명확히 표시한다.
- A7 불변조건을 유지한다.

### Phase A9 — Approval UX

- Common AgentApproval 기반 CTA.
- approval card.
- approve/reject action.
- pending/expired/resolved 상태.
- 중복 tap 방지.
- backend idempotency.
- 실패 시 recoverable UI.
- audit log.
- native approval API 제외.
- sensitive command 전문 push 금지.

### Phase A10 — Diagnostics / Alpha Release Gate

- `pokit doctor agents` 또는 동등한 진단.
- daemon health.
- adapter status.
- active sessions.
- parser confidence.
- last error.
- mobile connection state.
- exportable diagnostic bundle.
- detector/log resolver/parser version/debug output.
- parser failure telemetry.
- Alpha/release gate checklist.
- raw secret 노출 금지.

## 검증에이전트 운영 규칙

검증에이전트는 각 Phase 커밋을 받을 때 다음을 수행한다.

1. 원격 최신 커밋 fetch.
2. clean worktree 확인.
3. executor commit과 parent diff 확인.
4. 해당 Phase scope와 금지 사항 대조.
5. 자동 테스트 직접 실행.
6. redaction, fallback, mobile hardcoding을 직접 검색.
7. ACCEPT 또는 REJECT 문서 작성.
8. 검증 문서 커밋/푸시.

권장 검증 문서명:

```text
docs/AGENT_PHASE_A0_ACCEPTANCE.md
docs/AGENT_PHASE_A1_REVIEW.md
docs/AGENT_PHASE_A1_ACCEPTANCE.md
docs/AGENT_PHASE_A<N>_IMPLEMENTATION_REVIEW.md
```

권장 판정 형식:

```text
Phase:
Executor commit:
Verifier decision: ACCEPT | REJECT

Scope verification:
Security/redaction:
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

## 반드시 검색할 패턴

agent 이름 문자열이 모두 금지되는 것은 아니다. agent-specific parser, display label,
registration에는 필요하다. 금지되는 것은 common/mobile behavior가 agent 이름으로 분기하는 것이다.

권장 검색:

```sh
rg -n "claude|codex|gpt|antigravity|gemini" mobile companion-daemon/internal companion-daemon/cmd
rg -n "AgentEvent|AgentStatus|AgentIdentity|AgentApproval" companion-daemon mobile/src
rg -n "approval|tool_call|waiting_approval|degraded" companion-daemon mobile/src
rg -n "panic\\(|log\\.Fatal|os\\.Exit" companion-daemon/internal companion-daemon/cmd
rg -n "TOKEN|API_KEY|SECRET|Bearer|sk-" companion-daemon/internal/agent docs
```

Phase A1 이후 fixture redaction 검색:

```sh
rg -n "/Users/|/home/|Bearer |sk-[A-Za-z0-9]|api[_-]?key|token|secret" companion-daemon/internal/agent/testdata docs
```

## 자동 검증 기본 세트

Phase별로 추가 테스트가 생기면 반드시 더한다.

기본:

```sh
cd companion-daemon
GOCACHE=/tmp/devremote-agent-go-cache go vet ./...
GOCACHE=/tmp/devremote-agent-go-cache go test ./...
GOCACHE=/tmp/devremote-agent-go-cache go test -race ./...

cd ../mobile
node_modules/.bin/tsc --noEmit
```

주의:

- `go test ./...`와 `go test -race ./...`는 `httptest` local listener 때문에 sandbox 밖 실행이
  필요할 수 있다.
- 네트워크 의존 테스트나 외부 agent 실행은 기본 검증 세트에 넣지 않는다.
- 실제 Mac 로그 탐색은 민감정보가 출력될 수 있으므로 결과를 그대로 커밋하지 않는다.

## 다음 대화에서 실행에이전트에게 줄 요청

```text
de3197dbc 이후 docs/AGENT_PHASE_A8_EXECUTOR_ONBOARDING.md,
docs/AGENT_ADAPTER_LAYER_PLAN.md,
docs/NEXT_SESSION_AGENT_ADAPTER_HANDOFF.md,
docs/AGENT_PHASE_A8_PREP_SCOPE.md,
docs/AGENT_PHASE_A7_ACCEPTANCE.md를 읽고,
Phase A8 Third Agent Backend Slice implementation을 시작해줘.
Antigravity 실제 redacted fixture는 de3197dbc에서 확보됐으니,
production detector/resolver/parser/telemetry path를 common Agent contract로 연결해줘.
구현 중 실제 production blocker가 확인되면 A8 완료로 포장하지 말고 blocker 문서만 커밋/푸시해줘.
mobile/common vendor-specific behavior branch는 추가하지 마.
```

검증에이전트에게 줄 요청:

```text
<executor-commit> 기준으로 Agent Adapter Phase A8을 검증해줘.
구현하지 말고 Agent Adapter Phase A8 Third Agent Backend Slice acceptance만 엄격히 판정해줘.
특히 실제 redacted fixture 존재, 가짜 third agent 금지, parser/detector/resolver 연결,
common event/status contract 변환, A5/A6/A7 regression 유지,
mobile/common vendor-specific behavior branch 금지를 확인해줘.
```

## 현재 판단

Phase 7 이후 방향은 "새 terminal adapter 추가"가 아니라 Agent Adapter Layer가 맞다.

다만 바로 parser 구현으로 들어가면 Claude 전용 구조가 될 가능성이 높다. 반드시 다음 순서를
지킨다.

```text
A0 Scope
→ A1 실제 로그/fixture
→ A2 Common model
→ A3 Contract harness
→ A4 Detector/resolver
→ A5a Claude detection bridge
→ A5b Claude parser/event/UX
→ A6 Codex backend slice
→ A7 Agent Agnostic UX / Product Gate
→ A8 Third agent backend slice
→ A9 Approval UX
→ A10 Diagnostics / Alpha Release Gate
```

첫 번째 실제 구현 고비는 A1 fixture/redaction이고, backend 추상화 고비는 A6 두 번째 agent 추가다.
이후 제품화 고비는 A7 Agent Agnostic UX였다. A8부터는 실제 fixture 없는 parser 구현을 금지하고,
A7 불변조건을 regression으로 유지하면서 제3 agent backend slice를 붙인다.
