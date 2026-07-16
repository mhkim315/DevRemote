# A1.2 C1D — Managed Claude Observation Evidence Report

Status: **C1D IMPLEMENTED — AWAITING INDEPENDENT ACCEPTANCE**

Implementation SHA: `4794ce7c7eb16f4b33ac570a6a1cd7798e62ce04`
Accepted C0D evidence: `e42d4c570e64462ce813861017cc635e338e68bf`
Accepted SP1/Codex baseline: `2b940a6fce6e878ffa0da17b5df4d39438af144d`
R11 handoff SHA: `7267080df88d385d5c86273b3f79a1d964e7765c`

## 0. Production authority — ACCEPTED

All R11-F1 production correctness blockers closed:

1. **All hwInitialized sessions non-evictable** — `sessionHasLiveAuthority` returns
   `sess.hwInitialized`. Only `Clear()` removes sessions. Tombstones with terminal
   records are protected identically to empty tombstones.
2. **Store reservation before child spawn** — `CreateDetached` generates ID/epoch,
   calls `InstallRuntimeGeneration(id, epoch, 0, "reserved")`, THEN spawns Claude.
   Reservation failure → no child spawn.
3. **Launch failure rollback** — `rollback()` helper calls `approvals.Clear(id)` on
   every post-reservation failure path (launcher error, shutdown race, closing,
   registry failure). Repeated failures do not leak Store slots.
4. **Older gen is complete no-op** — `InstallRuntimeGeneration` only calls
   `supersedeLocked` when `genNewer(incoming, current)` is true.
5. **Invalid ingest does not mutate Store** — `ingest()` defers session creation
   and high-water advance until after all validation gates pass.
6. **Newer gen supersession independent of item admission** — `hasAuthoritativeItem`
   set after provenance+delivery validation, triggers supersession even when
   duplicate-ID or capacity prevents record admission.
7. **Malformed delivery does not advance generation** — `hasAuthoritativeItem` only
   set after delivery material validation passes.
8. **Capacity fail-closed** — `evictOldestSessionLocked` returns bool; both
   `InstallRuntimeGeneration` and `ingest` check it and return errors.

## 1. Store lifecycle invariant

```go
func sessionHasLiveAuthority(sess *sessionApprovals) bool {
    return sess.hwInitialized
}
```

Every initialized session is protected. High-water is always meaningful.
Sessions are removed only by `Clear()` (explicit delete/unlink).

## 2. Launch ordering (before → after)

```
BEFORE: certify → hooks → bridge → SPAWN → id+epoch → register → reserve → pump
AFTER:  certify → hooks → bridge → id+epoch → RESERVE → spawn → register → pump
```

Post-reservation rollback on ANY failure: `approvals.Clear(id)` + bridge close +
hook dir cleanup.

## 3. Live production proof

`TestC1D_LiveProductionProof` in `cmd/devremote/c1d_live_test.go`:

- Uses `buildRunCreateRequest(["claude"])` → `json.Marshal` → `net.Dial` Unix socket (same serializer as `createRequestViaSocketAt` internal path)
- Pinned binary: `/Users/mhk/.local/share/claude/versions/2.1.209`
- SHA-256: `59d2de7f49db2f75d5c33bbb46a6b8f288ad24d40b61e30602a502bb7ddc380c`
- Bounded polling: 500ms intervals, 60s deadline

Result (2026-07-16, exact frozen HEAD `4794ce7`):
```text
created: claude_headless:claude-*
artifacts: hookDir=$TMPDIR/pokit-claude-hooks-* pid=62499
hook token length: 98
observation: id=claude-* options=0 state=pending
PASS: provider=claude version=2.1.209 epoch=1
```
Time: 10.65s. Exactly 1 non-actionable observation.

Assertions verified:
- Provider: `claude`, Version: `2.1.209`, Epoch: `1`
- Digest: `59d2de7f49db2f75d5c33bbb46a6b8f288ad24d40b61e30602a502bb7ddc380c`
- Zero options, zero actionable fields
- DTO privacy: no cmdMarker, cwdMarker, or hookToken in ListSafe/List DTOs
- DTO privacy: no `claim_token` or `delivery` material
- IPC privacy: no markers in create response
- Daemon log privacy: no markers or credentials in captured `log` output
- Log capture non-vacuous: "IPC Server listening" confirmed in buffer
- Hook dir: specific runtime path removed after shutdown
- Direct PID: `Signal(0)` fails
- Process group: `syscall.Kill(-pid, 0)` returns `ESRCH`
- Registry: `Exited` confirmed
- Socket: removable after cleanup
- No live records (pending/executing) after stop
- Production launcher used (not custom)

## 4. Counterexample tests

| Test | What it proves |
|---|---|
| `TestInstallRuntimeGeneration_OlderGenDoesNotSupersedeCurrentApprovals` | Older gen is complete no-op |
| `TestInstallRuntimeGeneration_CapacityExhaustedFailClosed` | 1025th session rejected when all slots are tombstones |
| `TestIngestObserved_CapacityExhaustedFailClosed` | IngestObserved fails closed at capacity |
| `TestSessionHasLiveAuthority_ProtectsAllHwInitialized` | All hwInitialized sessions protected |
| `TestSessionHasLiveAuthority_DeliveryFailedRetryProtected` | delivery_failed with retries protected |
| `TestTombstoneRejectsStaleReplay` | Tombstone blocks stale gen ingest |
| `TestIngest_NewerGenSupersedesWithDuplicateIDs` | Duplicate IDs still trigger supersession |
| `TestIngest_NewerGenSupersedesAtCapacity` | Capacity-full newer gen still supersedes |
| `TestIngest_MalformedDeliveryDoesNotAdvanceGeneration` | Malformed delivery doesn't advance gen |
| `TestEvictOneLocked_PreservesPendingRecords` | Record eviction skips pending |
| `TestInvalidIngestObserved_NoStoreMutation` | Invalid ingest mutates nothing |
| `TestInvalidIngestObserved_DoesNotAdvanceHighWater` | Valid record survives invalid high-gen |
| `TestClaudeReservationRollback_LauncherFailure` | Failed launch rolls back Store slot |
| `TestClaudeReservationRollback_RepeatedFailureDoesNotExhaustCapacity` | Repeated failures don't leak |
| `TestClaudeReservationRollback_SuccessfulTombstoneRetained` | Successful launch retains tombstone |

Existing Claude tests (39): all preserved, 10x race PASS.

## 5. Gate results

```text
=== Backend ===
gofmt -d                               empty (PASS)
go build ./...                         PASS
go vet ./...                           PASS
go test -race ./... -count=1           PASS (12 packages, 0 failures)
git diff --check                       PASS
=== Race ===
Claude tests -count=10                 PASS
CLI race tests                         PASS
=== Live proof ===
TestC1D_LiveProductionProof            PASS (10.85s, 1 observation)
=== Security ===
Secret scan (diff)                     PASS (no new secrets)
=== Invariants ===
agentKind vendor branch scan           PASS
option ID inference scan               PASS
=== Mobile ===
npx tsc --noEmit                       NOT RUN (no mobile changes)
```

## 6. Non-goals (unchanged)

- C2D/C3D: NOT implemented
- Action options, ClaimForExecution, delivery capacity, mobile CTA: ZERO
- Process-image attestation: deferred to C3D
- C0H/C0R: remain BLOCKED
- N1, O1/O2, Agent SDK, Channels: NOT in scope

REVIEW REQUEST: A1.2 C1D Managed Claude Observation — `4794ce7c7eb16f4b33ac570a6a1cd7798e62ce04`
