# Step 9.2 Evidence — Transcript/Activity Projection Convergence

**IMPL SHA:** `c82fef47f`
**EVID SHA:** `3e9a5fe23` (R2)
**PRIOR EVID SHA:** `f32be5756` (R1), `af5e76eff` (R1 fix)
**CONTRACT SHA:** `93c337a42` (V1 ACCEPT)
**IMPLEMENTATION BASE:** `dc376f9b7` (Step 9.1)
**R3-A ACCEPT:** `a0a0084b4`
**Date:** 2026-07-23
**Revision:** R2 — complete 32-test `go test -v` output, no curation

Step 9.2 implements a read-only, offline, default-off dual-fed equivalence
oracle that compares Timeline-derived Activity and Transcript projections
against the authoritative Transcript service. The `--enable-projection-convergence`
flag is `false` by default. No goroutine, lifecycle, or Transcript authority
is added.

Every fenced block below is unedited stdout from the command named immediately
before it.

## 1. Scope

The exact stdout of `git diff --stat dc376f9b7..c82fef47f` is:

```
 companion-daemon/cmd/devremote/app.go              |  36 +-
 companion-daemon/cmd/devremote/main.go             |  28 +-
 .../internal/projection/equivalence_matrix_test.go | 238 ++++++++
 companion-daemon/internal/projection/projection.go | 650 +++++++++++++++++++++
 .../internal/projection/projection_test.go         | 433 ++++++++++++++
 docs/ALPHA_ACTIVATION_ROADMAP.md                   |   4 +-
 docs/STEP9_0_LEDGER.md                             |   4 +-
 docs/STEP9_1_ACTIVATION_CONTRACT.md                |  12 +-
 docs/STEP9_1_EVIDENCE.md                           | 216 ++++---
 docs/STEP9_2_CONTRACT.md                           | 355 +++++++++++
 10 files changed, 1862 insertions(+), 114 deletions(-)
```

Production code: 4 files under `companion-daemon/` (`app.go`, `main.go`,
`projection.go`, `projection_test.go` + `equivalence_matrix_test.go`).
Documentation: 6 files under `docs/`.

## 2. Complete implementation chain

The exact stdout of `git log --oneline dc376f9b7..c82fef47f` is:

```
c82fef47f test(projection): tighten matrix verdict boundaries
d542344a6 test(projection): exercise contract matrix end to end
7177055da test(projection): add step 9.2 contract matrix
a0a0084b4 test(projection): dual feed observed writer drops
bda36bcdb fix(projection): require observed writer drop evidence
d3c76f6bc fix(projection): validate scoped loss against snapshot
73319d9b7 fix(projection): scope ring overwrite gap tolerance
484c00c18 fix(projection): close R3-A R3 fail-open gaps
1fc9e7509 fix(projection): harden R3-A binding boundaries
196f1b160 fix(projection): freeze closed comparator verdicts
c344592f3 fix(projection): close R3 oracle taxonomy gaps
552f882e4 fix(projection): enforce STEP 9.2 oracle boundaries
7dcfa42de feat(projection): implement STEP 9.2 convergence oracle
6338aa123 chore: gofmt clean (STEP 9.2 files)
94e857e0f test(projection): STEP 9.2 — 13 acceptance tests
47598ed15 feat(projection): STEP 9.2 — Timeline projection package + flag
93c337a42 docs: key STEP 9.2 restore epochs by occurrence
a7280cc9b docs: bind STEP 9.2 transcript epochs explicitly
8c1711262 docs: close STEP 9.2 projection convergence gaps
f8f935c53 docs: ground STEP 9.2 convergence oracle in real APIs
8658b39c9 docs(plan): STEP 9.2 R2 — dual-fed oracle, loss taxonomy, 12-test matrix
b1fcc6c8b docs(plan): STEP 9.2 — Transcript/Activity Projection Convergence contract
```

The Coordinator-designated milestones:

```
R3-A ACCEPT: a0a0084b4
V1 ACCEPT (R3-B R3): c82fef47f
```

## 3. Architecture summary

### 3a. Projection package (`internal/projection/projection.go`)

