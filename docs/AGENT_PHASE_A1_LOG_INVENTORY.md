# Phase A1 — Agent Log Inventory

Date: 2026-07-07

## Summary

| Agent | Primary Log | Entries | Format |
|-------|-----------|---------|--------|
| Claude | `~/.claude/projects/*/<uuid>.jsonl` | 80 sessions, ~3K entries/session | JSONL `{type, mode, sessionId, ...}` |
| Codex | `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl` | 3 recent sessions, ~5K entries/session | JSONL `{timestamp, type, payload}` |
| Antigravity | `~/.gemini/antigravity/brain/<uuid>/.system_generated/logs/transcript.jsonl` | 50 sessions, 10-31KB each | JSONL `{step_index, source, type, status, created_at, content}` |

## Claude

- **Path pattern**: `~/.claude/projects/<project-path>/<session-uuid>.jsonl`
- **Format**: JSONL, one JSON object per line
- **Key fields**: `type` (mode, user, assistant, attachment, file-history-snapshot, last-prompt, permission-mode, ai-title), `mode`, `sessionId`
- **History**: `~/.claude/history.jsonl` (1,103 entries) — prompt history only, not agent events
- **Fixtures available**: user message, assistant message, tool call, approval request (from Claude Code sessions)

## Codex

- **Path pattern**: `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`
- **Format**: JSONL, one JSON object per line
- **Key fields**: `timestamp`, `type` (event_msg, response_item, session_meta, turn_context), `payload` (nested object with `type` field)
- **History**: `~/.codex/history.jsonl` (233 entries) — simple prompt/timestamp log
- **Fixtures available**: user message, assistant message, tool call, system event

## Antigravity

- **Path pattern**: `~/.gemini/antigravity/brain/<uuid>/.system_generated/logs/transcript.jsonl`
- **Format**: JSONL, one JSON object per line
- **Key fields**: `step_index`, `source` (USER_EXPLICIT), `type` (USER_INPUT, AGENT_OUTPUT, TOOL_CALL, ...), `status` (DONE), `created_at`, `content`
- **Fixtures available**: user request, agent output, tool call

## Sensitivity Assessment

| Agent | Contains | Risk |
|-------|----------|------|
| Claude | Prompt text, file paths, API keys possible | HIGH — needs thorough redaction |
| Codex | Prompt text, model responses, tool call args | HIGH — needs thorough redaction |
| Antigravity | User request content, agent output, tool args | HIGH — needs thorough redaction |

All agents contain: user prompts, project names, file paths, potential tokens.
Redaction required: `<HOME>`, `<USER>`, `<PROJECT>`, `<TOKEN>`, `<PROMPT>`, `<CODE>`, `<CMD>`, `<PATH>`, `<UUID>`, `<IP>`, `<EMAIL>`.

## Next: Redacted Fixture Collection

Target: minimum 2 agents, 3 scenarios each, with at least 1 tool call + 1 approval/waiting input fixture.
