# A1 Approval Safety — Authoritative Planning Boundary

Status: **PLAN ACCEPTED; IMPLEMENTATION REJECTED AT `3b56c4f` — B1-B8 REMEDIATION REQUIRED**

Baseline reviewed: `ace458056699c26a323eea49d44f8dc1efdaded7`.
Accepted S1.1 implementation ancestor:
`02c8385e3270fbbc4df45e0c71ccad6ebe11a076`.

This document freezes A1 scope and acceptance after the independent
`ACCEPT WITH REQUIRED PLAN CHANGES` review. The first implementation at
`3b56c4f05d16653b5fd9c494d8f1682135701603` was independently rejected because
it did not implement the frozen atomic claim, delivery receipt, action/
idempotency, lifecycle-race, provider-actionability, DTO, auth, and acceptance-
test boundaries. Execute only
`docs/NEXT_SESSION_A1_APPROVAL_SAFETY_REMEDIATION_HANDOFF.md`.

## 1. Authority boundary

`waiting_approval` is display-only. It must never:

- create an authoritative Approval;
- authorize an action or create an execution claim;
- trigger terminal input or synthesize `Y/N`;
- become authority through screen, PTY, prompt text, process name, CWD, or any
  heuristic evidence.

Only an accepted, currently correlated adapter may create an actionable
approval, and only when all three checks pass:

1. its frozen descriptor declares `CapApprovalDetection` for the accepted exact
   provider version;
2. `DetectApproval` returns an approval bound to the accepted source event;
3. the source event passes `SafeApprovalGate` and current runtime-identity
   validation.

Adapters without accepted approval capability produce zero actionable
approvals. Legacy parsers, terminal capabilities, prompt-like text, status, and
provider metadata may not create options or execution authority.

## 2. Two distinct records

An authoritative pending approval is runtime/action authority, not requester
authority. It is immutably bound to:

- ApprovalID and canonical SessionID;
- exact adapter/provider identity and supported provider version;
- current LaunchGeneration and StreamGeneration where applicable;
- accepted source-event identity, authoritative provenance, and bounded
  confidence;
- a closed set of verified mapped actions and their canonical ActionDigests;
- creation time, expiry, and bounded non-sensitive display projection.

It is not pre-bound to a device. The authenticated requester is bound only when
the server successfully creates an execution claim.

## 3. Atomic ClaimForExecution

The following sequence is prohibited:

```text
Lookup -> Validate -> Resolve -> Deliver
```

Execution authority can be acquired only by one atomic store transition:

```text
pending
  -> ClaimForExecution(full binding, ActionDigest, requester context,
                       permission, idempotency key)
  -> executing
```

The transition atomically validates:

- ApprovalID and exact SessionID;
- adapter/provider identity and supported version;
- current LaunchGeneration and StreamGeneration where applicable;
- canonical ActionDigest;
- creation/expiry boundary and current approval state;
- authenticated requester context and required permission;
- idempotency key.

A successful claim returns an opaque claim token, or an equivalent unforgeable
exclusive-ownership handle. It is internal and never appears in a public DTO.
Concurrent approve/reject, duplicate claims, expiry, runtime replacement,
session deletion/unlink/termination, and correlation loss must never create two
execution owners.

Requester identity is derived only from `PrincipalFromContext` or equivalent
server-authenticated context. The claim may bind server-derived DeviceID,
HostID, BearerSessionID, boot/auth context, and permission set. The client may
not assert those fields, runtime generations, or adapter/provider authority;
unknown or conflicting identity fields fail closed.

## 4. Canonical action and idempotency

Every executable option has one immutable canonical action representation. Its
ActionDigest includes every value that changes delivery meaning:

- verified provider action/option ID;
- action schema version;
- normalized arguments and normalized user input;
- input type and placement;
- any other closed, delivery-semantic field.

Raw display text and arbitrary provider payloads are never digest input. Claim
and delivery receipt use the identical ActionDigest.

Retry rules are fixed:

- same idempotency key + same digest -> `already_accepted`;
- same idempotency key + different digest -> `conflict`;
- non-idempotent actions are never automatically retransmitted;
- a manual retry preserves the original key and digest unless a new Approval is
  created.

Accepted Codex evidence does not by itself prove executable options. A
controlled, redacted fixture must prove each exact evidence-to-action and
action-to-delivery mapping. Without that proof, the event may be displayed as
non-actionable intervention information, action buttons remain hidden, and
`ClaimForExecution` is unavailable. Blind `y\n`/`n\n` or arbitrary terminal
payload synthesis is prohibited.

## 5. Approval-specific delivery boundary

`CommandBroker.Put`, or any generic session-level overwrite queue, is not A1
delivery authority. A1 requires a dedicated conceptual operation (the exact Go
name may differ):

```text
DeliverApprovalAction(
  claimToken,
  approvalID,
  runtimeRef,
  actionDigest,
  idempotencyKey,
  exactPayload,
) -> DeliveryReceipt
```

The receipt is immutably bound to ApprovalID, SessionID, adapter/provider,
LaunchGeneration, StreamGeneration where applicable, ActionDigest, and
idempotency key. Its minimum closed outcomes are:

- `accepted`: the exact action was accepted once by the correct daemon-owned
  delivery boundary;
- `already_accepted`: the same key and digest were already accepted;
- `stale_runtime`;
- `runtime_mismatch`;
- `unavailable`;
- `conflict`: the same key was used with a different digest;
- `rejected`.

