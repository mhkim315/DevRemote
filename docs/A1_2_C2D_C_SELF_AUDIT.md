## C2D-C Remediation Self-Audit

### C-R1: Real decision delivery boundary

| Requirement | Production code | Test |
|---|---|---|
| Use WriteHandle decision | `Deliver` derives `decision` from `certifiedClaudeDecision[b.OptionID]` (line 97) | `TestClaudeDelivery_FullChainAllow` verifies `lastWritten() == "allow"` |
| Strict encode allow/deny | `certifiedClaudeDecision` map: allow_once→"allow", deny→"deny" (coordinator.go:44) | `TestClaudeDelivery_FullChainDeny` verifies deny write |
| Route through concrete boundary | `ClaudeResponseWriter.WriteResponse(decision)` (line 149) | `recordingResponseWriter` captures exact bytes |
| ConfirmWrite only after accepted write | `ConfirmWrite(true)` only after `WriteResponse` returns nil (line 155) | `TestClaudeDelivery_WriteFailure` proves write error → ConfirmWrite skipped → DeliveryConflict |
| Bind witness kind before I/O | `expectedKind` derived from `decision` before route call (line 168) | `waitForWitness` selects route by kind; `MarkWitnessed` validates kind vs decision |
| Production-shaped routes | `ClaudeWitnessRoute` interface: PostToolUseRoute + DenialRoute | `stubWitnessRoute` / `channelWitnessRoute` / `timeoutWitnessRoute` stubs |
| Preallocate receipt ID | `newClaudeReceiptID()` at line 110, BEFORE any provider I/O | `TestClaudeDelivery_FullChainAllow` verifies non-empty ReceiptID |
| Uninstalled | Not wired in `app.go` | Confirmed: no app.go changes in diff |

### C-R2: Terminal cleanup

| Requirement | Production code | Test |
|---|---|---|
| Pre-write cleanup | `cleanupPreWrite`: CancelEntry + RemoveIdentity (line 217) | `TestClaudeDelivery_WriteFailure`: identity=0, pending=0 after failure |
| Write failure → ambiguous | `ConfirmWrite(false)` + cleanup → DeliveryConflict | `TestClaudeDelivery_WriteFailure` |
| Timeout cleanup | `timeoutWitnessRoute` → DeliveryConflict, identity cleaned | `TestClaudeDelivery_Timeout`: identity=0 |
| Nil dependencies | Early nil check (line 86): svc, coordinator, responseWriter | `TestClaudeDelivery_NilService`, `_NilCoordinator`, `_NilResponseWriter` |
| No lock across I/O | `WriteResponse` called outside coordinator lock; routes called outside lock | Architecture: coordinator mutex scope is ClaimWrite/ConfirmWrite/MarkWitnessed only |
| Identity consumed after success | `RemoveIdentity` after MarkWitnessed success (line 192) | `TestClaudeDelivery_DuplicateClaim`: second delivery returns Unavailable |

### C-R3: Non-vacuous A1 composition

| Requirement | Production code | Test |
|---|---|---|
| Real ClaimForExecution | `mustClaim` calls `store.ClaimForExecution` | `TestClaudeDelivery_FullChainAllow` |
| Real RecordDelivery | `store.RecordDelivery(receipt)` after delivery | `TestClaudeDelivery_FullChainAllow`: commit.Committed=true |
| Real Store ingestion | `ingestActionableRecord` calls `store.IngestObserved` with delivery material | Both full-chain tests |
| Concrete I/O double | `recordingResponseWriter` records writes | `TestClaudeDelivery_FullChainAllow`: lastWritten() check |
| Barriers (no sleep) | `d.barrier` at post-claim, post-confirm | `TestClaudeDelivery_CancelAtClaim` |
| Channels for concurrency | `channelWitnessRoute` for `TestClaudeDelivery_WitnessRace` | Verified: uses channels, not time.Sleep for sync |
| Known-bad: fake confirmation | `TestClaudeDelivery_FakeConfirmation`: nil writer → Unavailable, coordinator untouched | identity=1, pending=0 after |
| Known-bad: write failure | `TestClaudeDelivery_WriteFailure`: failWith set, write fails, cleanup verified | identity=0, pending=0 |
| Cross-session witness | `TestClaudeDelivery_CrossSessionWitness`: wrong SessionID → Rejected | ✓ |
| Timeout | `TestClaudeDelivery_Timeout`: timeoutWitnessRoute → Conflict + cleanup | ✓ |
| Duplicate claim | `TestClaudeDelivery_DuplicateClaim`: second delivery Unavailable | ✓ |

### Non-goals verified

- No `app.go` changes: confirmed
- No mobile changes: confirmed
- No C2D-D/C3D: confirmed
- No live model turns: confirmed
- No provider SDK: confirmed
