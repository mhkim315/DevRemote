# A1.2 C2D Closed-Catalog Final Acceptance

Status: **ACCEPT**

Reviewed implementation: `91160409c9fc7e41a0c60b1c97a6ec6a7c4cffb4`

This acceptance closes C2D only. It authorizes the separately bounded C3D
production-activation packet; it does not itself enable Claude actionability or
declare A1.2 complete.

## Accepted chain

- C1D managed Claude observation: `4794ce7f42380c388c1a5614b4b2518bc1722870`;
- C2D-C denial decoder/binding: `48ce57d81781646fc1c6c7445b5900b58fd5cef3`;
- closed-catalog classifier P1: `f29e1b6dc776bc8f4799809b59accd7ceb755072`;
- private identity P2A: `f93e81ef126c2f8f6e1013697a66ca363a442cf3`;
- bound display metadata P2B: `d841a88b5f371b41d78e5d2958c029b7114b8d6b`;
- final controlled composition P3: `91160409c9fc7e41a0c60b1c97a6ec6a7c4cffb4`.

The historical C0H/C0R blocks and arbitrary-command D1 rejection remain valid.
C2D succeeds only through the one-entry server-owned catalog amendment.

## What is accepted

The controlled production code path proves one exact private action identity:

- provider/adapter `claude_headless`;
- exact version `2.1.209`;
- tool `Bash`;
- catalog ID `claude.bash.approval_probe.v1`;
- exact catalog command classified by a strict nested decoder;
- public summary `Run Claude approval verification probe`;
- options `allow_once` and `deny` under the accepted Claude decision schema.

The catalog ID is carried from the real hook boundary through pending state,
coordinator identity and the private Approval Store record. The Store binds the
static summary only after provider/version/catalog validation. Raw command,
description, tool input and digest source material do not enter the public DTO.

The controlled P3 composition proves both allow and deny through the existing A1
claim, Claude delivery, provider-consumption witness, bound receipt and final Store
commit. It also proves mutated payload and stale-runtime failures do not commit.
Duplicate receipt recording does not create a second commit.

## Frozen boundary after acceptance

Production remains observation-only at this reviewed HEAD:

- `ManagedClaudeService` has no production install transition;
- `app.go` does not dispatch handler delivery to Claude;
- the generic `provenActionMapping` remains non-actionable;
- no existing non-actionable record is upgraded;
- mobile receives no Claude action buttons from this commit;
- unsupported version, non-catalog command, malformed input and stale identity
  remain non-actionable.

The accepted Codex path and provider-neutral A1 authority are unchanged.

## Verification basis

Independent review confirmed the final P3 assertions check the exact receipt and
commit binding rather than substring evidence: receipt ID, delivered payload
digest, approval/session/runtime identity, option, delivery schema, action and
payload digests, terminal state, duplicate non-commit, mutated payload rejection
and stale-runtime rejection. Focused race tests, backend build/vet, formatting and
diff checks passed on the reviewed tree.

## Next authorized work

Only `docs/NEXT_EXECUTOR_A1_2_C3D_PRODUCTION_ACTIVATION_HANDOFF.md` is active.
C3D must install the exact catalog capability atomically, reuse the authenticated
A1/mobile path, and prove real pinned-Claude allow and deny. N1 and post-Claude
cleanup remain blocked until independent final C3D/A1.2 acceptance.
