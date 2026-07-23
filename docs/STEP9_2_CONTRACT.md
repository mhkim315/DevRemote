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

Each fixture creates one ordered set of accepted `agent.AgentEvent` values for
each fixture epoch. The required per-epoch protocol is:

1. Before replacing a non-initial epoch, snapshot the prior
   `ListTranscript` / `BuildResponse` result.
2. Establish the initial epoch with `EnableQueue`, or establish a later one
   with `ReplaceTranscript`.
3. Call `Transcript.Service.SetCorrelation(sessionID, CorrelationState{
   SessionID: sessionID, Correlation: contract.CorrelationProven or
   contract.CorrelationManagedLaunch, Provider: provider})`.
4. Feed that epoch's same event set to
   `Transcript.Service.ProjectAgentEvents(sessionID, events)` and to the
   Timeline-envelope construction/Writer path.
5. Obtain `BuildResponse(sessionID, ListTranscript(sessionID))` and record the
   explicit epoch binding.

`ReplaceTranscript` clears the service arbiter, so step 3 is mandatory after
every replacement. `ProjectAgentEvents` must not be called before
`SetCorrelation`: the real service otherwise fail-closes and emits no primary
semantic segments.

Every fixture uses the closed event-to-segment mapping in §2b.1 before it can
compare an event. An unsupported Timeline-only operational event is a FAIL,
not an implicitly unpaired item.

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

### 2b.1 Timeline-derived Transcript projection

`projection.TranscriptItem` is the Timeline-side comparison output. It does
not call or replace `internal/transcript`; it makes the mapping used by the
oracle explicit:

```go
type TranscriptItem struct {
    AgentEventRef     string // Envelope.T0Event.ID
    SessionID         string // Envelope.SessionID
    AgentKind         string // Envelope.T0Event.AgentKind
    EventType         string // Envelope.T0Event.Type
    Text              string // closed Transcript-projector display mapping below
    ToolName          string // T0Event.ToolName truncated to the projector's 128-byte bound
    RuntimeID         string
    LaunchGeneration  int64
    SourceIncarnation string
    ProjectionOrder   int64
}
```

The closed mappings are:

| Timeline `EventKind` | Required `T0Event.Type` | `TranscriptItem.Text` / `ToolName` normalization | Authoritative `TranscriptSegment` expectation |
|---|---|---|---|
| `provider_invocation_started` | `agent_started` | `Text="Agent started"`, `ToolName=""` | `KindAgentEvent` / `SourceAgentEvent`, `EventType=agent_started`, `AgentEventRef=T0Event.ID` |
| `provider_invocation_finished` | `completed`, `failed`, or `interrupted` | `Text="Completed"`, `"Failed"`, or `"Interrupted"` respectively; `ToolName=""` | same identity fields and matching event type |
| `tool_call_started`, `tool_call_finished` | matching tool event type | `Text=""`; `ToolName` is `T0Event.ToolName` truncated to 128 bytes | same identity fields and matching normalized `ToolName` |
| `approval_requested`, `approval_resolved` | matching approval event type | `Text="Approval requested"` or `"Approval resolved"`; `ToolName=""` | same identity fields and matching event type |
| `stream_observed` | `thinking` | `Text=""`, `ToolName=""` | same identity fields and `EventType=thinking` |

No other `contract.EventKind` is in the Step 9.2 dual-fed set. Any mapping
outside this table, any wrong `T0Event.Type`, missing correlation, missing
`AgentEventRef`, or cross-session projection is a failure.

### 2c. Canonical Normalization

Before comparison, both sources are normalized:

| Dimension | Transcript | Activity | Normalization |
|-----------|-----------|----------|--------------|
| Source-event identity | `TranscriptSegment.AgentEventRef` | `Envelope.T0Event.ID` | Direct compare in the dual-fed fixture |
| Timeline identity | mapped expected event | `Envelope.EventID` | Compare Activity projection to defined expected output |
| Epoch/incarnation | `TranscriptResponse.Generation` | fixture-assigned `EpochOccurrence` | Compare through the explicit fixture binding in §2c.1; never use runtime fields or `SourceIncarnation` as the repeated-incarnation key |
| Session/provider | `TranscriptSegment.SessionID`, `AgentKind` | `Envelope.SessionID`, `Provider` | Direct compare under explicit provider mapping |
| Ordering | `TranscriptSegment.Seq` | `ReadRecent` projection order | Exact mapped order; no reorder tolerance |
| Content | `TranscriptSegment.Text`, `EventType`, `ToolName` | `TranscriptItem.Text`, `EventType`, `ToolName` | Exact closed mapping in §2b.1; Timeline payload is not Transcript display text |
| Gaps | `KindDegraded` only where the fixture deliberately emits one | `Stats().Dropped`, `HealthSnapshot()` | Explicit gap-marker taxonomy below |

