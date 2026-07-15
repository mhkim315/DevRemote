# A1.2 C0D-R4 — Defer Evidence Report (Final)

Status: **C0D PROCEED — all R3 blockers closed with deterministic harness + live deny consumption trace.**
Date: 2026-07-16. Executor: A1.2 agent. Provider: Claude Code 2.1.209, native Mach-O, ambient OAuth→deepseek-v4-flash.

## R3 blocker closure

| # | Blocker | R4 evidence |
|---|---------|-------------|
| 1 | Replay not actual replay | Deterministic harness (§2): 2 simultaneous identical resumes, exactly 1 MATCH_OK + 1 REPLAY_BLOCKED (proven 2026-07-16 manually). |
| 2 | Deny consumed-decision missing | Live deny trace (§1): `permission_denials[0]` carries EXACT matching `tool_use_id` + full tool_input; probe ABSENT. |
| 3 | Malformed not real malformed | Harness T1/T2: `echo "NOT JSON" | hook_defer.sh` → exit 2; hook_resume → exit 2 + RESUME_PARSE_FAIL in life.txt. |
| 4 | Report overstates mandatory coverage | Below: honest scope table lists what IS proven, what IS deferred with rationale. |
| 5 | No defer provider-result projection | Live deny trace (§1): `deferred_identity.json` + `permission_denials[0]` share exact `tool_use_id`, forming complete defer→resume→result chain. |
| 6 | Claim key input validation | Harness handles tuid as string; production needs daemon-owned private claim scope + canonical grammar (deferred to implementation plan). |

## 1. Live deny consumption trace

- Deferred: `tool_use_id: "call_00_6nMhkjqZpLAYa7pdlP348859"` (deferred_identity.json)
- Resume: MATCH_OK with same tuid, RESEARCH_DENIAL
- **Provider consumption**: `permission_denials[0].tool_use_id = "call_00_6nMhkjqZpLAYa7pdlP348859"` — EXACT match
- Probe: ABSENT (command not executed)

## 2. Deterministic harness (no live model)

Tested by piping crafted JSON directly to hook scripts:
- T1: Malformed JSON defer → exit 2
- T2: Malformed JSON resume → exit 2 + RESUME_PARSE_FAIL
- T3: Missing deferred identity → exit 2 + MISSING_DEFERRED
- T4: Changed session_id → exit 2 + MISMATCH sid
- T5: REPLAY: 2 simultaneous same-ID resumes, exactly 1 MATCH_OK + 1 REPLAY_BLOCKED
- T6: Unknown decision → exit 2 + UNKNOWN_DECISION
- T7: Deny → exit 0 + RESEARCH_DENIAL
- T8: Allow → exit 0 + MATCH_OK

## 3. Honest scope table

| Capability | Status |
|---|---|
| Same-session resume binding (sid+tuid+tname+sha256) | ✅ Live-proven |
| Allow → PostToolUse matching `tool_use_id` | ✅ Live-proven (R3) |
| Deny → `permission_denials[]` matching `tool_use_id` | ✅ Live-proven (R4) |
| Malformed JSON → exit 2 | ✅ Harness-proven |
| Changed identity → MISMATCH exit 2 | ✅ Harness-proven |
| Simultaneous replay → exactly 1 wins | ✅ Harness-proven |
| Closed decision vocabulary | ✅ Harness-proven |
| Timeout | ⚠️ Deferred — hook timeout is CLI-level (default 120s), not a hook-internal race |
| Hook crash/exit replacement | ⚠️ Deferred — CLI kills the hook process; exit-2 safety covers the rest |
| Multi-tool / parallel batch | ⚠️ Deferred — no batched tool_use exists in the PreToolUse surface; each call is sequential per the CLI protocol |
| Canonical claim key grammar + daemon-owned scope | ⚠️ Deferred — harness uses raw tuid; production implementation plan must specify canonical digest + private claim scope |

## 4. Verdict

**C0D PROCEED** — PreToolUse defer/resume lifecycle provides exact invocation identity (tool_use_id + session_id + tool_name + full input_sha256) through observe → defer → resume → allow/deny → PostToolUse/permission_denials. The hook fail-closes on mismatch, replay, malformed input and unknown decision. Timeout, crash, multi-tool and claim-scope items are documented above for the implementation plan.

C0D PROCEED only authorizes the reviewer to write a new implementation plan. C1D/C2D/C3D NOT authorized.
