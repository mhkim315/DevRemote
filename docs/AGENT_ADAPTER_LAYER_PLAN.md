# Agent Adapter Layer Plan

- 작성일: 2026-07-07
- 기준 브랜치: `feature/phase10-multi-adapter`
- Terminal Adapter Layer 기준 상태: Phase 7 accepted (`b163c1b`)
- 문서 목적: Phase 7 이후 후속 방향을 실행 가능한 계획으로 고정한다.
- 핵심 전제: Phase 1~7은 "어디에서 실행되는가"를 추상화했다. Agent Adapter Layer는
  "그 안에서 무엇이 실행 중인가"를 추상화한다.

## 0. 현재 기준선

Terminal Adapter Layer는 다음을 이미 증명했다.

- tmux, cmux, LocalPTY를 하나의 Session 모델로 다룰 수 있다.
- backend 이름 의존을 API/mobile/telemetry 경계에서 제거했다.
- fixture adapter로 기존 production adapter 수정 없는 확장을 증명했다.
- LocalPTY라는 실제 세 번째 backend를 feature flag 뒤에 추가했다.
- mobile은 backend 이름 목록이 아니라 capability로 Activity/history/live behavior를 결정한다.
- Phase 7에서 운영 문서, mobile smoke, 상태/진단 한계가 정리됐다.

따라서 다음 단계는 terminal backend를 계속 늘리는 것이 아니라, terminal 안에서 실행되는
AI agent를 공통 UX로 관제하는 계층을 추가하는 것이다.

```text
POKIT
├─ Terminal Adapter Layer
│  ├─ tmux
│  ├─ cmux
│  └─ LocalPTY
│
└─ Agent Adapter Layer
   ├─ Claude
   ├─ Codex/GPT
   ├─ Antigravity
   └─ unknown/fallback
```

## 1. 목표

Agent Adapter Layer의 목표는 Claude, Codex/GPT, Antigravity 등 서로 다른 AI agent의
로그, 화면 출력, 승인 요청, 작업 상태를 공통 모델로 정규화하는 것이다.

목표는 agent 이름을 숨기는 것이 아니다. 사용자는 계속 agent 이름을 볼 수 있어야 한다.

예:

```text
Claude Code     waiting_approval    [Approve] [Reject] [Open Terminal]
Codex           working             [View Activity] [Interrupt]
Antigravity     unknown             [Open Terminal]
```

하지만 POKIT 내부 로직과 mobile UX는 `claude`, `codex`, `antigravity`라는 이름이 아니라
공통 상태와 공통 이벤트로 동작해야 한다.

최종적으로 증명해야 하는 시나리오:

```text
새 AgentAdapter package 작성
  → composition root에 한 번 등록
  → session의 process/log/screen evidence로 agent 감지
  → agent-specific 로그를 Common AgentEvent로 변환
  → mobile Activity/Status/Approval UX가 agent 이름 분기 없이 동작
  → parser 실패 시 terminal session은 계속 사용 가능
  → 기존 Claude/Codex production parser 수정 없이 세 번째 agent 추가 가능
```

## 2. Non-goals

초기 Agent Adapter Layer에서 하지 않는다.

- Claude, Codex, Antigravity 실행 방식을 대체하지 않는다.
- 사용자가 agent를 실행하는 방식을 강제하지 않는다.
- agent process를 wrapper로 감싸지 않는다.
- 모든 agent를 동시에 완벽 지원한다고 주장하지 않는다.
- native approval API를 처음부터 구현하지 않는다.
- 로그 포맷 변경에 자동으로 대응한다고 주장하지 않는다.
- parser failure를 terminal session failure로 취급하지 않는다.
- mobile push/diagnostic에 raw prompt, token, command 전문을 노출하지 않는다.

초기 approval action은 terminal input 기반 fallback으로 제한한다.

예:

- approve → `y\n`
- reject → `n\n`
- open_terminal → terminal screen으로 이동

agent별 native control API가 확인되면 추후 capability로 분리한다.

## 3. 설계 원칙

### 3.1 기존 agent를 건드리지 않는다

POKIT은 agent runtime을 대체하지 않고 관제 계층으로 존재한다.

