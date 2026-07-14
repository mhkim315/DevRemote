# Next Session Handoff — A1 Approval Safety Remediation 6

Status: **READY — R6-A THROUGH R6-C ONLY; R6-D PRESERVED; R6-E BLOCKED**

Risk class: **delivery authority + concurrency + bounded ownership**

Preserve accepted A1 remediation work. Fix only independent re-verification-5
findings. Do not begin N1/O1/O2 and do not implement a provider channel.

## 0. Canonical repository recovery

```text
repository: https://github.com/mhkim315/DevRemote.git
local path: /Users/mhk/Documents/codex/DevRemote
branch: feature/phase10-multi-adapter
reviewed report HEAD: 2e96f24f96eec30d117fd3d7ec2a16fcf678445a
reviewed implementation: 0f95c7c3cdac6dc1524a363027829782dd09579e
accepted S1.1 ancestor: 02c8385e3270fbbc4df45e0c71ccad6ebe11a076
```

The repository root is the `DevRemote` child. Before edits, report `pwd`, remote,
branch, full local/remote SHAs, accepted-S1.1 and reviewed-implementation ancestry,
and clean worktree. Fetch and fast-forward only. Never reset, rewrite reviewed
history, discard another agent's work or force-push.

Read in order:

1. `docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md`;
2. `docs/A1_APPROVAL_SAFETY_PLAN.md`;
3. `docs/A1_APPROVAL_SAFETY_REVERIFICATION_5.md`;
4. this handoff;
5. remediation-5 contract note/report and current code/tests.

Before implementation, write the required bounded contract note. Its tables must
freeze pre-append validation, endpoint-safe eviction eligibility, activation
failure atomicity, same-runtime idempotence and the exact final-tree gate. First
reproduce both reviewer failures before modifying the gate.

## 1. Preserve accepted properties

- complete requester and permission validation at the store;
- exact claim/idempotency/action/receipt comparisons;
- captured opaque endpoint handles and typed defensive queue items;
- ordinary A→B replacement retains accepted A items and rejects new A accepts;
- entropy failure, item/byte bounds, queue-full and capacity-zero fail closed;
- all cleanup owners deactivate acceptance;
- exact final report HEAD gate discipline;
- production capacity zero and empty provider mapping;
- no blind input, generic command queue, public binding/token exposure or fake
  positive provider evidence.

## 2. Ordered task packets

### R6-A — validate the exact item before daemon acceptance

The gate must not treat `ApprovalExecutionBinding` and payload as unrelated
trusted inputs. Before append, validate the canonical internal item, including:

- non-empty ApprovalID, SessionID, ActionDigest, PayloadDigest, idempotency key
  and opaque claim token;
- Binding.SessionID equals the current endpoint session;
- Binding.Runtime exactly equals the endpoint RuntimeRef;
- Binding.PayloadDigest exactly equals the domain-separated digest of the exact
  payload bytes being appended;
- payload and metadata bounds.

Use the existing canonical key/binding validators where appropriate; do not
invent a second action canonicalization model. Any mismatch returns
non-acceptance, produces no ReceiptID/handle and appends nothing.

First add the reviewer regression: binding owns the digest of one payload, request
carries different bytes, real gate must reject and captured drain remains empty.
Add individually malformed-field, cross-session/runtime and aliasing negatives.

### R6-B — safe bounded endpoint admission, never destructive eviction

Define safe eviction as: retired, non-current, empty queue, zero queued bytes and
no outstanding accepted ownership. Only such endpoints may be reclaimed.

When endpoint capacity is exhausted and no safe victim exists, `Activate` must
fail before retiring the current endpoint or publishing a new one. Existing
handles, current mapping, queues, total bytes and receipt ownership must remain
unchanged. Never evict an active endpoint or a retired endpoint with accepted
items merely to satisfy a bound.

First add the reviewer regression: an endpoint with one successful accepted item
survives attempts to exceed `maxGateEndpoints`; drain by its handle still returns
that exact item. Add:

- all-active exhaustion;
- retired-nonempty exhaustion;
- retired-empty reclamation;
- failed activation leaves current unchanged;
- concurrent accept/activate/reclaim deterministic barriers;
- total endpoint/item/byte invariant assertions.

Do not solve this with unbounded retention. Explicit fail-closed admission is the
required backpressure.

### R6-C — idempotent production activation for an unchanged RuntimeRef

Audit the actual `TelemetryService.processSession` polling path. An identical
SessionID + RuntimeRef + no-channel configuration must reuse the current endpoint
and return/preserve the same handle; it must not allocate entropy, append to
endpoint order or retire the current endpoint on each poll.

A changed launch/stream/provider/version or explicit channel configuration change
remains a true serialized replacement. Keep replacement and cleanup behavior
distinct.

Add a production-path test that processes repeated correlated polls with the same
runtime and proves one endpoint/handle, followed by a genuine generation change
that produces exactly one replacement. Direct gate idempotence tests alone are not
sufficient.

### R6-D — preserve exact final-tree gate integrity

Do not change the accepted scanner behavior or reintroduce token-shaped examples.
After the report/REVIEW REQUEST is committed, rerun the complete
documentation-sensitive gate on that exact candidate. Any later tree change
requires rerun.

### R6-E — positive provider path remains blocked

Reconfirm only from existing production code. Keep mapping empty, capacity zero,
actions hidden and A1 **BLOCKED** if no controlled channel exists. No provider
research or implementation is authorized.

## 3. Required audit and gates

Map every pre-append field check, endpoint admission/reclamation transition and
same-runtime production activation to exact code and non-vacuous tests. Identify
the shared linearization points and verify no later success masks a failed
admission.

Run focused gate/store/telemetry tests under `-race`, deterministic mismatch and
capacity races, accepted A1/T0/T1/T2/S1/S1.1 regressions, backend build/vet/full
race, mobile TypeScript/full Jest, Android when available, invariants, secret scan
and `git diff --check`.

Freeze HEAD, commit/push focused work, add the report, then run the final gate on
the exact report candidate. Report local/remote equality, ancestry and clean
worktree. Stop for independent re-verification; N1 remains blocked.

## 4. Explicit exclusions

No N1 notifications, provider implementation/research, Task/Dispatch, worker
acknowledgement/completion, Executor-Verifier, automatic policy, generic terminal
commands, CLI redesign, ConPTY/Windows, cloud relay, lock-screen actions, O1 or
O2.
