# Execution Agent Task Packet Protocol

Status: **ACTIVE OPERATING RULE**

This document records the execution pattern that produced the fastest reliable
remediation during S1-B/C. It is intended for fresh execution agents, including
Claude Code sessions backed by DeepSeek V4 Pro, and applies to future bounded
stages unless the authoritative stage handoff says otherwise.

## 1. Why this protocol exists

Small scope helped S1-B/C, but scope alone was not sufficient. The material
improvement came from replacing broad requests such as "harden status handling"
with executable, one-to-one requirements:

```text
exact production caller
+ forbidden behavior
+ required code behavior
+ required negative test
+ completion evidence
```

This reduces semantic improvisation. The execution agent still chooses the
smallest implementation, but it does not have to guess which authority boundary,
race, fallback, or production path the reviewer will inspect.

## 2. Mandatory repository recovery

Every fresh agent must begin from the canonical remote and full SHA recorded in
the active handoff. It must fetch before concluding that a commit or document is
missing. Local chat summaries and an old worktree are not authoritative.

The agent must report, before editing:

- canonical remote URL;
- branch;
- local full SHA and remote full SHA;
- clean/dirty worktree state;
- required baseline ancestry results;
- the exact authoritative handoff it read.

It must not recreate a missing handoff from memory, use `reset --hard`, discard
another agent's changes, or force-push.

## 3. Unit of work

One task packet covers one bounded production boundary. Examples are:

- accepted evidence -> session-owned status store;
- authenticated API -> strict mobile DTO;
- approval request -> digest-bound user decision;
- orchestrator transition -> one terminal state.

Do not combine an adjacent roadmap stage merely because the code is nearby.
Checkpoint commits should remain reviewable and stage-prefixed.

## 4. Required task packet format

Every execution instruction should contain these fields:

```text
Stage and checkpoint:
Canonical repository / branch / exact baseline:
Authoritative handoff:
Allowed implementation scope:
Explicitly prohibited scope:
Exact production caller and required call graph:
Existing accepted inputs/contracts:
Authority and privacy invariants:
Risk class (ordinary / authority / concurrency):
Required pre-implementation contract note (if authority/concurrency):

Blocker matrix:
  ID:
  Bad behavior to prevent:
  Required production behavior:
  Required negative/production-path test:

Focused commands:
Full final gate:
Required report/evidence:
Commit/push rules:
Stop condition:
```

Each blocker must map to at least one test that would fail if the blocker were
reintroduced. A helper-only test is insufficient when the requirement names a
production caller, route, import chain, or component.

## 5. Risk-proportional contract note

The full discipline in this section is mandatory for authentication,
authorization, approval or execution authority, runtime identity, lifecycle,
persistence, replay, idempotency, cross-component delivery, and concurrency
work. An ordinary UI or mechanical change needs only a brief scope and
regression note.

For authority or concurrency work, the handoff must require a short
pre-implementation contract note. The executor must finish and self-check this
note before editing production code. Keep it bounded: normally one page or less,
or a compact table plus state diagram. It must identify:

- authority owner;
- canonical stored inputs;
- complete immutable binding;
- state transitions;
- the single linearization point shared by conflicting operations;
- exact success evidence;
- failure, invalidation, cleanup, and restart behavior;
- explicit capacity behavior for every affected bounded resource;
- one adversarial counterexample for each critical invariant;
- explicit non-goals.

The note must include a binding table with one row per authority field and these
columns:

```text
Field | Created/derived at | Stored at | Recomputed/compared at |
Copied into request/receipt at | Invalidated at | Negative test
```

This note is not an invitation to redesign the stage. If it reveals that the
handoff cannot be satisfied within scope, the executor must report that fact
before implementation rather than silently expanding the task.

To prevent analysis from replacing progress, the executor should stop extending
the note once every required row and transition is explicit. New architecture,
future-stage concepts, and speculative fields are prohibited unless needed to
close a demonstrated current blocker.

## 6. Authority-boundary implementation rules

The handoff for authority-sensitive work must state all of the following:

1. Enforce each invariant at the deepest callable authority boundary. A correct
   HTTP handler does not make a store, registry, broker, or delivery interface
   safe for other callers.
2. Derive or independently recompute permissions, identities, digests,
   generations, runtime references, provider identity, and requester context
   from canonical stored state or authenticated server context whenever
   possible. Do not trust a caller merely because it is currently internal.
3. Use one canonical representation across claim, delivery, receipt, replay,
   retry, and commit. Compare every applicable binding field at every authority
   transition.
4. If an existing abstraction cannot enforce the contract, first demonstrate
   the bypass with a counterexample or negative test. Then replace or isolate
   the smallest unsafe boundary. Do not retain an unsafe abstraction solely to
   minimize the diff, and do not perform a broad rewrite without this evidence.
5. Fail closed on missing authentication/permission context, uncertain or stale
   runtime identity, unproven provider mapping, canonicalization failure,
   incomplete binding/receipt, capacity exhaustion, generation change, and an
   unresolved race.
6. Every bounded map, queue, ledger, cursor, or store must define capacity
   behavior. Silent omission followed by continued authority-sensitive work is
   forbidden.

The task packet must name direct API-level negative tests when a boundary is
callable independently. Production handlers and trusted callers are not a
substitute for testing the authority object itself.

## 7. Concurrency and external-I/O rules

Before implementing a concurrency fix, the task packet must describe the
failing interleaving and the contested intermediate state. The executor must:

- use deterministic barriers, channels, hooks, or fault injection rather than
  timing sleeps;