관찰 가능한 입력:

- terminal session ID
- terminal backend capability
- process name / pid / command line
- cwd
- known log path
- JSONL/log file
- terminal screen text / scrollback
- manual link metadata

### 3.2 Agent 이름이 아니라 의미를 추상화한다

agent-specific record는 다음 공통 의미로 변환된다.

- `agent_started`
- `user_message`
- `assistant_message`
- `thinking`
- `tool_call_started`
- `tool_call_finished`
- `approval_requested`
- `approval_resolved`
- `waiting_input`
- `completed`
- `failed`
- `interrupted`
- `unknown`

### 3.3 포맷 변화의 영향 범위를 adapter에 가둔다

```text
Claude JSONL format changed
        ↓
ClaudeAgentAdapter 수정
        ↓
Common AgentEvent 유지
        ↓
Mobile / telemetry / approval UX 변경 없음
```

추상화는 변화를 없애는 것이 아니라, 변화를 한 곳으로 모으는 것이다.

### 3.4 Terminal Adapter와 Agent Adapter를 분리한다

Terminal Adapter는 "어디에서 실행되는가"를 담당한다.

- tmux
- cmux
- LocalPTY
- SSH
- Docker

Agent Adapter는 "무엇이 실행 중인가"를 담당한다.

- Claude
- Codex/GPT
- Antigravity
- Gemini
- unknown

Claude가 tmux에서 실행되든 LocalPTY에서 실행되든 Agent Adapter의 parse 결과는 같은
Common AgentEvent여야 한다.

### 3.5 실패는 degraded metadata로 격리한다

Agent Adapter failure는 terminal 기능을 죽이면 안 된다.

예:

- parser malformed record → 해당 record skip
- log permission denied → agent diagnostics에 표시
- detector confidence 낮음 → unknown agent
- parser panic → recover 후 adapter degraded
- log resolver 실패 → terminal만 표시

## 4. 공통 모델 초안

이 모델은 Phase A2에서 fixture 기반으로 확정한다. 여기서는 의미를 고정하고 이름은 조정
가능하다.

### 4.1 AgentIdentity

```go
type AgentIdentity struct {
    Kind        string  `json:"kind"`        // claude, codex, antigravity, gemini, unknown
    DisplayName string  `json:"displayName"` // Claude Code, Codex, Antigravity
    Version     string  `json:"version,omitempty"`
    Confidence  float64 `json:"confidence"`
}
```

규칙:

- `Kind`는 내부 stable key다.
- `DisplayName`은 UI 표시용이다.
- `Confidence`가 낮으면 mobile은 "Unknown Agent" 또는 "possibly Claude"처럼 표시한다.
- agent 감지가 실패해도 session 자체는 계속 표시한다.

### 4.2 AgentStatus

```go
type AgentStatus string

const (
    AgentStatusUnknown         AgentStatus = "unknown"
    AgentStatusIdle            AgentStatus = "idle"
    AgentStatusThinking        AgentStatus = "thinking"
    AgentStatusWorking         AgentStatus = "working"
    AgentStatusWaitingApproval AgentStatus = "waiting_approval"
    AgentStatusWaitingInput    AgentStatus = "waiting_input"
    AgentStatusCompleted       AgentStatus = "completed"
    AgentStatusFailed          AgentStatus = "failed"
    AgentStatusInterrupted     AgentStatus = "interrupted"
    AgentStatusDegraded        AgentStatus = "degraded"
)
```

상태 우선순위 초안:

```text
manual link / explicit user action
→ explicit structured event
→ process/log evidence
→ screen fallback
→ unknown
```

충돌 예:

- JSONL에서 `approval_requested`가 감지되면 screen이 idle처럼 보여도
  `waiting_approval`이 우선한다.
- process가 종료됐고 마지막 event가 failure면 `failed`가 우선한다.
- 로그가 없고 process만 살아 있으면 `working` 또는 `unknown`으로 낮은 confidence를 둔다.

### 4.3 AgentEvent