No approval may enter a successful terminal state before `accepted` or
`already_accepted`. Delivery failure remains non-success, for example
`delivery_failed`. A1 proves daemon-boundary acceptance only; worker execution
acknowledgement and completion belong to O1.

## 6. State model and recovery

Minimum conceptual state machine:

```text
pending   -> executing -> delivered/committed
pending   -> expired/cancelled/invalidated
executing -> delivery_failed
```

Exact names may differ, but the implementation must preserve one execution
owner, no success before delivery acceptance, current-runtime validation, and
bounded idempotent retry. Replacement, correlation loss, deletion, unlink, and
termination invalidate incompatible pending/executing authority.

Daemon restart begins with no restored actionable pending/executing authority.
Persisted display history, if any, is non-actionable until separately proven by
a future reviewed persistence contract. A stale request cannot regain authority
from replay.

## 7. Safe public DTO boundary

Public/backend-mobile approval DTOs use a bounded structural allowlist. They may
expose only:

- a bounded redacted summary;
- a safe option identifier and safe display label;
- bounded required-input metadata;
- expiry and current non-sensitive state.

They never expose raw provider prompts, terminal output, provider/delivery
payloads, full commands, tokens/secrets, absolute paths, arbitrary provider
labels/placeholders, internal claim tokens, or ActionDigest source material.
Delivery payloads remain server-side.

Backend and mobile both enforce explicit UTF-8 byte/array/string limits, closed
vocabularies, exact session binding, and unknown-field rejection. Display labels
and placeholders are Pokit-owned or produced by an independently verified,
bounded mapping; raw provider values are not passed through.

## 8. Preserved A1-A through A1-E slices

### A1-A — authority audit and contract freeze

Freeze the current-vs-target call graph, closed state/error vocabulary,
ActionDigest representation, requester-binding rules, delivery receipt, DTO
decision, and threat matrix. No production behavior changes before this packet
is reviewable.

### A1-B — generation-bound authoritative ApprovalStore

Implement immutable bounded records, atomic `ClaimForExecution`, exclusive claim
ownership, exact runtime/action/auth binding, expiry, invalidation, replay
defence, race safety, and deterministic eviction.

### A1-C — accepted-adapter production ingestion

Use only capability-gated `DetectApproval` over accepted, correlated events that
pass `SafeApprovalGate`. Prove exact action mapping with controlled fixtures;
otherwise create no actionable options. Claude/no-capability and legacy or
heuristic sources remain zero-actionable.

### A1-D — authenticated exact-action delivery

Use the server-derived paired-device principal, atomic claim, and dedicated
approval delivery receipt. Remove generic overwrite queues from approval
authority. Commit success only after exact daemon-boundary acceptance.

### A1-E — strict mobile path and integrated gate

Use the host-bound paired-device write transport, strict public DTO decoder,
pending-authority-only UI, stale-response/session-switch guards, same-key manual
retry semantics, complete race/production regression evidence, and final gate.

Independent acceptance is requested only after all five slices pass on one
stable final tree.

## 9. Mandatory A1 acceptance gate

A1 cannot be accepted without production-path and race-enabled evidence for all
of the following:

1. `waiting_approval` alone creates no Approval, CTA, claim, or delivery;
2. accepted Codex evidence flows through `DetectApproval`, `SafeApprovalGate`,
   Store, authenticated API, atomic claim, delivery receipt, and final commit;
3. Claude, no-capability, heuristic, PTY, prompt, screen, process/CWD, and legacy
   parser paths create zero actionable approval;
4. stale launch generation is rejected;
5. stale stream generation is rejected;
6. adapter/provider/version mismatch is rejected;
7. cross-session replay is rejected;
8. modified action or arguments fail the ActionDigest check;
9. concurrent approve versus reject yields one execution owner;
10. duplicate claim yields no second owner;
11. expiry before and during claim fails closed;
12. runtime replacement between claim and delivery cannot commit stale success;
13. deletion, unlink, and termination during claim invalidate authority;
14. delivery failure creates no successful state;
15. identical idempotency retry returns `already_accepted`;
16. same key with a modified digest returns `conflict`;
17. generic command overwrite cannot affect approval delivery;
18. daemon restart restores no stale actionable authority;
19. paired-device principal and `PermTerminalInput` are enforced;
20. legacy bearer and cross-host credentials are rejected remotely;
21. mobile `resolveApproval` uses host-bound authenticated write transport;
22. bounded/redacted DTO rules pass at backend and mobile boundaries;
23. secret, path, prompt, payload, command, and log-leak negatives pass;
24. backend build/vet/full race, mobile TypeScript/Jest, Android, invariant,
    ID-inference, secret-scan, and diff gates pass;
25. final SHA has a clean worktree, local/remote equality, and accepted S1.1
    ancestry.

## 10. Explicit exclusions and next boundary

A1 excludes N1 implementation, Task/Dispatch, worker acknowledgement or
completion, Executor-Verifier workflow, automatic approval policy, generic
command execution, local CLI redesign, ConPTY/Windows work, cloud relay redesign,
lock-screen approval actions, workspace isolation, and Git operations.

Existing Stop/Kill remain under the accepted lifecycle contract. Resume, Retry,
generic Send Message, task continuation, workflow actions, and orchestration are
not A1 approval options and remain deferred to O1/O2 or a separately accepted
contract.

N1 may begin only after independent A1 acceptance. The next action after this
documentation remediation is independent A1 plan re-verification, not
implementation.
