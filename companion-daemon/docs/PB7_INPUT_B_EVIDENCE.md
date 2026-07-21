# Input-B Evidence — Delivery Semantics, Reconnect Loss, and Concurrency

**Implementation SHA:** `06c5b2a81`
**Evidence SHA:** `414138e88`
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
npx jest --runInBand               482/482 pass, 35 suites
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

### Mobile FeedScreen Input-B
```
feedScreenInput.test.ts — all PASS (482 total Jest)
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

## Concurrency Tests

### ConcurrentDuplicateArrivalRace
Pre-stores a cache entry, then 99 concurrent goroutines call `get()` to read the
cached result (sequential write, concurrent readers). All readers see the
identical cached outcome. A `getConflict` check verifies that same-digest
returns false and different-digest returns true. The write is sequential; only
the reads are concurrent.

### ConcurrentCacheAccessRace
100 goroutines race on `canStore` + `store` with unique inputIds, using an
atomic counter. Every stored entry is verifiably readable with the correct
outcome. Tests that concurrent cache access does not cause panics, corruption,
or lost entries. Does not assert single-write semantics.

### PermissionRefreshInterleaving
Two independently-created fixtures: one with an authorized principal (has
`terminal:input`) and one with an unauthorized principal (no `terminal:input`).
Connection A (authorized fixture) receives `accepted` with bytes written.
Connection B (unauthorized fixture) receives `permission_denied` with zero
bytes written. Proves that each connection's authorization is determined by its
own principal at connect time.

### Transport Replacement Race
Two transports created for the same session ID. Old transport's write is
bound to the captured instance. Replacement transport writes to the new
instance. No cross-generation leak.

### Mobile Concurrent ACK (sequential fake-timer)
A standalone input is sent via postMessage, then the fake timer advances past
the ACK timeout (3s), producing "Possible partial delivery". A late
`input_result` with outcome `accepted` is then delivered — it must NOT
overwrite the "Possible partial delivery" state. Uses Jest fake timers; no
real concurrency.

## QR and Input-A

QR renderer and Input-A production behavior were NOT modified in this change.
Only Input-B delivery semantics, reconnect handling, and concurrency tests
were changed.

## Changed Files

- `mobile/src/screens/FeedScreen.tsx` — reconnect delivery_unknown, 2-frame sibling-accepted, standalone write_failed
- `companion-daemon/internal/term/input_b_ack_test.go` — concurrent cache + permission tests
- `companion-daemon/docs/PB7_INPUT_B_EVIDENCE.md` — this document