```go
type AgentEvent struct {
    ID         string            `json:"id"`
    SessionID  string            `json:"sessionId"`
    AgentKind  string            `json:"agentKind"`
    Type       AgentEventType    `json:"type"`
    Timestamp  time.Time         `json:"timestamp"`
    Text       string            `json:"text,omitempty"`
    ToolName   string            `json:"toolName,omitempty"`
    ApprovalID string            `json:"approvalId,omitempty"`
    RawRef     string            `json:"rawRef,omitempty"`
    Confidence float64           `json:"confidence"`
    Source     AgentEventSource  `json:"source"`
    Metadata   map[string]string `json:"metadata,omitempty"`
}
```

규칙:

- `RawRef`는 raw content가 아니라 source record를 추적할 수 있는 참조다.
- agent-specific field는 `Metadata` 또는 adapter 내부 raw model에 격리한다.
- mobile이 `Metadata` key에 의존해 behavior를 바꾸면 안 된다.
- event ID는 parser 재시작과 incremental parse에서 안정적이어야 한다.

### 4.4 AgentEventType

```go
type AgentEventType string

const (
    EventAgentStarted       AgentEventType = "agent_started"
    EventUserMessage        AgentEventType = "user_message"
    EventAssistantMessage   AgentEventType = "assistant_message"
    EventThinking           AgentEventType = "thinking"
    EventToolCallStarted    AgentEventType = "tool_call_started"
    EventToolCallFinished   AgentEventType = "tool_call_finished"
    EventApprovalRequested  AgentEventType = "approval_requested"
    EventApprovalResolved   AgentEventType = "approval_resolved"
    EventWaitingInput       AgentEventType = "waiting_input"
    EventCompleted          AgentEventType = "completed"
    EventFailed             AgentEventType = "failed"
    EventInterrupted        AgentEventType = "interrupted"
    EventUnknown            AgentEventType = "unknown"
)
```

### 4.5 AgentEventSource

```go
type AgentEventSource string

const (
    SourceJSONL      AgentEventSource = "jsonl"
    SourceLogFile    AgentEventSource = "log_file"
    SourceScreen     AgentEventSource = "screen"
    SourceProcess    AgentEventSource = "process"
    SourceManualLink AgentEventSource = "manual_link"
)
```

### 4.6 AgentApproval

```go
type AgentApproval struct {
    ID         string             `json:"id"`
    SessionID  string             `json:"sessionId"`
    AgentKind  string             `json:"agentKind"`
    Prompt     string             `json:"prompt"`
    Options    []ApprovalOption   `json:"options"`
    Default    string             `json:"default,omitempty"`
    Source     AgentEventSource   `json:"source"`
    Confidence float64            `json:"confidence"`
    Metadata   map[string]string  `json:"metadata,omitempty"`
}

type ApprovalOption struct {
    ID      string `json:"id"`      // approve, reject, send_text, send_key, open_terminal
    Label   string `json:"label"`
    Payload string `json:"payload,omitempty"`
}
```

초기에는 approval action을 native API로 보내지 않는다. terminal input으로 처리하거나 terminal을
열어 사용자가 직접 조작하게 한다.

## 5. Agent Adapter 계약 초안

Phase A2/A4에서 fixture와 contract harness를 통해 확정한다.

```go
type SessionContext struct {
    SessionID            string
    TerminalAdapter      string
    TerminalCapabilities []string
    LocalID              string
    DisplayID            string
    CWD                  string
    PID                  int
    CommandLine          []string
    ProcessName          string
    ScreenText           string
}

type AgentDetector interface {
    Detect(ctx context.Context, session SessionContext) (AgentIdentity, error)
}

type AgentLogResolver interface {
    ResolveLogs(ctx context.Context, session SessionContext) ([]AgentLogRef, error)
}

type AgentLogRef struct {
    Path       string
    Kind       string
    UpdatedAt  time.Time
    Confidence float64
}

type AgentParseInput struct {
    Session  SessionContext
    Identity AgentIdentity
    Logs     []AgentLogRef
    Cursor   AgentCursor
    Screen   string
}

type AgentParseResult struct {
    Events      []AgentEvent
    Status      AgentStatus
    Cursor      AgentCursor
    Approvals   []AgentApproval
    Diagnostics []AgentDiagnostic
}

type AgentParser interface {
    Parse(ctx context.Context, input AgentParseInput) (AgentParseResult, error)
}

type AgentAdapter interface {
    Name() string
    Descriptor() AgentAdapterDescriptor
    Detect(ctx context.Context, session SessionContext) (AgentIdentity, error)
    ResolveLogs(ctx context.Context, session SessionContext) ([]AgentLogRef, error)
    Parse(ctx context.Context, input AgentParseInput) (AgentParseResult, error)
}
```