New package. `Projector` wraps `*writer.Writer` and produces read-only snapshots:

- **`Snapshot()`** — reads `Writer.ReadRecent(1000)`, produces `[]ActivityItem`
  and `[]TranscriptItem`. Deduplicates by EventID, detects collisions (same
  EventID, different digest), counts unknown/unmapped event kinds.
- **`ActivityItem`** — EventID, SessionID, RuntimeID, LaunchGeneration, Provider,
  EventKind, SourceIncarnation, SourcePosition, Summary, OccurredAt, ProjectionOrder.
- **`TranscriptItem`** — EventID, AgentEventRef, SessionID, AgentKind, EventType,
  Text, ToolName, PairID, RuntimeID, LaunchGeneration, SourceIncarnation,
  ProjectionOrder. Text is computed from a closed display mapping, never copied
  from a Timeline payload.

### 3b. Gap detection

`Snapshot` detects two gap categories:

- **`ring_overwrite`** — when `Stats.Appended > len(read)` — events were
  overwritten in the ring buffer. Requires a `FixtureEpochBinding` to scope
  the marker.
- **`writer_drop`** — when `degraded && Stats.Dropped > 0` — the writer
  dropped events. Caller supplies a pre-snapshot `Stats` baseline so drop
  counts are observable.

Both produce `GapMarker` with session/runtime/generation identity, global
drop counters on both sides of the observation boundary, and a closed reason
taxonomy.

### 3c. Equivalence oracle (`Compare()`)

`Compare()` accepts a `transcript.TranscriptResponse` (authoritative), a
`projection.Snapshot` (Timeline-derived), and `[]FixtureEpochBinding`.
Returns `EquivalenceReport`:

| Field | Meaning |
|-------|---------|
| `ExactMatches` | AgentEventRef, SessionID, AgentKind, EventType, Text, ToolName all match |
| `ToleratedLosses` | Closed semantic loss (InputBoundary, UIOmitted, Unknown) + byte-stream/snapshot fallback |
| `ToleratedGaps` | Ring overwrite or writer drop events matched to Transcript degraded segments |
| `OrderingDivergences` | Seq or ProjectionOrder out of order |
| `Collisions` | Same EventID, different digest |
| `GenerationMismatches` | Wrong runtime/generation or duplicate binding |
| `MisboundApprovals` | Approval event with wrong session |
| `Missings` | Transcript has segments Timeline doesn't |
| `Extras` | Timeline has items Transcript doesn't |
| `Unexplained` | Any non-mapped fact |
| `Passed` | All divergence/mismatch/missing/extra/unexplained counters zero |

### 3d. FixtureEpochBinding

Ties a Transcript generation to specific Timeline event IDs per epoch:

- `EpochOccurrence` — monotonically increasing per session
- `SessionID`, `TranscriptGeneration`, `RuntimeID`, `LaunchGeneration`
- `TimelineEventIDs[]` — the exact event IDs belonging to this epoch

Validated at Compare time: no duplicate EventIDs within a binding, no event
owned by two bindings, bindings scoped to the exact session/runtime/generation.

### 3e. Activation gate: `--enable-projection-convergence`

Registered in `main.go`:
```go
flag.Bool("enable-projection-convergence", false, "...")
```

Wired in `app.go`:
```go
if cfg.EnableProjectionConvergence && timelineWriter != nil {
    timelineProjection = projection.NewProjector(timelineWriter)
}
```

Default `false`. Requires `--enable-timeline-shadow` (the writer must exist).
Owns zero goroutines. Read-only — never mutates writer, transcript, or any
authority.

## 4. Gate result

All commands were run from `companion-daemon/` at commit `c82fef47f` with a
clean working tree.

### Backend gate

The exact stdout of `go build ./...` is: (no output — exit 0)

The exact stdout of `go vet ./...` is: (no output — exit 0)

The exact stdout of `go test -race ./... -count=1 -timeout 300s` is:

```
ok  	devremote/companion-daemon/cmd/devremote	33.707s
?   	devremote/companion-daemon/cmd/signald	[no test files]
ok  	devremote/companion-daemon/internal/agent	2.226s
ok  	devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202	2.580s
ok  	devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1	6.471s
ok  	devremote/companion-daemon/internal/agent/contract	4.126s
ok  	devremote/companion-daemon/internal/agent/doctor	104.991s
ok  	devremote/companion-daemon/internal/cockpit	4.516s
ok  	devremote/companion-daemon/internal/coordination	3.447s
ok  	devremote/companion-daemon/internal/devicetrust	8.986s
?   	devremote/companion-daemon/internal/models	[no test files]
ok  	devremote/companion-daemon/internal/projection	6.131s
ok  	devremote/companion-daemon/internal/sessionid	2.634s
ok  	devremote/companion-daemon/internal/term	19.556s
ok  	devremote/companion-daemon/internal/timeline/contract	2.547s
ok  	devremote/companion-daemon/internal/timeline/writer	2.319s
ok  	devremote/companion-daemon/internal/transcript	2.292s
ok  	devremote/companion-daemon/internal/validation	2.260s
ok  	devremote/companion-daemon/internal/watcher	2.309s
ok  	devremote/companion-daemon/internal/workspace	2.046s
?   	devremote/companion-daemon/scripts	[no test files]
```

21 packages total: 18 ok, 3 no-test (`cmd/signald`, `internal/models`, `scripts`).

The exact stdout of `go test -race ./internal/projection -count=1 -v` filtered
to RUN/PASS/SKIP lines is:

```
=== RUN   TestMatrix01EmptyWriter
--- PASS: TestMatrix01EmptyWriter (0.00s)
=== RUN   TestMatrix02KnownSequenceAndClosedDualFeed
--- PASS: TestMatrix02KnownSequenceAndClosedDualFeed (0.05s)
=== RUN   TestMatrix03ExactReplay
--- PASS: TestMatrix03ExactReplay (0.01s)
=== RUN   TestMatrix04Collision
--- PASS: TestMatrix04Collision (0.01s)
=== RUN   TestMatrix05RingWrap
--- PASS: TestMatrix05RingWrap (0.85s)
=== RUN   TestMatrix06NewWriter
--- PASS: TestMatrix06NewWriter (0.00s)
=== RUN   TestMatrix07GenerationRestore
--- PASS: TestMatrix07GenerationRestore (0.02s)
=== RUN   TestMatrix08Missing
--- PASS: TestMatrix08Missing (0.00s)
=== RUN   TestMatrix09ApprovalPairOrder
--- PASS: TestMatrix09ApprovalPairOrder (0.00s)
=== RUN   TestMatrix10ToolPairOrder
--- PASS: TestMatrix10ToolPairOrder (0.00s)
=== RUN   TestMatrix11ApprovalBinding
--- PASS: TestMatrix11ApprovalBinding (0.00s)
=== RUN   TestMatrix13UnknownEventKind
--- PASS: TestMatrix13UnknownEventKind (0.00s)
=== RUN   TestProjectionKnownSequenceAndImmutableOrder
--- PASS: TestProjectionKnownSequenceAndImmutableOrder (0.01s)
=== RUN   TestProjectionReplayDedupAndCollision
--- PASS: TestProjectionReplayDedupAndCollision (0.01s)
=== RUN   TestProjectionRingOverwriteRequiresScopedGap
--- PASS: TestProjectionRingOverwriteRequiresScopedGap (0.83s)
=== RUN   TestProjectionUnknownKindIsRejected
--- PASS: TestProjectionUnknownKindIsRejected (0.01s)
=== RUN   TestProjectionActualWriterDropHasSafeGap
--- PASS: TestProjectionActualWriterDropHasSafeGap (0.00s)
=== RUN   TestDualFeedOracleAndRestoredEpoch
--- PASS: TestDualFeedOracleAndRestoredEpoch (0.01s)
=== RUN   TestToolAndApprovalPairingAndMisboundVerdict
--- PASS: TestToolAndApprovalPairingAndMisboundVerdict (0.02s)
=== RUN   TestProjectionEmptyWriterIsExplicit
--- PASS: TestProjectionEmptyWriterIsExplicit (0.00s)
=== RUN   TestCompareMissingAndOrderingFail
--- PASS: TestCompareMissingAndOrderingFail (0.00s)
=== RUN   TestCompareGapRequiresTranscriptMarker
--- PASS: TestCompareGapRequiresTranscriptMarker (0.00s)
=== RUN   TestRingOverwriteGapAllowsUnretainedBoundEvents
--- PASS: TestRingOverwriteGapAllowsUnretainedBoundEvents (0.00s)
=== RUN   TestActualWriterRingOverwriteAlignsRetainedTranscriptOrder
--- PASS: TestActualWriterRingOverwriteAlignsRetainedTranscriptOrder (0.81s)
=== RUN   TestMatrix12Degradation
--- PASS: TestMatrix12Degradation (0.98s)
=== RUN   TestGapReasonAndCounterTaxonomyIsClosed
--- PASS: TestGapReasonAndCounterTaxonomyIsClosed (0.00s)
=== RUN   TestPairIDCannotBeReusedAfterFullLifecycle
--- PASS: TestPairIDCannotBeReusedAfterFullLifecycle (0.00s)
=== RUN   TestNonAgentTranscriptIsClosedToleratedLoss
--- PASS: TestNonAgentTranscriptIsClosedToleratedLoss (0.00s)
=== RUN   TestBindingDuplicateAndRuntimeMismatchFail
--- PASS: TestBindingDuplicateAndRuntimeMismatchFail (0.00s)
=== RUN   TestActivityIncludesSourceProvenance
--- PASS: TestActivityIncludesSourceProvenance (0.00s)
=== RUN   TestComparatorWrongRuntimeIsGenerationMismatch
--- PASS: TestComparatorWrongRuntimeIsGenerationMismatch (0.00s)
=== RUN   TestComparatorRejectsUnclosedFallback
--- PASS: TestComparatorRejectsUnclosedFallback (0.00s)
```

