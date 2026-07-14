# Next Session Handoff — A1 Approval Safety Remediation 7

Status: **READY — R7-A AND R7-B ONLY; POSITIVE PROVIDER PATH BLOCKED**

Risk class: **bounded delivery authority + concurrency evidence**

Preserve accepted A1 remediation work. Fix only independent re-verification-6
findings. Do not implement a provider channel and do not begin N1/O1/O2.

## 0. Canonical repository recovery

```text
repository: https://github.com/mhkim315/DevRemote.git
local path: /Users/mhk/Documents/codex/DevRemote
branch: feature/phase10-multi-adapter
reviewed report HEAD: 5a658ede9d7f590d6ed40cea1ca1d9baaef358a4
reviewed implementation: 17dd248e
accepted S1.1 ancestor: 02c8385e3270fbbc4df45e0c71ccad6ebe11a076
```

The repository root is the `DevRemote` child. Before edits, report `pwd`, remote,
branch, full local/remote SHAs, accepted-S1.1 and reviewed-implementation ancestry,
and clean worktree. Fetch and fast-forward only. Never reset, rewrite reviewed
history, discard another agent's work or force-push.

Read in order:

1. `docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md`;
2. `docs/A1_APPROVAL_SAFETY_PLAN.md`;
3. `docs/A1_APPROVAL_SAFETY_REVERIFICATION_6.md`;
4. this handoff;
5. remediation-6 contract note/report and current delivery code/tests.

Before production edits, write the required short contract note and binding
table. Freeze every retained field, its canonical format, individual byte bound,
aggregate accounting, failure behavior and test. First reproduce the unbounded
metadata acceptance and the omitted contested capacity state.

## 1. Preserve accepted properties

- complete requester/store authority and immutable claim binding;
- exact pre-append payload-digest equality;
- generation-owned opaque endpoint handles and typed defensive queue items;
- ordinary A-to-B replacement retains accepted A items;
- safe victim eligibility is retired, non-current and empty only;
- same-runtime/capacity activation is idempotent;
- entropy failure and payload/item/endpoint capacity fail closed;
- cleanup deactivates acceptance;
- production capacity zero, empty provider mapping and non-actionable UI;
- exact final-tree gate discipline.

## 2. Ordered task packets

### R7-A — bound all retained delivery metadata

The deepest gate boundary must not rely on the store as its only resource
validator. Define and reuse one canonical validator for endpoint identity and
`AcceptedDelivery` metadata.

At minimum:

- reuse `maxSessionIDLen`, `authMaxApprovalIDLen`, `maxVersionLen` and the existing
  canonical idempotency-key rule;
- bound adapter/provider identity with a closed or existing accepted grammar;
- require ActionDigest and PayloadDigest to use the canonical fixed-size digest
  encoding produced by the store;
- require ClaimToken to use the canonical opaque-token encoding produced by the
  store;
- bound ReceiptID construction and endpoint handle/nonce identity;
- calculate a repository-owned maximum retained metadata size per item and total
  retained queue bytes including metadata, not payload alone;
- bound SessionID and RuntimeRef metadata before `Activate` publishes an endpoint.

Every malformed, oversized or aggregate-full request fails before mutation,
receipt creation or append. Do not truncate authority fields. Do not silently
clamp a usable channel. Ensure defensive copies preserve the validated immutable
value.

Add non-vacuous tests for exact limit, one-over limit, malformed digest/token,
aggregate metadata exhaustion, payload-plus-metadata accounting and caller
aliasing. The reviewer counterexample with multi-megabyte metadata must fail and
leave queue/accounting unchanged.

### R7-B — complete frozen production and concurrency evidence

Add the tests required but omitted from remediation 6:

1. Drive the real accepted-adapter `TelemetryService.processSession` path through
   repeated correlated polls. Prove one endpoint/handle and no order growth for
   an unchanged RuntimeRef. Then advance the real launch or stream generation and
   prove exactly one replacement.
2. Fill all endpoint slots with active endpoints and prove the next activation
   fails with current mappings and ownership unchanged.
3. Fill the bound while a retired endpoint holds an accepted item. Attempt the
   extra activation before drain; it must fail and the captured item must remain.
4. Drain/deactivate an endpoint and prove it becomes the deterministic safe
   victim without affecting another endpoint.
5. Use deterministic barriers/channels for accept versus activation/reclamation.
   Inspect the contested intermediate mapping, queue, item count and total-byte
   state. Include a known-bad control or fault seam demonstrating the test would
   catch destructive or post-mutation admission.

Do not use sleeps. Do not allow a later successful activation or drain to mask an
earlier mutation. Keep all external I/O outside the gate mutex.

### R7-C — preserve the honest production blocker

Reconfirm only from existing production code. Keep provider mapping empty,
capacity zero, action controls hidden and A1 **BLOCKED**. No provider research,
fixtures substituted as production evidence, or delivery implementation is
authorized.

## 3. Required gates and stop condition

Run focused delivery/store/telemetry tests under `-race`, the deterministic
capacity interleavings, accepted A1/T0/T1/T2/S1/S1.1 regressions, backend
build/vet/full race, mobile TypeScript/full Jest, Android when available,
invariants, secret scan and `git diff --check`.

Freeze HEAD before the authoritative final gate. Commit and push focused code,
then add the report and run the complete documentation-sensitive gate on that
exact report HEAD. Any later tree change requires a rerun. Report local/remote
equality, ancestry and clean worktree. Stop for independent re-verification; N1
remains blocked.

## 4. Explicit exclusions

No provider implementation/research, N1 notifications, Task/Dispatch, worker
acknowledgement/completion, Executor-Verifier, automatic policy, generic terminal
commands, CLI redesign, ConPTY/Windows, cloud relay, lock-screen actions, O1 or
O2.
