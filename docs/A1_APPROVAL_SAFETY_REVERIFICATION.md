# A1 Approval Safety — Independent Re-verification

Verdict: **REJECT — focused remediation 2 required**

Reviewed branch: `feature/phase10-multi-adapter`

Reviewed report HEAD: `0ae85db89a60f1925dbbbcc2997b99fbf0bf5646`

Reviewed implementation: `ed466094cd7a38d148d845d7057c63fe2019e1c9`

Accepted S1.1 ancestor: `02c8385e3270fbbc4df45e0c71ccad6ebe11a076`

At review start local HEAD equalled the canonical remote, accepted-S1.1 ancestry
passed, and the worktree was clean. The focused `internal/term` race suite and
the repository build gate (with the documented native-environment skip) passed.
Those green gates do not close the authority defects below.

## 1. Accepted remediation foundation

Preserve these changes:

- approval creation remains limited to accepted correlated adapter evidence via
  capability, `DetectApproval`, and `SafeApprovalGate`;
- `waiting_approval`, legacy parser, PTY, prompt, process and CWD evidence remain
  display-only and create no actionable authority;
- production `provenActionMapping` returns no fabricated executable mapping and
  `NewUnavailableApprovalDelivery` writes no terminal bytes;
- requester identity is derived from the paired-device principal and the mobile
  write path has no legacy transport fallback;
- the backend public DTO is a structural allowlist and non-actionable records
  expose no options;
- a dedicated approval-delivery interface replaces generic `CommandBroker.Put`.

## 2. Blocking findings

### R2-A — claim does not validate the canonical action or stored permission

Confirmed in `internal/term/approval_store_gen.go`,
`AuthoritativeApprovalStore.ClaimForExecution`:

- it checks only that the selected option exists and that the caller-provided
  `ActionDigest` is non-empty; it does not recompute or compare the digest from
  the stored immutable option plus normalized input;
- `ClaimRequest.RequiredPerm` can replace the stored permission when non-empty.
  A caller can therefore request a weaker permission instead of being checked
  against `approvalRecord.requiredPerm`;
- an empty idempotency key is accepted even though the frozen contract requires
  the key in the atomic claim.

The HTTP handler currently computes a digest and copies the displayed permission,
but the store is the authority boundary and must be safe for every caller. The
claim must receive the canonical action material (or a store-issued immutable
action handle), recompute/verify its digest inside the lock, always use the stored
permission, and require a bounded non-empty canonical idempotency key.

### R2-B — the session-level idempotency ledger can grant false success

`sessionApprovals.idempotency` stores only `{digest, accepted}`. It is not bound
to ApprovalID, RuntimeRef, requester/authorization context or the accepted
receipt. Consequently, the same key and digest accepted for approval A can make
approval B in the same session return `already_accepted` without B ever being
claimed or delivered. This is a cross-approval authority replay.

The 256-entry cap also fails open: once full, a new claim is granted without
recording its key. A failed delivery leaves a non-accepted ledger entry and a
terminal `delivery_failed` record, so the documented bounded same-key retry is
not implemented; it returns `already_owned` indefinitely.

Bind every entry to the exact ApprovalID, SessionID, RuntimeRef, ActionDigest,
requester authorization context and accepted receipt identity. Capacity must
fail closed or use a deterministic safe eviction rule that cannot evict live or
accepted replay authority. Freeze explicit bounded retry/lease semantics and
test them; do not claim retry support that the state machine cannot perform.

### R2-C — delivery receipt is not fully bound or fully verified

`internal/term/approval_delivery.go`, `DeliveryReceipt`, omits RuntimeRef.
`AuthoritativeApprovalStore.RecordDelivery` verifies only the separately supplied
claim token and receipt ActionDigest. It does not compare receipt ApprovalID,
SessionID, idempotency key or runtime identity. Existing tests prove the gap by
successfully committing receipts containing only `Outcome` and `ActionDigest`.

The receipt must include and the store must exactly compare claim ownership,
ApprovalID, SessionID, adapter/provider/version, launch generation, applicable
stream generation, ActionDigest and idempotency key before accepting success.
Mismatched or incomplete receipts must be non-success.

### R2-D — runtime replacement is not linearized through delivery commit

`HandleApprovalAction` performs a second `RuntimeOf` check before calling
`Deliver`, but there is no post-delivery check and the receipt cannot carry the
runtime identity. `InvalidateSession` invalidates pending records only, leaving an
executing claim untouched. A future accepting delivery implementation could race
replacement/correlation loss after the pre-delivery check and still commit stale
success.

The delivery/commit contract must prove acceptance by the exact current runtime
or atomically reject a generation that has been invalidated. Replacement,
correlation loss, deletion, unlink and termination must make pending and executing
authority non-current without allowing a stale accepted receipt to win. Do not
hold the store lock across external I/O; use a generation-bound claim/receipt and
a linearizable invalidation/high-water rule.

### R2-E — DTO byte bounds/log safety and positive provider path remain open

`mobile/src/lib/approvalRequest.ts`, `boundedString`, uses JavaScript UTF-16
`.length`, not UTF-8 byte length, so it does not mirror the backend byte bounds.
Approval handler logs interpolate raw session, approval and action identifiers;
these require a closed safe grammar or bounded sanitization/hash to prevent log
injection and sensitive path/provider material leakage.

More importantly, `provenActionMapping` intentionally yields no actionable
options and production wires `NewUnavailableApprovalDelivery`. This is honest and
safe, but mandatory acceptance item 2 — accepted provider evidence through
claim, exact delivery receipt and final commit on a real production path — is not
met. Controlled fixture machinery cannot substitute for a controlled,
redistributable provider/version fixture and verified provider-specific delivery
channel. Until that evidence exists, A1 remains incomplete and all production
approvals must remain non-actionable.

## 3. Required negative tests

Add focused production/race tests for:

1. arbitrary non-empty digest rejected by the store;
2. request permission cannot override the stored permission;
3. empty/malformed idempotency key rejected;
4. same key+digest across two ApprovalIDs is not `already_accepted`;
5. ledger capacity cannot grant an untracked claim;
6. documented same-key retry behavior after delivery failure;
7. receipt mismatch independently for ApprovalID, SessionID, RuntimeRef,
   ActionDigest and idempotency key;
8. replacement/correlation loss during delivery cannot commit;
9. executing authority is invalidated safely on deletion/unlink/termination;
10. UTF-8 multibyte strings beyond backend byte limits are rejected on mobile;
11. control/newline/path/token-like identifiers do not reach logs;
12. a real accepted provider mapping/delivery fixture, or an explicit blocked
    acceptance result with no claim of A1 completion.

## 4. Scope and verdict

This is still A1-only work. Do not begin N1, Task/Dispatch, worker completion,
automatic policy, generic command execution, CLI redesign, ConPTY/Windows,
cloud relay, lock-screen approval actions or O1/O2.

N1 remains blocked until an independent A1 ACCEPT. The next action is the narrow
remediation in `docs/NEXT_SESSION_A1_APPROVAL_SAFETY_REMEDIATION_2_HANDOFF.md`.
