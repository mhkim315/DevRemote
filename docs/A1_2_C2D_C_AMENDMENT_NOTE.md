# A1.2 C2D-C — Remediation Contract Amendment

Status: **PRE-IMPLEMENTATION — C2D-C remediation blocked until this note exists**

This amendment augments `docs/A1_2_C2D_PACKET_CONTRACT_NOTE.md` to fix the
rejected C2D-C implementation (`cf877aa`). It covers C-R1 (real decision
delivery), C-R2 (terminal cleanup), and C-R3 (non-vacuous composition proof).

## 1. Authority owner

The **C2D-B `claudeResumeCoordinator`** remains the sole authority for
reservation, write-claim, confirmation, and witness acceptance. C2D-C adds a
**concrete managed Claude resume boundary** that:

- writes the exact decision bytes to the provider hook HTTP response;
- routes consumption witnesses through production-shaped paths (hook bridge for
  PostToolUse, stream-json parser for permission_denials);
- never fabricates success.

The A1 `AuthoritativeApprovalStore` owns `ClaimForExecution` and
`RecordDelivery`. C2D-C tests MUST exercise the complete chain:
observation → Store admission → `ClaimForExecution` → `Deliver` →
`RecordDelivery`.

## 2. Immutable binding table (C-R1 additions to §2.3)

| # | Field | Created at | Stored in | Compared at | Tested by |
|---|---|---|---|---|---|
| R6 | Decision bytes | `WriteHandle.Decision()` from coordinator | written to hook HTTP response | `ConfirmWrite` claim token + state == writeClaimed | `TestClaudeDelivery_ResponseReachesProvider` |
| R7 | Expected witness kind | `certifiedClaudeDecision[OptionID]` → allow→PostToolUse, deny→PermissionDenials | bound in Deliver BEFORE I/O; passed to witness route | `MarkWitnessed` validates kind against stored decision | `TestClaudeDelivery_WrongWitnessKind` |
| R8 | Hook response acceptance | `ClaudeResponseWriter.WriteResponse(decision)` → written bytes | response writer records exact bytes | `ConfirmWrite(true)` only after accepted write | `TestClaudeDelivery_WriteFailure` |
| R9 | Receipt ID | `newClaudeReceiptID()` BEFORE first provider write | `DeliveryReceipt.ReceiptID` | entropy failure → `DeliveryConflict` before any provider I/O | `TestClaudeDelivery_EntropyFailure` |
| R10 | Production-shaped witness | `PostToolUseRoute(sessionID)` or `PermissionDenialRoute(sessionID)` | routes return `(ClaudeWitness, error)` | witness fields validated by `MarkWitnessed` | `TestClaudeDelivery_ProductionShapedWitness` |

## 3. States (C-R1 corrected)

```
reserved (ReserveEntry)
  → response written (ClaudeResponseWriter.WriteResponse)
  → write claimed (ClaimWrite)
  → response accepted (ConfirmWrite true, only after WriteResponse success)
  → decision written (stateDecisionWritten)
  → exact witness (PostToolUseRoute or PermissionDenialRoute)
  → receipt (RecordDelivery)
```

Pre-bind expected witness kind AFTER `deriveDecision` and BEFORE any I/O.
`WitnessKind(0)` is forbidden.

## 4. Linearization point

Reservation, claim, invalidation, and witness routing all linearize under the
coordinator mutex. The external I/O boundary is OUTSIDE the lock:

- `ClaudeResponseWriter.WriteResponse` — hook HTTP response, outside lock
- `PostToolUseRoute` / `PermissionDenialRoute` — stream-json parsing, outside lock

No coordinator or service lock is held across spawn, hook response I/O, or
stream reading.

## 5. Cleanup and restart (C-R2)

Pre-write failures (identity lookup, ReserveEntry, ClaimWrite, response write):
- `coordinator.CancelEntry(claimToken)` + `coordinator.RemoveIdentity(approvalID)`
- Return non-success delivery outcome

Post-write (ConfirmWrite returned, witness routing):
- On witness failure: identity already consumed by MarkWitnessed's internal state
  check (returns false, entry unchanged); identity removed by delivery on success
- On timeout: `coordinator.CancelEntry` cleans up stale writeClaimed/decisionWritten
- On stop/kill/delete/epoch replacement: `coordinator.ClearRuntime` atomically
  invalidates all entries

After any confirmed write, never auto-retransmit. Return honest ambiguous/conflict.

Nil-safe: nil service, nil coordinator, nil response writer, nil witness routes
all fail closed with `DeliveryUnavailable` without panic.

## 6. Capacity and entropy

- Receipt ID preallocated BEFORE first provider write. Entropy failure → stop
  before Claude can consume a decision.
- Coordinator bounded: `maxCoordinatorIdentities` (16), `maxCoordinatorEntries` (16).
- Witness/claim timeout bounded: `defaultClaudeDeliveryTimeout` (120s).

## 7. Known-bad counterexamples

| Counterexample | What it proves |
|---|---|
| Fake confirmation: `ConfirmWrite(true)` without `WriteResponse` call | Response must reach provider; synthetic success is fraud |
| Substituted response: write "deny" when binding says "allow_once" | Decision bytes must match derived decision |
| Early witness: witness arrives before `ConfirmWrite` | Entry must be in decisionWritten state |
| Stale runtime: witness with wrong `LaunchGen` | RuntimeRef validation in MarkWitnessed |
| Orphaned reservation: terminate without cleanup | Coordinator ClearRuntime must fire before I/O |

## 8. Explicit non-goals (unchanged from C2D contract §8)

No C2D-D, C3D, production wiring, mobile CTA, provider SDK, or live model turns.
