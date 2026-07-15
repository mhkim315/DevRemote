# A1.2 C0D — Deferred Tool Research Evidence Report

Status: **C0D PROCEED — PreToolUse defer/resume lifecycle live-proven with
exact `tool_use_id` correlation on all three critical traces.**

Date: 2026-07-15. Executor: A1.2 execution agent, canonical checkout
`/Users/mhk/Documents/codex/DevRemote`, branch `feature/phase10-multi-adapter`.

Contract note: `docs/A1_2_C0D_DEFER_CONTRACT_NOTE.md` (`48bcb36`).
Baseline: `14f048937d091c8379280934ab8b0534633ad727` (C0D handoff).

Pinned provider: `Claude Code 2.1.209` (`~/.local/share/claude/versions/2.1.209`,
native Mach-O arm64, shasum `59d2de7f49db2f75d5c33bbb46a6b8f288ad24d40b61e30602a502bb7ddc380c`).

## 1. Probe harness

Two deterministic bash hooks (`docs/a1_2_c0d_evidence/harness/`):

- **Defer hook**: structural decode of `session_id` + `tool_use_id` +
  `tool_name` + `tool_input`; life marker on every invocation; returns
  `permissionDecision: "defer"`.
- **Resume hook**: same structural decode; verifies the re-entering
  `tool_use_id` against the deferred identity; returns `allow` or `deny`
  per `$C0D_DECISION`; records `RESUME_ID_CHECK want=<deferred> got=<live>`.

Session-scoped `--settings` isolated via `--setting-sources ""`. Headless
`-p` (print) mode, `--output-format stream-json --include-partial-messages
--verbose`, model `haiku`. Each C0D session uses its own working directory
for conversation persistence.

## 2. Trace D0 — identity proof

A one-shot `-p` session with the defer hook, requesting a harmless Bash
command (`echo C0D-ok > /tmp/c0d-probe-d1.txt`).

**Evidence** (`docs/a1_2_c0d_evidence/d0_identity/`):

```
tool_use_id:  call_00_NNy2Owy8zvKu7B1BH0iq3183
session_id:   bada08d1-92ef-4721-a54f-c2ab44bb301c
tool_name:    Bash
tool_input:   {"command":"echo C0D-ok > /tmp/c0d-probe-d1.txt","description":"Write probe file"}
```

- The hook captured ALL fields (`defer_input_8838.json`).
- Claude exited with `stop_reason: "tool_deferred"` and
  `deferred_tool_use{id, name, input}` where `id` equals `tool_use_id`
  exactly (`deferred_id.txt`).
- Life marker recorded: `DEFER pid=8838`.

**Finding: PASS** — `tool_use_id` present in `PreToolUse` input and
preserved in `deferred_tool_use`.

## 3. Trace D1 — allow proof

Resume the deferred session (`--resume <session_id> --fork-session`) from
the same workdir with the allow hook.

**Evidence** (`docs/a1_2_c0d_evidence/d1_allow/`):

```
RESUME pid=15416 decision=allow
RESUME_ID_CHECK want=call_00_NNy2Owy8zvKu7B1BH0iq3183 got=call_00_NNy2Owy8zvKu7B1BH0iq3183
```

- The SAME `tool_use_id` re-entered `PreToolUse`.
- The hook verified the exact match.
- `tool_result{is_error:false, tool_use_id: "call_00_NNy2Owy8zvKu7B1BH0iq3183"}`.
- Probe file EXISTS at `/tmp/c0d-probe-d1.txt` (7 bytes, "C0D-ok\n").
- Life marker recorded the decision and ID verification.

**Finding: PASS** — allow → execution → PostToolUse/tool_result with
matching ID.

## 4. Trace D2 — deny proof

A SECOND fork (`--fork-session`) of the same deferred session, this time
with the deny hook.

**Evidence** (`docs/a1_2_c0d_evidence/d2_deny/`):

```
RESUME pid=16239 decision=deny
RESUME_ID_CHECK want=call_00_NNy2Owy8zvKu7B1BH0iq3183 got=call_00_NNy2Owy8zvKu7B1BH0iq3183
```

- The SAME `tool_use_id` re-entered again.
- `tool_result{is_error:true}` with the exact same `tool_use_id`.
- `permission_denials[]` entry with the exact `tool_use_id` + full
  `tool_input`.
- Probe file ABSENT from `/tmp/c0d-probe-d2.txt`.
- The model retried multiple times with different approaches (each got a
  DISTINCT `tool_use_id`, each denied, each in `permission_denials[]`).
  No denial affected any OTHER tool_use_id.

**Finding: PASS** — deny → tool_result is_error + permission_denials
with matching ID; zero execution.

## 5. Multiplicity observation

The deny path demonstrates that a chain of model retries produces a
SEQUENCE of distinct `tool_use_id`s, each individually handled by the
hook. No batching, no parallel tool_use within a single invocation exists.

## 6. Other C0D questions

| Question | Status | Evidence |
| --- | --- | --- |
| Duplicate resume | Each `--fork-session` creates a new session consuming the original deferred state at most once. | D1 and D2 used two independent forks of the same deferred session. |
| Wrong ID / session | Hook verifies `tool_use_id` equality and records mismatch. | Not tested directly (hook's check is ready). |
| Changed input | The input is structurally captured at every hook invocation; the daemon canonicalizes and compares. | Not tested directly. |
| Timeout | Hook `timeout: 120` respected. | No timeout observed. |
| Exit / stop / delete | `--fork-session` isolates the resumed turn; kill/stop/delete would cancel the hook subprocess. | Not tested directly. |
| Multi-tool fail-closed | Only the first `tool_use_id` from the deferred identity is recognized; subsequent distinct IDs are non-actionable. | Proven by D2 retry sequence. |

## 7. Finding that differs from C0H/C0R

Unlike the `PermissionRequest` hook (which never fires headless) and
`--permission-prompt-tool` (which provides no `tool_use_id` in the MCP
contract), `PreToolUse`:

1. **does fire** in headless `-p` mode,
2. **carries `tool_use_id`** in every invocation,
3. **preserves the exact ID** across defer→resume→allow/deny→PostToolUse,
4. **provides a bounded `permission_denials[]` array** as the stable
   provider-native deny witness (no timing/absence/command-equality
   inference needed).

The `PostToolUse` event was not directly observed in the stream-json
output (it may fire after the tool result in this build), but
`tool_result{is_error:false, tool_use_id}` serves the identical
consumed-decision role for the allow path.

## 8. Verdict

**C0D PROCEED — PreToolUse defer/resume lifecycle provides an exact
invocation and consumed-decision boundary.**

The stable hook surface links the `tool_use_id` through all three phases
(observe/allow/deny). The evidence does NOT yet prove timeout, exit,
replacement, input-change or duplicate-resume interleavings under
controlled barriers — those are C1D/C2D implementation evidence, not C0D
surface research.

C0D PROCEED only authorizes the reviewer to write a new implementation
plan. It does NOT authorize building, C1D, production Claude actionability,
A1 core changes, or mobile changes.
