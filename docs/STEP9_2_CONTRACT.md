# STEP 9.2 — Transcript/Activity Projection Convergence Contract

**Status:** IMPLEMENTATION CONTRACT — PENDING IMPLEMENTATION

**Branch:** `feature/canonical-timeline-foundation`
**PREREQUISITE:** Step 9.1 ACCEPTED at `dc376f9b7`

## 1. Scope and authority boundary

Step 9.2 builds a read-only offline equivalence harness that compares Timeline-derived
Activity and Transcript projections against the existing authoritative Transcript
service. No production cutover — the existing Transcript service remains authoritative
throughout.

This step produces an **equivalence report**, not a migration. It proves that Timeline
shadow events can faithfully reconstruct the same semantic content the Transcript
service already provides. Differences are classified, not hidden.

**What is NEW:**
- `internal/projection/` — read-only package that derives Activity and Transcript
  projections from Timeline envelopes in the ring buffer
- `--enable-projection-convergence` — default-off CLI flag
- Offline equivalence comparator that compares projection output against existing
  Transcript service API responses

**What stays UNCHANGED:**
- Existing `internal/transcript/` service, API routes, and consumers
- `Transcript.Service` authority (sole writer of transcript data)
- `Recorder` (sole PTY reader)
- Cockpit (already reads ring buffer — no change needed)
- All REST/WS handlers, mobile code, device trust, pairing, approval

## 2. Projection definitions

### 2a. Activity projection

Derived from Timeline envelopes. A bounded, ordered view of provider-native events:

```
Timeline Envelope → Activity Item:
  kind:     envelope.EventKind (mapped to display label)
  state:    derived from event context (started/finished/resolved)
  summary:  envelope.RedactedPayload.Summary (redacted, safe for display)
  origin:   {provider, sessionId, runtimeId, generation}
  time:     envelope.OccurredAt
```

Activity items are read from the Timeline writer's ring buffer (`ReadRecent`).
Ordering is insertion order (monotonic by observation time, not guaranteed causal).

### 2b. Transcript projection

Derived from Timeline envelopes. A user-facing conversation transcript:

```
Timeline Envelope → Transcript Segment:
  role:      "assistant" (for tool calls, streams) | "system" (for lifecycle)
  content:   envelope.RedactedPayload.Summary (redacted)
  timestamp: envelope.OccurredAt
  source:    {provider, sessionId, generation}
```

This is NOT a replacement for the existing Transcript service. It is a
comparison artifact for the equivalence harness only.

## 3. Equivalence harness

The harness is a standalone Go test helper in `internal/projection/equivalence_test.go`
(or a separate test binary). It:

1. Creates a Timeline writer (in-memory, no filesystem)
2. Feeds the writer a known sequence of envelopes (replay of recorded events)
3. Derives Activity and Transcript projections from the ring buffer
4. Compares against the existing `Transcript.Service.ListTranscript()` API output
5. Produces a structured equivalence report

**Comparison dimensions:**
- **Generation reset:** after a new runtime generation, projections must reset
  (not carry forward stale events)
- **Reconnect:** after writer restart (empty ring buffer), projections are empty
- **Ordering:** insertion order preserved; no reordering
- **Duplicates:** duplicate EventIDs are suppressed (first-wins)
- **Missing events:** gaps are explicit (not filled with synthetic events)
- **Degradation:** when writer is degraded (drops), projection exposes gap markers
- **Request/result pairing:** approval-requested must precede approval-resolved
- **Approval relationships:** approval events must carry correct session/generation binding

## 4. Implementation files

**May create:**
- `internal/projection/activity.go` — Activity projection from ring buffer
- `internal/projection/transcript.go` — Transcript projection from ring buffer
- `internal/projection/equivalence_test.go` — offline equivalence harness
- `cmd/devremote/app.go` — add `--enable-projection-convergence` flag (wires nothing yet)

**Must NOT change:**
- Any `internal/transcript/` file
- Any `internal/term/` file
- Any `mobile/` file
- Existing REST/WS handlers

## 5. Fail-open guarantee

- Projections are read-only from the ring buffer
- If the Timeline writer is nil (disabled), projections return empty results
- If the ring buffer is empty, projections return empty (no synthetic events)
- Cockpit degradation endpoint already exposes drops; projections mirror that state
- No projection code may call `log.Fatal`, `panic`, or `os.Exit`

## 6. Acceptance tests

Before the implementation is accepted, automated tests must prove:

1. **Empty writer** → empty projections (both Activity and Transcript)
2. **Known sequence** → correct Activity items in insertion order
3. **Duplicates** → first-wins, second suppressed
4. **Ring buffer wrap** → oldest items dropped, most recent preserved
5. **Generation reset** → after reset, projections start fresh
6. **Degradation** → when writer has drops, projections expose gap markers
7. **Equivalence** → offline comparison against real Transcript API output matches
8. **Default-off** — without `--enable-projection-convergence`, zero projection code paths execute

## 7. Gate

- [ ] All existing tests pass (`go test -race ./...`, `npx tsc --noEmit`, `npx jest --runInBand`)
- [ ] New projection tests pass
- [ ] Equivalence report generated
- [ ] No production import regressions
- [ ] `--enable-projection-convergence` default-off, zero runtime effect when disabled
- [ ] Separate evidence commit records equivalence results

## 8. Stop conditions

Stop and reject if the change:
- Modifies existing Transcript service or API behavior
- Adds a new production route or mobile screen
- Introduces a production goroutine, channel, or blocking call in the read path
- Derives authority from projections (projections are read-only)
- Requires the writer to be enabled for daemon startup
- Changes the default value of any existing flag
