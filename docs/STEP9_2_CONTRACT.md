# STEP 9.2 — Transcript/Activity Projection Convergence Contract

**Status:** IMPLEMENTATION CONTRACT — PENDING IMPLEMENTATION

**Branch:** `feature/canonical-timeline-foundation`
**PREREQUISITE:** Step 9.1 ACCEPTED at `dc376f9b7`

## 1. Scope and authority

Step 9.2 builds a dual-fed equivalence oracle that compares Timeline-derived
Activity and Transcript projections against the authoritative Transcript
service. The oracle is read-only, offline, and default-off.

**What is NEW:**
- `internal/projection/` — Activity + Transcript projections from Timeline ring buffer
- `internal/projection/equivalence_test.go` — dual-fed equivalence oracle
- `--enable-projection-convergence` — default-off CLI flag

**What stays UNCHANGED:**
- `internal/transcript/` — authoritative Transcript service and API
- `Recorder` — sole PTY reader; raw bytes are NOT reinterpreted as Timeline events
- Cockpit, mobile, device trust, approval, terminal transport — unchanged

## 2. Dual-fed Oracle Model

The equivalence oracle compares TWO independently sourced projections:

### 2a. Transcript Oracle (authoritative)

Source: `Transcript.Service.ListTranscript(sessionID)` API response.

Each `TranscriptResponse` contains:
- `BuildResponse` — stable structural identity
- `PrimarySource` — provider/session/runtime provenance
- `Generation` — generation at observation time
- `Suppression` — explicit gap/error markers
- `IDs` — canonical IDs for dedup
- `Seq` — monotonic sequence

### 2b. Activity Oracle (Timeline-derived, comparison target)

Source: `writer.Writer.ReadRecent(n)` ring buffer → projection pipeline.

Each Activity item maps to a named comparison target:
```
Timeline envelope → projection.ActivityItem:
  ID, SessionID, RuntimeID, Generation, Provider,
  EventKind, Summary, OccurredAt, Seq, Source
```

### 2c. Canonical Normalization

Before comparison, both sources are normalized:

| Dimension | Transcript | Activity | Normalization |
|-----------|-----------|----------|--------------|
| ID | `TranscriptResponse.IDs` | `Envelope.EventID` | Sort+dedup |
| Generation | `TranscriptResponse.Generation` | `Envelope.LaunchGeneration` | Direct compare |
| Session | `TranscriptResponse.PrimarySource` | `Envelope.SessionID` | Direct compare |
| Ordering | `TranscriptResponse.Seq` | Insertion order | Monotonic |
| Content | `TranscriptResponse.BuildResponse` | `Envelope.RedactedPayload.Summary` | Semantic |
| Gaps | `TranscriptResponse.Suppression` | `Writer.Health().degraded` | Tolerated loss taxonomy |

### 2d. Tolerated Loss Taxonomy

Not every difference is a failure. The taxonomy:

| Category | Meaning | Verdict |
|----------|---------|---------|
| `exact_match` | Byte-identical after normalization | PASS |
| `tolerated_loss` | Timeline projection is empty/partial; Transcript has full data | PASS (expected during shadow) |
| `tolerated_gap` | Writer drop detected; gap marker present in both | PASS |
| `ordering_divergence` | Items in different order but same content set | TOLERATED (eventual consistency) |
| `collision` | Same EventID, different canonical digest | FAIL |
| `extra` | Timeline has item not in Transcript | FAIL |
| `missing` | Transcript has item not in Timeline (no gap marker) | FAIL |
| `generation_mismatch` | Same session, different generation binding | FAIL |
| `misbound_approval` | Approval events bound to wrong session/generation | FAIL |
| `unexplained` | Any difference not covered above | FAIL |

### 2e. PASS/FAIL Schema

The oracle produces a structured verdict per comparison run:

```go
type EquivalenceReport struct {
    ComparedSessions   int
    TotalComparisons   int
    ExactMatches       int
    ToleratedLosses    int
    ToleratedGaps      int
    OrderingDivergences int
    Collisions         int
    Extras             int
    Missings           int
    GenerationMismatches int
    MisboundApprovals  int
    Unexplained        int
    Passed             bool // true when all FAIL categories are zero
}
```

Per roadmap §5: "There is no PASS with unexplained missing, extra, reordered,
duplicated, misbound, unknown-version, collision, gap, or degraded input."