선택 capability:

- `events`
- `status`
- `tool_call_detection`
- `approval_detection`
- `approval_action_terminal_input`
- `approval_action_native`
- `incremental_parse`
- `screen_fallback`
- `process_detection`
- `log_file_detection`

주의:

- `approval_action_native`는 초기 scope에서 제외한다.
- `screen_fallback`은 신뢰도가 낮으므로 structured log보다 우선하면 안 된다.
- agent parser가 terminal history capability에 의존하면 안 된다. Terminal backend가 live-only여도
  process/log 기반 parse는 가능해야 한다.

## 6. 보안 / 개인정보 원칙

Agent Adapter는 민감정보를 다룰 가능성이 높다.

주의 대상:

- user prompt
- source code
- command
- tool result
- API key / token
- file path
- git diff
- terminal output
- local username / home path

규칙:

- fixture는 redaction 후 commit한다.
- raw prompt/token이 debug dump, mobile push, telemetry에 들어가면 안 된다.
- approval notification은 command 전문이 아니라 요약만 포함한다.
- diagnostic은 log path를 필요 최소한으로 표시한다.
- redaction 전 원본 fixture는 repo 밖 임시 위치에만 둔다.
- redaction 규칙은 fixture metadata에 기록한다.

권장 redaction token:

```text
<USER_HOME>
<PROJECT_ROOT>
<TOKEN>
<API_KEY>
<EMAIL>
<SECRET>
<PRIVATE_PATH>
```

## 7. Phase 계획

### Phase A0 — Agent Adapter Scope 확정

목표:

- Agent Adapter Layer의 범위, non-goals, Terminal Adapter와의 경계를 고정한다.
- 실행에이전트가 implementation 전에 같은 목표를 보게 만든다.

작업:

- 이 문서를 검토하고 필요한 보완 사항을 반영한다.
- `docs/NEXT_SESSION_AGENT_ADAPTER_HANDOFF.md`를 기준으로 실행/검증 역할을 분리한다.
- 초기 대상 agent를 확정한다.
- native approval API, wrapper 실행, 모든 agent 완전 지원을 명시적으로 제외한다.
- Terminal Adapter Phase 1~7 산출물을 새 작업의 기준선으로 연결한다.

추천 초기 대상:

1. Claude
2. Codex/GPT 계열
3. Antigravity는 로그 위치와 구조가 확인된 뒤 vertical slice 여부 결정

합격 기준:

- 구현 변경 없음.
- `Agent Adapter`와 `Terminal Adapter` 책임 경계가 문서상 명확하다.
- Phase A1~A9의 acceptance가 구체적으로 정의되어 있다.
- 실행에이전트가 다음 작업으로 무엇을 해야 하는지 문서만 보고 알 수 있다.

권장 커밋:

```text
docs: define agent adapter layer plan
```

### Phase A1 — 실제 로그 인벤토리와 fixture 수집

목표:

- 상상 기반 parser 설계를 금지한다.
- 실제 Mac에 남은 agent 로그와 process evidence를 조사해 fixture로 고정한다.

작업:

- Claude 로그 위치 탐색.
- Codex/GPT 로그 위치 탐색.
- Antigravity 로그 위치 탐색.
- 최근 30일 내 JSONL/log 후보 파일 inventory 작성.
- 각 후보의 agent, path pattern, mtime, size, format hint, 민감정보 포함 가능성을 문서화.
- 최소 2개 agent에서 redacted fixture 확보.
- fixture naming 규칙과 metadata schema 작성.
- raw fixture는 repo에 넣지 않고 redacted fixture만 commit한다.

탐색 후보 (2026-07-07 Phase A0에서 실제 Mac 로그 기준 확인):

