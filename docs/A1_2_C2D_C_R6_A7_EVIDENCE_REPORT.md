# A1.2 C2D-C R6-A7 — Safe Denial Evidence Report

Status: **PACKET A + PACKET B COMPLETE — EVIDENCE ONLY — R6-B NOT ENTERED**

Contract: `docs/A1_2_C2D_C_R6_A7_EVIDENCE_CONTRACT_NOTE.md`
(committed `7ce9154`, vocabulary amendment `d8b069a`, both before any probe
change). Rejected R6-A6 probe deleted at `79f2f54`. Neither rejected probe
(`98fe899`, `23dc740`) was ever executed by this packet.

## 1. Evidence tree

| Artifact | Commit |
|---|---|
| Evidence contract note | `7ce9154`, amended `d8b069a` |
| Rewritten safe probe `docs/a1_2_c2d_c_r6a7_safe_deny_probe.py` | `79f2f54` |
| Deterministic no-model tests `docs/a1_2_c2d_c_r6a7_probe_tests.py` (57 tests) | `79f2f54` |
| Rejected R6-A6 probe deletion | `79f2f54` |
| Bounded projection run-a `docs/a1_2_c2d_c_r6a7_projection_run_a.json` | `c0a1570` |
| Bounded projection run-b `docs/a1_2_c2d_c_r6a7_projection_run_b.json` | `c0a1570` |
| This report | HEAD of the review request |

## 2. Packet A — deterministic no-model verification (before any live turn)

`python3 docs/a1_2_c2d_c_r6a7_probe_tests.py` — **57/57 PASS, three
consecutive clean runs**, zero Claude invocations. Mapping to the handoff §3
required checks:

| Check | Proven by |
|---|---|
| Timeout terminates/reaps the owned group incl. child+grandchild, without signaling the runner's group | `TestGroupCleanup.test_timeout_kills_child_and_grandchild_group` (real process tree, real `GroupRunner`, ESRCH-verified) |
| Normal exit leaves no owned live child | `test_normal_exit_with_leftover_child_is_detected_and_killed` (leftover detected → `killed_leftover`), `test_normal_exit_without_children_is_clean` |
| Wrong session/tool-use/tool-name/input-digest each falsify the join | `TestIdentityJoin.test_each_single_field_mutation_makes_join_false`, `test_join_deferred_binds_all_four_fields`, denial variants (`wrong_tool_use_id`, `wrong_session`, `input_digest_mismatch`), plus full-lifecycle mutations in `TestLifecycle` (8 dedicated failure tests) |
| Matching control makes the join true | `test_matching_control_makes_join_true`, `test_denial_matching_control_with_input_digest`, `test_happy_path_produces_validated_projection` |
| Non-pinned/global executable rejected before spawn | `TestPinVerification` — wrong realpath / wrong digest / wrong arch each rejected with **zero recorded spawn attempts** (`SpawnRecorder` over a runner that asserts if reached); orchestrator-level substitution also rejected pre-spawn |
| Deliberately created marker falsifies `side_effect_absent` | `test_marker_created_by_execution_fails_run`: the fake creates the **exact marker the bound command would create** → `marker_violation`. Linkage itself is enforced (`test_wrong_captured_command_breaks_marker_linkage`, `test_non_bash_tool_fails_closed`) |
| No raw prompt/command/cwd/temp path/input keys/token-shaped text/exception values in stdout or projection | `TestPrivacy` (3 tests): sentinel-seeded inner tool-input keys, concatenation-built token-shaped strings, owned-dir paths, marker/command/prompt shapes, and a raising provider with a path-laden exception — all absent from captured stdout and serialized projections |
| Any failure/unstable structure/cleanup failure/failed control → nonzero exit, no success projection | `TestFailureExit` (failed run and unstable structure: rc=1, zero files written), `TestProjector.test_validate_projection_rejects_false_evidence_booleans` (a false evidence boolean can never serialize), plus fail-closed enums for spawn failure and unknown codes |

