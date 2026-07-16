# A1.2 C2D-C R6-A6 Safe Denial Wire Probe Handoff

Status: **A5 ROLLBACK VERIFIED — A5 PROBE REJECTED — EVIDENCE ONLY**  
Verified rollback: `39ba76e` restores the relevant production/test files to
`7ad9239b9e73fb1be713b201416c1b3fe4ded473`  
Rejected probe: `1b18015ccc3109551550ba9c66e1d555447d5c18`

Do not modify production/mobile/A1 authority code. Do not start R6-B/C2D-D/C3D.

## 1. Why the A5 probe is rejected

The committed script does not exercise the required lifecycle:

- it launches only the initial `-p` invocation whose hook returns `defer`;
- it never resumes the exact Claude session with an exact deny response;
- therefore it cannot reliably produce the post-resume
  `permission_denials` result under investigation.

It also violates evidence hygiene:

- writes raw PreToolUse input to fixed `/tmp/r6a5_pretool_stdin.json` outside
  the owned temp directory and never deletes it;
- prints every denial entry value, potentially including raw tool input;
- prints canonical raw tool input;
- has no process timeout or process-group cleanup;
- `set -e` can exit before capturing the expected nonzero provider outcome;
- does not verify `claude --version == 2.1.209`;
- runs once rather than twice;
- commits no evidence report or bounded projection.

Do not run this script. Replace it or delete it in the evidence commit.

## 2. R6-A6 safe probe requirements

Use a small isolated harness, preferably Python `subprocess` with explicit
timeouts and process-group cleanup. All files must live under one mode-0700
temporary directory guarded by a trap/finally. No fixed `/tmp` file is allowed.

### Exact lifecycle

1. Resolve the exact pinned executable by absolute path.
2. Verify version output is exactly 2.1.209 and record executable SHA-256.
3. Launch initial headless invocation with isolated settings and a PreToolUse
   hook that writes only a bounded private capture inside the owned directory
   and returns `defer`.
4. Privately parse the exact `tool_deferred` result and join session ID,
   tool-use ID, tool name and canonical input digest.
5. Launch `--resume <exact session_id>` with isolated settings and a repeated
   PreToolUse hook that validates the full private identity and returns the
   exact deny decision.
6. Capture the resumed stream-json privately and locate the matching
   provider-native denial result.
7. Verify the harmless side effect is absent.
8. Terminate/reap the whole owned process group and remove every private raw
   file in `finally`, including on timeout/error.

Use the existing accepted C0D harness semantics. Do not infer a denial from
initial `tool_deferred`, process exit, missing side effect or timing.

### Safe committed projection

The projector may commit only:

- pinned path/version/hash and architecture;
- run identifier containing no host/user/path information;
- sorted top-level field names and JSON types;
- sorted denial-entry field names and JSON types;
- booleans indicating tool input/provider digest presence;
- pseudonymized equality relations for session/tool-use IDs;
- locally computed input digest equality as a boolean, never raw input;
- provider result/denial discriminator;
- exit/timeout/cleanup results;
- a digest of the private raw capture before deletion.

Never print or commit prompt, command, cwd, raw tool input, transcript, auth
material, provider payload, absolute temp path or host/user identity.

## 3. Stability and negative controls

Run the complete defer→resume→deny lifecycle twice in fresh directories. The
structural field/type projections must match. Include negative controls proving:

- wrong tool-use ID is not joined;
- wrong input digest is reported mismatch;
- timeout kills/reaps the owned group and leaves no raw file;
- global Claude is not substituted for pinned 2.1.209.

The negative controls do not authorize a decision and must not create side
effects.

## 4. Evidence deliverables and stop

- safe probe harness and focused tests;
- two bounded redacted projections;
- evidence report explaining exactly how every derived field was computed;
- explicit statement whether raw denial contains tool input, provider digest,
  or only invocation identity;
- cleanup/process/privacy proof;
- no production/mobile changes.

Run harness tests, `git diff --check`, and the repository documentation secret
scan. Commit/push and stop with:

```text
REVIEW REQUEST: A1.2 C2D-C R6-A6 Safe Denial Wire Evidence — <evidence SHA>
```

The verifier—not the executor—will choose the next production contract.
