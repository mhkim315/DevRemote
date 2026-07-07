# Agent Adapter Phase A1 Acceptance

Date: 2026-07-07

Executor commit under review: `319414685`

Verifier decision: **ACCEPT**

## Scope verification

Phase A1 is accepted as log inventory plus redacted fixture baseline.

Reviewed files:

- `docs/AGENT_PHASE_A1_LOG_INVENTORY.md`
- `docs/AGENT_PHASE_A1_SELF_CHECK.md`
- `docs/AGENT_ADAPTER_LAYER_PLAN.md`
- `companion-daemon/internal/agent/testdata/claude/*`
- `companion-daemon/internal/agent/testdata/codex/*`
- `companion-daemon/internal/agent/testdata/antigravity/*`

No production daemon code or mobile code was changed.

## Acceptance criteria

### 1. Minimum agent coverage

Accepted.

Committed redacted fixtures exist for three agents:

```text
claude
codex
antigravity
```

This exceeds the Phase A1 minimum of two agents.

### 2. Scenario coverage

Accepted.

Committed fixture counts:

```text
claude       4 jsonl fixtures, 12 records
codex        3 jsonl fixtures, 9 records
antigravity  3 jsonl fixtures, 7 records
```

The fixture set includes:

- user message
- assistant/agent message
- tool call
- tool result
- approval or waiting-for-approval
- malformed or unknown records

### 3. JSON/JSONL validity

Accepted.

All committed metadata and fixture files parse as valid JSON/JSONL.

Verified with:

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

### 4. Metadata integrity

Accepted.

`entryCount` now matches actual committed JSONL record counts:

```text
claude       metadata 12, actual 12
codex        metadata 9,  actual 9
antigravity  metadata 7,  actual 7
```

`expectedEvents` now uses Common AgentEvent names rather than raw agent-specific names:

```text
agent_started
user_message
assistant_message
thinking
tool_call_started
tool_call_finished
approval_requested
approval_resolved
unknown
```

Raw source names are separated into `observedSourceTypes`, which is the right boundary
for Phase A2/A3.

### 5. Redaction safety

Accepted.

No obvious raw home path, token, UUID, private IP, email, or local workspace path was
found in committed fixture files during spot check.

Verified with:

```sh
rg -n "/Users/|/home/|Bearer |sk-[A-Za-z0-9]|api[_-]?key|token|secret|mhk|Documents/codex|github\\.com|@|192\\.168|10\\.[0-9]|172\\.(1[6-9]|2[0-9]|3[0-1])|[A-Fa-f0-9]{8}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{12}" companion-daemon/internal/agent/testdata docs/AGENT_PHASE_A1_LOG_INVENTORY.md docs/AGENT_ADAPTER_LAYER_PLAN.md docs/AGENT_PHASE_A1_SELF_CHECK.md
```

The matches were generic documentation words, redaction-token examples, or already
redacted fixture values such as `<UUID>` and `<HOME>/<PROJECT>`.

The current canonical token set in the plan and fixture metadata is:

```text
<HOME> <USER> <PROJECT> <TOKEN> <PROMPT> <UUID> <PATH> <CMD> <IP> <EMAIL>
```

## Non-blocking notes

### P2 — Self-check wording is slightly too broad

`docs/AGENT_PHASE_A1_SELF_CHECK.md` says:

```text
No `<PRIVATE_PATH>` or other variant anywhere in repo.
```

That is true for the current plan and fixture metadata, but not literally true for old
review/acceptance documents that mention `<PRIVATE_PATH>` as historical context.

This is not a blocker because:

- current plan/metadata use the canonical token set;
- historical review documents should not be rewritten just to erase past findings;
- Phase A2 should read the current plan and accepted A1 fixture metadata, not old
  rejected-review text.

If desired, the wording can be changed later to:

```text
No `<PRIVATE_PATH>` remains in the current plan or fixture metadata.
```

## Verification commands

```sh
git diff --check HEAD~1..HEAD
```

Passed.

JSON/JSONL validation command above: passed.

Metadata count check:

```sh
python3 - <<'PY'
import json, pathlib, sys
root=pathlib.Path('companion-daemon/internal/agent/testdata')
ok=True
for meta in sorted(root.glob('*/metadata.json')):
    d=json.loads(meta.read_text())
    actual=sum(len(p.read_text().splitlines()) for p in meta.parent.glob('*.jsonl'))
    if d.get('entryCount') != actual:
        print('entry count mismatch', meta, d.get('entryCount'), actual)
        ok=False
sys.exit(0 if ok else 1)
PY
```

Passed.

No Go/mobile test was necessary because Phase A1 only adds documentation and redacted
testdata fixtures.

## Next phase permission

**ALLOWED**

Phase A2 may begin.

Required constraints for Phase A2:

1. Treat `expectedEvents` as Common AgentEvent expectations.
2. Treat `observedSourceTypes` as raw adapter/source evidence only.
3. Do not let Claude-specific JSONL field names leak into the common model.
4. Keep raw logs out of the repository.
5. Preserve parser failure isolation: malformed fixture records must not imply terminal
   session failure.