```text
# Claude — 확인됨
~/.claude/history.jsonl                          (1,099 entries)
~/.claude/projects/*/<uuid>.jsonl                (프로젝트별 세션)

# Codex — 확인됨 (handoff의 ~/.codex/projects/ 패턴과 다름)
~/.codex/history.jsonl                           (231 entries)
~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl     (실제 세션 경로)

# Antigravity — 확인됨
~/.gemini/antigravity/brain/<uuid>/.system_generated/logs/transcript.jsonl  (50 sessions, 49 transcripts)

# 미확인 후보
~/.config/claude/
~/.cache/claude/
~/.openai/
~/Library/Application Support/
~/.config/
~/.local/share/
terminal screen/scrollback evidence
```

탐색 명령 예:

```sh
find ~ -type f \( -name "*.jsonl" -o -name "*.log" \) -mtime -30 2>/dev/null
find ~/Library/Application\ Support ~/.config ~/.local/share -type f -mtime -30 2>/dev/null
ps aux | grep -iE "claude|codex|gpt|antigravity|gemini"
```

fixture layout 초안:

```text
companion-daemon/internal/agent/testdata/
  claude/
    approval_request.jsonl
    tool_call_success.jsonl
    failed_run.jsonl
    metadata.json
  codex/
    tool_call_success.jsonl
    interrupted.jsonl
    malformed_record.jsonl
    metadata.json
  antigravity/
    inventory.md
```

metadata 예:

```json
{
  "agent": "claude",
  "scenario": "approval_request",
  "source": "jsonl",
  "redactions": ["<USER_HOME>", "<PROJECT_ROOT>", "<TOKEN>"],
  "expectedStatus": "waiting_approval",
  "expectedEvents": [
    "user_message",
    "tool_call_started",
    "approval_requested"
  ]
}
```

합격 기준:

- 최소 2개 agent에서 fixture를 확보한다.
- 각 agent별 최소 3개 scenario fixture가 있다.
- 전체 fixture 중 tool call 포함 fixture가 최소 1개 있다.
- 전체 fixture 중 approval 또는 waiting input fixture가 최소 1개 있다.
- malformed/unknown field fixture가 최소 1개 있다.
- redaction 규칙이 문서화되어 있고 raw secret/path/token이 repo에 없다.
- fixture metadata가 expected status/events를 포함한다.

주의:

- 민감정보 redaction이 불충분하면 Phase A1은 실패다.
- fixture가 Claude 하나뿐이면 Phase A1은 부분 성공일 수 있으나 Phase A2 확정으로 넘어가면 안 된다.

### Phase A2 — Common Agent Model 정의

목표:

- AgentEvent, AgentStatus, AgentIdentity, AgentApproval의 최소 공통 모델을 확정한다.
- Claude 전용도 아니고, 너무 추상적이라 UX에 못 쓰는 모델도 아니어야 한다.

작업:

- Phase A1 fixture를 기준으로 공통 event type 정의.
- 공통 status 정의.
- confidence 정책 정의.
- event source 우선순위 정의.
- unknown/fallback 정책 정의.
- redacted raw reference 정책 정의.
- API DTO를 additive하게 설계한다.
- mobile이 사용해야 할 필드와 사용하면 안 되는 metadata 필드를 구분한다.

합격 기준:

- Claude fixture가 Common AgentEvent로 표현 가능하다.
- Codex/GPT fixture가 Common AgentEvent로 표현 가능하다.
- Antigravity fixture가 없으면 unknown/screen fallback 정책으로 명시된다.
- agent-specific field는 common model에 침투하지 않고 metadata/raw adapter model에 격리된다.
- mobile이 `status`, `event.type`, `capabilities`로 behavior를 결정할 수 있다.
- legacy session payload가 agent field 없이도 안전하게 표시된다.

금지:

- `if agent == "claude"`를 mobile behavior에 넣지 않는다.
- common model에 Claude JSONL field명을 그대로 노출하지 않는다.
- 불확실한 parser output을 확정 상태처럼 표시하지 않는다.

### Phase A3 — Agent Parser Contract Harness

목표:

- production parser보다 먼저 공통 contract test를 만든다.
- 새 AgentAdapter가 추가될 때 기존 adapter 파일 수정 없이 같은 suite를 통과하게 한다.

