# A1.2 C2D-C R6-A7 — Safe Denial Evidence Contract Note

Status: **EVIDENCE ONLY — PACKET A BEFORE ANY LIVE INVOCATION — R6-B PROHIBITED**

This note is required by `NEXT_EXECUTOR_A1_2_C2D_C_R6_A7_FRESH_AGENT_HANDOFF.md`
§1 and is committed before the rejected probe (`98fe899`, `23dc740`) is
replaced. It binds the rewritten harness to a closed private identity contract,
a closed public projection allowlist, an owned-process lifecycle, fixed failure
enums, and one adversarial counterexample per Packet A check. No production,
mobile, A1-authority, or accepted Codex file is touched by this packet.

## 1. Private identity binding

All raw values live only inside one `mkdtemp` mode-0700 owned directory and are
deleted in `finally`. The single canonicalizer for every digest is:

```text
canonical_digest(obj) = SHA-256( json.dumps(obj, sort_keys=True,
                       separators=(",",":"), ensure_ascii=False).encode("utf-8") )
```

| Field | Captured at | Private storage | Joined/compared at |
|---|---|---|---|
| Claude `session_id` | initial PreToolUse hook stdin | `<owned>/defer_capture.json` | `tool_deferred` result join; repeated PreToolUse; denial result |
| `tool_use_id` | initial PreToolUse hook stdin | same file | `deferred_tool_use.id`; repeated PreToolUse; matched denial entry |
| `tool_name` | initial PreToolUse hook stdin | same file | `deferred_tool_use.name`; repeated PreToolUse; matched denial entry when present |
| `input_digest` | `canonical_digest(tool_input)` at capture | same file | `canonical_digest(deferred_tool_use.input)`; repeated PreToolUse digest; `canonical_digest(denial tool_input)` when present |
| expected Bash command | chosen by harness before spawn | harness memory + prompt | equality with captured `tool_input.command` (marker linkage) |
| marker path | `<owned>/marker-<hex>` chosen before spawn | harness memory | absent before initial spawn; absent after deny |

Join rules:

- **Defer join** — the `tool_deferred` result is joined on all four fields
  (`session_id`, `deferred_tool_use.id`, `deferred_tool_use.name`,
  `canonical_digest(deferred_tool_use.input)`). Any single-field mismatch makes
  the join false.
- **Resume join** — the repeated PreToolUse hook compares the complete four
  field identity against the privately stored expected identity before it
  returns the documented deny response. A mismatch still returns deny with
  exit 2 (fail-closed) but is never counted as joined evidence. One-shot
  consumption is an atomic `mkdir` claim inside the owned directory (no fixed
  `/tmp` path).
- **Denial join** — among resume stream-json `result` events with a non-empty
  `permission_denials` array, exactly one entry may match the bound
  `tool_use_id` (with `session_id` matching on the carrying event, and
  `tool_name` matching when the entry carries it). Zero or multiple matches
  fail with `no_denial_match` / `ambiguous_denial`. If the matched entry
  carries raw tool input it is hashed privately with the same canonicalizer
  and only the equality boolean is recorded; if it does not,
  `provider_input_present=false` is recorded and no digest is synthesized.
- **Marker linkage** — the captured private `tool_input.command` must equal
  the expected `touch marker-<hex>` string and the provider process `cwd` is
  the owned directory, so executing the exact private input would create the
  exact asserted marker. Without this equality the marker assertion is
  meaningless and the run fails closed (`unexpected_tool_input`).

## 2. Public projection allowlist (closed)

The serialized projection contains exactly this flat key set and nothing else;
the projector asserts the exact key set before writing. No recursion into
`tool_input` or any nested provider object is permitted; nested values are
typed as `"object"`/`"array"` only.

| Key | Type / bound |
|---|---|
| `schema` | fixed `"a1_2.c2d_c.r6a7.denial_projection.v1"` |
| `pinned_version` | fixed `"2.1.209"` |
| `pinned_sha256` | hex64 of the pinned executable |
| `architecture` | fixed vocabulary (`"arm64"`) |
| `executable_locator` | home-redacted fixed locator `~/.local/share/claude/versions/2.1.209` |
| `run` | pseudonym `"run-a"` / `"run-b"` |
| `result_top_fields` | top-level field-name → JSON-type map of the matched denial-carrying result event; ≤ 32 names, each ≤ 64 chars, types from `{null,boolean,number,string,array,object}`; exceeding a bound fails closed (`projection_bounds_exceeded`), never truncates silently |
| `denial_entry_top_fields` | same bounds, top level of the matched denial entry only |
| `denial_discriminator` | fixed `"permission_denials"` |
| `denial_match_unique` | boolean — exactly one matching entry |
| `provider_input_present` | boolean |
| `provider_input_digest_equal` | boolean, or `null` iff `provider_input_present=false` |
| `defer_join_session_equal`, `defer_join_tool_use_equal`, `defer_join_tool_name_equal`, `defer_join_input_digest_equal` | booleans |
| `resume_join_session_equal`, `resume_join_tool_use_equal`, `resume_join_tool_name_equal`, `resume_join_input_digest_equal` | booleans |
| `marker_command_bound` | boolean (captured command equals expected marker command) |
| `marker_absent_pre`, `marker_absent_post` | booleans |
| `initial_exit`, `resume_exit` | enum `{exited_zero, exited_nonzero, timeout}` |
| `initial_cleanup`, `resume_cleanup` | enum `{clean, killed_leftover}` (`cleanup_failed` aborts the run and no projection is written) |
| `raw_capture_sha256` | hex64 digest of the concatenated private raw capture computed immediately before deletion |

