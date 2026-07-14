# A1 Approval Safety — Remediation 6 pre-implementation contract note

Baseline `0f95c7c3` (R5). Fixes only re-verification-5 findings R6-A..E.

## Reviewer counterexamples to close first (as failing tests)

1. `binding.PayloadDigest = digest("authorized")`, `req.Payload = "substituted"` →
   `gate.Accept` returns ok=true and the captured endpoint contains "substituted".
2. activate first endpoint → accept item (success receipt) → create endpoints past
   `maxGateEndpoints` → `Drain(first handle)` returns empty (accepted item silently
   evicted).

## Pre-append item validation (R6-A)

`Accept` rejects (non-acceptance, no ReceiptID/handle, no append) unless ALL hold:

| check | rule |
|---|---|
| ApprovalID, SessionID, ActionDigest, PayloadDigest, IdempotencyKey, ClaimToken | non-empty |
| IdempotencyKey | `validCanonicalKey` |
| Binding.SessionID | == the current endpoint's session (the map key it resolved) |
| Binding.Runtime | exactly == endpoint.runtime |
| Binding.PayloadDigest | exactly == `payloadDigest(req.Payload)` (domain-separated) |
| payload bytes | <= maxGateItemBytes; total <= maxGateTotalQueuedBytes |

Reuses the existing `payloadDigest` and `validCanonicalKey`; no second action model.

## Endpoint admission / safe eviction (R6-B)

- Safe victim = retired (inactive) AND non-current AND `len(queue)==0` AND
  `queuedBytes==0`. Only such an endpoint may be reclaimed.
- Activate is all-or-nothing. Before mutating, it computes the projected endpoint
  count (replacing an EMPTY current endpoint is net 0; replacing a NON-EMPTY current
  retires it, +1; no current, +1). If the projection exceeds `maxGateEndpoints`, it
  needs a safe victim; if none exists it FAILS (returns "", false) with the current
  mapping, all endpoints, queues, total bytes and receipt ownership UNCHANGED.
- Never evicts an active endpoint or a retired endpoint holding accepted items.
- Entropy tokens are generated before any mutation; entropy failure also fails
  closed without mutation.

## Same-runtime idempotence (R6-C)

Activate first checks the current endpoint: if it is active and its RuntimeRef AND
effective capacity equal the request, it returns the existing handle with NO entropy
allocation, NO order growth, NO retirement. A genuine RuntimeRef/capacity change is a
true serialized replacement (retire A keeping its items, publish B). The production
caller (`processSession` → `Activate` every correlated poll) therefore preserves one
handle across repeated same-generation polls.

## Linearization

All Activate/Accept/Deactivate/Drain-snapshot operations are single critical sections
under the gate mutex; external I/O runs on drained copies outside the lock. A failed
admission or a mismatched item performs no mutation, so no later success masks it.

## Final-tree gate (R6-D) & blocked (R6-E)

Rephrased docs contain no token-shaped examples; the full gate is run on the EXACT
final report HEAD after the report commit (no scanner exclusion). Production capacity
0, `provenActionMapping` empty, no provider channel → all approvals non-actionable,
A1 positive-path item 2 BLOCKED.
