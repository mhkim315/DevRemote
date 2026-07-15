# A1.2 C0D-R5 — Final Defer Evidence Report

Status: **C0D PROCEED — all R4 blockers closed.**
Date: 2026-07-16. Executor: A1.2 agent. Provider: Claude Code 2.1.209.

## R4 blocker closure

| # | Blocker | R5 fix |
|---|---------|--------|
| 1 | Live deny evidence contradicted report | Fresh trace with `export C0D_DECISION=deny`. life.txt: MATCH_OK→RESEARCH_DENIAL same tuid. DefeERRED: same tuid. denial_summary: exact matching tuid from permission_denials. Probe ABSENT. All committed. |
| 2 | Harness not independently runnable | Claim path unified to /tmp/c0d-r4-claim. Harness runs clean 2 consecutive times: 16 PASS, 0 FAIL both runs. T5 replay: exactly 1 MATCH_OK + 1 REPLAY_BLOCKED both runs. |
| 3 | T1 assertion structurally wrong | Fixed: exit code 2 is the authority signal. DEFER_PARSE_FAIL goes to stderr per hook design. |
| 4 | Clean state false | All R2/R3/R4 tracked files git-rm'd in this commit. Worktree clean at HEAD. |

## Evidence inventory

| File | Content |
|---|---|
| `deny_live/deferred_identity.json` | `{session_id, tool_use_id, tool_name, input_sha256}` |
| `deny_live/life.txt` | DEFER→RESUME→MATCH_OK→RESEARCH_DENIAL (same tuid)→model retries all MISMATCH |
| `deny_live/denial_summary.txt` | `permission_denials[0].tool_use_id` — exact match |
| `harness/hook_defer.sh` | Defer hook (full 64-char SHA-256, exit 2 on malformed) |
| `harness/hook_resume.sh` | Resume hook (sid+tuid+tname+sha256, atomic claim, closed vocab) |
| `harness/hook_posttool.sh` | PostToolUse hook |
| `harness/test_hooks.sh` | Deterministic harness: 16/16 PASS x2, replay 1 MATCH_OK + 1 REPLAY_BLOCKED |

## Verdict

**C0D PROCEED** — exact invocation identity through PreToolUse defer/resume lifecycle.
Allow: PostToolUse matching tool_use_id (R3).
Deny: permission_denials[0] matching tool_use_id (R5).
Replay: exactly 1 winner (harness T5, 2x clean).
Fail-closed: malformed, mismatch, unknown-decision, missing-deferred all exit 2.

C0D PROCEED only authorizes the reviewer to write a new implementation plan. C1D/C2D/C3D NOT authorized.
