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

Source: `Transcript.Service.ListTranscript(sessionID) []TranscriptSegment`.
The oracle obtains the response envelope, when needed, with
`Transcript.Service.BuildResponse(sessionID, segments) TranscriptResponse`.
It must use the actual public fields, not invented response fields:

| Oracle input | Actual field(s) used |
|---|---|
| Session and incarnation | `TranscriptResponse.SessionID`, `TranscriptResponse.Generation`, `TranscriptSegment.SessionID` |
| Ordered semantic source | `TranscriptResponse.Semantic`; `TranscriptSegment.Seq` is the immutable per-session ordering key |
| Segment identity and provider correlation | `TranscriptSegment.ID`, `TranscriptSegment.AgentEventRef`, `TranscriptSegment.AgentKind`, `TranscriptSegment.EventType` |
| Safe rendered content | `TranscriptSegment.Text`, `Kind`, `Source`, `ToolName`, `DegradedReason` |
| Source/fallback state | `TranscriptResponse.PrimarySource`, `Fallback`, `ByteStreamSuppressed`, `ContractVersion` |

Only correlated `KindAgentEvent` / `SourceAgentEvent` segments participate in
the dual-fed semantic equivalence set. Byte-stream, snapshot, input-boundary,
UI-omitted, unknown, and existing Transcript-degraded segments remain
Transcript-owned fallback or diagnostic data; each is an explicit excluded
mapping, never an implicit Timeline loss.

### 2a.1 Dual-fed fixture rule

Each fixture creates one ordered set of accepted `agent.AgentEvent` values and
feeds that same set to `Transcript.Service.ProjectAgentEvents(sessionID, events)`
and to the Timeline-envelope construction/Writer path. It then reads the
actual Transcript segments with `ListTranscript(sessionID)` and Timeline
envelopes with `ReadRecent`. A fixture must state its event-kind mapping before
comparison; unsupported Timeline-only operational evidence is not silently
paired with a Transcript segment.

### 2b. Activity Oracle (Timeline-derived, comparison target)

Source: `writer.Writer.ReadRecent(n)` ring buffer → projection pipeline.

Each Activity item is a defined projection of an actual
`contract.Envelope`, not a structural copy of Transcript data:
```
Timeline envelope → projection.ActivityItem:
  EventID, SessionID, RuntimeID, LaunchGeneration, Provider,
  EventKind, SourceIncarnation, SourcePosition, OccurredAt,
  safe payload projection, projectionOrder
```

`projectionOrder` is the order returned by `Writer.ReadRecent`; it is not an
Envelope field. The Activity oracle defines the expected output by applying
this projection to the dual-fed Timeline envelopes, including the closed
`contract.EventKind` vocabulary. It must not fabricate `Seq`, `Source`, or a
Transcript response field on an Envelope.

### 2c. Canonical Normalization

Before comparison, both sources are normalized:

| Dimension | Transcript | Activity | Normalization |
|-----------|-----------|----------|--------------|
| Source-event identity | `TranscriptSegment.AgentEventRef` | `Envelope.T0Event.ID` | Direct compare in the dual-fed fixture |
| Timeline identity | mapped expected event | `Envelope.EventID` | Compare Activity projection to defined expected output |
| Generation | `TranscriptResponse.Generation` | `Envelope.LaunchGeneration` | Direct compare for the same session incarnation |
| Session/provider | `TranscriptSegment.SessionID`, `AgentKind` | `Envelope.SessionID`, `Provider` | Direct compare under explicit provider mapping |
| Ordering | `TranscriptSegment.Seq` | `ReadRecent` projection order | Exact mapped order; no reorder tolerance |
| Content | `TranscriptSegment.Text`, `EventType`, `ToolName` | safe payload projection, `T0Event.Type` | Only explicitly declared event-kind mappings compare content |
| Gaps | `KindDegraded` only where the fixture deliberately emits one | `Stats().Dropped`, `HealthSnapshot()` | Explicit gap-marker taxonomy below |

### 2d. Tolerated Loss Taxonomy

Not every difference is a failure. The taxonomy:

