# Next Session Handoff — Agent Adapter Layer

## 역할 분담 — 반드시 유지

이 작업에는 두 역할이 존재한다.

- **실행 에이전트**: 승인된 계획에 따라 각 Phase를 수행하고 커밋을 제출한다.
- **검증 에이전트**: 실행 에이전트의 결과를 독립적으로 검증한다.

다음 세션의 역할은 사용자의 지시에 따라 달라진다. 사용자가 "실행에이전트"라고 하면
이 문서를 implementation handoff로 사용한다. 사용자가 "검증에이전트"라고 하면 구현하지
말고 계획 또는 실행 커밋을 검증한다.

현재 사용자의 의도는 **실행에이전트에게 Phase 7 이후 Agent Adapter Layer 작업을 넘기는 것**이다.

## 현재 상태

- 브랜치: `feature/phase10-multi-adapter`
- Terminal Adapter Layer acceptance: Phase 7 accepted
- Phase 7 acceptance verifier commit: `b163c1b`
- 핵심 결론:
  - tmux/cmux production adapter를 깨지 않고 LocalPTY까지 붙였다.
  - mobile은 terminal backend 이름 목록이 아니라 capability로 동작한다.
  - Phase 7은 운영/UX/진단 정리까지 ACCEPT됐다.

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

1. `docs/AGENT_ADAPTER_LAYER_PLAN.md`
2. `docs/ADAPTER_PHASE_7_ACCEPTANCE.md`
3. `docs/08-phase7-adapter-ops.md`
4. `docs/09-phase7-mobile-smoke.md`
5. `docs/ADAPTER_EXPANSION_PLAN.md`
6. `docs/ARCHITECTURE.md`

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

### 첫 작업: Phase A0 — Scope 검증/확정

구현하지 말고 문서 검토부터 한다.

해야 할 일:

1. `docs/AGENT_ADAPTER_LAYER_PLAN.md`를 읽는다.
2. Terminal Adapter Phase 7 acceptance와 충돌하는 지점을 찾는다.
3. Agent Adapter와 Terminal Adapter 경계가 모호한 지점을 보완한다.
4. Phase A1~A10 acceptance가 실행 가능한지 검토한다.
5. 필요한 경우 문서만 수정한다.
6. 커밋하고 푸시한다.

Phase A0에서 코드 구현은 금지다.

권장 커밋 메시지:

```text
docs: validate agent adapter layer scope
```

Phase A0 완료 보고 형식:

```text
Phase A0 완료 — <commit>

주요 변경:
- ...

검증 포인트:
- Terminal Adapter Layer와 Agent Adapter Layer 경계 확인
- native approval API 초기 제외 확인
- parser failure terminal isolation 원칙 확인
```

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

### Phase A5 — Claude Vertical Slice

- Claude detector/resolver/parser.
- Claude events/status/approval을 Common AgentEvent로 변환.
- tmux + Claude, LocalPTY + Claude에서 semantics 동일해야 함.
- mobile에 Claude 이름 기반 behavior branch 금지.

### Phase A6 — Codex/GPT Vertical Slice

- Claude parser 수정 없이 Codex/GPT adapter 추가.
- 같은 mobile Activity UI 사용.
- 이 Phase가 Agent Adapter Layer의 실제 추상화 검증점이다.

### Phase A7 — Third Agent / Antigravity Slice

- Antigravity fixture가 충분하면 Antigravity.
- 아니면 Gemini/OpenCode/Aider 등 실제 fixture 확보 가능한 agent.
- structured log가 없으면 screen/process fallback과 low confidence UX 검증.

### Phase A8 — Approval UX

- Common AgentApproval 기반 CTA.
- 초기 approve/reject는 terminal input fallback.
- native approval API 제외.
- sensitive command 전문 push 금지.

### Phase A9 — Agent UX

- Session card에 terminal backend와 agent identity 분리 표시.
- Activity unavailable, no activity, unknown agent, degraded parser 구분.
- behavior는 common status/event/capability 기반.

### Phase A10 — Diagnostics / Doctor

- `pokit doctor agents` 또는 동등한 진단.
- detector/log resolver/parser version/debug output.
- parser failure telemetry.
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
b163c1b 이후 docs/AGENT_ADAPTER_LAYER_PLAN.md와
docs/NEXT_SESSION_AGENT_ADAPTER_HANDOFF.md를 읽고,
Phase A0부터 시작해줘. 구현하지 말고 계획/범위 문서 검증과 보완만 하고
커밋/푸시해줘.
```

검증에이전트에게 줄 요청:

```text
<executor-commit> 기준으로 Agent Adapter Phase A0을 검증해줘.
구현하지 말고 docs/AGENT_ADAPTER_LAYER_PLAN.md와
docs/NEXT_SESSION_AGENT_ADAPTER_HANDOFF.md의 범위/순서/acceptance만 판정해줘.
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
→ A5 Claude
→ A6 Codex/GPT
→ A7 Third agent
→ A8 Approval UX
→ A9 Agent UX
→ A10 Diagnostics
```

첫 번째 실제 구현 고비는 A1 fixture/redaction이고, 실제 추상화 고비는 A6 두 번째 agent 추가다.
