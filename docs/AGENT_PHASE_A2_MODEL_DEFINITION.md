# Phase A2 — Common Agent Model Definition

Date: 2026-07-07
Baseline: A1 accepted (`d62edae55`)

## Event Type → Fixture Mapping

Each common `AgentEventType` must have at least one concrete fixture exemplar.

| Common Event | Fixture Evidence |
|-------------|-----------------|
| `agent_started` | Codex session_task.jsonl (session_meta), Codex session_task.jsonl (task_started) |
| `user_message` | Claude user_assistant.jsonl (type=user), Codex session_task.jsonl (response_item/user), Antigravity user_agent.jsonl (USER_INPUT) |
| `assistant_message` | Antigravity user_agent.jsonl (AGENT_OUTPUT) |
| `thinking` | Claude user_assistant.jsonl (assistant/type=thinking) |
| `tool_call_started` | Claude user_assistant.jsonl (assistant/type=tool_use), Antigravity tool_call.jsonl (TOOL_CALL) |
| `tool_call_finished` | Claude tool_result.jsonl (user/type=tool_result), Antigravity tool_call.jsonl (TOOL_RESULT) |
| `approval_requested` | Claude approval_waiting.jsonl (permission-mode=ask), Codex approval_waiting.jsonl (waiting_for_approval) |
| `approval_resolved` | Codex approval_waiting.jsonl (approval_resolved) |
| `unknown` | Claude malformed.jsonl (unknown_event_type), Codex malformed.jsonl (unknown_msg_type), Antigravity malformed.jsonl (UNKNOWN_TYPE) |

Not yet covered by any fixture (deferred to Phase A5+):
- `waiting_input` — no fixture yet
- `completed` — no fixture yet
- `failed` — no fixture yet
- `interrupted` — no fixture yet

## How Raw Evidence Maps to Common Events

The mapping is NOT a 1:1 field rename. It's a semantic classification:

- Claude `type=user` → `user_message` (context: role=user in message chain)
- Claude `type=assistant, content[].type=thinking` → `thinking`
- Claude `type=assistant, content[].type=tool_use` → `tool_call_started`
- Claude `type=user, content[].type=tool_result` → `tool_call_finished` (context: following tool_use)
- Claude `type=permission-mode, permissionMode=ask` → `approval_requested`
- Codex `type=session_meta` → `agent_started` (context: first event in session)
- Codex `type=event_msg, payload.type=task_started` → `agent_started`
- Codex `type=event_msg, payload.type=waiting_for_approval` → `approval_requested`
- Codex `type=event_msg, payload.type=approval_resolved` → `approval_resolved`
- Antigravity `type=USER_INPUT` → `user_message`
- Antigravity `type=AGENT_OUTPUT` → `assistant_message`
- Antigravity `type=TOOL_CALL` → `tool_call_started`
- Antigravity `type=TOOL_RESULT` → `tool_call_finished`

## Source Detection

| Source | How to detect |
|--------|--------------|
| `log` | Event parsed from JSONL/log file |
| `screen` | Event inferred from terminal screen analysis |
| `process` | Event inferred from process name/cmdline |
| `manual` | User manually linked agent to session |
| `unknown` | Source cannot be determined |

## Go Types

`internal/agent/models.go`:
- `AgentIdentity` — {Kind, DisplayName, Version, Confidence}
- `AgentStatus` — 10 string constants (unknown..degraded)
- `AgentEventType` — 13 string constants (agent_started..unknown)
- `AgentEventSource` — 5 string constants (log..unknown)
- `AgentEvent` — normalized event struct
- `AgentApproval` — approval request/resolution struct

No Claude-specific field names. No `type`, `sessionId`, `payload` from raw JSONL.
All agent-specific evidence isolated to `Metadata map[string]string`.
