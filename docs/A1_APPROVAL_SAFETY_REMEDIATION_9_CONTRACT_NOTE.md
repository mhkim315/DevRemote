# A1 Approval Safety — Remediation 9 Contract Note

Risk class: **authority canonicalization + bounded state + deterministic concurrency**

Scope: `companion-daemon/internal/term/approval_delivery.go` (canonical identity +
accounting) and focused delivery/telemetry tests. Per
`docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md` §5 this note is frozen once every row
and transition below is explicit. No redesign; no provider-positive path; N1/O1/O2
excluded.

## 1. Authority owner

`RuntimeDeliveryGate` is the sole acceptance authority. Both authority boundaries —
`Activate` (endpoint publication) and `Accept` (item admission) — validate the
COMPLETE canonical identity and bounded accounting under one mutex BEFORE any mutation
of `current`, `endpoints`, `order`, per-endpoint `queue`/`queuedBytes`/`seq`, or the
global `totalBytes`. A correct caller (TelemetryService.processSession) does not make
the gate safe for other callers; enforcement lives at the deepest callable boundary.

## 2. Canonical grammar + exact byte bound per retained field

| Field | Canonical grammar | Bound | Validated by |
|---|---|---|---|
| Binding.SessionID | `mux.ParseSessionID` → non-empty adapter+local, `Canonical()==input`, `SessionRef.Validate()==nil`, `mux.ValidateAdapterName(adapter)==nil` | ≤ `maxSessionIDLen` (512) | `validSessionID` |
| Binding.ApprovalID | non-empty; store bound | ≤ `authMaxApprovalIDLen` (256) | `validApprovalID` |
| Runtime.Adapter | `mux.ValidateAdapterName` (`[a-z][a-z0-9_-]*`) | ≤ `maxVersionLen` (64) | `validAdapterID` |
| Runtime.Version | `[A-Za-z0-9][A-Za-z0-9._-]{0,63}` (rejects slash/backslash/control/space/traversal) | ≤ `maxVersionLen` (64) | `validVersion` |
| ActionDigest / PayloadDigest | 64 lowercase hex | == 64 | `isHex64` |
| ClaimToken | 32 lowercase hex | == 32 | `validClaimToken` |
| IdempotencyKey | `[A-Za-z0-9._:-]+` | ≤ `maxIdempotencyKeyLen` (128) | `validCanonicalKey` |

## 3. Binding table (create → store → recompare → copy → invalidate → negative test)

| Field | Created/derived at | Stored at | Recompared at | Copied into receipt at | Invalidated at | Negative test |
|---|---|---|---|---|---|---|
| SessionID | store claim | `genEndpoint.sessionID` | `Accept` (`e.sessionID==b.SessionID`) + `validSessionID` | receipt.Binding | Deactivate / replacement | grammar/control/path/one-over |
| Runtime (Adapter/Version/LaunchGen/StreamGen) | store claim | `genEndpoint.runtime` | `Accept` (`e.runtime.equal`) + adapter/version grammar | receipt.Binding | generation change | empty/grammar/one-over/stale-gen |
| ActionDigest/PayloadDigest | store recompute | binding | `isHex64` + `PayloadDigest==payloadDigest(payload)` | receipt | — | short/nonhex/substituted bytes |
| ClaimToken | store `newClaimToken` | binding | `validClaimToken` | receipt.ClaimToken | — | short/nonhex |
| IdempotencyKey | store | binding | `validCanonicalKey` | receipt.Binding | — | empty/oversize/badchar |

## 4. Exact vs conservative accounting

```text
exactRetainedVariableBytes = len(payload)
  + len(ApprovalID)+len(SessionID)+len(ActionDigest)+len(PayloadDigest)
  + len(IdempotencyKey)+len(Runtime.Adapter)+len(Runtime.Version)+len(ClaimToken)   [EXACT]
gateItemFixedCharge = 64                                                            [CONSERVATIVE, named]
chargedItemBytes = exactRetainedVariableBytes + gateItemFixedCharge                 [CHARGED]
```

Overflow proof: payload ≤ `maxGateItemBytes` (4096); the eight identity/digest/token
fields are each individually capped (512+256+64+64+128+64+64+32 = 1184); fixed charge
= 64. So `chargedItemBytes` < 5344 for any admissible item, and the running
`g.totalBytes` is bounded by `maxGateTotalQueuedBytes` (2 MiB). `g.totalBytes +
chargedItemBytes` < 2 MiB + 6 KiB ≪ math.MaxInt32, so the addition cannot overflow.
The estimate (`gateItemFixedCharge`) is never called "exact".

## 5. Capacity / failure behavior (every bounded resource)

- capacity out of `[0, maxGateCapacity]` → `Activate` returns `("", false)`, no publication.
- entropy failure → no publication.
- endpoint count > `maxGateEndpoints` with no SAFE (retired+empty, non-current) victim → fail closed.
- per-item payload > `maxGateItemBytes` → reject; charged item > `maxGateItemBytes` → reject; meta > `maxGateItemMetaBytes` → reject; `totalBytes + charged > maxGateTotalQueuedBytes` → reject.
- any rejection mutates nothing: `current`, `endpoints`, `order`, `queue`, `queuedBytes`, `seq`, `totalBytes` all unchanged.
- production capacity is 0 (no provider channel) → Accept always unavailable; retired endpoints always empty.

## 6. Linearization / contested point (R9-B3)

Single `g.mu` linearizes `Accept` vs `Activate`/reclaim. Contested point: an `Accept`
bound to generation A that ENTERS before an `Activate` replacement (A→B). Because
`Accept` re-reads `g.current[SessionID]` under the lock at commit time, the stale
accept resolves the NEW current (B) and is rejected by `e.runtime.equal` (A≠B). No
lock is held across external/provider I/O (Drain snapshots typed copies under the lock;
provider I/O runs on copies outside). Lock order: only `g.mu`; no nested gate locks.

Counterexample per blocker:
- length-only identity accepts `SessionID="../etc"` or a control char → canonical validator rejects.
- estimate-as-exact hides meta bytes → separated charged accounting counts them.
- check-then-write (capture endpoint at entry, append after replacement) admits a stale item into the retired generation → the single-lock re-read at commit rejects it; a `staleGate` negative control demonstrates the admission the real gate prevents.

## 7. Non-goals (explicit)

No provider action mapping, no non-zero production capacity, no heuristic CTA/claim,
no telemetry redesign, no store/handler/DTO contract change, no N1/O1/O2. A1 and the
provider path remain BLOCKED after R9.
