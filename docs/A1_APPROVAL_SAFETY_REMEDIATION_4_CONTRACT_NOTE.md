# A1 Approval Safety — Remediation 4 pre-implementation contract note

Baseline `0ac1acf3` (R3). Fixes only re-verification-3 findings R4-A..E. Every
requester field and cleanup owner maps to production code and a non-vacuous test.

## Reviewer counterexamples to close (verbatim intent)

1. `RequesterContext.present()` requires only DeviceID+BearerSessionID → a claim is
   GRANTED with empty HostID, and separately with empty BootID (reproduced through the
   real store `ClaimForExecution`).
2. `reconcileSessions` (registry disappearance) clears telemetry/status/approval state
   but never `deliveryGate.Deactivate(id)` → the old generation still accepts payload.
3. `RuntimeDeliveryGate.Accept` holds `mu` while calling the arbitrary
   `DeliverySink.Enqueue` interface → not a guaranteed bounded in-memory op; a sink can
   block replacement or do I/O under the lock.
4. `scripts/build-gate.sh` adds a heading-content `grep -v` that can suppress an
   arbitrary secret-scan line.

## Requester-presence contract (R4-A)

Mandatory server-derived fields, validated INSIDE the store (deepest boundary) for
every path via one shared `requesterAuthorized(RequesterContext, storedPerm)`:

| field | source | required |
|---|---|---|
| DeviceID | Principal | non-empty |
| HostID | Principal | non-empty |
| BearerSessionID | Principal | non-empty |
| BootID | Principal.DeviceBootID | non-empty |
| permission | stored `requiredPerm` | must be held |

Applies identically to: initial claim, bounded manual retry, already_accepted replay.
No handler convention substitutes; no client identity field.

## Cleanup-owner map (R4-B)

Each owner deactivates the exact generation-owned endpoint:

| owner | function | lock discipline |
|---|---|---|
| registry disappearance | `reconcileSessions` | collect pruned IDs under s.mu; Deactivate + store clears AFTER releasing s.mu (no nested gate lock) |
| delete / unlink | `Clear` | Deactivate (own lock) |
| launch replacement | `invalidateForLaunch` | Deactivate |
| stream change | poll stream branch | Deactivate |
| correlation loss / version conflict | poll ingest else | Deactivate |

## Bounded acceptance contract (R4-C)

The gate directly owns a concrete `genEndpoint{runtime, active, capacity, queue, seq,
nonce}`. `Accept` under one lock: verify current==expected + active + capacity>0; if
`len(queue) >= capacity` → non-acceptance (writes nothing); else append a copy of the
exact bytes + seq++ → ReceiptID. NO callback under the lock. `Drain` snapshots+clears
the queue under a brief lock; external/provider I/O runs on the returned bytes OUTSIDE
the lock. capacity 0 (production, no channel) → always unavailable.

Deterministic tests: bounded accept + capacity + queue-full + old-gen; external drain
does not hold the transition gate; check-then-write negative control vs the atomic gate.

## Secret-scan integrity (R4-D)

Remove the content-based exclusion; rely on the upstream heading rewording (no
`sk-`/`ghp_`-shaped substring). Negative control: a real secret-shaped value is still
detected; the reworded heading is not.

## Blocked (R4-E)

No controlled provider mapping / production delivery channel → `provenActionMapping`
empty, every endpoint activated with capacity 0, all production approvals
non-actionable, A1 positive-path item 2 **BLOCKED**. No fixture substitutes for the
production positive path.
