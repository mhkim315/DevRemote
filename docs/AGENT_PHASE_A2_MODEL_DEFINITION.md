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

## Source Taxonomy

Matches `AgentEventSource` in models.go per AGENT_ADAPTER_LAYER_PLAN.md §4.5:

| Source | How to detect |
|--------|--------------|
| `jsonl` | Structured JSONL log (Claude projects, Codex sessions, Antigravity transcript) |
| `log_file` | Unstructured log file |
| `screen` | Terminal screen analysis |
| `process` | Process name/command line |
| `manual_link` | User manually linked agent to session |

## Go Types

`internal/agent/models.go`:
- `AgentIdentity` — {Kind, DisplayName, Version, Confidence}
- `AgentStatus` — 10 string constants (unknown..degraded)
- `AgentEventType` — 13 string constants (agent_started..unknown)
- `AgentEventSource` — 5 string constants (jsonl, log_file, screen, process, manual_link)
- `AgentEvent` — normalized event struct
- `ApprovalOption` — {ID, Label, Payload} per option
- `AgentApproval` — approval request/resolution with stable option IDs

Common Go model (internal/agent/models.go) does not expose raw JSONL field names
such as `type`, `sessionId`, `payload`. Raw field names appear only in fixture
metadata and this mapping document as reference for parser implementors.
All agent-specific evidence is isolated to `Metadata map[string]string`.