작업:

- `internal/agent` 또는 동등한 package에 contract test harness 추가.
- fixture metadata를 읽어 expected events/status를 검증한다.
- mock/fixture adapter를 suite에 등록해 harness 자체를 검증한다.
- parser failure가 terminal failure로 전파되지 않는지 검증한다.

필수 contract 항목:

- valid fixture parse
- malformed record skip
- unknown field ignore
- missing field fallback
- duplicate event 방지
- event ordering
- incremental cursor resume
- status transition
- approval detection
- tool call detection
- parser panic recover
- low confidence unknown fallback

합격 기준:

- 아직 Claude production parser가 없어도 fixture/mock parser로 suite가 동작한다.
- 새 parser는 contract suite에 등록되어야 한다.
- malformed JSONL 하나가 전체 telemetry를 죽이지 않는다.
- parser panic이 process를 죽이지 않고 degraded diagnostic으로 변환된다.

이 Phase가 약하면 이후 Claude parser가 사실상 architecture가 되므로 엄격히 검증한다.

### Phase A4 — Agent Detector / Log Resolver 기반 구축

목표:

- session에서 실행 중인 agent와 관련 log를 추론하는 기반을 만든다.
- detector는 확정이 아니라 confidence 기반 evidence aggregator여야 한다.

입력:

- terminal session ID
- terminal adapter name/capabilities
- local ID/display ID
- cwd
- process name/pid/command line
- known log path
- screen text
- manual link metadata

작업:

- process 기반 detector.
- cwd 기반 log resolver.
- known path pattern resolver.
- manual link 우선순위 설계.
- confidence score 도입.
- ambiguous detection 처리.
- permission denied / path missing / stale log diagnostic 정리.

합격 기준:

- Claude와 Codex/GPT session을 감지하거나 unknown으로 안전하게 fallback한다.
- 잘못된 감지보다 unknown fallback을 선호한다.
- detector 실패가 terminal session을 숨기지 않는다.
- detector contract test가 있다.
- log resolver가 raw file content를 diagnostic에 노출하지 않는다.

### Phase A5 — Claude Agent Adapter Vertical Slice

목표:

- 가장 확실한 agent 하나를 end-to-end로 구현한다.
- 이 Phase의 목적은 Claude 완성도가 아니라 Agent Adapter pipeline의 vertical slice다.

작업:

- Claude detector.
- Claude log resolver.
- Claude JSONL/log parser.
- Claude status inference.
- Claude tool call event mapping.
- Claude approval event mapping.
- Activity feed 연결.
- mobile status badge 표시.
- parser degraded state 표시.

합격 기준:

- Claude session에서 AgentIdentity가 표시된다.
- Claude user/assistant/tool/approval event가 Common AgentEvent로 표시된다.
- Claude approval request가 common Approval UX로 표시된다.
- incremental parsing이 동작한다.
- tmux + Claude와 LocalPTY + Claude에서 같은 AgentEvent semantics가 나온다.
- parser failure 시 terminal은 계속 사용 가능하다.
- mobile에 Claude 이름 기반 behavior branch가 없다.

권장 검증:

- fixture contract tests.
- targeted parser tests.
- API DTO compatibility tests.
- mobile TypeScript compile.
- tmux/LocalPTY smoke if available.

### Phase A6 — Codex/GPT Agent Adapter Vertical Slice

목표:

- 두 번째 agent로 Agent Adapter Layer가 Claude 전용이 아님을 증명한다.

작업:

- Codex/GPT 로그 위치 확정.
- Codex/GPT parser 구현.
- tool/error/interrupted/waiting input mapping.
- contract suite 등록.
- Activity feed 연결.
- mobile에서 동일한 UX로 표시.

합격 기준:

- Claude production parser 수정 없이 Codex/GPT adapter가 추가된다.
- Codex/GPT fixture가 Common AgentEvent로 변환된다.
- 같은 mobile Activity UI에서 Claude와 Codex/GPT가 표시된다.
- unknown/new field에도 parser가 안전하게 동작한다.
- agent-specific UI branch가 생기지 않는다.

