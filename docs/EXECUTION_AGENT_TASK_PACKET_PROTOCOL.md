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

## 5. Model-specific execution rules

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

## 6. Reviewer-to-executor remediation loop

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

## 7. Evidence levels

Reports must distinguish:

- **code present**: a type/helper exists;
- **focused proof**: the relevant unit or production-path test passes;
- **contract proof**: authority, negative, race, privacy, and failure-isolation
  cases pass;
- **full gate proof**: the prescribed final gate passed on the final tree;
- **physical/M-track proof**: an actual device or external environment was used.

Never claim a stronger level from weaker evidence. Environmental skips must be
listed explicitly.

## 8. Resource hygiene

Use bounded, reusable caches where possible. After validation, stop spawned
processes and remove stage-specific temporary worktrees, generated candidates,
build products, and caches that are not required for the next review. Report any
large retained directory and why it must remain. Never delete user files or
shared caches without explicit authorization.

## 9. When to rotate execution agents

A fresh execution agent is recommended after an independently accepted stage or
after a long remediation chain has accumulated stale assumptions. The outgoing
handoff must contain the canonical remote, exact accepted SHA, accepted
invariants, known deferred work, the next bounded task packet, gates, and stop
condition. Model rotation is not a substitute for a complete handoff.