- assert intermediate observable states, not only the final state;
- ensure a later successful operation cannot mask an earlier regression;
- identify the one linearization point used by all conflicting operations;
- document lock ordering when more than one lock or component participates;
- never hold internal state locks across external I/O;
- use generation-bound claim, delivery acceptance, invalidation, and commit so
  stale completion cannot defeat replacement or cleanup.

For a critical race test, the report must explain why the test is non-vacuous.
Where practical, a negative control should show that removing/bypassing the
serialization or binding makes the test fail with the intended interleaving.

## 8. Test-quality and completion audit

Security and concurrency tests must exercise the production authority boundary.
A known-bad fixture, fault injection, dependency stub, deliberately invalid
binding, or regression reproduction may drive the failure, but test-only wiring
must not bypass the production operation being claimed.

Heuristics, prompt/screen parsing, synthetic terminal input, fake receipts,
test-only providers, and unavailable implementations cannot prove production
support. Tests may use controlled doubles to prove internal machinery, but the
report must label that evidence `test-only` and must not use it to satisfy a
required positive production-path gate.

Before the implementation commit, the executor must:

1. re-read the authoritative handoff;
2. produce an invariant-by-invariant self-audit;
3. map every requirement to exact production code and a non-vacuous test;
4. distinguish `production-wired`, `test-only`, `unavailable`, `skipped`, and
   `blocked` behavior;
5. compare the completed binding table with the pre-implementation table and
   explain any authorized change.

Repeat the audit before push only if rebase, conflict resolution, generated
output, an additional commit, or another tree-changing operation occurred.
Freeze HEAD before the authoritative final gate and push that exact verified
tree. Do not commit or amend while the final gate is running. Green tests are
necessary but do not establish contract completeness by themselves.

## 9. Model-specific execution rules

For a Claude Code + DeepSeek V4 Pro execution session:

1. Inspect the existing call graph before adding types or helpers.
2. Restate the exact caller, allowed files, and prohibited files before editing.
3. Implement blockers in the supplied order; do not redesign accepted contracts.
4. Prefer closed vocabularies, exact equality, explicit generation/session
   binding, and fail-closed validation over inference.
5. Never add a legacy fallback merely to make a product path appear reachable.
6. Test the imported production function/component, not a copied equivalent.
7. Treat skipped required tests, zero executed tests, or a pre-change gate as a
   failure, not a pass.
8. Use the full SHA in the final report and include a `REVIEW REQUEST` marker only
   at the acceptance boundary named by the handoff.
9. Stop at the stated checkpoint. Do not start the next roadmap stage.

For this model pairing, handoffs should prefer a small numbered packet per
blocker, exact file/symbol boundaries, a concrete forbidden behavior, and the
negative test that must fail before the fix. Avoid broad verbs such as "harden",
"make safe", or "handle races" without the binding/interleaving described above.

## 10. Reviewer-to-executor remediation loop

The reviewer should return a numbered, line- or path-linked blocker list. Each
blocker should state the violated contract and the minimum acceptance proof. The
execution agent then:

1. changes only the blocker-related scope;
2. adds the matching regression test;
3. runs focused tests first;
4. runs the required full gate on the final tree;
5. creates one narrow remediation commit;
6. pushes without rewriting reviewed history;
7. reports local/remote SHA equality and a clean worktree;
8. stops for re-verification.

Do not convert a release blocker into an undocumented follow-up. Conversely, do
not broaden a narrow blocker into an architectural rewrite without a separately
reviewed reason.

## 11. Evidence levels

Reports must distinguish:

- **code present**: a type/helper exists;
- **focused proof**: the relevant unit or production-path test passes;
- **contract proof**: authority, negative, race, privacy, and failure-isolation
  cases pass;
- **full gate proof**: the prescribed final gate passed on the final tree;
- **physical/M-track proof**: an actual device or external environment was used.

Never claim a stronger level from weaker evidence. Environmental skips must be
listed explicitly.

## 12. Blocked work and honest partial results

When a required production capability cannot be proven, the executor must:

- leave the unsupported behavior disabled;
- finish only independent safe work explicitly permitted by the handoff;
- label the milestone `BLOCKED`, not complete or ready for acceptance;
- list the missing external evidence or authority without weakening the gate;
- stop before scope expansion, heuristic substitution, or work on the next
  milestone.

An unavailable, heuristic, non-actionable, or test-only path is never a
completed production capability. A safe partial implementation may be retained
when independently useful and authorized, but it cannot satisfy the missing
positive path.

## 13. Resource hygiene

Use bounded, reusable caches where possible. After validation, stop spawned
processes and remove stage-specific temporary worktrees, generated candidates,
build products, and caches that are not required for the next review. Report any
large retained directory and why it must remain. Never delete user files or
shared caches without explicit authorization.

## 14. Future handoff author checklist

Every new authority/concurrency handoff must reference this protocol and include:

- the risk class and required pre-implementation contract note;
- the complete binding fields, not only the high-level object names;
- the authority owner and deepest callable enforcement boundary;
- the forbidden caller-supplied authority fields;
- explicit capacity and restart semantics;
- the failing race interleaving and required deterministic test seams;
- production-positive evidence required for acceptance;
- an honest blocked outcome when that evidence is unavailable;
- the pre-commit audit, stable-HEAD gate, conditional pre-push re-audit, and stop
  condition.

Historical accepted handoffs do not need rewriting. Active and future handoffs
must use this checklist whenever their scope enters the risk classes in section
5.

## 15. When to rotate execution agents

A fresh execution agent is recommended after an independently accepted stage or
after a long remediation chain has accumulated stale assumptions. The outgoing
handoff must contain the canonical remote, exact accepted SHA, accepted
invariants, known deferred work, the next bounded task packet, gates, and stop
condition. Model rotation is not a substitute for a complete handoff.
