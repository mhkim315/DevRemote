# Next Executor Handoff — A1.2 C2D-D Safe Review and Final Evidence

Status: **SUPERSEDED AFTER D1 REJECTION — USE THE CLOSED-CATALOG HANDOFF**

Active instructions moved to
`docs/NEXT_EXECUTOR_A1_2_C2D_CATALOG_HANDOFF.md`. This file remains as the
historical handoff that correctly forced the D1 fail-closed decision.

This was the handoff that split the rejected arbitrary-command path into review
checkpoints. Its remaining D2/D3 instructions are no longer authorized. Follow the
closed-catalog handoff instead.

## 1. Canonical start state

- Remote: `https://github.com/mhkim315/DevRemote.git`
- Branch: `feature/phase10-multi-adapter`
- Checkout: `/Users/mhk/Documents/codex/DevRemote`
- Accepted C1D implementation: `4794ce7f42380c388c1a5614b4b2518bc1722870`
- Accepted C2D-C denial evidence: `dbf6e50feb316c5a2f0dd9728f9619efb11fa720`
- Accepted C2D-C R6-B implementation: `48ce57d81781646fc1c6c7445b5900b58fd5cef3`

Before work, fetch and fast-forward the named branch. Require local HEAD to equal
remote HEAD, all three SHAs above to be ancestors, and the worktree to be clean.
Use only the canonical checkout. Never force-push or rewrite history.

Read, in order:

1. `docs/A1_2_CLAUDE_APPROVAL_EXTENSION_PLAN.md`;
2. this handoff;
3. `docs/A1_2_C2D_PACKET_CONTRACT_NOTE.md`, especially section 7.3;
4. `docs/NEXT_EXECUTOR_A1_2_CLAUDE_APPROVAL_HANDOFF.md` for frozen exclusions;
5. accepted R6-B code and tests at `48ce57d`.

## 2. Frozen boundary

C2D-D may prove a safe public review of the already certified Claude `Bash`
request and assemble final controlled C2D evidence. It may not:

- install Claude delivery in the production composition root;
- make `provenActionMapping` non-empty or give a gate positive production capacity;
- add an API handler, mobile CTA, or mobile production behavior;
- alter the provider-neutral A1 store, claim, receipt, idempotency, or permission model;
- change the accepted C2D coordinator, denial decoder, witness routing, lifecycle,
  or runtime identity to make a projection test pass;
- add live Claude turns, provider upgrades, generic provider interfaces, N1, or C3D.

The projection is display evidence only. It does not create an Approval and is not
execution authority. Actionability remains zero throughout C2D-D.

## 3. D1 — projection contract decision

Start with a short contract amendment and a table containing:

- the exact stored certified source field used for review;
- its canonical byte representation and UTF-8 byte limit;
- every accepted and rejected Bash shape;
- fields allowed in the public projection;
- the relation between the complete projected action and the existing
  `ActionDigest`/input digest;
- failure behavior for secrets, absolute paths, controls, invalid UTF-8,
  unsupported shell forms, over-bound input, ambiguity, and canonicalization error.

The initial subset must be syntactic and closed. Do not certify arbitrary shell
text by attempting general redaction. A description, model summary, digest,
truncated prefix, normalized display text, or terminal output is never a substitute
for the complete execution-relevant action.

If no useful closed Bash subset can be represented completely inside the frozen
public DTO bounds without revealing prohibited material, record **C2D BLOCKED** and
stop. Do not broaden the DTO, weaken privacy, or start D2.

Commit D1 separately and stop for independent review. D2 is unauthorized until D1
is accepted.

## 4. D2 — pure safe-review projector

Implement only the accepted D1 projection as a pure bounded function close to the
Claude provider boundary. It returns either a complete immutable safe review or no
review; partial success is prohibited.

Required focused tests:

- exact accepted-shape golden and deterministic repeat;
- one-byte-under, exact-bound, and one-byte-over UTF-8 cases;
- unknown/missing/duplicate fields and wrong JSON types;
- invalid UTF-8, controls, newlines, shell metacharacters, substitutions and
  multi-command forms according to the D1 grammar;
- absolute and traversal paths, environment expansion, secret-shaped values and
  every D1 prohibited category;
- mutation/aliasing resistance;
- proof that an omitted or rejected projection produces no actionable mapping.

Critical tests must include a known-bad counterexample showing truncation or
display-only redaction would make two meaningfully different actions look the same.
Tests use deterministic barriers or immutable fixtures, never sleeps.

Do not add exported `ForTest` methods, production callbacks, observation channels,
or authority-mutating test seams. Test through the pure projector and existing
production entry boundaries. If a required test needs a change to accepted C2D-C
lifecycle or authority code, report **BLOCKED** before making that change.

Run only proportional format, diff, focused test/race, and secret checks for D2.
Commit and stop for independent D2 verification.

## 5. D3 — controlled composition and final C2D evidence

After D2 acceptance, add no new semantics. Assemble controlled production-
composition tests using the real A1 store, accepted Claude coordinator/delivery,
accepted denial decoder, and D2 projector while production installation stays off.

Prove both decisions:

- allow: exact binding and safe review, one decision write, matching PostToolUse
  witness, bound receipt and commit;
- deny: exact binding and safe review, one decision write, matching strict
  `permission_denials` witness, bound receipt and commit.

Retain negative coverage for modified input/projection, cross-session and
cross-tool substitution, duplicate/late witness, stale runtime, timeout,
stop/kill/delete, malformed denial, result-before/during/after write, and replay.
Inspect contested intermediate states; a later success must not hide an earlier
invalid write or commit.

No paid/live provider turn is required in D3. Accepted C0D/R6 evidence freezes
native provider behavior, and C3D owns final live/mobile proof. If that evidence
cannot answer a concrete contract question, stop and request independent approval
before a new live probe.

Freeze HEAD, run the complete repository gate, then write the bounded evidence
report. If the report changes HEAD, rerun documentation diff/secret checks on the
exact report HEAD. Push only the exact verified tree and stop with:

```text
REVIEW REQUEST: A1.2 C2D Exact Claude Decision Delivery — <implementation SHA>
```

C3D remains prohibited until independent final C2D ACCEPT.

## 6. Executor discipline and stop conditions

Before each packet, write the authority owner, immutable binding, state transition,
linearization point, success evidence, invalidation paths, capacity behavior and one
adversarial counterexample. Re-read this handoff before committing.

Use this execution order:

1. focused red test or counterexample;
2. smallest implementation;
3. focused deterministic tests;
4. proportional race/regression gate;
5. invariant-by-invariant self-audit;
6. packet commit and stop.

Do not run the full repository gate after every edit; run it only at D3 finalization.
Do not repair unrelated failures, redesign accepted seams, or call test-only behavior
production evidence. Report production-wired, controlled-composition, test-only,
skipped and blocked behavior separately.

Stop immediately when:

- the D1 safe-review contract cannot be satisfied;
- an accepted C2D-C contract would need to change;
- actionability or production composition would need to turn on;
- a test requires heuristic identity, command equality, timing or FIFO authority;
- a provider/live capability not frozen by accepted evidence becomes necessary.

The next independently reviewable unit is **D1 only**.
