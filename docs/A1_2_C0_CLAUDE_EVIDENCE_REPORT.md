# A1.2 C0 — Claude Approval Surface Evidence Report

Status: **C0 BLOCKED — PermissionRequest hook never fires in headless print
mode. Contract note: `docs/A1_2_C0_CLAUDE_SURFACE_CONTRACT_NOTE.md`.**

Date: 2026-07-15. Executor: A1.2 execution agent, canonical checkout
`/Users/mhk/Documents/codex/DevRemote`, branch `feature/phase10-multi-adapter`.

## 1. Pinned provider identity

- Binary: `~/.local/share/claude/versions/2.1.209` (native Mach-O arm64,
  240MB, shasum prefix `59d2de7f…`)
- `claude --version`: `2.1.209 (Claude Code)`
- Global install unchanged; no credentials copied; ambient `~/.claude/`
  auth.

## 2. Probe harness design

A session-scoped `--settings.json` injected exactly one
`PermissionRequest` command hook matching `Bash` tools:

```json
{"hooks":{"PermissionRequest":[{"matcher":"Bash","hooks":[{"type":"command","command":"<hook.sh>","timeout":120}]}]}}
```

The hook script (`docs/a1_2_c0_evidence/hook.sh`) on every invocation:
1. writes a `life.log` marker recording PID/PPID/timestamp,
2. captures the full stdin to `perm_input_$$.json`,
3. emits the JSON decision (`allow` or `deny`) per the `$C0_DECISION`
   env var.

Claude was invoked headless via:
```
~/.local/share/claude/versions/2.1.209 -p "<prompt>" --settings <settings.json> --setting-sources "" --permission-mode default --model haiku
```

The prompt requested a harmless file-writing Bash command in an isolated
temp directory (the CP0 recipe). Runner script:
`docs/a1_2_c0_evidence/run_probe.sh`.

## 3. Traces and findings

Two traces were run against the identical provider identity and
configuration, differing only in `$C0_DECISION`:

| Trace | Label | Hook `life.log` | Provider outcome |
| --- | --- | --- | --- |
| allow | t1 | **ABSENT** | "needs your approval…" — the headless print session reached the dialog but the hook **did not fire**; the session hung with no consumer for the blocking request |
| deny | t2 | **ABSENT** | "denied by your permission system" — the static permission system (pre-existing user rules) denied the command; the hook **did not fire** |

Artifacts: `stdout_allow_t1.log` / `stdout_deny_t2.log`;
`life_marker_missing_proves_hook_never_ran` (empty sentinel file — the
hook script was never executed for either trace).

## 4. Exact missing invariant

The `PermissionRequest` hook fire is **conditional on the interactive
permission dialog appearing**, which `-p` (print) mode does not trigger
with `--permission-mode default`. Without the hook firing, there is no:

- exact invocation boundary (one subprocess per provider request);
- blocking per-invocation surface for a managed decision;
- provider-native request identity the daemon can bind to.

The static permission system (pre-existing allow/deny rules) resolved the
deny path **without the hook participating**, so it cannot serve as the
managed authority surface either — it would let unrelated rules bypass
the hook entirely.

## 5. Alternate surface assessment

- `--permission-prompt-tool`: not listed in `claude --help` for `2.1.209`.
  A future build may expose it; this evidence does not assess it.
- Agent SDK: replaces the shell-provider model entirely (different
  architecture, separately reviewed plan expansion).
- Channel permission relay: excluded per plan §2.4 (research preview,
  `input_preview` only, requires dangerous development flag).

## 6. Verdict

**C0 BLOCKED — stable hook headless fire not provable.**

No production code changed. C1, C2, C3, N1, O1, O2 and any Claude
actionability are NOT started. This blocking finding is exactly one day's
surface work (the time box).

The next executor may re-assess against a build where
`--permission-prompt-tool` is GA, or begin an Agent SDK plan expansion.
