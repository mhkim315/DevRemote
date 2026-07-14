# A1 Approval Safety — Independent Re-verification 6

Verdict: **REJECT — remediation 7 required; production path remains BLOCKED**

Reviewed branch: `feature/phase10-multi-adapter`

Reviewed report HEAD: `5a658ede9d7f590d6ed40cea1ca1d9baaef358a4`

Reviewed implementation: `17dd248e`

Accepted S1.1 ancestor: `02c8385e3270fbbc4df45e0c71ccad6ebe11a076`

Local/remote equality, accepted-S1.1 and implementation ancestry, clean
worktree and `git diff --check` passed. Focused delivery-gate and telemetry tests
passed under `-race`. The full gate passed independently on the exact report
HEAD after rerunning outside the filesystem/network sandbox: backend build,
vet and race, mobile TypeScript and 383 Jest tests, invariants and secret scan
passed. Android/Kotlin remained the documented environmental skip.

## 1. Accepted remediation-6 changes

Preserve these corrections:

- `RuntimeDeliveryGate.Accept` checks payload-digest equality before append, so
  substituted bytes receive no accepted receipt and enter no queue;
- endpoint admission no longer evicts active or retired-nonempty endpoints;
- entropy is acquired before mutation and an admission failure preserves the
  previous mapping and queues;
- exact same SessionID, RuntimeRef and effective capacity reuse the current
  endpoint handle;
- a genuine runtime replacement still retires A while retaining A's accepted
  items for captured-handle drain;
- the exact final report HEAD passes the documentation-sensitive gate without a
  scanner exclusion.

These close the original R6-A payload-substitution defect and the underlying
R6-B/R6-C implementation defects. They do not close all frozen remediation-6
acceptance requirements.

## 2. Blocking findings

### R7-A — accepted queue metadata remains unbounded

`companion-daemon/internal/term/approval_delivery.go`,
`RuntimeDeliveryGate.Accept`, bounds `req.Payload` and accounts only payload
bytes in `queuedBytes` and `totalBytes`. It copies the complete
`ApprovalExecutionBinding` and `ClaimToken` into `AcceptedDelivery` without
bounding their stored strings. `Activate` likewise stores caller-provided
session, adapter and version strings without a local bound.

The remediation-5 and remediation-6 handoffs explicitly require “maximum item
bytes and metadata” and “payload and metadata bounds.” Non-empty checks are not
resource bounds. The reviewer reproduced the real gate accepting and retaining
one item containing multi-megabyte ApprovalID, ActionDigest and ClaimToken values
while accounting only its one-byte payload. Thus `maxGateTotalQueuedBytes` is
not a total queued-memory bound and the deepest callable acceptance boundary can
retain memory far beyond its advertised limit.

Minimum correction: define one canonical internal delivery-metadata validator
and repository-owned individual/aggregate byte bounds. Reuse existing session,
approval-ID, version and idempotency limits; require canonical fixed-size digest
and opaque-token encodings where those values are generated canonically. Account
all retained variable-size metadata plus payload before append. Bound endpoint
identity metadata before publication. Any over-limit or malformed value must
fail before mutation, receipt creation or append. Add exact-boundary,
one-over-boundary, aggregate-exhaustion and caller-aliasing negatives.

### R7-B — mandatory R6-B/R6-C production and concurrency evidence is absent

The submitted tests prove direct-gate happy cases but omit explicit tests frozen
in `NEXT_SESSION_A1_APPROVAL_SAFETY_REMEDIATION_6_HANDOFF.md`:

- there is no `TelemetryService.processSession` repeated-correlated-poll test
  proving a single production endpoint/handle and exactly one real-generation
  replacement; `TestDeliveryGate_IdempotentSameRuntimeActivation` calls the gate
  directly;
- the safe-eviction test fills to the bound, drains the protected item, and only
  then attempts the next activation. It does not attempt exhaustion while the
  retired endpoint is nonempty and assert failed activation leaves every
  contested intermediate state unchanged;
- no deterministic accept/activate/reclaim barrier test covers the capacity
  race, and there is no known-bad control for that interleaving;
- the old resource test checks only `len(endpoints) <= maxGateEndpoints`; it does
  not assert the required all-active failure, current mapping/queue/byte
  invariants or receipt ownership.

Static inspection indicates the new serialized implementation is directionally
correct, but the authoritative handoff made these non-vacuous production/race
tests acceptance evidence, not optional follow-up work. Green direct unit tests
cannot substitute for the omitted production call path and contested-state
assertions.

Minimum correction: add the missing production-path repeated-poll test and
deterministic capacity interleaving tests. Test all-active exhaustion,
retired-nonempty exhaustion, retired-empty reclamation and failed-admission
immutability explicitly. A later drain or successful activation must not mask an
earlier mutation.

### R7-C — mandatory positive provider path remains unavailable

Production capacity remains zero, `provenActionMapping` remains empty, and no
controlled provider delivery channel exists. Preserve the honest non-actionable
state. Delivery-gate machinery and controlled tests are not positive production
evidence. A1 remains BLOCKED after R7-A/R7-B until this separately reviewed
capability exists or the acceptance contract is separately revised. N1 remains
blocked.

## 3. Required remediation-7 evidence

1. Every variable-size endpoint and accepted-item field is bounded before it is
   retained; total accounting includes payload and retained metadata.
2. Canonical digest/token formats and existing ID/version/key limits fail closed
   at the deepest gate boundary.
3. Exact-limit inputs pass and one-over-limit inputs append nothing and leave all
   accounting unchanged.
4. All-active and retired-nonempty endpoint exhaustion fail before mutation;
   retired-empty reclamation remains deterministic.
5. A deterministic capacity interleaving inspects current mapping, endpoint
   ownership, queue contents and byte totals before any later operation.
6. Repeated accepted `processSession` polls with one RuntimeRef preserve one
   handle; a real generation change produces exactly one replacement.
7. The exact final report HEAD passes the full gate without exclusions.
8. Positive provider delivery remains an explicit blocker if unavailable.

## 4. Scope and next action

This remains A1 delivery-boundary remediation only. Do not begin provider
research/implementation, N1, Task/Dispatch, worker protocol, generic commands,
CLI redesign, ConPTY/Windows, cloud relay, lock-screen actions, O1 or O2.

Continue only from
`docs/NEXT_SESSION_A1_APPROVAL_SAFETY_REMEDIATION_7_HANDOFF.md`.
