# A1.2 C2D-C R6-A5 Rollback and Denial Evidence Handoff

> **Partially superseded:** rollback `39ba76e` is verified, but probe
> `1b18015ccc3109551550ba9c66e1d555447d5c18` cannot generate the required
> resumed denial and leaks raw evidence outside its temp directory. Use
> `NEXT_EXECUTOR_A1_2_C2D_C_R6_A6_SAFE_PROBE_HANDOFF.md` for evidence only.

Status: **R6-A4 REJECTED — ROLLBACK, THEN EVIDENCE ONLY**  
Rejected implementation: `317a4eb7e6d5ed812cf55a8d221e43e7d588a517`  
Last preserved PostToolUse fix: `7ad9239b9e73fb1be713b201416c1b3fe4ded473`

R6-B/C2D-D/C3D remain prohibited. Do not implement another denial decoder in
this packet.

## 1. Why `317a4eb` is rejected

The commit violated the evidence-only R6-A4 boundary:

- it modified production and composition code without a pinned live probe;
- it treated `input_sha256` from the **redacted projection** as a provider wire
  field. The projection uses transformed fields (`eventType`, singular
  `denied`) and is not the raw Claude stream-json schema;
- it changed the controlled test to emit the same invented field, making the
  test circular;
- when `input_sha256` is absent it falls back to `ctx.inputDigest`, recreating
  the fail-open behavior that R6-A4 was intended to arbitrate;
- it added no evidence/report, raw-shape provenance, contract amendment or
  adversarial decoder tests.

Passing synthetic composition tests do not establish provider support.

## 2. A5-R — exact rollback only

Create one rollback commit that applies the exact inverse of
`317a4eb7e6d5ed812cf55a8d221e43e7d588a517`. Preserve the earlier `7ad9239`
PostToolUse asymmetry fix. Do not reset or rewrite history.

After rollback, verify:

- `handlePostTool` still routes only `WitnessPostToolUse`;
- no production decoder claims Claude emits `input_sha256`;
- no missing-field fallback is present;
- local equals remote and worktree is clean.

The rollback commit is not R6-A acceptance.

## 3. A5-E — pinned evidence only

After the rollback, use exact Claude Code 2.1.209 and the isolated accepted C0D
harness for one harmless deny invocation. Do not modify production code.

Record privately, then commit only bounded/redacted structural evidence:

- executable absolute path, version, package/artifact hashes;
- isolated settings and exact harmless probe description;
- actual top-level stream-json field names and JSON types;
- actual `permission_denials` entry field names and types;
- whether a denied entry contains `tool_input`, a provider digest, or neither;
- exact session ID/tool-use ID equality with the deferred invocation;
- if tool input exists, a locally computed canonical digest and equality with
  the earlier bound input digest;
- exact provider-native deny outcome and absent side effect;
- timeout/cleanup and no residual provider process.

Do not commit raw prompt, tool input, command, cwd, transcript, auth material or
provider payload. A structural schema projection must clearly label every
derived field; derived `input_sha256` must never be presented as a wire field.

## 4. Required evidence counterchecks

- Demonstrate that the raw event—not the existing projection—was the source of
  every claimed wire field.
- Include a small projector test showing raw private fixture → committed
  redacted schema projection while raw values are absent.
- Run the evidence harness twice to confirm field-shape stability for the exact
  pinned version.
- Record global Claude version separately and never silently substitute it.
- If exact 2.1.209 or the deny event cannot be obtained, report BLOCKED.

## 5. Stop for verifier contract decision

Commit and push only:

- exact rollback commit;
- evidence contract note/report;
- bounded redacted projection/hashes;
- isolated evidence harness/tests if justified.

Run focused harness checks, documentation secret scan and `git diff --check`.
Stop with:

```text
REVIEW REQUEST: A1.2 C2D-C R6-A5 Denial Wire Evidence — <evidence SHA>
```

The verifier will choose one next contract:

- tool input present → evidence-derived digest routing;
- only exact invocation ID present → explicit Claude-specific witness contract,
  never a fabricated digest;
- ambiguous identity → deny actionability remains BLOCKED.

Do not implement any of these outcomes before the verdict.