## 3. Loss Semantics

### 3a. Duplicate EventID

Two cases:

1. **Exact replay (idempotent):** same EventID, same CanonicalDigest.
   First-wins, subsequent suppressed. Counted as `exact_match`. Not an error.

2. **Collision:** same EventID, DIFFERENT CanonicalDigest.
   Producer error or corruption. Counted as `collision` → FAIL.
   Per `contract.SameEvidence()`: returns `ErrEventIDCollision`.

### 3b. Writer restart / empty ring

An empty ring buffer after restart is NOT silent. The `Writer.Health()`
degradation flag is set on restart (ring buffer empty, no data yet).
The projection returns:

- `Activity`: empty with `gap_marker: "writer_restart"` metadata
- `Transcript`: empty with `unavailable: "timeline_restart"` metadata

The equivalence oracle detects this as `tolerated_loss` (Timeline has less
data than Transcript — expected after restart).

### 3c. Global writer drops

When `Writer.Stats().Dropped > 0`, the writer emits a synthetic
`EventDegraded` envelope with `metadata: {dropped: N, generation: G, session: S}`.
This envelope is pushed into the ring buffer so projections can expose an
explicit gap boundary with session+generation ordering.

```go
type GapMarker struct {
    SessionID    string
    Generation   int64
    DroppedCount uint64
    OccurredAt   time.Time
}
```

The projection inserts `GapMarker` entries at the drop site (between known
items). The equivalence oracle counts these as `tolerated_gap` (both
Transcript and Timeline acknowledge the gap).

## 4. Acceptance Matrix

One acceptance test per dimension. All tests use the existing
`writer.Writer` ring buffer and `writer.Health()` as the realizable source.

| # | Dimension | Test | Source |
|---|-----------|------|--------|
| 1 | Empty writer | Projections return empty, not nil; oracle classifies as `tolerated_loss` | Ring buffer empty |
| 2 | Known sequence | 10 envelopes → Activity items in insertion order | Ring buffer with 10 items |
| 3 | Exact replay | Same EventID + same digest → suppressed, not counted as duplicate | `contract.SameEvidence()` |
| 4 | Collision | Same EventID + different digest → FAIL | `contract.ErrEventIDCollision` |
| 5 | Ring buffer wrap | 200 items → oldest 72 dropped, 128 retained | Ring capacity 128 |
| 6 | Reconnect | After writer restart → empty ring → `tolerated_loss` oracle verdict | `Writer.Health()` + new writer |
| 7 | Generation reset | Claude N→N+1→restored-N: each generation has own ordering | Three generations |
| 8 | Missing events | Transcript has item not in Timeline → FAIL (no gap marker) | Oracle comparison |
| 9 | Request/result ordering | Approval-requested before approval-resolved | Insertion order |
| 10 | Approval binding | Approval events carry correct session+generation | Envelope identity |
| 11 | Degradation | Writer has drops → gap markers in projection → `tolerated_gap` | `Writer.Health().degraded` |
| 12 | Unknown version | Unknown EventKind → FAIL in oracle, dropped by writer | `isBoundEventKind` |

## 5. Implementation files

**May create:**
- `internal/projection/activity.go` — Activity projection from ring buffer
- `internal/projection/transcript.go` — Transcript projection from ring buffer
- `internal/projection/equivalence_test.go` — 12 acceptance tests
- `internal/projection/equivalence.go` — oracle, normalization, taxonomy
- `cmd/devremote/app.go` — add `--enable-projection-convergence` flag (wires nothing)

**Must NOT change:**
- `internal/transcript/`, `internal/term/`, `mobile/`, existing REST/WS handlers

## 6. Gate

- [ ] All existing tests pass
- [ ] 12 new acceptance tests pass
- [ ] Equivalence report PASS (all FAIL categories zero on clean data)
- [ ] Default-off flag: zero runtime effect when disabled
- [ ] Separate evidence commit records equivalence results

## 7. Stop conditions

Stop and reject if the change:
- Modifies existing Transcript service or API behavior
- Interprets raw PTY bytes as Timeline events
- Treats empty ring as silent success (must be explicit gap/unavailable)
- Treats duplicate EventID with different digest as idempotent (must FAIL)
- Unexplained difference in oracle output (must classify every difference)