### 2c.1 Fixture epoch-to-incarnation binding

`TranscriptSegment` has no `RuntimeID`, `LaunchGeneration`, or
`SourceIncarnation`; `TranscriptResponse.Generation` is only the service's
monotonically increasing per-session Transcript generation. The fixture must
therefore record the association when it creates each epoch:

```go
type FixtureEpochBinding struct {
    EpochOccurrence      uint64 // fixture-local, strictly increasing per session
    SessionID            string
    TranscriptGeneration int64 // observed from BuildResponse after EnableQueue/ReplaceTranscript
    RuntimeID            string
    LaunchGeneration     int64
    TimelineEventIDs     []string // EventIDs submitted during this occurrence
}
```

The fixture establishes the initial Transcript epoch with `EnableQueue` and
observes it through `BuildResponse`; it uses `ReplaceTranscript` for every
later epoch, followed by a new `SetCorrelation` before projection. The binding
is one-to-one within a fixture and is the sole generation oracle.
For a restore, the fixture calls `ReplaceTranscript`, observes its next
monotonic `TranscriptResponse.Generation`, and binds that new Transcript epoch
to the new, strictly increasing `EpochOccurrence` and its submitted
`TimelineEventIDs`. Thus an original N and a restored N may reuse the same
`RuntimeID`, `LaunchGeneration`, and derived `SourceIncarnation`, but cannot
merge: their fixture occurrence and Transcript epoch differ. A missing,
duplicate, or inconsistent binding is `generation_mismatch` and fails.

### 2d. Tolerated Loss Taxonomy

Not every difference is a failure. The taxonomy:

| Category | Meaning | Verdict |
|----------|---------|---------|
| `exact_match` | Exact mapped semantic identity, content, generation, and order | PASS |
| `tolerated_loss` | Only one of the closed intentional-loss mappings below | PASS, counted with its mapping name |
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

### 2f. Closed intentional-loss mappings

No fixture may invent a tolerated-loss category. The complete list is:

| Mapping name | Exact condition | Comparison treatment |
|---|---|---|
| `transcript_fallback_not_dual_fed` | A `TranscriptResponse.Fallback` item whose `Source` is `byte_stream` or `snapshot_delta`, or a semantic `input_boundary`, `ui_omitted`, `unknown`, or existing Transcript `degraded` segment | Excluded before the dual-fed set is formed; report the named mapping, never a Timeline missing event |
| `stream_text_redacted` | `stream_observed` / `thinking` maps to a correlated `KindAgentEvent` segment | Compare identity, fixture epoch binding, type, order, and the explicit normalized empty `Text`; no provider thinking body is compared |

All other empty, partial, missing, extra, reordered, duplicate, degraded, or
unknown-version results fail. In particular, ring overwrite is not an
intentional-loss mapping.

## 3. Loss Semantics

### 3a. Duplicate EventID

Two cases:

1. **Exact replay (idempotent):** same EventID, same CanonicalDigest.
   First-wins, subsequent suppressed. Counted as `exact_match`. Not an error.

2. **Collision:** same EventID, DIFFERENT CanonicalDigest.
   Producer error or corruption. Counted as `collision` → FAIL.
   Per `contract.SameEvidence()`: returns `ErrEventIDCollision`.

### 3b. Empty ring and process restart

An empty ring is never itself a pass or a `tolerated_loss`. The projection
samples the actual writer APIs at its comparison boundary:

```go
stats := w.Stats()                         // Stats{Appended, Dropped, Failures}
degraded, reason := w.HealthSnapshot()     // actual public API
```

`Writer.Open`/`newWriter` creates a healthy in-memory `Health`, and neither
Health nor the ring has persisted state. Step 9.2 therefore has no restart
state source and MUST NOT infer restart from an empty ring or from
`HealthSnapshot()`. A new writer with an empty ring is simply empty and fails
when its dual-fed Transcript set is non-empty. Cross-process restart comparison
is deferred until a separately authorized persisted writer/projection cursor
exists.

### 3c. Global writer drops

`Writer.Stats().Dropped` is a global counter: it does not identify the lost
event's session, runtime, or generation. The writer must not fabricate a
synthetic `EventDegraded` Envelope or attribute a global counter to a lost
event. Instead, the projection records its pre/post global counter values and,
when they differ or HealthSnapshot is degraded, emits a marker at the explicit
comparison boundary supplied by the dual-fed fixture. That marker identifies
the fixture's `SessionID`, `RuntimeID`, `LaunchGeneration`, and
`EpochOccurrence`; `SourceIncarnation` is retained only as envelope evidence,
never as the repeated-incarnation key.