이 Phase가 Agent Adapter Layer의 핵심 고비다. Codex/GPT를 붙이기 위해 Claude 코드나
mobile event rendering을 많이 고쳐야 한다면 A2/A3 설계가 실패한 것이다.

### Phase A7 — Antigravity 또는 Third Agent Slice

목표:

- 세 번째 agent 또는 screen-fallback 중심 agent를 붙여 확장성을 재검증한다.

선택 기준:

- Antigravity 로그 구조가 확인되면 Antigravity를 우선한다.
- 로그 구조가 불명확하면 Gemini/OpenCode/Aider 중 실제 fixture 확보가 가능한 agent를 선택한다.
- fixture 확보가 불충분하면 implementation 대신 inventory/unknown fallback 강화로 제한한다.

작업:

- third agent log resolver.
- parser 또는 screen fallback mapper.
- status/event mapping.
- contract 등록.
- 기존 Claude/Codex parser 무수정 확장 증명.

합격 기준:

- 세 번째 agent가 기존 production parser 수정 없이 추가된다.
- structured log가 없으면 screen/process fallback의 낮은 confidence가 명확히 표시된다.
- mobile UX는 common status/event 기반을 유지한다.
- unknown agent fallback이 실제로 안전하다.

### Phase A8 — Approval UX 공통화

목표:

- agent별 approval prompt를 공통 CTA로 표시한다.
- native control 없이도 안전한 terminal input fallback을 제공한다.

작업:

- Common AgentApproval DTO 확정.
- approval_requested event와 active approval state 연결.
- mobile approval CTA 추가.
- action capability별 버튼 노출.
- `Approve`, `Reject`, `Open Terminal` 기본 동작.
- terminal input fallback payload 정책.
- sensitive command summary/redaction.

합격 기준:

- approval event가 있으면 공통 CTA가 노출된다.
- agent 이름 기반 approval UI branch가 없다.
- approval action capability가 없으면 Open Terminal fallback만 표시된다.
- push/notification에는 민감한 command 전문이 포함되지 않는다.
- 승인 action 실패가 terminal session을 죽이지 않는다.

### Phase A9 — Agent UX 정리

목표:

- 사용자가 agent 차이를 이해하되 조작 방식은 공통으로 느끼게 한다.

Session Card 예:

```text
Claude Code
tmux · working · tool running

[Open Terminal] [Activity]
```

Unknown 예:

```text
Unknown Agent
LocalPTY · terminal active

[Open Terminal]
```

작업:

- Session card에 terminal backend와 agent identity를 분리 표시.
- AgentStatus badge 추가.
- Activity feed event rendering 정리.
- unsupported/degraded/unknown action 표시.
- confidence 낮은 감지 UX.
- agent activity unavailable state.

합격 기준:

- agent 이름은 보이지만 UI behavior는 common status/event/capability 기반이다.
- unknown agent도 terminal session으로 안전하게 표시된다.
- agent 감지 실패가 terminal 사용을 막지 않는다.
- activity unavailable과 no activity가 구분된다.
- low confidence detection이 사용자에게 과장되어 표시되지 않는다.

### Phase A10 — Agent Diagnostics / Doctor

목표:

- 로그 포맷 변경과 agent 업데이트에 대응 가능한 운영 구조를 만든다.

작업:

- `pokit doctor agents` 또는 동등한 diagnostics command.
- agent detector debug output.
- log resolver debug output.
- parser version 표시.
- fixture update guide.
- failed parse telemetry.
- adapter health 표시.
- permission denied / malformed / stale cursor / no log found 구분.

합격 기준:

- 특정 agent parser가 깨져도 다른 agent에 영향이 없다.
- parser failure가 mobile/diagnostics에 명확히 표시된다.
- 로그 포맷 변경 시 fixture 추가로 회귀 검증 가능하다.
- diagnostic이 raw prompt/token/secret을 노출하지 않는다.

## 8. 추천 순서와 이유

추천 순서:

```text
A0 Scope
A1 Log inventory + fixtures
A2 Common model
A3 Parser contract harness
A4 Detector / resolver base
A5 Claude vertical slice
A6 Codex/GPT vertical slice
A7 Third agent / Antigravity slice
A8 Approval UX
A9 Agent UX polish
A10 Diagnostics / doctor
```

