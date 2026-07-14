# A1 Approval Safety — Remediation 5 pre-implementation contract note

Baseline `5360ec61` (R4). Fixes only re-verification-4 findings R5-A..E.

## Reviewer counterexample to close first (R5-A, as a failing test)

```text
activate generation A -> accept item A (success receipt) -> activate generation B
-> Drain(captured A) must return exactly item A; current-session drain must never
   redirect it to B; a deactivated A accepts nothing new.
```

Current `Activate` overwrites the session map entry (destroys A's queue) and
`Drain(sessionID)` is a mutable lookup, so item A is lost after B is published.

## Frozen queue-item binding (R5-B)

`acceptedItem` (immutable, internal, never in a public/mobile DTO):

| field | source | purpose |
|---|---|---|
| Binding.ApprovalID, SessionID | claim | correlation |
| Binding.Runtime (adapter/version/launch/stream) | claim | generation |
| Binding.ActionDigest | claim | action integrity |
| Binding.PayloadDigest | claim | exact bytes integrity |
| Binding.IdempotencyKey | claim | idempotency |
| ClaimToken | claim | opaque ownership |
| ReceiptID | gate (endpoint nonce + seq) | receipt correlation |
| Payload (defensive copy) | claim canonical payload | exact bytes |

`Drain` returns typed defensive copies (binding by value, payload copied). A
no-payload option is distinguished by its bound action (Binding), not raw bytes.

## Endpoint identity & ownership (R5-A)

- `genEndpoint{id, sessionID, runtime, active, capacity, queue, seq, nonce, queuedBytes}`.
  `id` is an opaque server-side handle (crypto/rand).
- `current: map[sessionID]endpointID`; `endpoints: map[endpointID]*genEndpoint`
  (current + retired). Drain uses the endpoint HANDLE, never a session lookup.
- Activate(session, rt, cap): retire the old current endpoint (mark `active=false`,
  keep its queue), create a new endpoint with a fresh id+nonce, publish it as current.
  Atomic under one lock. Returns the new handle (ok=false on entropy failure).
- Accept: only the current, active endpoint whose runtime matches accepts; it appends
  a typed item and returns (receipt, handle, true). A retired/inactive/wrong-runtime/
  capacity-0 endpoint accepts nothing.
- Bounded retention: at most `maxGateEndpoints` endpoints; over the bound, evict the
  oldest retired endpoint (explicit bounded terminal disposition). Production capacity
  is 0 so retired endpoints are always empty — no success receipt is ever destroyed.
- Restart: in-memory only — a fresh gate has no endpoints (fail closed; no residual).

## Linearization points

- Acceptance: the append under the gate lock (single critical section, no callback).
- Publication/retirement/deactivation: under the same lock (serialized vs Accept).
- Drain snapshot: brief lock; external/provider I/O runs on the returned copies OUTSIDE
  the lock.

## Resource + entropy bounds (R5-C, repository-owned constants)

```text
maxGateCapacity            = 64   // per-endpoint acceptance capacity ceiling
maxGateEndpoints           = 128  // active + retired total
maxGateItemsPerEndpoint    = 64   // == capacity ceiling, enforced by capacity
maxGateItemBytes           = 4096 // per accepted payload
maxGateTotalQueuedBytes    = 1<<20
```

- capacity < 0 or > maxGateCapacity → the endpoint is created with capacity 0 (an
  explicit no-channel/unavailable state), never silently clamped to a usable value.
- Entropy failure (crypto/rand or the test seam) → Activate returns ok=false and
  publishes NO accepting endpoint; Accept appends nothing and returns non-acceptance.
- Queue full / item too large / total bytes exceeded → non-acceptance, append nothing.
- A later success cannot hide an earlier rejected append (each Accept is independent).

## Final-tree gate (R5-D)

Token-shaped examples in docs are rephrased so the secret scan passes with NO
exclusion. The full authoritative gate is run on the EXACT final tree AFTER the
report/REVIEW-REQUEST commit (git diff --check + real secret scan + all doc-input
checks). The report states the actual final HEAD.

## Blocked (R5-E)

Production capacity 0, `provenActionMapping` empty, no provider channel → all
production approvals non-actionable; A1 positive-path item 2 remains BLOCKED.
