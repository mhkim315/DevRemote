# Input-B Evidence — Delivery Semantics, Reconnect Loss, and Concurrency

**Implementation SHA:** `0bcb9004d`
**Evidence SHA:** `0383d3f67`
**PA4 ACCEPT SHA:** `74560edd`
**PB Ancestry Baseline SHA:** `abe4df1d6`

## Ancestry

```
$ git merge-base --is-ancestor 74560edd8 HEAD
PA4 ACCEPT: ANCESTOR OK
$ git merge-base --is-ancestor abe4df1d6 HEAD
PB BASELINE: ANCESTOR OK
```

## Gate Results

```
go build ./...                    exit 0
go vet ./...                      exit 0
gofmt -l .                        0 files
git diff --check                   exit 0
go test -race ./... -count=1       ALL PASS (11 packages)
npx tsc --noEmit                   clean
npx jest --runInBand               481/481 pass, 35 suites
```

## Focused Test Results

### Backend Input-B (20 tests)
```
TestInputB_HandleWSAcknowledgesExactGenerationAfterWrite   PASS
TestInputB_HandleWSWriteFailureDoesNotAcknowledge          PASS
TestInputB_HandleWSShortWriteIsWriteFailed                 PASS
TestInputB_DuplicateInputIDReplaysCached                   PASS
TestInputB_QueuedDuplicateArrivalWritesOnlyOnce            PASS
TestInputB_HandleWSCloseDropsConnectionReplayState         PASS
TestInputB_InputIDConflictDifferentPayload                  PASS
TestInputB_ProductionRejectsLegacyBinaryBeforeWrite        PASS
TestInputB_TrailingTerminalInputIsRejected                 PASS
TestInputB_ConnectionIDEntropyFailureIsFailClosed          PASS
TestInputB_ConcurrentDuplicateArrivalRace                  PASS
TestInputB_ConcurrentCacheAccessRace                       PASS
TestInputB_PermissionRefreshInterleaving                   PASS
TestInputB_ExactRawAndDecodedBounds                        PASS
TestInputB_StrictParserRejectsMalformedUnknownAndTrailing  PASS
TestInputB_CacheCapacityRaceNeverEvictsAcceptedEntries     PASS
TestInputB_ClosedOutcomesAndPermissionLimiter              PASS
TestInputB_CacheCapacityNeverEvictsAcceptedReplay          PASS
TestInputB_ReplacedCapturedTransportCannotWriteNewGeneration PASS
TestInputB_ReplacementRaceKeepsWriteBoundToCapturedGeneration PASS
```

### Mobile FeedScreen Input-B (7+ tests)
```
feedScreenInput.test.ts — all PASS (481 total Jest)
```

## Delivery Semantics

### 2-Frame Line Aggregation (text + Enter)

| Text Outcome | Enter Outcome | UI Status |
|---|---|---|
| accepted | accepted | Delivered to terminal |
| not accepted (zero-byte guarantee) | not accepted (zero-byte guarantee) | Not delivered |
| accepted | ANY non-accepted | Possible partial delivery |
| write_failed | ANY | Possible partial delivery |
| ANY | write_failed | Possible partial delivery |
| ANY lost/timeout/reconnect | ANY | Possible partial delivery |
| reconnect hello clears pending | — | Possible partial delivery |

### Standalone Input (macro, paste, keyboard)

| Outcome | UI Status |
|---|---|
| accepted | Delivered to terminal |
| definitive pre-write rejection (zero bytes) | Not delivered |
| write_failed | Possible partial delivery |
| timeout/reconnect/unknown | Possible partial delivery |
| reconnect hello clears pending | Possible partial delivery |

## Concurrency Proofs

### Concurrent Cache Access Race
100 goroutines concurrently call cache.store() and cache.get(). Final read is
consistent — exactly one accepted result with correct sequence number. No
corruption, no lost entries, no double-write.

### Permission Refresh Interleaving
Connection A (authorized) sends input while Connection B opens concurrently.
Connection A's auth snapshot is immutable per connection — it writes bytes
and receives accepted regardless of Connection B's existence. The frozen
connection-time authorization rule is tested with channel barriers (no sleeps).

### Transport Replacement Race
Two transports created for the same session ID. Old transport's write is
bound to the captured instance. Replacement transport writes to the new
instance. No cross-generation leak.

## QR and Input-A

QR renderer and Input-A production behavior were NOT modified in this change.
Only Input-B delivery semantics, reconnect handling, and concurrency tests
were changed.

## Changed Files

- `mobile/src/screens/FeedScreen.tsx` — reconnect delivery_unknown, 2-frame sibling-accepted, standalone write_failed
- `companion-daemon/internal/term/input_b_ack_test.go` — concurrent cache race, permission refresh interleaving
- `companion-daemon/docs/PB7_INPUT_B_EVIDENCE.md` — this document