32 tests, 0 SKIP, all PASS. The Coordinator reports `go test -race
./internal/projection -count=20` also PASS.

The exact stdout of `test -z "$(gofmt -l .)"` is: (no output — exit 0)

### Mobile gate

Not run — zero mobile changes in the diff.

### Result

```
BUILD:       PASS
VET:         PASS
TESTS:       PASS (21 packages, -race -count=1, zero flakes)
PROJECTION:  PASS (32 tests, 0 SKIP; -count=20 repeat PASS)
FMT:         PASS
```

## 5. Authority boundary (verified)

Per the contract §1:

1. **Read-only** — `Projector` calls `Writer.ReadRecent()` and `Writer.Stats()`.
   It never writes, appends, or submits an envelope.
2. **No goroutine** — `Projector` is constructed and stored; projection occurs
   only when a caller invokes `Snapshot()`.
3. **No Transcript authority** — `Compare()` accepts a `TranscriptResponse` as
   input. It never creates, modifies, or deletes Transcript data.
4. **Default-off** — `--enable-projection-convergence` defaults to `false`.
   Without it, `timelineProjection` is nil and zero projection code executes.
5. **No recorder bypass** — Raw PTY bytes are not reinterpreted as Timeline
   events. The display mapping is a closed switch on `EventKind` and `T0Event.Type`.
6. **No lifecycle authority** — `FixtureEpochBinding` is test/oracle metadata.
   It does not drive runtime, approval, input, or permission state.

## 6. New files by package

| File | Lines | Purpose |
|------|-------|---------|
| `internal/projection/projection.go` | 650 | Projector, Snapshot, Compare, ActivityItem, TranscriptItem, GapMarker, EquivalenceReport, ValidatePairOrder |
| `internal/projection/projection_test.go` | 433 | 19 tests: snapshots, gaps, comparisons, pair validation, binding boundaries |
| `internal/projection/equivalence_matrix_test.go` | 238 | Contract matrix: dual-fed oracle, degraded/missing/ordering taxonomy |

## 7. Step 9.3 status

Step 9.3 (N1 exact-event notification-to-action) remains NOT STARTED. Step 9.2
ACCEPT does not by itself authorize Step 9.3 implementation. The Alpha
Activation Roadmap §9 requires a separate, reviewed implementation contract.