이 순서의 이유:

- 실제 로그 fixture 없이 common model을 확정하면 Claude 또는 특정 agent 상상에 갇힌다.
- contract harness 없이 Claude vertical slice를 먼저 만들면 Claude parser가 architecture가 된다.
- 두 번째 agent를 빨리 붙여야 Agent Adapter가 Claude 전용 추상화가 아님을 증명할 수 있다.
- Approval UX는 parser/detection이 안정된 뒤 공통화해야 한다.
- diagnostics는 마지막이 아니라 각 Phase에서 쌓되, A10에서 제품화 수준으로 정리한다.

## 9. 가장 어려운 고비

### 고비 1 — 실제 로그 수집과 redaction

agent 로그에는 prompt, code, command, token, path가 섞일 수 있다. 이 단계에서 raw 민감정보가
repo에 들어가면 안 된다. redaction이 불충분하면 구현보다 먼저 중단해야 한다.

### 고비 2 — Common Agent Model

너무 Claude 중심이면 Codex/GPT가 붙을 때 깨진다. 반대로 너무 일반화하면 mobile UX가 실제로
쓸 수 없는 모델이 된다. fixture 기반으로만 확정해야 한다.

### 고비 3 — 두 번째 agent 추가

Claude 이후 Codex/GPT를 붙일 때 기존 Claude parser나 mobile UI를 크게 수정해야 하면
추상화 실패다. 이 지점이 Agent Adapter Layer의 실제 PoC 판정점이다.

## 10. 검증 원칙

검증 에이전트는 다음을 엄격히 확인한다.

- 실행에이전트의 자체 테스트 결과를 그대로 믿지 않는다.
- diff와 부모 커밋을 모두 읽는다.
- fixture redaction을 직접 검사한다.
- mobile에 agent 이름 기반 behavior branch가 생겼는지 `rg`로 조사한다.
- parser failure가 terminal failure로 전파되지 않는지 확인한다.
- existing terminal adapter behavior가 회귀하지 않았는지 필요한 범위에서 재검증한다.
- 새 agent 추가가 기존 production parser 수정으로 이어졌는지 확인한다.

권장 검색:

```sh
rg -n "claude|codex|gpt|antigravity|gemini" mobile companion-daemon/internal companion-daemon/cmd
rg -n "approval|tool_call|AgentEvent|AgentStatus|AgentIdentity" companion-daemon mobile/src
rg -n "panic\\(|log\\.Fatal|os\\.Exit" companion-daemon/internal/agent companion-daemon/cmd
```

agent 이름 문자열이 모두 금지되는 것은 아니다. display name, adapter registration,
agent-specific parser 내부는 허용된다. 금지되는 것은 common UI/API behavior가 agent 이름으로
분기하는 것이다.

## 11. 최종 성공 기준

Agent Adapter Layer PoC의 최종 성공 기준:

- Claude와 Codex/GPT 포함 최소 2개 agent가 Common AgentEvent로 정규화된다.
- 가능하면 세 번째 agent 또는 screen fallback agent가 추가된다.
- 같은 mobile Activity UI에서 agent별 이벤트가 표시된다.
- approval 요청이 공통 UX로 표시된다.
- parser 실패가 terminal session에 영향을 주지 않는다.
- agent별 출력 포맷 변경은 해당 AgentAdapter 수정으로 국한된다.
- fixture 기반 contract test가 존재한다.
- 새로운 agent adapter 추가 시 기존 Claude/Codex production parser를 수정하지 않는다.
- unknown agent도 terminal-only session으로 안전하게 표시된다.

## 12. 다음 실행 지시

다음 실행에이전트는 바로 구현하지 말고 Phase A0 문서 검증부터 시작한다.

첫 요청 예:

```text
b163c1b 이후 docs/AGENT_ADAPTER_LAYER_PLAN.md와
docs/NEXT_SESSION_AGENT_ADAPTER_HANDOFF.md를 읽고,
구현하지 말고 Phase A0 계획 자체를 검증해줘.
```

Phase A0 ACCEPT 전에는 Phase A1 로그 수집을 시작하지 않는다.