Additional coverage: the **actual generated hook scripts** (the same bytes
Packet B executes) were run as real subprocesses on synthetic stdin
(`TestHookScripts`, 5 tests): defer capture + defer decision; malformed input
→ deny exit 2 without capture; resume full-match → documented deny + one-shot
atomic claim; replay → "already consumed" deny exit 2; identity mismatch →
deny exit 2 with **no capture written**; missing expected identity → deny
exit 2. Bounded projector tests prove count/name bounds fail closed and that
nested objects are typed without recursion.

No sleep is used as correctness evidence anywhere; group cleanup is bounded
polling for ESRCH against a deadline, and success is only ever declared on
observed group absence.

## 3. Packet B — two live deny probes (after Packet A passed)

Executed exactly twice (`run-a`, `run-b`), each in a fresh `mkdtemp` 0700
owned directory used as the provider process `cwd`:

```text
run-a: pin verified
run-a: initial exit=exited_zero cleanup=clean
run-a: defer join complete
run-a: resume exit=exited_zero cleanup=clean
run-a: resume join complete
run-a: denial matched unique=True provider_input_present=True
run-b: (identical closed status lines)
PACKET-B: OK   (exit 0)
```

Pinned artifact verified before every spawn: realpath equal to
`~/.local/share/claude/versions/2.1.209`, SHA-256
`59d2de7f49db2f75d5c33bbb46a6b8f288ad24d40b61e30602a502bb7ddc380c`, Mach-O
arm64 header, `--version` first token exactly `2.1.209`. No PATH lookup; the
spawn argv uses the verified realpath only. No live negative controls were
run (Packet A owns them). Both committed projections satisfy the handoff §6
success predicate, and after removing `run` and `raw_capture_sha256` the two
projections are byte-identical (checked programmatically; a mismatch exits
nonzero as `unstable_structure`).

## 4. Field-by-field private derivation

Single canonicalizer everywhere:
`sha256(json.dumps(x, sort_keys=True, separators=(",",":"), ensure_ascii=False).encode("utf-8"))`.

| Projection field | Private derivation |
|---|---|
| `schema` | constant `a1_2.c2d_c.r6a7.denial_projection.v1` |
| `pinned_version` | first token of `--version` stdout of the verified pinned artifact; must equal `2.1.209` |
| `pinned_sha256` | SHA-256 of the executable bytes at the pinned realpath, compared to the expected constant **before** any spawn |
| `architecture` | Mach-O header parse (magic `0xfeedfacf` LE + cputype `0x0100000c` → `arm64`); no spawn |
| `executable_locator` | fixed home-redacted constant; never computed from runtime paths |
| `run` | pseudonym (`run-a`/`run-b`); carries no host/user/path information |
| `result_top_fields` | sorted top-level key → JSON-type map of the single denial-carrying `result` event; ≤ 32 names, ≤ 64 chars each, closed type vocabulary; exceeding a bound aborts (`projection_bounds_exceeded`), nothing truncates silently; no recursion |
| `denial_entry_top_fields` | same bounded typing of the single matched denial entry's top level only |
| `denial_discriminator` | constant `permission_denials` |
| `denial_match_unique` | exactly one `result` event carries a non-empty `permission_denials` array **and** its `session_id` equals the bound session, **and** exactly one entry's `tool_use_id` equals the bound tool-use ID; zero → `no_denial_match`, more → `ambiguous_denial` (both abort) |
| `provider_input_present` | `"tool_input" in matched entry` (truthful classification; never synthesized) |
| `provider_input_digest_equal` | when present: canonical digest of the entry's raw `tool_input`, computed privately, equals the initial invocation's private input digest; inequality aborts (`identity_mismatch`); `null` iff absent |
| `defer_join_{session,tool_use,tool_name,input_digest}_equal` | equality of the private initial PreToolUse capture (hook stdin) against the `tool_deferred` result's `session_id` / `deferred_tool_use.id` / `.name` / canonical digest of `.input`; any false aborts (`defer_join_failed`) |
| `resume_join_{session,tool_use,tool_name,input_digest}_equal` | equality of the private capture against the repeated PreToolUse identity written by the resume hook — which itself only writes after its own full four-field match and a one-shot atomic `mkdir` claim inside the owned dir; any false aborts (`resume_join_failed`) |
| `marker_command_bound` | captured private `tool_input.command` equals the expected `touch marker-<hex>` string and `tool_name == "Bash"`; with provider `cwd` = owned dir, executing the exact private input would create the exact asserted marker |
| `marker_absent_pre` / `marker_absent_post` | existence check of the exact marker path before the initial spawn and after the denial; presence aborts (`marker_violation`) |
| `initial_exit` / `resume_exit` | closed enum from the direct child's reaped return code / timeout |
| `initial_cleanup` / `resume_cleanup` | closed enum from bounded ESRCH polling of the owned process group after reap; undrainable group aborts (`cleanup_failed`) and can never serialize |
| `raw_capture_sha256` | SHA-256 over length-prefix-framed concatenation of (initial stdout, resume stdout, `defer_capture.json`, `expected_identity.json`, `resume_capture.json`), computed immediately before the `finally` deletion of the owned directory; deletion is re-verified (`raw_delete_failed` otherwise) |

