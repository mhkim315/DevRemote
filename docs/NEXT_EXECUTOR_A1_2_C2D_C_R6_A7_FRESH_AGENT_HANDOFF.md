# A1.2 C2D-C R6-A7 Fresh-Agent Denial Evidence Handoff

Status: **R6-A6 V1/V2 REJECTED — NEW EXECUTOR — EVIDENCE ONLY**

Rejected probe commits:

- `98fe899f433c71b69fcddd6e022ca5a7ba2aa7ad`
- `23dc740b1cc91bb1d3d7de891bd24a469d63868d`

Verified production rollback: `39ba76e` restores the affected production and
test files to accepted checkpoint `7ad9239b9e73fb1be713b201416c1b3fe4ded473`.

Do not modify production, mobile, A1 authority, or accepted Codex code. Do not
start R6-B, C2D-D, C3D, N1, or post-Claude restructuring. This packet may
replace/delete the rejected probe and add evidence-only harness tests, bounded
projections, and a report.

## 1. Mandatory startup

Use only the canonical checkout:

```text
/Users/mhk/Documents/codex/DevRemote
```

Before editing, fetch and fast-forward the canonical branch, then record:

- branch `feature/phase10-multi-adapter`;
- local and remote SHA equality;
- ancestry from `39ba76e`, `7ad9239`, and `23dc740`;
- clean worktree;
- exact changed-file boundary since `7ad9239`.

Read, in order:

1. this handoff;
2. `NEXT_EXECUTOR_A1_2_C2D_C_R6_A6_SAFE_PROBE_HANDOFF.md` as historical intent;
3. `NEXT_EXECUTOR_A1_2_CLAUDE_APPROVAL_HANDOFF.md`;
4. the accepted C0D evidence contract and the C2D packet contract note.

Write a short evidence contract note before changing the probe. Include the
private identity binding, public projection allowlist, owned-process lifecycle,
failure enums, and one adversarial counterexample for each check below.

Do not run either rejected probe.

## 2. Why R6-A6 v2 is rejected

The v2 report claims closures that the code does not provide:

1. It creates `side-effect-should-not-exist` itself before resume, then tests
   whether that file is absent. Claude is asked to touch a different relative
   file in the inherited repository working directory. The check cannot prove
   deny prevented execution and can pollute the checkout.
2. `deny-wrong-tuid` and `deny-timeout` are merely invalid decision strings.
   They neither substitute a tool-use ID nor force a timeout. Wrong-digest is
   absent.
3. The global-Claude check searches the result text for a literal string. It
   does not attempt or reject executable substitution.
4. The program exits zero and writes a projection even when runs, stability,
   or negative controls fail.
5. `field_types` recursively publishes provider/tool-input key names without a
   count or byte bound. Only top-level structural fields are permitted.
6. Provider exit and supervisor reap do not prove the owned process group has
   no remaining child. Initial-process cleanup is also not recorded.
7. Error redaction is string replacement, not a closed projection; timeout and
   file exceptions may still carry command, prompt, path, or provider values.
8. The deferred provider result is not fully joined to the captured PreToolUse
   identity, and the denial's private tool input is not digested and compared
   when present.

## 3. Packet A — deterministic harness safety tests first

Do not invoke Claude in Packet A. Build the smallest testable harness with
dependency injection for the executable runner, parser, projector, and cleanup
observer.

Required deterministic tests:

- a fake process plus child/grandchild times out; the owned process group is
  terminated and reaped without signaling the test runner's group;
- normal exit also leaves no owned live child;
- wrong session ID, tool-use ID, tool name, and input digest each make the join
  false;
- a matching control makes the join true;
- an injected non-pinned/global executable is rejected before spawn;
- a deliberately created marker makes `side_effect_absent` false, proving the
  assertion is non-vacuous;
- raw prompt, command, cwd, temp path, input keys, token-shaped text, and raw
  exception values never appear in stdout or the serialized projection;