Never serialized or printed: prompt, command text, cwd, absolute/temp paths,
tool-input keys or values, transcript, auth material, provider payload,
exception text, PID, host/user identity. Probe stdout consists only of fixed
status lines, closed enums/booleans, and the projection itself.

## 3. Owned-process lifecycle

Every provider (or fake) process is spawned with `start_new_session=True` and
`cwd=<owned dir>`, giving an owned process group distinct from the harness
group (asserted: `pgid != os.getpgrp()`; violation → `cleanup_failed`).

```text
spawn → communicate(timeout=120s)
  on timeout:      killpg(SIGKILL) → wait() reaps direct child
  on normal exit:  wait() already reaped direct child
finally (both paths):
  bounded poll (20ms interval, 5s deadline) of killpg(pgid, 0)
    ProcessLookupError immediately            → clean
    members remain → killpg(SIGKILL) → poll   → killed_leftover
    deadline exceeded with members remaining  → cleanup_failed (abort, nonzero)
```

The kill signal is sent only to the owned group; the harness never signals its
own group. Both the initial and the resume invocation record their own cleanup
enum. Raw private files are deleted in `finally`; deletion is re-verified and
an undeletable artifact fails the run (`raw_delete_failed`). No sleep is used
as correctness evidence — only bounded polling against an explicit deadline.

## 4. Failure enums (closed vocabulary)

Every error path maps onto exactly one of:

```text
version_mismatch      executable_not_pinned   spawn_failed
timeout               hook_capture_missing    no_deferred_result
defer_join_failed     resume_join_failed      identity_mismatch
unexpected_tool_input no_denial_match         ambiguous_denial
marker_violation      schema_invalid          projection_bounds_exceeded
cleanup_failed        raw_delete_failed       provider_failed
```

Raw exception values never reach stdout or any committed artifact. Any
failure, unstable structure, cleanup failure, or failed control causes a
nonzero program exit and no success projection can be created (the projection
writer is reachable only after the full success predicate holds).

## 5. Packet A checks and adversarial counterexamples

Packet A never invokes Claude. The harness is dependency-injected (executable
runner, pin verifier, stream parser, projector, cleanup observer) so each
check runs against fakes or fixtures.

| # | Required check | Adversarial counterexample the test must defeat |
|---|---|---|
| A1 | Fake process + child/grandchild times out; owned group terminated and reaped without signaling the runner's group | A harness that kills only the direct child PID leaves the grandchild `sleep` alive; the test detects it via `killpg(pgid, 0)` still succeeding and fails |
| A2 | Normal exit leaves no owned live child | A fake provider that forks a background sleeper and exits 0 fools a wait()-only harness; the observer must detect the surviving group member (`killed_leftover`), while a childless exit reports `clean` |
| A3 | Wrong session ID, tool-use ID, tool name, or input digest each make the join false | A joiner that compares only `tool_use_id` (the rejected v2 behavior) passes three of the four mutations; each single-field mutation must independently yield join=false |
| A4 | A matching control makes the join true | A joiner that always returns false trivially passes A3; the exact-match fixture must return true |
| A5 | An injected non-pinned/global executable is rejected before spawn | A same-version-string copy at a different realpath (or PATH-resolved `claude`) passes a version-text check; the verifier must reject on realpath/digest with the spawn observer recording zero spawns |
| A6 | A deliberately created marker makes `side_effect_absent` false | The rejected v2 pre-created an unrelated file, making absence vacuous; creating the exact expected marker must flip the assertion to false and fail the run |
| A7 | Raw prompt, command, cwd, temp path, input keys, token-shaped text, and raw exception values never appear in stdout or the serialized projection | Sentinel-seeded private data (fake command, cwd, concatenation-built token-shaped strings, exception text with absolute paths) defeats string-replacement redaction; the closed projector must emit none of the sentinels |
| A8 | Failure, unstable structure, cleanup failure, or a failed control causes nonzero exit and cannot create a success projection | The rejected v2 wrote a projection and exited 0 on failed runs; an injected failing stage must produce a nonzero exit and no projection file |

## 6. Packet B scope (context, executed only after Packet A passes)

Exactly two fresh live deny probes against the pinned executable
(`realpath == ~/.local/share/claude/versions/2.1.209`, SHA-256 verified,
version output exactly `2.1.209`, arm64), each in a fresh owned directory with
provider `cwd` set to it, full defer/resume identity join, one unambiguous
provider-native denial, marker absent, bounded enums, complete cleanup. No
live negative controls (Packet A already proves rejection of altered
identities and cleanup failures). Structural projections of the two runs must
be equivalent after removing `run` and `raw_capture_sha256`.

## 7. Non-goals

No production/mobile/A1-authority change, no R6-B, no C2D-D/C3D/N1, no
decision authority: the verifier alone decides the production denial contract.
