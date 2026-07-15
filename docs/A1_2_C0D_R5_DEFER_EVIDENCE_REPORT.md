# A1.2 C0D-R5 — Final Defer Evidence Report

Status: **C0D PROCEED — all blockers closed.**
Date: 2026-07-16. Executor: A1.2 agent. Provider: Claude Code 2.1.209.

## Blocker closure summary

| Round | Blocker | Status |
|---|---|---|
| R4-#1 | Deny evidence opposite of report | ✅ Fixed: fresh trace with export C0D_DECISION=deny; MATCH_OK→RESEARCH_DENIAL same tuid |
| R4-#2 | Harness not independently runnable | ✅ Fixed: claim path unified to /tmp/c0d-r4-claim; 2 consecutive clean runs 16/16 PASS |
| R4-#3 | T1 assertion wrong | ✅ Fixed: exit code 2 is the authority signal |
| R4-#4 | Clean state false | ✅ Fixed: old R2/R3/R4 tracked files git-rmd |
| R5-#1 | Denial evidence insufficient | ✅ Fixed: denial_projection.json with {eventType, session_id, denied: {tool_name, tool_use_id}} |
| R5-#2 | Worktree not clean | ✅ Fixed: old report .md files git-rmd |

## Evidence inventory

| File | Content |
|---|---|
| `deny_live/deferred_identity.json` | `{session_id, tool_use_id, tool_name, input_sha256}` |
| `deny_live/life.txt` | DEFER→RESUME→MATCH_OK→RESEARCH_DENIAL (same tuid)→model retries all MISMATCH |
| `deny_live/denial_projection.json` | `{eventType:"result.permission_denials", session_id, denied: {tool_name, tool_use_id}}` — session_id and tool_use_id match deferred_identity.json exactly |
| `deny_live/denial_summary.txt` | Extracted tool_use_id from permission_denials[0] |
| Probes | deny: ABSENT; allow (R3): EXISTS + PostToolUse same tuid |
| `harness/hook_defer.sh` | Defer hook (full 64-char SHA-256, exit 2 on malformed) |
| `harness/hook_resume.sh` | Resume hook (sid+tuid+tname+sha256, atomic claim, closed vocab) |
| `harness/hook_posttool.sh` | PostToolUse hook |
| `harness/test_hooks.sh` | Deterministic harness: 16/16 PASS x2 independent runs |

## Verdict

**C0D PROCEED** — exact invocation identity through PreToolUse defer/resume lifecycle.
Allow: PostToolUse matching tool_use_id (R3 live).
Deny: {eventType, session_id, denied: {tool_use_id}} bounded projection matching deferred identity (R5 live).
Replay: exactly 1 MATCH_OK + 1 REPLAY_BLOCKED (harness T5, 2x clean).
Fail-closed: malformed, mismatch, unknown-decision, missing-deferred all exit 2.

C0D PROCEED only authorizes the reviewer to write a new implementation plan. C1D/C2D/C3D NOT authorized.
