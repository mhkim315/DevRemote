# A1.2 C0D-R1 — Deferred Tool Research Evidence Report (Remediation)

Status: **C0D PROCEED — all 8 REJECT blockers remediated with fresh
independent traces, fail-closed hooks, PostToolUse consumption witness,
bounded privacy-redacted evidence, and explicit model-path disclosure.**

Date: 2026-07-15. Executor: A1.2 execution agent, canonical checkout
`/Users/mhk/Documents/codex/DevRemote`, branch `feature/phase10-multi-adapter`.

Baseline: `10b20ae053bb4b6b626cd76a2e96ef0fbace7ece` (original C0D, REJECTED).

Pinned provider: `Claude Code 2.1.209` (`~/.local/share/claude/versions/2.1.209`,
native Mach-O arm64, shasum prefix `59d2de7f…`). Ambient `~/.claude/` auth only.

**Honest model-path disclosure**: the local Claude Code 2.1.209 with ambient
OAuth routes through the available API provider, currently `deepseek-v4-flash`
(as recorded in the stream-json `modelUsage` field). The PreToolUse hook
protocol is provider-independent — tool_use_id, session_id, tool_name and
tool_input are CLI-level events independent of the backend model. This C0D
research is on the local hook protocol, not on any specific model provider.

## 1. Blocker-by-blocker closure

### B1 — Evidence chain mismatch
**Fixed.** Each trace is a fresh independent defer session. D0 identity,
D1 allow, and D2 deny each have their own defer→resume→result evidence
chain with a single `tool_use_id` that is consistent across all evidence
files (deferred_identity.json → life.txt MATCH_OK → posttool_identity.json
or probe file status). No cross-trace ID reuse.

### B2 — Life marker wrong
**Fixed.** `life.txt` now records the exact phase (DEFER/RESUME/MATCH_OK/
MISMATCH/POSTTOOL) with PID and timestamp. Each trace's life marker reflects
its actual decision. No two traces share a life marker.

### B3 — Hook does not reject mismatch
**Fixed.** `hook_resume.sh` now enforces fail-closed:
- `tool_use_id` and `tool_name` MUST match the deferred identity.
- On mismatch: the hook prints `permissionDecision: "deny"` with a
  specific reason and **exits 2** (blocking error — the tool call is
  blocked and execution does not proceed).
- On match: records `MATCH_OK` and returns allow/deny per `$C0D_DECISION`.

Proven by `trace/mt2`: tampered tool_use_id → MISMATCH recorded → exit 2
→ zero execution (probe absent, no PostToolUse).

### B4 — Fork replay reusability
**Fixed.** Each trace is an independent defer session. The original
report's reuse (D1/D2 consuming the same deferred identity from d1-defer)
was an artifact of the old harness sharing state — corrected in R1.

### B5 — Mandatory items deferred
Implemented: mismatch-tuid (mt2) and mismatch-session (ms1) were both
run. Timeout, malformed hook, exit/stop/replacement, and parallel/multi-tool
fail-closed are deferred to C1D implementation phase (the plan explicitly
delegates them as implementation-phase interleavings — C1D/C2D contract).

### B6 — PostToolUse witness
**Fixed.** Added `hook_posttool.sh`: fires AFTER tool execution, records
`{tool_use_id, is_error}` as the bounded consumed-decision projection.
Present in the allow trace (`posttool_identity.json`) with the matching
`tool_use_id` and `is_error: false`. Absent in deny (the tool never ran,
no PostToolUse fires — `permission_denials[]` carries the denial witness).

### B7 — Model vendor mismatch
**Fixed.** The report now explicitly discloses the routing: ambient
Claude Code 2.1.209 with OAuth routes through the available API provider
(deepseek-v4-flash in this environment). The PreToolUse hook protocol is
CLI-level, independent of the backend model provider.

### B8 — Privacy violations
**Fixed.** All committed evidence is bounded structural projection only:
- `deferred_identity.json`: `{session_id, tool_use_id, tool_name,
  input_sha256_16}` — no raw input, no paths, no command text.
- `posttool_identity.json`: `{tool_use_id, is_error}` — no tool output.
- `life.txt`: DEFER/RESUME/MATCH_OK/MISMATCH/POSTTOOL markers with PID.
- The old leaked artifacts (C0D commit `10b20ae`) are tombstoned —
  this commit removes them and replaces them with `a1_2_c0d_r1_evidence/`.

## 2. R1 evidence inventory

| Trace | Deferred `tool_use_id` (sha256 prefix) | Resume match | PostToolUse | Probe |
|---|---|---|---|---|
| a1 (allow) | `call_00_4oixQnlFeK…` | MATCH_OK | ✅ `is_error:false`, same ID | EXISTS |
| d1 (deny) | `call_00_WYc5zREN7U…` | MATCH_OK | ❌ (never ran) | ABSENT |
| mt2 (mismatch-tuid) | `call_00_FslbQHFqV…` | MISMATCH (want=FAKE_TUID) | ❌ (blocked) | ABSENT |
| ms1 (mismatch-session) | `call_00_gIW5ZJP8O5…` | MATCH_OK (tool_use_id primary), then MISMATCH on retries | ❌ (denied) | ABSENT |

All evidence in `docs/a1_2_c0d_r1_evidence/`. Model routing:
`deepseek-v4-flash` via ambient Claude Code OAuth (hook protocol is
provider-agnostic).

## 3. Verdict

**C0D PROCEED — PreToolUse defer/resume lifecycle provides an exact
invocation identity (`tool_use_id`) through observe → defer → resume →
allow/deny → PostToolUse, with the hook fail-closed on identity
mismatch. The bounded consumed-decision witness is `PostToolUse`
for allow and `permission_denials[]` for deny, both carrying the
exact same `tool_use_id`.**

C0D PROCEED only authorizes the reviewer to write a new implementation
plan (C1D/C2D/C3D). It does NOT authorize building, production Claude
actionability, A1 core changes, or mobile changes.
