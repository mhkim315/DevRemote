# Next Session Handoff — A1 Approval Safety Remediation 5

Status: **SUPERSEDED — REMEDIATION 5 REJECTED; USE REMEDIATION 6 HANDOFF**

Current execution authority is
`docs/NEXT_SESSION_A1_APPROVAL_SAFETY_REMEDIATION_6_HANDOFF.md` after reading
`docs/A1_APPROVAL_SAFETY_REVERIFICATION_5.md`. Preserve this document as reviewed
history; do not execute it again.

Risk class: **authority + concurrency + bounded persistence + gate integrity**

Preserve accepted A1 remediation work. Fix only independent re-verification-4
findings. Do not begin N1/O1/O2 and do not invent the missing provider channel.

## 0. Canonical repository recovery

```text
repository: https://github.com/mhkim315/DevRemote.git
local path: /Users/mhk/Documents/codex/DevRemote
branch: feature/phase10-multi-adapter
reviewed report HEAD: d3aa0b098af945a96e5175fefef21f3dbf6db44c
reviewed implementation: 5360ec617efd84a6b4ad1d0421452c1fe59acaad
accepted S1.1 ancestor: 02c8385e3270fbbc4df45e0c71ccad6ebe11a076
```

The repository root is the `DevRemote` child above, not its parent. Before any
edit, print and report `pwd`, remote URL, branch, full local/remote SHAs,
accepted-S1.1 and reviewed-implementation ancestry, and clean worktree. Fetch and
fast-forward only. Never reset, rewrite reviewed history, discard another
agent's work or force-push.

Read in order:

1. `docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md`;
2. `docs/A1_APPROVAL_SAFETY_PLAN.md`;
3. `docs/A1_APPROVAL_SAFETY_REVERIFICATION_4.md`;
4. this handoff;
5. remediation-4 contract note/report and current production code/tests.

Before production edits, write the required short contract note. Freeze:

- exact accepted queue-item binding;
- endpoint identity and ownership across replacement;
- acceptance and drain linearization points;
- retired-endpoint, item and byte bounds;
- receipt entropy failure;
- cleanup/restart semantics;
- the exact final-tree gate procedure.

First reproduce the generation-A accepted-item loss as a failing test. Do not
start R5-A implementation until the binding/lifecycle table is complete.

## 1. Preserve these accepted properties

- complete requester presence and stored permission are checked at the store for
  fresh claim, retry and replay;
- idempotency, action/payload digest, claim and receipt comparisons remain exact;
- registry disappearance and explicit cleanup deactivate old acceptance;
- no arbitrary callback is invoked under the transition mutex;
- queue full/capacity zero fail closed;
- bounded manual retry, safe DTOs, authenticated device transport and log
  redaction remain unchanged;
- production stays non-actionable with capacity zero and empty provider mapping;
- no blind Y/N, generic command queue, fake receipt or test-only production claim.

## 2. Ordered task packets

### R5-A — immutable generation endpoint ownership

The current `Drain(sessionID)` is a mutable lookup and `Activate` destroys the
old queue. Replace or isolate that boundary so a successful acceptance returns
or records ownership in one immutable generation endpoint. Replacement must
atomically close A to new accepts and publish B, while already-accepted A items
remain associated only with A until their bounded terminal disposition.

First add this deterministic regression:

```text
activate A -> accept item A -> publish B -> drain captured A
```

It must return exactly item A; current-session drain must never redirect it to B.
Also prove B publication cannot mask an A loss and deactivated A accepts nothing
new. Define bounded retention/eviction and daemon-restart behavior. Eviction may
produce explicit non-success/degradation but may not silently preserve a success
receipt while destroying its only accepted item.

### R5-B — one typed, fully-bound queue item

Replace raw `[][]byte` entries with an internal immutable value that binds at
least:

- ApprovalID and SessionID;
- full RuntimeRef;
- ActionDigest and PayloadDigest;
- idempotency key;
- opaque claim ownership/reference;
- ReceiptID;
- defensive exact payload bytes.

The gate accepts this item atomically and returns the same ReceiptID represented
in the item. Drain returns defensive typed copies from the captured endpoint.
Every field changing delivery meaning must be compared or derived from the
claim's canonical binding. Internal tokens/bindings remain server-side and never
enter public/mobile DTOs.

Add cross-approval, cross-generation, receipt mismatch, payload mutation and
caller-aliasing negatives. A no-payload option must still remain distinguishable
by its bound action, not by raw bytes.

### R5-C — fixed resource and entropy failure boundaries

Replace the fixed nonce fallback with fail-closed behavior. Introduce a narrow
test seam for entropy failure; failed receipt identity generation must append
nothing and return unavailable/non-acceptance.

Enforce repository-owned constants for:

- maximum active/retired endpoints;
- maximum items per endpoint;
- maximum total queued bytes;
- maximum item bytes and metadata.

Reject or clamp nothing silently: negative, zero/no-channel, oversized and full
states need explicit fail-closed outcomes. Test every boundary and ensure a later
success cannot hide an earlier rejected append.

### R5-D — exact final-tree gate integrity

Rephrase or safely assemble token-shaped examples in remediation-4 documents so
the secret scan passes without an exclusion. Do not weaken the scanner or add a
path/content bypass.

Run focused and full gates on a frozen implementation tree. After adding the
report/REVIEW REQUEST, rerun at least `git diff --check`, the real secret scan and
all checks whose inputs include documentation. Prefer running the complete gate
on the exact final remote candidate. If any commit, rebase or report edit changes
the tree afterward, repeat the required audit/gate. Report the actual final HEAD,
not the earlier implementation-only result.

### R5-E — positive provider path remains blocked

Reconfirm only from current accepted production code. If mapping/channel remains
unavailable, keep production capacity zero, mapping empty and all approvals
non-actionable. Report A1 **BLOCKED** and stop. Do not implement or research a
provider channel, synthesize input, or change acceptance criteria.

## 3. Required audit and gates

Before commit, map every queue-item field from claim to acceptance, receipt,
drain and invalidation. Map endpoint identity through activation, replacement,
deactivation, retention and cleanup. Identify the exact shared linearization
points. Label production-wired, test-only, unavailable, skipped and blocked.

Run focused store/gate/cleanup tests under `-race`, deterministic replacement
and entropy/capacity negatives, accepted A1/T0/T1/T2/S1/S1.1 regressions,
backend build/vet/full race, mobile TypeScript/full Jest, Android when available,
invariants, secret scan and `git diff --check`.

Freeze HEAD, complete the invariant audit, commit/push focused work, and run the
final documentation-sensitive gate on that exact tree. Report local/remote
equality, ancestry and clean worktree. Stop for independent re-verification; N1
remains blocked.

## 4. Explicit exclusions

No N1 notifications, provider implementation/research, Task/Dispatch, worker
acknowledgement/completion, Executor-Verifier, automatic policy, generic terminal
commands, CLI redesign, ConPTY/Windows, cloud relay, lock-screen actions, O1 or
O2.