The projection writer re-asserts the exact 28-key set, closed vocabularies,
and the success predicate (`validate_projection`); a failed run structurally
cannot produce a projection file, and the program exits nonzero on any
failure enum.

## 5. Observed denial wire contract (Claude Code 2.1.209, stream-json)

Statement of the observed contract, stable across both independent runs:

1. After the repeated PreToolUse hook of the resumed session returned the
   documented deny for the fully joined identity, the resumed process emitted
   **exactly one** `type:"result"` event carrying `permission_denials` as an
   array.
2. The carrying result event's top level had exactly these 20 fields:
   `api_error_status`(null), `duration_api_ms`, `duration_ms`,
   `fast_mode_state`, `is_error`(boolean), `modelUsage`(object),
   `num_turns`, `permission_denials`(array), `result`(string),
   `session_id`(string), `stop_reason`(string), `subtype`(string),
   `terminal_reason`(string), `time_to_request_ms`, `total_cost_usd`,
   `ttft_ms`, `ttft_stream_ms`, `type`(string), `usage`(object),
   `uuid`(string).
3. The matched denial entry's top level had exactly three fields:
   `tool_input`(object), `tool_name`(string), `tool_use_id`(string).
   **The raw denial does contain the raw tool input.** It contains **no
   provider-computed digest field**; identity is carried as
   `tool_use_id` + `tool_name` + raw `tool_input`.
4. The entry's `tool_use_id` and the carrying event's `session_id` exactly
   matched the deferred private identity, and the canonical digest of the
   entry's raw `tool_input` equaled the initial invocation's private input
   digest (`provider_input_present=true`,
   `provider_input_digest_equal=true` in both runs).
5. The exact bound marker remained absent after the denial
   (`marker_absent_post=true` with `marker_command_bound=true`), so the
   denied invocation's exact command was not executed in the provider `cwd`.
6. Both invocations exited `exited_zero` and both owned process groups
   drained to ESRCH without leftovers (`clean`).

Implication recorded for the verifier (decision remains the verifier's): the
deny witness defined in `A1_2_C2D_PACKET_CONTRACT_NOTE.md` §4.2 (match on
`tool_use_id` within a session-bound `permission_denials` result) is
satisfiable on the live wire, and the entry additionally permits an input
digest re-check because raw `tool_input` is present.

## 6. No production/mobile change

`git diff --name-only 39ba76e..HEAD` at review-request HEAD lists only
`docs/` paths (handoffs, contract note, probe, tests, projections, this
report). `git diff --name-only 7ad9239..HEAD` contains no path outside
`docs/`. Production, mobile, A1 authority, and accepted Codex code are
untouched; R6-B, C2D-D, C3D, and N1 were not entered.

## 7. Final gates (frozen HEAD)

| Gate | Result |
|---|---|
| Focused harness tests (`a1_2_c2d_c_r6a7_probe_tests.py`) | 57/57 PASS ×3 |
| Python syntax (`py_compile` probe + tests) | PASS |
| `git diff --check` | PASS |
| Repository documentation secret scan (build-gate pattern over `internal/` + `docs/`) | PASS (no findings) |
| Ancestry `39ba76e` / `7ad9239` / `23dc740` → HEAD | PASS |
| Local == remote, clean worktree | recorded in the review request |

The independent verifier alone decides the production denial contract. No
result in this packet authorizes R6-B.
