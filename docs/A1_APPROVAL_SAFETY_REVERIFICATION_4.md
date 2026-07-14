# A1 Approval Safety — Independent Re-verification 4

Verdict: **REJECT — remediation 5 required; production path remains BLOCKED**

Reviewed branch: `feature/phase10-multi-adapter`

Reviewed report HEAD: `d3aa0b098af945a96e5175fefef21f3dbf6db44c`

Reviewed implementation: `5360ec617efd84a6b4ad1d0421452c1fe59acaad`

Accepted S1.1 ancestor: `02c8385e3270fbbc4df45e0c71ccad6ebe11a076`

Local/remote equality, accepted-S1.1 and implementation ancestry, clean worktree
and `git diff --check` passed at the reviewed remote. The submitted
agent/term/transcript race suite passed independently. The full build gate did
**not** pass at the final report HEAD: backend build/vet/race, mobile TypeScript
and Jest, and invariants passed; Android remained the documented environmental
skip; secret scan failed on two newly committed documentation lines. Green
implementation-HEAD tests cannot substitute for the exact final-tree gate.

## 1. Accepted remediation-4 changes

Preserve these corrections:

- `RequesterContext.present` now requires DeviceID, HostID, BearerSessionID and
  BootID, and one store-level validator precedes fresh claim, retry and
  idempotent replay;
- registry disappearance now gathers pruned IDs under `TelemetryService.mu` and
  performs store/gate cleanup after releasing that mutex;
- the arbitrary `DeliverySink` callback was removed from the transition lock;
- queue-full and capacity-zero paths return non-acceptance;
- the heading-content secret-scan exclusion was removed.

The reviewer's prior empty-host/empty-boot and post-reconcile old-endpoint
counterexamples now pass in the submitted tests.

## 2. Blocking findings

### R5-A — accepted work is destroyed by generation replacement

`internal/term/approval_delivery.go`, `RuntimeDeliveryGate.Activate`, replaces
the session map entry with a new `genEndpoint`. `Drain` accepts only a SessionID
and looks up that current map entry. It does not hold or accept a captured
generation endpoint.

The reviewer reproduced this through the real gate:

```text
activate generation A
accept payload for A -> success receipt
activate generation B
drain(session) -> empty
```

The test failed because the generation-A queue was unreachable after replacement.
The implementation may therefore return `accepted` and let the approval store
commit success even though the only queued copy is discarded before the external
drain can own it. This contradicts the handoff requirement that external I/O
drain the captured immutable generation endpoint.

Minimum correction: define the accepted-item lifecycle before coding. A
replacement must prevent new old-generation accepts without silently destroying
items for which successful receipts already exist. Use an opaque generation
endpoint/handle or equivalent immutable ownership; do not perform a mutable
SessionID lookup during drain. Bound retained/retired endpoints and define their
restart and cleanup policy fail closed.

### R5-B — the queue does not contain the exact bound approval request

`genEndpoint.queue` is `[][]byte`. `Accept` receives only SessionID, RuntimeRef
and payload; the queue entry contains no ApprovalID, ActionDigest,
PayloadDigest, idempotency key, claim token or ReceiptID. `Drain` returns only raw
byte slices. The `DeliveryReceipt` binding exists on a separate call stack and is
not joined to the queued work.

This is not the exact approval-specific delivery item frozen by the A1 plan. A
future drain cannot prove which approval/binding its bytes belong to, cannot
correlate the returned receipt ID, and cannot distinguish different no-payload
options. Raw bytes alone are an unsafe generic queue boundary.

Minimum correction: enqueue one immutable internal item containing the complete
`ApprovalExecutionBinding`, opaque claim ownership or safe internal reference,
ReceiptID and exact payload/digest. Drain typed defensive copies from the
captured endpoint. No internal binding or token may enter public/mobile DTOs.
Add mutation, cross-approval, cross-generation and receipt/item mismatch tests.

### R5-C — receipt entropy and capacity fail open or remain caller-defined

`newGateNonce` returns the fixed string `"n"` when `crypto/rand.Read` fails. A
new endpoint can then reuse the same nonce and sequence, creating duplicate
ReceiptIDs instead of failing closed. `Activate` also accepts an arbitrary
integer capacity with no fixed maximum; the queue is only as bounded as an
internal caller chooses.

Minimum correction: entropy failure must make activation/acceptance unavailable
and append nothing. Enforce one repository-owned maximum queue size; negative,
zero, oversized and exhausted capacity must all have explicit fail-closed
semantics. Provide deterministic entropy-failure and capacity-overflow tests.

### R5-D — the exact submitted HEAD fails the mandatory secret gate

The implementation report says the secret scan passed at frozen implementation
HEAD `5360ec617`, then commits report/contract-note HEAD `d3aa0b09`. At the latter
HEAD, the build gate reports two findings in those new documents because they
contain token-shaped examples. The executor did not rerun the authoritative gate
after the tree-changing report commit.

Remove or assemble/rephrase token-shaped documentation examples without adding
scanner exclusions. Run the complete authoritative gate only after the final
report commit, or commit a final marker only after a frozen-tree gate whose
inputs cannot change. Report exact HEAD truthfully.

### R5-E — mandatory positive provider path remains unavailable

The production endpoint still has capacity zero, `provenActionMapping` remains
empty, and there is no controlled provider delivery channel. This is the honest
and safe state. Keep every production approval non-actionable and do not use the
new queue work to imply provider support.

Even after R5-A through R5-D pass, A1 remains BLOCKED until the mandatory
positive provider path is separately proven or the acceptance contract is
separately reviewed and changed. N1 remains blocked.

## 3. Required remediation-5 evidence

1. An item accepted by generation A remains owned by the captured A endpoint
   across A-to-B replacement; it is neither lost nor delivered to B.
2. Replacement/deactivation prevents every new A acceptance.
3. Every queue item is immutably bound to the exact approval, runtime, action,
   payload digest, idempotency key and receipt identity.
4. Drain cannot use a mutable SessionID lookup to cross generations and returns
   defensive typed copies.
5. Retired-endpoint, queue and item bounds are repository-owned constants;
   exhaustion fails closed.
6. Entropy failure emits no accepted receipt and enqueues nothing.
7. Cleanup/replacement-versus-accept/drain interleavings use deterministic
   barriers and inspect intermediate state.
8. The exact final report HEAD passes the full gate without a secret-scan bypass.
9. Accepted R4-A, R4-B and prior A1 authority/DTO behavior remains green.
10. Positive provider delivery remains an explicit release blocker if unavailable.

## 4. Scope and next action

This remains A1-only remediation. Do not begin N1, Task/Dispatch, worker
acknowledgement/completion, provider implementation/research, generic command
execution, CLI redesign, ConPTY/Windows, cloud relay, lock-screen actions, O1 or
O2.

Continue only from
`docs/NEXT_SESSION_A1_APPROVAL_SAFETY_REMEDIATION_5_HANDOFF.md`.