| Category | Meaning | Verdict |
|----------|---------|---------|
| `exact_match` | Exact mapped semantic identity, content, generation, and order | PASS |
| `tolerated_loss` | Only a declared, intentional mapping: Transcript fallback-only source, or a known Timeline event class that has no Step 9.1 producer mapping | PASS, counted with its mapping name |
| `tolerated_gap` | Writer drop detected; gap marker present in both | PASS |
| `ordering_divergence` | Same content set in a different order | FAIL |
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
    OrderingDivergences int // FAIL; reordering is never tolerated
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

An empty ring is never itself a pass or a `tolerated_loss`. The projection
samples the actual writer APIs at its comparison boundary:

```go
stats := w.Stats()                         // Stats{Appended, Dropped, Failures}
degraded, reason := w.HealthSnapshot()     // actual public API
```

Restart is detected only by the degraded HealthSnapshot state with the
implementation-defined restart reason; a newly constructed empty writer with
`degraded == false` is merely an empty input and fails if the dual-fed
Transcript set is non-empty. A restart observation emits an explicit
`GapMarker{Reason: "writer_restart"}` into the projection stream for the
comparison's session and generation. It can pass only as `tolerated_gap` when
the matching declared marker is present on both sides of the oracle.

### 3c. Global writer drops

`Writer.Stats().Dropped` is a global counter: it does not identify the lost
event's session, runtime, or generation. The writer must not fabricate a
synthetic `EventDegraded` Envelope or attribute a global counter to a lost
event. Instead, the projection records its pre/post global counter values and,
when they differ or HealthSnapshot is degraded, emits a marker at the explicit
comparison boundary supplied by the dual-fed fixture. That marker identifies
the fixture's `SessionID`, `RuntimeID`, and `LaunchGeneration`, and preserves
restored incarnations as distinct epochs even when an earlier generation value
is restored.

```go
type GapMarker struct {
    SessionID        string
    RuntimeID        string
    LaunchGeneration int64
    SourceIncarnation string
    GlobalDroppedBefore uint64
    GlobalDroppedAfter  uint64
    DegradedReason   string
    Reason           string
    ProjectionOrder  int64
}
```

The marker describes only the observed boundary, not a guessed drop site.
The equivalence oracle counts it as `tolerated_gap` only if the fixture's
Transcript side explicitly declares the same boundary; otherwise any missing,
extra, degraded, or gap input fails under roadmap §5.

## 4. Acceptance Matrix

One acceptance test per dimension. All tests use the existing
`writer.Writer` ring buffer, `Stats()`, and `HealthSnapshot()` as the
realizable sources.

| # | Dimension | Test | Source |
|---|-----------|------|--------|
| 1 | Empty writer | Empty projection with non-empty dual-fed Transcript is FAIL; only a matching explicit degraded/restart marker may classify `tolerated_gap` | Ring buffer + `HealthSnapshot()` |
| 2 | Known sequence | 10 envelopes → Activity items in insertion order | Ring buffer with 10 items |
| 3 | Exact replay | Same EventID + same digest → suppressed, not counted as duplicate | `contract.SameEvidence()` |
| 4 | Collision | Same EventID + different digest → FAIL | `contract.ErrEventIDCollision` |
| 5 | Ring buffer wrap | 200 items → oldest 72 dropped, 128 retained | Ring capacity 128 |
| 6 | Reconnect | Degraded `HealthSnapshot()` restart state emits a session+runtime+generation gap marker; unmarked empty ring FAILS | `HealthSnapshot()` + new writer |
| 7 | Generation reset/restore | Claude N→N+1→restored-N: compare the full `(SessionID, RuntimeID, LaunchGeneration, SourceIncarnation)` scope, so the restored-N epoch cannot merge with its earlier N epoch | Three scoped incarnations |
| 8 | Missing events | Transcript has item not in Timeline → FAIL (no gap marker) | Oracle comparison |
| 9 | Request/result ordering | Approval-requested before approval-resolved | Insertion order |
| 10 | Approval binding | Approval events carry correct session+generation | Envelope identity |
| 11 | Degradation | Global `Stats().Dropped` delta + `HealthSnapshot()` → scoped boundary marker; unmatched marker FAILS | actual writer APIs |
| 12 | Unknown version | Writer rejects an unknown `EventKind` before it reaches `ReadRecent`; a separately injected invalid projection item must fail closed in the oracle | writer validation + oracle unit boundary |

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