```go
type GapMarker struct {
    SessionID        string
    RuntimeID        string
    LaunchGeneration int64
    EpochOccurrence  uint64
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

### 3d. Ring overwrite is an explicit gap or a failure

`ReadRecent` retains only 128 envelopes and ring overwrite does not increment
`Stats().Dropped`. The projection must compare its `Stats().Appended` baseline
with the retained `ReadRecent` result. If retained evidence proves an
overwrite, it emits `GapMarker{Reason: "ring_overwrite"}` for every affected
known comparison scope. A single-scope fixture of 200 accepted envelopes must
therefore emit one scoped marker covering the lost 72 entries.

If an overwrite spans scopes whose lost session/runtime/generation/incarnation
cannot be determined from the fixture/projection cursor, no marker may guess
their ownership: the comparison fails as `unexplained`. Ring overwrite without
the required marker always fails.

## 4. Acceptance Matrix

One acceptance test per dimension. All tests use the existing
`writer.Writer` ring buffer, `Stats()`, and `HealthSnapshot()` as the
realizable sources.

| # | Dimension | Test | Source |
|---|-----------|------|--------|
| 1 | Empty writer | Empty projection with non-empty dual-fed Transcript is FAIL; no restart marker is inferred | Ring buffer + `HealthSnapshot()` |
| 2 | Known sequence | 10 envelopes → Activity items in insertion order | Ring buffer with 10 items |
| 3 | Exact replay | Same EventID + same digest → suppressed, not counted as duplicate | `contract.SameEvidence()` |
| 4 | Collision | Same EventID + different digest → FAIL | `contract.ErrEventIDCollision` |
| 5 | Ring buffer wrap | 200 one-scope envelopes → 128 retained plus explicit `ring_overwrite` marker covering 72; absent/unscopeable marker FAILS | `Stats().Appended` baseline + `ReadRecent` |
| 6 | New writer | New healthy writer has an empty ring; non-empty dual-fed Transcript comparison FAILS (no restart inference) | `HealthSnapshot()` + new writer |
| 7 | Generation reset/restore | Claude N→N+1→restored-N: bind each `BuildResponse.Generation` to a unique, increasing `EpochOccurrence`; restored-N cannot merge with earlier N even if runtimeID/generation/source-incarnation repeat | Three `FixtureEpochBinding` records |
| 8 | Missing events | Transcript has item not in Timeline → FAIL (no gap marker) | Oracle comparison |
| 9 | Approval request/result ordering | `approval_requested` precedes its matching `approval_resolved` in both Transcript `Seq` and Timeline `ProjectionOrder` | approval reference + order |
| 10 | Tool call request/result ordering | `tool_call_started` precedes its matching `tool_call_finished` in both Transcript `Seq` and Timeline `ProjectionOrder`; mismatched or reordered tool pair FAILS | tool-call reference + order |
| 11 | Approval binding | Approval events carry correct session + `FixtureEpochBinding` | Envelope identity + epoch binding |
| 12 | Degradation | Global `Stats().Dropped` delta + `HealthSnapshot()` → scoped boundary marker; unmatched marker FAILS | actual writer APIs |
| 13 | Unknown version | Writer rejects an unknown `EventKind` before it reaches `ReadRecent`; a separately injected invalid projection item must fail closed in the oracle | writer validation + oracle unit boundary |

## 5. Implementation files

**May create:**
- `internal/projection/activity.go` — Activity projection from ring buffer
- `internal/projection/transcript.go` — Transcript projection from ring buffer
- `internal/projection/equivalence_test.go` — 13 acceptance tests
- `internal/projection/equivalence.go` — oracle, normalization, taxonomy
- `cmd/devremote/app.go` — add `--enable-projection-convergence` flag (wires nothing)

**Must NOT change:**
- `internal/transcript/`, `internal/term/`, `mobile/`, existing REST/WS handlers

## 6. Gate

- [ ] All existing tests pass
- [ ] 13 new acceptance tests pass
- [ ] Equivalence report PASS (all FAIL categories zero on clean data)
- [ ] Default-off flag: zero runtime effect when disabled
- [ ] Separate evidence commit records equivalence results

## 7. Stop conditions

Stop and reject if the change:
- Modifies existing Transcript service or API behavior
- Interprets raw PTY bytes as Timeline events
- Treats an empty ring as success or infers a restart marker without a persisted state source
- Treats duplicate EventID with different digest as idempotent (must FAIL)
- Unexplained difference in oracle output (must classify every difference)
