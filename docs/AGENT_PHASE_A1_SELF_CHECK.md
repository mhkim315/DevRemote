# Phase A1 — Self-Check

Date: 2026-07-07

## JSON/JSONL Validation

```sh
python3 -c "
import json, os
base='companion-daemon/internal/agent/testdata'
errors=0; files=0
for r,d,fs in os.walk(base):
    for f in fs:
        path=os.path.join(r,f)
        try:
            with open(path) as fp: c=fp.read()
            if f.endswith('.json'): json.loads(c)
            elif f.endswith('.jsonl'):
                for l in c.strip().split('\n'):
                    if l.strip(): json.loads(l)
            files+=1
        except Exception as e:
            print(f'FAIL {os.path.relpath(path,base)}: {e}')
            errors+=1
print(f'{files} files, {errors} errors')
"
```

Result: `13 files, 0 errors`

## Acceptance Criteria Check

| Criteria | Status | Evidence |
|----------|--------|----------|
| 2+ agents | ✅ | Claude, Codex, Antigravity (3) |
| 3+ scenarios/agent | ✅ | Claude(4), Codex(3), Antigravity(3) |
| tool call fixture | ✅ | Claude tool_use/tool_result, Antigravity TOOL_CALL/TOOL_RESULT |
| approval/waiting fixture | ✅ | Claude permission-mode=ask, Codex waiting_for_approval+approval_resolved |
| malformed/unknown fixture | ✅ | All 3 agents have malformed.jsonl |
| redaction rules | ✅ | metadata.json per agent with redactions list |
| JSON/JSONL valid | ✅ | 13 files, 0 errors |
| No raw secrets | ✅ | All UUIDs, paths, tokens redacted |

## expectedEvents → Common AgentEvent Mapping

| Agent | Raw Source Types | Common Events |
|-------|-----------------|---------------|
| Claude | user, assistant, permission-mode, unknown_event_type | user_message, thinking, tool_call_started, tool_call_finished, approval_requested, unknown |
| Codex | session_meta, event_msg, response_item, unknown_msg_type | agent_started, user_message, approval_requested, approval_resolved, unknown |
| Antigravity | USER_INPUT, AGENT_OUTPUT, TOOL_CALL, TOOL_RESULT, UNKNOWN_TYPE | user_message, assistant_message, tool_call_started, tool_call_finished, unknown |

## Redaction Token Set (Final)

```
<HOME> <USER> <PROJECT> <TOKEN> <PROMPT> <UUID> <PATH> <CMD> <IP> <EMAIL>
```

No `<PRIVATE_PATH>` or other variant anywhere in repo.
