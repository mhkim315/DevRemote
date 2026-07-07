# Phase A8-prep — Antigravity Fixture Collection

Date: 2026-07-07
Baseline: A7 accepted

## Goal

Antigravity CLI 세션의 실제 로그를 수집하여 redacted fixture로 변환.
A8 구현은 fixture가 확보된 후에만 착수.

## Source

`~/.gemini/antigravity/brain/<uuid>/.system_generated/logs/transcript.jsonl`

Confirmed types (from actual sessions):
- `USER_INPUT` (source=USER_EXPLICIT)
- `VIEW_FILE` (source=MODEL)
- `PLANNER_RESPONSE` (source=MODEL)
- `SEARCH_WEB`, `LIST_DIRECTORY` — tool calls
- `CHECKPOINT`, `ERROR_MESSAGE`, `GENERIC` — system events
- `EPHEMERAL_MESSAGE` (source=SYSTEM)

## Fixture Plan

Three scenarios from actual logs:
1. **user_agent.jsonl**: USER_INPUT → model responses (VIEW_FILE, PLANNER_RESPONSE)
2. **tool_call.jsonl**: SEARCH_WEB, LIST_DIRECTORY tool executions
3. **malformed.jsonl**: UNKNOWN type, missing fields

All redacted: `<HOME>`, `<USER>`, `<PROJECT>`, `<TOKEN>`, `<PROMPT>`, `<UUID>`, `<PATH>`, `<CMD>`.

## Acceptance

- 3+ scenario fixtures from actual Antigravity logs
- Redacted per A1 token rules
- metadata.json with expectedEvents using common AgentEventType
- No raw secrets committed
