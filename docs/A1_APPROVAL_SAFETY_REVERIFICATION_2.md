# A1 Approval Safety — Independent Re-verification 2

Verdict: **REJECT — remediation 3 required; production path remains BLOCKED**

Reviewed branch: `feature/phase10-multi-adapter`

Reviewed report HEAD: `35c632ea1a6d81b1ee7dbaf1ecb82cc0df7f3ad7`

Reviewed implementation: `0521c3828b9d78028f64f9479f8269f469050f85`

Accepted S1.1 ancestor: `02c8385e3270fbbc4df45e0c71ccad6ebe11a076`

The reviewed remote was verified in an isolated temporary worktree so the
reviewer's unpushed executor-instruction commit remained intact. Required
ancestry and clean-tree checks passed. The submitted agent/term/transcript race
suite, backend build/vet/race, mobile TypeScript, and all 383 Jest tests passed.
Android/Kotlin remained an explicit environment skip. Green submitted tests do
not close the counterexamples below.

## 1. Accepted remediation-2 changes

Preserve these corrections:

- `ClaimForExecution` recomputes ActionDigest from the stored option/input,
  removes caller `RequiredPerm`, validates stored permission and requires a
  canonical non-empty idempotency key;
- cross-Approval key reuse conflicts and ledger capacity fails closed;
- receipt token and the fields currently present in
  `ApprovalExecutionBinding` are compared before commit;
- superseded executing records cannot commit successful receipts;
- mobile approval DTO limits use UTF-8 bytes;
- production remains non-actionable and `NewUnavailableApprovalDelivery` writes
  no bytes because no provider delivery channel is proven.

## 2. Blocking findings

### R3-A — idempotent replay bypasses current requester and runtime authority

`internal/term/approval_store_gen.go`, `ClaimForExecution`, consults the
idempotency ledger before checking record state, `superseded`, stored permission,
or the caller's current RuntimeRef. `idempotencyEntry` stores only
`requesterDevice`, not the complete server-derived requester/authorization
context. `ApprovalExecutionBinding` contains no requester context.

This produces concrete bypasses:

- after an accepted decision, the same DeviceID with a different HostID,
  BearerSessionID and boot context and no permission receives
  `ClaimAlreadyAccepted`;
- a replay can return `already_accepted` after runtime supersession because the
  fast path precedes `rec.superseded` and runtime comparison;
- the stored permission is not evaluated for an idempotent replay at the store,
  despite the store being declared the complete authority boundary.

The reviewer reproduced the first case with a direct store test; it failed with
`authority bypass reproduced: changed unauthorised context returned
"already_accepted"`.

Bind an immutable canonical requester authorization context to the claim and
ledger: DeviceID, HostID, bearer/auth session, boot/auth context, and an exact
canonical permission-set representation. Deep-copy or digest a sorted closed
set; do not retain a mutable caller slice. Before returning `already_accepted`,
validate the exact approval binding, current runtime/supersession policy, current
server-authenticated context, and stored permission. Add separate negatives for
each changed requester field, revoked permission, and stale launch/stream
generation.

### R3-B — exact delivery payload is outside the canonical execution binding

The claim's ActionDigest covers option, kind, input schema/placement and input,
but `ApprovalDeliveryRequest.Payload` is a separate unbound byte slice.
`HandleApprovalAction` constructs it after the claim from a display snapshot via
`serverPayloadFor`; neither `ApprovalExecutionBinding` nor `DeliveryReceipt`
contains the exact payload or a payload digest. A delivery implementation can
accept substituted bytes and return the unchanged claim binding, after which the
store commits success.

The reviewer reproduced this with a claim for input `authorised`, a simulated
delivery of `substituted`, and a receipt echoing the claim binding; commit
succeeded.

The store authority must return one immutable canonical action/delivery value or
an opaque action handle from which the delivery boundary derives exact bytes.
Alternatively bind a domain-separated exact payload digest and verify it at
delivery and receipt, but do not let the handler independently rebuild authority
from a display snapshot. Direct store canonicalization must also reject invalid
UTF-8 and unknown input-placement/schema values. Add a production-boundary
payload-substitution negative test.

### R3-C — `RuntimeDeliveryGate` is test-only and cannot linearize external acceptance

