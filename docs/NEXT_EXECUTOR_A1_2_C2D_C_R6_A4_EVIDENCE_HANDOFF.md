# A1.2 C2D-C R6-A4 Denial Evidence Arbitration Handoff

> **Superseded:** implementation `317a4eb7e6d5ed812cf55a8d221e43e7d588a517`
> invented a production `input_sha256` wire field from a redacted projection and
> added a fail-open fallback. Use
> `NEXT_EXECUTOR_A1_2_C2D_C_R6_A5_ROLLBACK_EVIDENCE_HANDOFF.md`.

Status: **R6-A3 REJECTED — RESEARCH/CONTRACT ARBITRATION ONLY**  
Reviewed implementation: `7ad9239b9e73fb1be713b201416c1b3fe4ded473`  
Current branch parent: `1f5a0cd931dd8c8b6fb7649b1dad19f3d925fa30`

Do not modify production code in R6-A4. Do not begin R6-B/C2D-D/C3D. The
PostToolUse-as-deny defect is fixed and must remain fixed, but the authoritative
denial schema is not currently established consistently enough to approve the
deny path.

## 1. Preserved improvement

`handlePostTool` now always routes `WitnessPostToolUse`. Independent race
execution confirms:

- allow accepted and committed;
- deny controlled fixture accepted;
- deny followed only by PostToolUse does not produce accepted delivery;
- earliest hook path passes.

Preserve the PostToolUse change. It is not permission to start lifecycle work.

## 2. Blocking evidence conflict

The repository contains three incompatible claims:

1. `docs/a1_2_c0d_r5_evidence/deny_live/denial_projection.json` contains an
   `input_sha256` matching `deferred_identity.json`.
2. `docs/A1_2_C0D_R5_DEFER_EVIDENCE_REPORT.md` describes the denial projection
   as only session ID, tool name and tool-use ID.
3. The current production implementation and commit message assert that the
   Claude `permission_denials` entry has no tool input and therefore pass the
   stored context digest to `MarkWitnessed`.

The retained projection does not establish whether its digest was computed from
the denial event or copied from the earlier deferred identity. Passing the
stored digest as if it were witness evidence is not an acceptable resolution.

## 3. R6-A4 evidence task

Use the exact pinned Claude Code 2.1.209 executable and the already accepted
isolated C0D defer/resume harness. Run one harmless deny invocation only. Capture
the complete private structural shape of the `permission_denials` result, then
commit only a bounded/redacted schema projection and hashes:

- exact executable path/version/hash and isolated settings;
- exact event and nested field names/types;
- session ID/tool-use ID equality to the deferred invocation;
- whether the denial entry contains full tool input, a digest, or neither;
- how the existing `input_sha256` projection was computed;
- deny side effect absent and exact provider-native denial outcome;
- cleanup/no residual process evidence.

Never commit raw prompt, command, cwd, auth data, provider payload or tool input.
Use a fixed harmless bounded input and publish only its digest.

If live recapture is unavailable, return R6-A4 BLOCKED. Do not infer the schema
from current Go structs, synthetic fixtures, ordering or prior prose.

## 4. Contract decision produced by the verifier

The executor must not choose the implementation contract. It stops with one of
these evidence findings:

### Finding A — denial event carries exact tool input

The next verifier may authorize evidence-derived canonical digest comparison
and the existing full-binding `MarkWitnessed` signature.

### Finding B — denial event carries only exact invocation identity

The next verifier must decide whether the provider-issued
`session_id + tool_use_id` is sufficient consumed-denial authority because the
same invocation input was already exactly bound at repeated PreToolUse. If
accepted, the contract must model this honestly with a Claude-specific denial
witness method/signature. It must not pass a stored digest pretending it came
from the denial event.

### Finding C — identity or consumption remains ambiguous

C2D deny actionability remains BLOCKED. Do not weaken the contract.

## 5. Test-quality amendment for later implementation

The current negative composition waits for a two-second delivery timeout. A
later implementation packet must replace timeout-as-success with a deterministic
intermediate assertion:

1. decision write confirmed;
2. PostToolUse handler returned;
3. claim-owned completion has not fired and state remains non-terminal success;
4. explicit lifecycle cancellation releases the waiter;
5. no accepted receipt or Store commit.

Use channels/barriers, not sleeps or elapsed-time inference.

## 6. Gate and stop

For this evidence-only packet run the focused harness checks, documentation
secret scan and `git diff --check`. Commit and push evidence/report only, then
stop with:

```text
REVIEW REQUEST: A1.2 C2D-C R6-A4 Denial Evidence Arbitration — <evidence SHA>
```

No production, mobile or authority Store file may change.
