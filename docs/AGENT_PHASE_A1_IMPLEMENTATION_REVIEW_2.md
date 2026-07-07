# Agent Adapter Phase A1 Review 2

Date: 2026-07-07

Executor commit under review: `04785c177`

Verifier decision: **REJECT**

## Summary

This commit fixes several previous blockers:

- Antigravity `metadata.json` is now valid JSON.
- Each of Claude, Codex, and Antigravity has at least three fixture scenarios.
- The fixture set includes tool-call, approval/waiting, and malformed/unknown scenarios.
- No obvious raw home path, token, UUID, private IP, or email leak was found in committed
  fixture files.

However, Phase A1 is still blocked because fixture metadata is not yet reliable enough
to become the Phase A2/A3 source of truth.

## Verification performed

```sh
git diff --check HEAD~1..HEAD
```

Result: passed.

JSON/JSONL validation:

```sh
python3 - <<'PY'
import json, pathlib, sys
root=pathlib.Path('companion-daemon/internal/agent/testdata')
ok=True
for p in sorted(root.rglob('*')):
    if not p.is_file():
        continue
    if p.suffix == '.json':
        json.loads(p.read_text())
    elif p.suffix == '.jsonl':
        for line in p.read_text().splitlines():
            if line.strip():
                json.loads(line)
sys.exit(0 if ok else 1)
PY
```

Result: passed.

Fixture count check:

```text
claude       4 jsonl fixtures, 12 lines
codex        3 jsonl fixtures, 9 lines
antigravity  3 jsonl fixtures, 7 lines
```

Redaction spot check:

```sh
rg -n "/Users/|/home/|Bearer |sk-[A-Za-z0-9]|api[_-]?key|token|secret|mhk|Documents/codex|github\\.com|@|192\\.168|10\\.[0-9]|172\\.(1[6-9]|2[0-9]|3[0-1])|[A-Fa-f0-9]{8}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{12}" companion-daemon/internal/agent/testdata docs/AGENT_PHASE_A1_LOG_INVENTORY.md docs/AGENT_ADAPTER_LAYER_PLAN.md
```

Result: no raw secret/path/token was found in committed fixtures. Documentation contains
generic words such as `token` and example redaction tokens; those are acceptable.

## Findings

### P1 — `expectedEvents` mixes raw agent event names with the common AgentEvent contract

Files:

- `companion-daemon/internal/agent/testdata/claude/metadata.json`
- `companion-daemon/internal/agent/testdata/codex/metadata.json`
- `companion-daemon/internal/agent/testdata/antigravity/metadata.json`

The approved plan defines common event types in `docs/AGENT_ADAPTER_LAYER_PLAN.md`:

```text
agent_started
user_message
assistant_message
thinking
tool_call_started
tool_call_finished
approval_requested
approval_resolved
waiting_input
completed
failed
interrupted
unknown
```

But committed metadata declares raw or non-canonical names:

```json
// claude
["user_message", "thinking", "tool_use", "tool_result", "approval_requested"]

// codex
["session_meta", "task_started", "user_message", "waiting_for_approval"]

// antigravity
["user_message", "agent_output", "tool_call", "tool_result"]
```

Why this blocks Phase A1:

- Phase A1 metadata is the input for Phase A2 common model validation and Phase A3
  contract harness.
- If metadata uses raw source names, the contract harness will validate adapter-specific
  semantics instead of Common AgentEvent semantics.
- This recreates the exact coupling the Agent Adapter Layer is supposed to prevent.

Required fix:

- Keep raw source names under a separate key such as `rawEvents`, `observedSourceTypes`,
  or per-fixture `records`.
- Make `expectedEvents` use only common AgentEvent names from the plan.
- Suggested mapping:

```text
Claude tool_use              → tool_call_started
Claude tool_result           → tool_call_finished
Codex session_meta           → agent_started
Codex task_started           → thinking or working status evidence, not necessarily event
Codex waiting_for_approval   → approval_requested or waiting_input
Antigravity AGENT_OUTPUT     → assistant_message
Antigravity TOOL_CALL        → tool_call_started
Antigravity TOOL_RESULT      → tool_call_finished
```

If a raw event should not become a Common AgentEvent, do not list it under
`expectedEvents`.

### P1 — Antigravity metadata `entryCount` is incorrect

File: `companion-daemon/internal/agent/testdata/antigravity/metadata.json`

Metadata says:

```json
"entryCount": 6
```

Actual committed JSONL line count:

```text
user_agent.jsonl   2
tool_call.jsonl    2
malformed.jsonl    3
total              7
```

Why this blocks Phase A1:

- Metadata is meant to describe committed fixtures precisely.
- A future contract harness will likely use metadata counts to check fixture integrity.

Required fix:

- Set `entryCount` to `7`, or replace it with per-fixture counts:

```json
"fixtures": {
  "user_agent.jsonl": {"entryCount": 2, ...},
  "tool_call.jsonl": {"entryCount": 2, ...},
  "malformed.jsonl": {"entryCount": 3, ...}
}
```

### P1 — Redaction token set is still not fully canonical

Files:

- `docs/AGENT_ADAPTER_LAYER_PLAN.md`
- `docs/AGENT_PHASE_A0_ACCEPTANCE.md`

`docs/AGENT_ADAPTER_LAYER_PLAN.md` still lists `<PRIVATE_PATH>` in the recommended token
block, while the redaction table and all committed metadata use `<PATH>`.

Why this blocks Phase A1 acceptance:

- A0 acceptance explicitly required token normalization before redacted fixtures were
  committed.
- Fixtures are now committed.
- The canonical token set must be unambiguous before more fixture metadata is added.

Required fix:

- Remove `<PRIVATE_PATH>` or replace it with `<PATH>`.
- Do not edit old acceptance text to rewrite history unless intentionally adding an
  erratum. The current plan and handoff should be canonical.

### P2 — Validation command is not documented in A1 inventory

File: `docs/AGENT_PHASE_A1_LOG_INVENTORY.md`

The previous review asked for a simple JSON/JSONL validation command to be recorded so
this cannot regress. The command was not added to the A1 inventory or a Phase A1
self-check document.

Required fix:

- Add a `Validation` section with the JSON/JSONL validation command and result.

## Required executor actions

1. Change fixture metadata `expectedEvents` to common AgentEvent names only.
2. Move raw source names to a separate metadata field if they are useful.
3. Fix Antigravity `entryCount`.
4. Normalize the redaction token set in the current plan/handoff docs.
5. Add a JSON/JSONL validation command and result to the Phase A1 inventory or a Phase A1
   self-check document.
6. Keep raw logs out of the repository.

## Next phase permission

**BLOCKED**

Do not proceed to Phase A2 until metadata uses common AgentEvent semantics and accurately
describes the committed fixture files.
