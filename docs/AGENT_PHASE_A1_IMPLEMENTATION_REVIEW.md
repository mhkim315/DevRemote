# Agent Adapter Phase A1 Review

Date: 2026-07-07

Executor commit under review: `feca0d0db`

Verifier decision: **REJECT**

## Scope verification

The commit stays within the Agent Adapter Phase A1 area:

- adds agent log inventory documentation;
- adds redacted fixture files under `companion-daemon/internal/agent/testdata`;
- does not change production daemon code;
- does not change mobile code.

However, Phase A1 cannot be accepted yet because the committed fixtures are not
contract-ready and do not meet the Phase A1 acceptance criteria.

## Automated checks performed

```sh
git diff --check HEAD~1..HEAD
```

Result: passed.

Fixture parse check:

```sh
python3 - <<'PY'
import json, pathlib
root=pathlib.Path('companion-daemon/internal/agent/testdata')
for p in sorted(root.rglob('*')):
    if not p.is_file():
        continue
    if p.suffix == '.json':
        json.loads(p.read_text())
    elif p.suffix == '.jsonl':
        for line in p.read_text().splitlines():
            json.loads(line)
PY
```

Result: failed on `companion-daemon/internal/agent/testdata/antigravity/metadata.json`.

Redaction spot check:

```sh
rg -n "/Users/|/home/|Bearer |sk-[A-Za-z0-9]|api[_-]?key|token|secret|mhk|Documents/codex|github\\.com|@|192\\.168|10\\.[0-9]|172\\.(1[6-9]|2[0-9]|3[0-1])|[A-Fa-f0-9]{8}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{12}" companion-daemon/internal/agent/testdata docs/AGENT_PHASE_A1_LOG_INVENTORY.md
```

Result: no obvious raw secret/path/token was found in committed fixture files. Some
documentation uses generic sensitivity words such as `token`; that is acceptable.

## Findings

### P0 — Antigravity metadata is invalid JSON

File: `companion-daemon/internal/agent/testdata/antigravity/metadata.json`

The `format.source`, `format.type`, and `format.status` strings contain literal
newlines:

```json
"source": "USER_EXPLICIT |
    AGENT_OUTPUT"
```

This makes the metadata file unparsable:

```text
json.decoder.JSONDecodeError: Invalid control character at: line 9 column 31
```

Why this blocks Phase A1:

- Phase A1 fixtures are supposed to become the basis for Phase A2 model design and
  Phase A3 contract harness.
- Invalid metadata cannot be consumed by tests.
- The fixture set is not contract-ready.

Required fix:

- Make all `metadata.json` files valid JSON.
- Prefer arrays for enum-like values:

```json
"source": ["USER_EXPLICIT", "AGENT_OUTPUT"]
```

### P1 — Phase A1 fixture count acceptance is not met

Phase A1 acceptance requires:

- minimum 2 agents with fixtures;
- at least 3 scenario fixtures per agent;
- at least one tool-call fixture;
- at least one approval or waiting-input fixture;
- at least one malformed or unknown-field fixture.

Current committed fixture files:

```text
claude/user_assistant.jsonl
codex/session_task.jsonl
antigravity/user_agent.jsonl
```

This is one scenario per agent, not three. There is no committed malformed/unknown
fixture. There is no committed approval/waiting-input fixture.

The inventory document says some fixture types are "available", but Phase A1 acceptance
requires committed redacted fixtures, not only availability notes.

Required fix:

- Either narrow the commit/report explicitly to "A1 progress, not complete" and do not
  request Phase A1 acceptance yet, or add the required redacted fixtures.
- For acceptance, include at minimum:
  - 3 redacted scenario fixtures for Claude;
  - 3 redacted scenario fixtures for Codex or Antigravity;
  - at least one tool-call scenario;
  - at least one approval/waiting-input scenario;
  - at least one malformed/unknown-field scenario.

### P1 — Metadata expected events do not match committed fixture contents

File: `companion-daemon/internal/agent/testdata/codex/metadata.json`

The metadata declares:

```json
"expectedEvents": ["agent_started", "user_message", "assistant_message", "tool_call_started"]
```

But `codex/session_task.jsonl` contains only:

- `session_meta`
- `event_msg` with `task_started`
- `response_item` user message

There is no assistant response and no tool call in the committed fixture.

File: `companion-daemon/internal/agent/testdata/antigravity/metadata.json`

The metadata declares:

```json
"expectedEvents": ["user_message", "agent_started"]
```

But `antigravity/user_agent.jsonl` contains:

- `USER_INPUT`
- `AGENT_OUTPUT`

`AGENT_OUTPUT` is more naturally an assistant/agent message than an agent-start event.
If the intended mapping is `agent_started`, the fixture or model note must explain why.

Why this blocks Phase A1:

- Phase A2/A3 will use metadata as parser truth.
- Incorrect expected events make the future contract harness validate the wrong
  behavior.

Required fix:

- Make `expectedEvents` match the actual fixture records.
- Add separate fixtures for tool call and approval/waiting-input rather than declaring
  those events in metadata for a fixture that does not contain them.

### P1 — Phase A0 redaction-token follow-up is only partially resolved

`docs/AGENT_ADAPTER_LAYER_PLAN.md` now mostly uses the newer token set, but the
redaction token list still contains `<PRIVATE_PATH>` while the table and metadata use
`<PATH>`.

Why this matters:

- Fixture metadata now lists applied redactions.
- Token naming should be canonical before additional fixtures are added.

Required fix:

- Choose one canonical token set.
- Update the recommended token list, redaction table, metadata examples, and committed
  metadata consistently.

### P2 — A1 inventory overstates fixture availability

File: `docs/AGENT_PHASE_A1_LOG_INVENTORY.md`

The inventory says:

- Claude fixtures available: user message, assistant message, tool call, approval request
- Codex fixtures available: user message, assistant message, tool call, system event
- Antigravity fixtures available: user request, agent output, tool call

But the committed fixture files do not include all of those scenarios.

Required fix:

- Distinguish "observed in local raw logs" from "committed as redacted fixture".
- For each scenario, record one of:
  - `observed_raw_only`
  - `redacted_committed`
  - `not_found`

## Required executor actions

1. Fix invalid Antigravity metadata JSON.
2. Normalize redaction token names across plan, examples, and metadata.
3. Make metadata `expectedEvents` match actual fixture contents.
4. Add enough redacted fixtures to satisfy Phase A1 acceptance, or clearly mark the next
   commit as A1 progress rather than A1 completion.
5. Add a simple JSON/JSONL validation command to the Phase A1 verification notes so this
   cannot regress.
6. Keep raw logs out of the repository.

## Next phase permission

**BLOCKED**

Do not proceed to Phase A2 until Phase A1 has valid metadata, sufficient redacted
fixtures, and fixture metadata that truthfully describes the committed records.