`RuntimeDeliveryGate` is referenced only by its standalone unit test. It is not
owned or called by `Handlers`, `TelemetryService`, lifecycle cleanup, link/unlink,
or the production `ApprovalDelivery` path. Telemetry wires store
`SupersedeRuntime`, which prevents stale commit but cannot prevent wrong-runtime
bytes from being accepted.

Even if wired, `AcceptDelivery` is a check that releases its mutex before any
external delivery I/O. Replacement can occur after it returns true and before
the write/enqueue, so the claimed “old generation accepts no bytes” property is
not established. `TestDeliveryGate_OldGenerationAcceptsNothing` checks only
sequential booleans, while `TestClaimDelivery_RaceChurn` asserts no contested
intermediate state.

Replace or isolate this smallest unsafe boundary. A generation-owned delivery
endpoint must atomically accept/enqueue the exact bound request under the same
per-session transition gate used by activation/replacement/deactivation; external
I/O then targets the captured generation-owned endpoint, not a mutable session
lookup. The receipt may be created only after this daemon-owned acceptance.
Wire actual launch/stream replacement, correlation loss, delete, unlink and
termination paths. Prove the failing check-to-write interleaving with deterministic
barriers and a negative control; no sleeps or standalone helper-only proof.

### R3-D — frozen retry semantics were changed without plan approval

The authoritative plan fixes manual retry with the original idempotency key and
digest and requires bounded idempotent retry. The remediation-3 handoff explicitly
required ownership/lease/restart behavior. The implementation instead makes
`delivery_failed` terminal and adds `TestIdempotency_NoRetryAfterDeliveryFailure`,
calling this “frozen no-retry.” That contradicts the frozen documents.

Implement a bounded manual retry only for a failure outcome that proves the
daemon boundary did not accept the action. Preserve the same key/binding, bound
attempt count/lease and restart behavior, and never automatically retransmit
non-idempotent input. Ambiguous acceptance must remain non-retryable. If this
cannot be made safe, stop and request a separately reviewed plan amendment; do
not silently redefine the contract in code or report text.

### R3-E — log redaction and positive production evidence remain incomplete

`sanitizeLogID` bounds strings and replaces control bytes, but leaves absolute
paths and token-like values unchanged. The prior handoff explicitly required
absolute-path and token-like negatives. The reviewer reproduced unchanged
`/Users/alice/private/repo`; `api_key=secret-value` is likewise unchanged.

Do not log caller/provider identifiers verbatim. Use a bounded opaque hash or the
project's conservative diagnostic sanitizer and capture actual log output in
tests for controls, Unix/Windows paths, token/secret patterns and truncation.

Separately, the implementation correctly reports that no controlled provider
mapping or production delivery channel exists. Mandatory A1 positive-path item 2
therefore remains blocked. Keep all production approvals non-actionable; do not
fabricate a fixture, Y/N input, provider channel, or completion claim.

## 3. Required remediation-3 evidence

1. Full requester-context binding and permission/runtime validation before every
   `already_accepted` result.
2. Same-key replay negatives for changed host, bearer, boot, permission and
   launch/stream generation.
3. Exact canonical payload/action bound from claim through acceptance and
   receipt; substituted payload cannot commit.
4. Invalid UTF-8 and unknown input schema/placement rejected at the store.
5. Production-owned, generation-specific delivery acceptance wired to actual
   replacement and cleanup paths.
6. Deterministic check-to-accept race test plus negative control and intermediate
   state assertions.
7. Frozen bounded manual retry, or an independently approved plan amendment.
8. Actual log-capture negatives for paths, secrets/tokens and controls.
9. Existing accepted R2-A–D tests and full gates remain green.
10. Positive provider delivery evidence remains an explicit release blocker if
    unavailable.

## 4. Scope and next action

This remains A1-only remediation. Do not begin N1, Task/Dispatch, worker
acknowledgement/completion, automatic policy, generic command execution, CLI
redesign, ConPTY/Windows, cloud relay, lock-screen actions, O1, or O2.

N1 remains blocked. Continue only from
`docs/NEXT_SESSION_A1_APPROVAL_SAFETY_REMEDIATION_3_HANDOFF.md`.