- failure, unstable structure, cleanup failure, or a failed negative control
  causes a nonzero program exit and cannot create a success projection.

Use barriers/events or bounded polling, never sleeps as correctness evidence.
Commit Packet A only after these no-model tests pass, then stop internally and
re-read this handoff before Packet B.

## 4. Packet B — exact private lifecycle

Only after Packet A passes, run exactly two fresh live deny probes using the
absolute pinned Claude Code 2.1.209 executable.

For each run:

1. Create one mode-0700 owned directory.
2. Set the provider process `cwd` to that directory.
3. Verify exact version, executable realpath, SHA-256, and architecture before
   spawn. Do not use PATH lookup or silently substitute global Claude.
4. Select one marker inside the owned directory. Assert it is absent before
   the initial invocation. The exact private Bash input must create that same
   marker if executed.
5. Capture initial PreToolUse privately, return `defer`, and join the provider
   `tool_deferred` result to exact session ID, tool-use ID, tool name, and
   canonical private input digest.
6. Resume the exact session. The repeated PreToolUse hook must compare the
   complete identity before returning the documented deny response. A mismatch
   remains deny/fail-closed but cannot count as joined evidence.
7. Locate the provider-native denial result and require one unambiguous match.
   If the denial contains raw tool input, hash it privately using the same
   canonicalizer and record only the equality boolean. If it does not, record
   `provider_input_present=false`; never synthesize a provider digest.
8. Assert the exact marker remains absent.
9. Record bounded exit and cleanup enums. Terminate/reap every owned process on
   timeout, error, and normal completion, then prove no owned group member
   remains.
10. Delete every private raw artifact in `finally` and fail if deletion cannot
    be confirmed.

Do not use live provider turns for negative controls. Packet A already proves
the harness rejects altered identities and cleanup failures.

## 5. Closed public evidence schema

The committed projection may contain only:

- probe/schema version;
- pinned version, artifact digest, architecture, and a home-redacted executable
  locator;
- run pseudonym;
- top-level result field names and JSON types, with strict count/name bounds;
- top-level denial-entry field names and JSON types, with strict count/name
  bounds;
- denial discriminator;
- booleans for private input presence and digest equality;
- pseudonymized equality booleans for session/tool identity;
- bounded exit, timeout, cleanup, and marker-absence enums/booleans;
- digest of the deleted private capture.

Do not recurse into `tool_input` or arbitrary provider objects. Never serialize
raw or derived prompt, command, cwd, path, tool input, transcript, auth data,
provider payload, exception text, PID, or host/user identity. Map every error to
a fixed vocabulary such as `version_mismatch`, `spawn_failed`, `timeout`,
`identity_mismatch`, `schema_invalid`, `cleanup_failed`, or `provider_failed`.

## 6. Success predicate and deliverables

The harness succeeds only if both independent runs satisfy all of:

- exact pinned artifact verified;
- full defer and resume identities joined;
- one matching provider denial observed;
- provider input presence truthfully classified;
- input digest equality true when provider input exists;
- exact marker absent;
- expected bounded provider outcome recorded;
- complete process and raw-file cleanup confirmed;
- structural projections equivalent after removing run-specific hashes.

Required deliverables:

- rewritten safe probe;
- deterministic no-model tests;
- evidence contract note;
- two separately committed bounded projections or one document containing two
  clearly separated projections;
- evidence report mapping every field to its private derivation;
- explicit statement of the observed denial wire contract;
- proof that no production/mobile file changed.

Run the focused harness tests, Python syntax check, `git diff --check`, and the
repository documentation secret scan. Freeze HEAD before the final checks.
Commit and push the exact verified evidence tree, confirm local/remote equality
and a clean worktree, then stop with:

```text
REVIEW REQUEST: A1.2 C2D-C R6-A7 Safe Denial Evidence — <evidence SHA>
```

The independent verifier alone decides the production denial contract. No
evidence result authorizes R6-B automatically.
