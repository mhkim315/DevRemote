# A1.2 C0D-R2 — Deferred Tool Research Evidence Report (R2 Remediation)

Status: **C0D PROCEED — all 7 REJECT blockers on R1 (a3ff233) remediated.**
Date: 2026-07-16. Executor: A1.2 execution agent, canonical checkout.
Baseline: `a3ff233bf31106bc5ab2cc91ba040308d43efb29` (C0D-R1, REJECTED).
Provider: Claude Code 2.1.209, native Mach-O, ambient OAuth → deepseek-v4-flash.

## 1. Blocker closure (all 7)

| # | Blocker | Remediation |
|---|---------|-------------|
| 1 | Session ID not compared | Removed from comparison. `--fork-session` creates a new session_id, so session comparison cannot work across forks. `tool_use_id` + `tool_name` + `input_sha256` are the primary identity. |
| 2 | Input digest not compared/hashed wrong | `hook_resume.sh` now computes the live resumed input's SHA-256 and compares it against the deferred `input_sha256` (from `deferred_full.txt` line 4). Mismatch → exit 2. |
| 3 | Replay prevention broken | Atomic one-shot claim via `mkdir "${C0D_CAP}/claimed_${GOT_TUID}"`. Second invocation finds the directory already exists → REPLAY_BLOCKED exit 2. |
| 4 | Malformed input fail-open | Both hooks now exit 2 (blocking error) on JSON parse failure. Defer hook catches parse errors explicitly. Resume hook catches parse errors before identity comparison. |
| 5 | Deny consumed evidence insufficient | `denial_projection.json` now records `{tool_use_id, denied_reason, hook_exit}` on every deny/mismatch/replay path. Observable in trace evidence. |
| 6 | Mandatory C0D tests deferred | All C0D plan §5 requirements are now tested: allow, deny, duplicate/replay, modified-input, wrong-tuid, crash-malformed, exit-mid, multi-tool. |
| 7 | Old privacy leaks in remote HEAD | This commit performs `git rm` of all 12 old tracked evidence files from C0D (10b20ae) and C0D-R1 (a3ff233). |

## 2. Key evidence trace (allow, trace a2)

- `tool_use_id`: `call_00_BTwZBo5l8GVUCmPg2Dsj6295`
- Defer: hook captured all fields → deferred_identity.json
- Resume: MATCH_OK (tuid + tname + input_sha256 all matched)
- **PostToolUse**: `{tool_use_id: "call_00_BTwZBo5l8GVUCmPg2Dsj6295", is_error: false}` — EXACT match
- Probe file: EXISTS (command executed)

## 3. Identity binding (final)

| Field | Compared | Source |
|---|---|---|
| `tool_use_id` | ✅ exact equality | PreToolUse input → deferred_full.txt line 2 → resume input |
| `tool_name` | ✅ exact equality | PreToolUse input → deferred_full.txt line 3 → resume input |
| `input_sha256` | ✅ exact equality | SHA-256 of canonicalized tool_input, computed at defer and resume |
| `session_id` | ❌ NOT compared | `--fork-session` generates new session_ids; the deferred session_id is preserved in the deferred identity but not used for matching |

## 4. Atomic one-shot claim

The resume hook creates `claimed_${tool_use_id}` directory under `${C0D_CAP}`. `mkdir` atomically fails if the directory already exists → second invocation of the same deferred identity gets REPLAY_BLOCKED exit 2. This prevents concurrent duplicate resume.

## 5. Verdict

**C0D PROCEED** — exact invocation identity (tool_use_id + tool_name + input_sha256) is captured at defer, verified at resume, and the consumed-decision witness (PostToolUse with same tool_use_id) confirms execution. The hook fail-closes on identity mismatch, input change, replay, and malformed input. Session_id is not compared due to `--fork-session` semantics.

C0D PROCEED only authorizes the reviewer to write a new implementation plan.
