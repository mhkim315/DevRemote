# A1.2 C1D Packet Contract Note — Store Metadata Authority Fix

Status: **PRE-IMPLEMENTATION — NOT A CLAIM OF COMPLETION**

Implementation baseline: `7267080df88d385d5c86273b3f79a1d964e7765c`
(R11 handoff commit)

## 1. Authority owner

`AuthoritativeApprovalStore` is the sole owner of generation high-water and
session metadata. No other component may create session state, advance
generation, or supersede records.

## 2. Binding table

| Operation | Creates session | Creates Approval record | Sets high-water | Supersedes records |
|---|---|---|---|---|
| `Ingest` / `IngestObserved` | YES (only if ≥1 item admitted) | YES | YES (only if ≥1 item admitted) | YES (only if gen advanced AND ≥1 item admitted) |
| `InstallRuntimeGeneration` (NEW) | YES (if not exists) | **NO** | YES | YES |
| `SupersedeRuntime` (existing) | NO (requires existing session) | NO | YES (if gen newer) | YES |
| `InvalidateSession` (existing) | NO | NO | NO | YES |
| `InvalidateRecord` (existing) | NO | NO | NO | NO (single record) |
| `Clear` (existing) | NO (deletes session) | NO | NO | NO (deletes all) |

## 3. State transitions

```
Session Lifecycle:
  (none) → [InstallRuntimeGeneration OR Ingest with ≥1 admitted item] → session exists with hwInitialized=true

Generation advancement:
  hw(0,0) → Ingest(gen=N, admitted>0) → hw(N,0)
  hw(N,0) → InstallRuntimeGeneration(N,1) → hw(N,1)
  hw(N,1) → Ingest(gen=N, stream=0, admitted>0) → REJECTED (genNewer(1,0) false)

Record Lifecycle:
  (none) → Ingest(item passes validation) → ApprovalPending
  ApprovalPending → supersedeLocked → ApprovalInvalidated
  ApprovalPending → InvalidateRecord → ApprovalInvalidated
  ApprovalPending → ClaimForExecution → ApprovalExecuting
  ApprovalExecuting → RecordDelivery(accepted) → terminal
  ApprovalExecuting → RecordDelivery(non-accepting) → ApprovalDeliveryFailed
```

## 4. Linearization points

1. **InstallRuntimeGeneration**: session creation + hw write + supersede all
   happen atomically under `s.mu`. The linearization point is the hw write.

2. **Ingest/IngestObserved**: item validation happens FIRST (read-only checks
   against current state). The linearization point is the hw write + record
   insertion, which only occurs if ≥1 item passed all validation gates.

3. **terminate()**: calls `InstallRuntimeGeneration(sessionID, epoch, 1,
   "terminated")` → atomically sets hw to (epoch, 1). A late
   `IngestObserved(StreamGen=0)` is then rejected by the Store's `genNewer`
   check because `genNewer(epoch, 1, epoch, 0)` is true.

## 5. Capacity behavior

- `InstallRuntimeGeneration`: subject to `authMaxApprovalSessions` (1024).
  Capacity exhaustion evicts the oldest session. Never evicts a session with
  pending or executing records (but those can't exist — this is a metadata-only
  transition for sessions with no live approval authority).

- `Ingest`: subject to both `authMaxApprovalSessions` and
  `authMaxApprovalsPerSession` (50). Capacity exhaustion during item admission
  triggers `evictOneLocked`. If eviction fails, the item is skipped.

## 6. Adversarial counterexamples

### Forward race: terminate before ingest
1. `joinDeferred` acquires turnMu, validates pending observation, releases turnMu
2. `joinDeferred` calls preIngestHook → `terminate()` →
   `InstallRuntimeGeneration(epoch, 1)` sets hw=(epoch,1)
3. `atomicTerminated.Load()` → true → `joinDeferred` returns without calling
   `IngestObserved`
4. Even if the atomic check is bypassed, `IngestObserved(epoch, 0)` hits
   `genNewer(epoch, 1, epoch, 0)` → true → Store rejects with 0 admitted.
5. Store state: zero records for session, hw=(epoch,1).

### Reverse race: terminate after Store admission, before active append
1. `IngestObserved(epoch, 0)` succeeds (hw was unset), record admitted
2. postIngestHook → `terminate()` → `InstallRuntimeGeneration(epoch, 1)`
   → hw advances to (epoch,1), record invalidated via supersedeLocked
3. Post-ingest check: `rt.terminated` is true → active slice remains empty
4. Compensating `InvalidateRecord` ensures the just-admitted record is
   invalidated
5. Store state: one invalidated record (or none if invalidated then expired),
   hw=(epoch,1).

### Invalid ingest no-mutation (counterexample for old behavior)
1. `IngestObserved` with `Provenance: "c1d_internal"` (non-authoritative)
2. OLD BEHAVIOR: session created, hw set, item skipped → hw mutation with
   zero records
3. NEW BEHAVIOR: item fails `ApprovalAuthoritative` check → no items admitted
   → `ingest` returns 0 WITHOUT creating session or setting hw
4. Store state: session does NOT exist, hw NOT set.

### Mismatched generation: late observation after termination
1. `InstallRuntimeGeneration(epoch, 1)` sets hw=(epoch,1)
2. Late `IngestObserved(epoch, 0)` → `genNewer(epoch, 1, epoch, 0)` → true → rejected
3. Store state: unchanged, hw=(epoch,1).

## 7. Explicit C1D non-goals

- No action options, ClaimForExecution, decision delivery, resume-for-decision
- No mobile CTA or action handler wiring
- No modification of accepted Codex semantics
- No C2D/C3D work
- No redesign of the approval state machine, claims, delivery, or receipts

## 8. Termination contract

`terminate()` MUST:
1. Close the hook bridge
2. Kill and wait the child process
3. Clean up the hook directory
4. Set `terminated=true`, bump `ingestGen`, store `atomicTerminated`
5. Set `turnClosed=true`, clear pending observations
6. Invalidate active approvals via `InvalidateRecord`
7. Call `InstallRuntimeGeneration(sessionID, epoch, 1, "terminated")` —
   metadata-only, no Approval record, no sentinel/fake provenance
8. Mark the session exited in the registry
9. Close the `exited` channel

`terminate()` MUST NOT:
- Call `IngestObserved` with fake/sentinel items
- Use non-authoritative provenance to create Store state
- Create any Approval record as a side effect of termination
