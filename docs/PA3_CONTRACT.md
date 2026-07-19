# PA3 — Mobile, Transcript, Activity, and Telemetry Authority Cutover Contract

Status: **PROPOSED CONTRACT — ADMISSION RESOLUTION (R7)**

This document freezes the PA3 ownership, scope, sequencing, gates, and
rollback contract before any production edit begins. It does not modify
production, mobile, or test code.

## 1. Baseline and authority

| Anchor | SHA | Role |
| --- | --- | --- |
| PA2d ACCEPT | `b93c521b7de45f3f577805dbb7de505e16f3172d` | current HEAD, authoritative baseline |
| PA2c ACCEPT | `958ce612f106db18515d5af4a1af14e8cefe8a1d` | lifecycle ownership baseline |
| PA2b ACCEPT | `2897a9e0943656883a885e75b08513982de507b7` | sessionid package baseline |
| PA2a ACCEPT | `34b5275bc6be51c6f8de386e311f76e9fc1725a0` | link removal baseline |
| PA1 ACCEPT | `b377268f5db464e7a885ff6a728b37cbe753a55c` | ManagedRuntimeCatalog baseline |
| Orca supervisor | `50c827cb134c9afd1b71980efd2a7c6a8b37774f` | standalone supervisor (unchanged) |

Canonical remote: `https://github.com/mhkim315/DevRemote.git`
Canonical branch: `feature/phase10-multi-adapter` (fast-forward only)

## 2. Current authority owners (pre-PA3)

This section inventories every production authority boundary that PA3
will change. It is based on inspection of the production code at the
PA2d accepted SHA (see `companion-daemon/docs/PA3_PLANNING_EVIDENCE.md`).

### 2.1 Session list / telemetry projection

| Authority | Owner (package/type) | Scope |
| --- | --- | --- |
| Session list DTO | `internal/term.SessionTelemetry` | `GET /api/sessions` JSON response |
| Telemetry state machine | `internal/term.TelemetryService` | background 2s sampling loop; owns screen parsing, log resolution, state inference (idle/thinking/working/waiting), agent detection bridge |
| Lifecycle merge | `internal/term.mergeLifecycleState` | merges OwnedPTYRuntime catalog rows into session list; appends retained terminal rows |
| Managed catalog merge | `internal/term.appendCatalogRows` | appends ManagedRuntimeCatalog (Codex/Claude) rows into session list |
| Agent-activity projection | `internal/term.AgentStatusStore` | S1 session-owned agent-activity store; `AgentActivityDTO` on `SessionTelemetry` |
| Approval listing | `internal/term.AuthoritativeApprovalStore.ListSafe` | `SafeApprovalDTO[]` on `SessionTelemetry` |
| Event listing | `internal/term.memoryEventStore.List` | legacy `events[]` field on `SessionTelemetry` |

### 2.2 Activity capture and read path

| Authority | Owner (package/type) | Scope |
| --- | --- | --- |
| Activity append | `internal/term.Recorder.readLoop` | sole PTY reader; appends `ActivityEvent` to `ActivityBuffer` |
| Activity storage | `internal/term.ActivityBuffer` | in-memory ring buffer (2000 capacity); merges adjacent `terminal_output` events; ANSI-strips text |
| Activity read API | `internal/term.Handlers.HandleSessionsV2` (`?activity=`) | `GET /api/sessions?activity=<id>` → `ActivityEvent[]` |
| Activity DTO | `internal/term.ActivityEvent` | `{id, seq, sessionId, type, text, bytes, hash, timestamp}` |

### 2.3 Transcript projection and read path

| Authority | Owner (package/type) | Scope |
| --- | --- | --- |
| Transcript store | `internal/transcript.Store` | in-memory per-session store; eviction by count + byte cap |
| Transcript service | `internal/transcript.Service` | owns store, byte-stream projector, agent-event projector, source arbiter |
| Byte-stream feed | `internal/term.Recorder.readLoop` → `Service.FeedBytes` | non-blocking enqueue via chunk queue worker |
| Agent-event feed | `internal/term.TelemetryService.processSession` → `Service.ProjectAgentEvents` | gated on `CorrelationManagedLaunch` |
| Snapshot feed | `internal/term.Recorder.readLoop` → `Service.AddSnapshotSegment` | cmux delta-only degraded path |
| Transcript API | `internal/transcript.HandleTranscript` | `GET /api/sessions/{id}/transcript` → `TranscriptResponse` |
| Transcript DTO | `internal/transcript.TranscriptSegment`, `TranscriptResponse` | versioned `t3.1`; semantic + fallback dual-channel envelope |

### 2.4 Legacy event store (pre-Transcript history)

| Authority | Owner (package/type) | Scope |
| --- | --- | --- |
| Event storage | `internal/term.memoryEventStore` (implements `EventStore`) | in-memory, max 500 events/session |
| Event append | `internal/term.TelemetryService.processSession` (legacy parser path) | `models.AgentEvent` from screen-parsed lines |
| Event read API | `internal/term.Handlers.HandleSessionsV2` (`?history=`) | `GET /api/sessions?history=<id>` → `AgentEvent[]` (with ScreenReader fallback) |
| Event field on list | `internal/term.SessionTelemetry.Events` | `events[]` field on every `GET /api/sessions` response |

### 2.5 Mobile consumption

| Authority | Owner (file) | Scope |
| --- | --- | --- |
| Session list | `mobile/src/lib/client.ts: listSessions()` | `GET /api/sessions` → `SessionTelemetry[]` |
| Activity read | `mobile/src/lib/client.ts: getActivityHistory()` | `GET /api/sessions?activity=` → `ActivityEvent[]` |
| History read | `mobile/src/lib/client.ts: getSessionHistory()` | `GET /api/sessions?history=` → legacy `AgentEvent[]` |
| Transcript read | `mobile/src/lib/client.ts: getTranscript()` | `GET /api/sessions/{id}/transcript` → `TranscriptResponse` (strictly validated) |
| Session rendering | `mobile/src/components/AgentCard.tsx` | consumes `SessionTelemetry` fields: state, lifecycleState, events, agentKind, agentStatus, agentConfidence, approvals, agentActivity |
| Activity rendering | `mobile/src/screens/FeedScreen.tsx` | Activity tab gated by `capabilities?.includes('history')` |
| Transcript rendering | `mobile/src/components/TranscriptRenderer.tsx` | consumes `ActivityEvent[]` or `OutputSpan[]` from classified events |
| Lifecycle actions | `mobile/src/lib/client.ts: stopSession/killSession/deleteSessionHistory` | managed lifecycle endpoints (unchanged by PA3) |

### 2.6 Push notification path

| Authority | Owner (package/type) | Scope |
| --- | --- | --- |
| Notifier interface | `internal/term.Notifier` | `ApprovalRequired(ctx, sessionID, message)` |
| Approval notification call | `internal/term.TelemetryService.processSession` | calls `Notifier.ApprovalRequired` when screen-parsed approval prompt detected |
| Push registration | `cmd/devremote/` `registerPush` | `POST /push/register` |

## 3. Target authority owners (post-PA3)

### 3.1 Transcript becomes the single authoritative read-path store

`internal/transcript.Service` becomes the **sole production store** for
session history content. After PA3:

- `ActivityBuffer` (`internal/term/activity.go`) is **deleted** (Step 6 —
  only after all consumers and producers are migrated in Steps 1–5).
- `EventStore` / `memoryEventStore` (`internal/term/eventstore.go`) is
  **deleted** (Step 6 — same constraint).
- `GET /api/sessions?activity=` is **removed** (404) (Step 3).
- `GET /api/sessions?history=` is **removed** (404) (Step 3).
- `SessionTelemetry.Events` field is **removed** (Step 4).
- Mobile moves to `GET /api/sessions/{id}/transcript` as the sole
  history/activity read path (Step 1, before any backend changes).

### 3.2 Telemetry simplifies to session listing + agent projection

`TelemetryService` retains:
- Session list snapshot assembly (canonical ID, adapter, capabilities, display fields)
- Lifecycle state merge from `OwnedPTYRuntime` + managed catalog
- Agent detection bridge (agentKind, agentConfidence)
- Agent-activity projection from `AgentStatusStore` (`AgentActivityDTO`)
- Approval listing from `AuthoritativeApprovalStore`
- Notifier field and approval notification hook (see §3.5)

`TelemetryService` drops:
- Legacy state machine (`idle`/`thinking`/`working`/`waiting` from screen parsing)
- Legacy log resolution and parser dispatch (the `processSession` parser path)
- Legacy `Events` field population
- `sessionStateData` (the cached per-session screen/log parsing state)
- `evaluateState`, `isApprovalPrompt`, `isThinkingFallback` screen heuristics

The `SessionTelemetry` DTO drops:
- `State` field (legacy idle/thinking/working/waiting — replaced by AgentActivity + AgentStatus)
- `Load` field (no longer computed)
- `Events` field (moved to Transcript)
- `Runner` / `RunnerColor` (replaced by AgentIdentity + profile data)

The `SessionTelemetry` DTO retains/gains:
- `ID`, `DisplayID`, `Adapter` (unchanged)
- `Capabilities`, `AdapterCapabilities` (unchanged)
- `LifecycleState` (unchanged — daemon-authoritative)
- `AgentKind`, `AgentStatus`, `AgentConfidence` (unchanged)
- `AgentActivity` (unchanged — advisory agent-activity projection)
- `Approvals` (unchanged — bounded safe approval DTOs)
- `Stale`, `LastSuccessAt`, `LastError` (unchanged — adapter health)

### 3.3 Recorder simplifies to PTY read + broadcast + Transcript feed

`Recorder` drops:
- `ActivityBuffer` append (the `r.activity.Append(...)` calls in `readLoop`)
- ANSI stripping and CR→LF transformation for activity (moves to Transcript projector)
- cmux sentinel detection for ActivityBuffer (delta/snapshot handling for Activity)

`Recorder` retains:
- PTY read loop (sole reader)
- Subscriber fan-out (live broadcast + ring-buffer bootstrap)
- Transcript `FeedBytes` (non-blocking byte-stream feed)
- TUI boundary detection for Transcript
- cmux sentinel detection for Transcript degraded path

### 3.4 Mobile unifies on Transcript read path

Mobile changes:
- `FeedScreen` Activity tab → Transcript tab (consumes `getTranscript()` instead of `getActivityHistory()`)
- `AgentCard` drops `state` field usage; uses `AgentActivity.status` + `AgentStatus` instead
- `AgentCard` drops `events` field rendering; Transcript is the separate read path
- `getActivityHistory()` function **deleted** from `client.ts`
- `getSessionHistory()` function **deleted** from `client.ts`
- `TranscriptRenderer` becomes the primary history/activity view (already exists, consumes classified events)
- `TranscriptResponse` generation field used to detect session reset (see §6.2)

**Explicitly retained** in `client.ts` (these functions are frozen, not deleted):
- `listSessions()`, `listSessionProfiles()`, `createSession()`, `stopSession()`, `killSession()`, `deleteSessionHistory()` — lifecycle + listing
- `getTranscript()` — Transcript read (unchanged; may gain generation-awareness in caller)
- `getManagedStatus()`, `getManagedEvents()`, `postManagedPrompt()` — managed sessions
- `terminalURL()`, `terminalWebSocketURL()`, `terminalWebSocketTicketURL()` — WebSocket
- `resolveApproval()` — approval action
- All `PokitError`, `ConnectivityFailure`, device-auth infrastructure

### 3.5 Approval notification path: concrete owner is TelemetryService

The legacy `Notifier.ApprovalRequired` call in `processSession` (triggered
by screen heuristics) is removed. The notification integration point moves
to the accepted adapter path inside `TelemetryService.processSession`:

**Concrete owner**: `TelemetryService` (not `AuthoritativeApprovalStore`,
which is frozen per A1/B6/B7).

**Mechanism**: After `ingestApprovals` successfully establishes new pending
approvals from the accepted adapter batch, `TelemetryService` checks whether
any ingested item is `actionable`. If so, it calls
`s.notifier.ApprovalRequired(ctx, sessionID, summary)` where `summary` is
the Pokit-owned bounded summary from `SafeApprovalDTO.Summary`.

**Production activation state (R2)**: `provenActionMapping` currently
returns `actionable=false` for ALL providers. No controlled-fixture-proven
action mapping exists for any provider today (B5). Therefore, the
notification hook point is **wired but dormant** — it will fire when a
future phase proves and accepts an actionable mapping. This is the
correct behavior: no false notifications are emitted from unproven
screen heuristics, and the integration point is already in the right
location for future activation.

**Managed Codex/Claude scope boundary**: The native managed Codex and
Claude services (`ManagedCodexService`, `ManagedClaudeService`) ingest
approvals through their own activation paths (`managed_approval_activation.go`,
`managed_claude_activation.go`) that call `AuthoritativeApprovalStore.Ingest`
directly — they do NOT route through `TelemetryService.ingestApprovals`.
Notification for native managed approvals is **out of PA3 scope**. The
managed activation, approval execution, and delivery paths are frozen
per A1/B6/B7/SP1/C1D. When a proven actionable mapping is accepted for
any managed provider, the notification hook for that path must be designed
in the acceptance contract for that mapping — not retrofitted into PA3.

**Why not ApprovalStore**: `AuthoritativeApprovalStore` is a frozen
generation-gated data structure, not an integration point. Adding a
notification side-effect to its `Append`/`Ingest` methods would violate
the A1 contract (which forbids modifying approval semantics). The
notification is an orthogonal concern owned by the integration layer
(`TelemetryService`), not the authority store.

## 4. Production data flow

### 4.1 Before PA3 (current state)

```
PTY
 ↓ (sole reader)
Recorder.readLoop
 ├─ raw bytes → bootstrap ring → live subscribers (WebSocket/IPC)
 ├─ ANSI-stripped text → ActivityBuffer.Append          ← DELETED in PA3
 ├─ raw bytes → transcript.Service.FeedBytes            ← RETAINED
 ├─ cmux delta → transcript.Service.AddSnapshotSegment  ← RETAINED
 └─ TUI detection → transcript.Service.Begin/EndTUIBurst ← RETAINED

TelemetryService.processSession (2s poll)
 ├─ log resolution → legacy parser → EventStore.Append  ← DELETED in PA3
 ├─ screen read → state machine (idle/thinking/...)      ← DELETED in PA3
 ├─ screen read → isApprovalPrompt → Notifier            ← DELETED in PA3
 ├─ accepted adapter → transcript.ProjectAgentEvents     ← RETAINED
 ├─ accepted adapter → AgentStatusStore.Update           ← RETAINED
 └─ accepted adapter → AuthoritativeApprovalStore        ← RETAINED

GET /api/sessions
 ├─ SessionTelemetry{ State, Load, Events, ... }         ← State/Load/Events DELETED
 ├─ SessionTelemetry{ LifecycleState, AgentKind, ... }   ← RETAINED
 └─ SessionTelemetry{ AgentActivity, Approvals }         ← RETAINED

GET /api/sessions?activity= → ActivityEvent[]            ← DELETED
GET /api/sessions?history=  → AgentEvent[]               ← DELETED
GET /api/sessions/{id}/transcript → TranscriptResponse   ← RETAINED (becomes primary)
```

### 4.2 After PA3 (target state)

```
PTY
 ↓ (sole reader — unchanged)
Recorder.readLoop
 ├─ raw bytes → bootstrap ring → live subscribers (WebSocket/IPC)
 ├─ raw bytes → transcript.Service.FeedBytes (byte-stream fallback)
 ├─ cmux delta → transcript.Service.AddSnapshotSegment (degraded)
 └─ TUI detection → transcript.Service.Begin/EndTUIBurst

TelemetryService (2s poll — simplified)
 ├─ accepted adapter → transcript.ProjectAgentEvents     (semantic primary)
 ├─ accepted adapter → AgentStatusStore.Update           (agent activity)
 ├─ accepted adapter → AuthoritativeApprovalStore        (approvals)
 └─ new pending actionable → Notifier.ApprovalRequired    (bounded summary)

GET /api/sessions
 ├─ SessionTelemetry{ ID, DisplayID, Adapter }
 ├─ SessionTelemetry{ Capabilities, AdapterCapabilities }
 ├─ SessionTelemetry{ LifecycleState }
 ├─ SessionTelemetry{ AgentKind, AgentStatus, AgentConfidence }
 ├─ SessionTelemetry{ AgentActivity }
 ├─ SessionTelemetry{ Approvals }
 └─ SessionTelemetry{ Stale, LastSuccessAt, LastError }

GET /api/sessions/{id}/transcript → TranscriptResponse   (sole history read path)
 ├─ generation    (int64 — monotonic; mobile detects reset when value changes)
 ├─ semantic[]    (AgentEvent-sourced when correlated)
 ├─ fallback[]    (byte-stream when AgentEvent primary; snapshot always)
 ├─ primarySource
 └─ contractVersion
```

## 5. API and DTO boundaries

### 5.1 Breaking changes

| Change | Current | Target | Impact |
| --- | --- | --- | --- |
| Remove `?activity=` query | `GET /api/sessions?activity=<id>` → `ActivityEvent[]` | 404 | Mobile must use Transcript API |
| Remove `?history=` query | `GET /api/sessions?history=<id>` → `AgentEvent[]` | 404 | Mobile must use Transcript API |
| Remove `State` field | `SessionTelemetry.State` ("idle"/"thinking"/"working"/"waiting") | absent | Mobile must use `AgentActivity.status` + `AgentStatus` |
| Remove `Load` field | `SessionTelemetry.Load` (int 0-100) | absent | Mobile drops spinner/load indicator |
| Remove `Events` field | `SessionTelemetry.Events` (`AgentEvent[]`) | absent | Mobile must use Transcript API for event history |
| Remove `Runner` / `RunnerColor` fields | `SessionTelemetry.Runner`, `RunnerColor` | absent | Replaced by `AgentKind` + `AgentIdentity.DisplayName` |
| Remove `state` type from mobile DTO | `SessionTelemetry.state: 'idle' \| 'thinking' \| 'working' \| 'waiting'` | removed | TypeScript DTO updated |

### 5.2 Additive changes

| Change | Field | Purpose |
| --- | --- | --- |
| Profile name on telemetry | `SessionTelemetry.ProfileID`, `SessionTelemetry.Name` (optional) | Display session profile and user-given name |
| Managed session marker | `SessionTelemetry.Managed` (bool, optional) | Distinguish managed (Codex/Claude) from PTY sessions |
| **TranscriptResponse generation** | `TranscriptResponse.Generation` (int64) | Monotonic generation counter; mobile detects reset when value changes between polls (see §6.2) |

### 5.3 Unchanged DTOs

| DTO | Status |
| --- | --- |
| `TranscriptSegment` (`t3.1`) | **frozen** — no change to segment shape |
| `AgentActivityDTO` (S1) | **frozen** — no change |
| `SafeApprovalDTO` (B6) | **frozen** — no change |
| `SessionLifecycle` DTO | **frozen** — no change |
| Lifecycle action endpoints (`/stop`, `/kill`, `DELETE /{id}`) | **frozen** — no change |
| `/api/session-profiles` | **frozen** — no change |
| WebSocket `/term/ws` protocol | **frozen** — no change |

The `TranscriptResponse` envelope gains one additive field (`generation`)
but is otherwise frozen. The `t3.1` contract version is retained; the
new field does not require a version bump because it is additive and
old clients safely ignore unknown JSON fields. Mobile clients that
validate strict field lists (like `validateTranscriptResponse`) must
be updated to accept the new field — this is part of Step 1.

## 6. Generation / session identity rules

### 6.1 Admission authority and generation-bound identity

PA3 aligns session replacement with production PA2d code patterns. Each
generation owns one immutable `GenerationCleanupCapability`. Before any
same-ID replacement installs a new adapter session, Recorder, or catalog
entry, shared admission authority atomically claims the old capability
exactly once. All cleanup I/O executes outside locks using captured
identities — never ID-addressed lookups. Completion uses `sync.Once`.

#### 6.1.1 GenerationCleanupCapability

```go
type GenerationCleanupCapability struct {
    Generation int64
    Session    mux.Session              // exact adapter session identity
    Recorder   *Recorder                // exact recorder instance
    Transport  *TerminalTransport       // exact transport handle
    Terminator mux.SessionIdentityTerminator // adapter that owns Session
    CanonicalID string                  // e.g. "controlled_pty:<localID>"
    LocalID    string                   // adapter-local session ID
    Completion *GenerationCompletion
}

type GenerationCompletion struct {
    once sync.Once
    done chan struct{}
}

func (c *GenerationCompletion) Complete() { c.once.Do(func() { close(c.done) }) }
func (c *GenerationCompletion) Done() <-chan struct{} { return c.done }
```

**Claim protocol** (under `o.mu`, microseconds, NO I/O):

1. Read current `o.entries[canonicalID]`
2. If `entry.Generation != derivedGen`: return `stale_generation`
3. Extract `entry.capability`, set `entry.capability = nil` (claimed at most once)
4. Release `o.mu`

**Cleanup execution** (outside ALL locks):

```go
func (cap *GenerationCleanupCapability) Execute(ctx context.Context) {
    defer cap.Completion.Complete() // ALWAYS — even on panic
    // All I/O outside locks, using captured fields. Nil-safe:
    // resources not yet created are skipped.
    if cap.Transport != nil {
        cap.Transport.RetireIfGeneration(cap.Generation)
    }
    if cap.Recorder != nil {
        DeleteRecorderIfSame(cap.CanonicalID, cap.Recorder)
    }
    if cap.Terminator != nil && cap.Session != nil {
        cap.Terminator.CompareAndTerminate(ctx, cap.LocalID, cap.Session)
    }
}
```

**Prohibited** in any post-capture cleanup path:

- `terminateAdapterSession(localID)` — ID-addressed, can hit replacement
- `SessionTerminator.TerminateSession(localID)` — ID-addressed
- `DeleteRecorder(canonicalID)` — ID-addressed, not instance-guarded
- `GetRecorder(canonicalID)` — ID-addressed lookup
- Registry or adapter lookup by canonical or local ID
- transport lookup by ID
- raw `close()` on any completion channel — use `Complete()` (sync.Once)
- function-wide `defer close(...)` combined with explicit rollback close

#### 6.1.2 Replacement admission — pre-install barrier

Before a replacement calls `CreateSession`, starts a Recorder, installs
an adapter session row, or publishes a catalog row:

1. **Claim old capability** (under `o.mu`, μs): extract old entry's
   `GenerationCleanupCapability`. Set `entry.State = LifecycleSuperseded`.
2. **Execute old cleanup** (no locks, all I/O outside): `cap.Execute(ctx)`
   which calls `CompareAndTerminate`, `DeleteRecorderIfSame`,
   `RetireIfGeneration`, and `Completion.Complete()`.
3. **Wait for completion**: `<-cap.Completion.Done()` — blocks until
   old cleanup finishes (even on error/panic, Complete runs).
4. **Proceed with replacement**: only now may `CreateSession` be called
   and a new Recorder started.

**Linearization**: The claim under `o.mu` (step 1) is the linearization
point. After claim, the old generation is superseded and inert. Its
cleanup cannot affect any replacement.

**Stale cleanup is inert**: `CompareAndTerminate` returns
`ErrStaleSessionIdentity` when the captured Session no longer matches the
adapter's current session at that local ID. `DeleteRecorderIfSame` is a
no-op when the captured `*Recorder` != the registry's current Recorder.
`RetireIfGeneration` only retires the transport if `gen == transport.gen`.

#### 6.1.3 Catalog state machine (7 states)

Absent, creating, running, stopping, retiring, terminal, superseded.

Replacement: `running → (claim capability under o.mu, μs) → superseded →
(execute cleanup: CompareAndTerminate + DeleteRecorderIfSame +
RetireIfGeneration, no locks) → Completion.Done() → (install replacement)`

#### 6.1.4 Linearization points (12 total)

| # | Point | Under | Decision |
| --- | --- | --- | --- |
| LP1 | Generation allocation | o.mu | `o.nextGen++` |
| LP2 | Catalog reservation | o.mu | Entry + capability created |
| LP3 | Old capability claimed | o.mu | Extracted before replacement CreateSession |
| LP4 | Transcript generation | Service lock | SetTranscriptGeneration / ReplaceTranscript |
| LP5 | Recorder binding | Recorder registry | StartRecorderUnconditional |
| LP6 | Publication | o.mu | LifecycleRunning; capability installed |
| LP7 | Stop claim | o.mu | Handle claimed; token released before I/O |
| LP8 | Kill claim | o.mu | Handle claimed; token released before I/O |
| LP9 | Terminal finalization | o.mu | Capability claimed at most once |
| LP10 | Cleanup execution | None | cap.Execute() — all I/O outside locks |
| LP11 | Transcript gen init | Service lock | gen=1 |
| LP12 | Transcript gen bump | Service lock | gen++ |

#### 6.1.5 Guarantees

**G1 — No conflicting identity**: Old capability claimed under `o.mu`
before replacement installs anything. Stale cleanup inert.

**G2 — Adapter identity isolation**: `CompareAndTerminate` uses captured
Session identity. Replacement's `CreateSession` runs AFTER old cleanup
completes.

**G3 — No stale lifecycle targeting**: Gen check under `o.mu` rejects
stale operations. Claimed capability is nil after extraction.

**G4 — Replacement cleanup via captured capability**: Old capability
executes `CompareAndTerminate` + `DeleteRecorderIfSame` +
`RetireIfGeneration`. All instance-guarded, never ID-addressed.
`Completion.Complete()` runs even on error/panic.

**G5 — Recorder closure ≠ lifecycle termination**: PTY close causes child
SIGHUP/exit. State machine captures as natural exit.

**G6 — No lock across I/O**: Claim under `o.mu` (μs). All cleanup I/O
outside locks.

**G7 — Failed creation cannot publish**: Rollback uses captured capability
(instance-guarded), never ID-addressed termination.

**G8 — Bounded completion signals**: `attemptCompletion.Complete()`
called exactly once via defer on every return path (success, error,
panic). `sync.Once` guarantees idempotency. Waiters always unblock.
`cap.Completion.Complete()` for cleanup completion. No raw `close()`.

#### 6.1.6 Creation flow (aligned to PA2d production)

**Pre-install** (o.mu, μs):
```go
o.mu.Lock()
existing := o.entries[canonicalID]
// If an in-progress create exists, block on its attemptCompletion.
for existing != nil && existing.State == LifecycleCreating {
    wait := existing.attemptCompletion
    o.mu.Unlock()
    <-wait.Done() // wait for prior create to finish (publish or rollback)
    o.mu.Lock()
    existing = o.entries[canonicalID]
}
var oldCap *GenerationCleanupCapability
if existing != nil {
    oldCap = existing.capability
    existing.capability = nil
    existing.State = LifecycleSuperseded
}
o.nextGen++
lifecycleGen := o.nextGen
attemptCompletion := &GenerationCompletion{done: make(chan struct{})}
completion := &GenerationCompletion{done: make(chan struct{})}
o.entries[canonicalID] = &CatalogEntry{
    ID: canonicalID, State: LifecycleCreating, Generation: lifecycleGen,
    attemptCompletion: attemptCompletion,
    // capability filled after Recorder start
}
o.mu.Unlock()

if oldCap != nil {
    oldCap.Execute(ctx)
    <-oldCap.Completion.Done()
}

// On ANY return path (success, error, panic): Complete() exactly once.
// sync.Once guarantees idempotency — second call is no-op.
defer func() {
    if retErr != nil {
        o.mu.Lock()
        entry := o.entries[canonicalID]
        if entry != nil && entry.Generation == lifecycleGen {
            delete(o.entries, canonicalID)
        }
        o.mu.Unlock()
    }
    attemptCompletion.Complete() // wakes waiters; idempotent
}()
```

**Install** (no locks):
```go
// PA3 implementation requirement: the controlled_pty adapter's
// CreateSession MUST return the created (localID, Session, error)
// atomically so the capability captures Session immediately.
// This requires adding a CreateSessionAndCapture method or
// equivalent to the adapter interface.
// creatorWithIdentity is a typed field on OwnedPTYRuntime,
// populated at construction: creatorWithIdentity, ok := spawn.(SessionCreatorWithIdentity); if !ok { return nil, fmt.Errorf("spawn adapter does not implement SessionCreatorWithIdentity") }
createdID, sess, err := o.creatorWithIdentity.CreateSessionAndCapture(ctx, opts)
if err != nil { completion.Complete(); return "", err }
canonicalID := build(adapter, createdID)
ref := sessionid.ParseSessionID(canonicalID)

// Construct capability IMMEDIATELY after CreateSession.
// Session is captured atomically — NOT obtained via fallible ListSessions.
cap := &GenerationCleanupCapability{
    Generation: lifecycleGen,
    Terminator: o.spawn.(mux.SessionIdentityTerminator),
    CanonicalID: canonicalID,
    LocalID:    ref.LocalID,
    Session:    sess,
    Completion: completion,
}
defer func() { if retErr != nil { cap.Execute(ctx) } }()

// Start Recorder unconditionally.
stream, err := sess.(mux.StreamOpener).OpenStream(ctx)
rec := StartRecorderUnconditional(canonicalID, stream, activity)
cap.Recorder = rec

// Create transport from session (session implements io.Writer + Resize).
transport := newTerminalTransport(canonicalID, lifecycleGen, sess, sess)
cap.Transport = transport

// Init Transcript generation.
o.transcript.SetTranscriptGeneration(canonicalID, 1)
```

**Publish** (o.mu, μs):
```go
o.mu.Lock()
entry := o.entries[canonicalID]
if entry == nil || entry.Generation != lifecycleGen {
    // Superseded: Complete() wakes waiters; do NOT mutate replacement.
    o.mu.Unlock()
    cap.Execute(ctx) // nil-safe cleanup of our resources
    return "", fmt.Errorf("session replaced")
}
entry.State = LifecycleRunning
entry.capability = cap
entry.handle = handle
entry.transport = transport
entry.StartedAt = time.Now()
o.mu.Unlock()
// defer in pre-install calls attemptCompletion.Complete() on return.
// On success, retErr is nil so defer does NOT delete the entry,
// but it DOES call Complete() — waking all waiters.
```

**Rollback** (capability already constructed, fields may be nil):
```go
// cap was constructed immediately after CreateSessionAndCapture.
// nil-safe Execute: all steps guard nil fields.
// Completion.Complete() ALWAYS runs (defer in Execute).
cap.Execute(ctx)
// attemptCompletion.Complete() is called by the defer in pre-install.
// sync.Once guarantees idempotency — publish path also calls it safely.
// NO: terminateAdapterSession(localID), DeleteRecorder(id), TerminateSession(id)
```

#### 6.1.7 Non-OwnedPTYRuntime sessions

Lazy generation init. No replacement path.

#### 6.1.8 New/modified method contracts

**New types**:

```go
// GenerationCompletion — sync.Once completion signal
type GenerationCompletion struct { once sync.Once; done chan struct{} }
func (c *GenerationCompletion) Complete()
func (c *GenerationCompletion) Done() <-chan struct{}

// GenerationCleanupCapability — immutable generation-bound cleanup.
// Constructed immediately after CreateSessionAndCapture.
type GenerationCleanupCapability struct {
    Generation int64
    Session    mux.Session
    Recorder   *Recorder
    Transport  *TerminalTransport
    Terminator mux.SessionIdentityTerminator
    CanonicalID string
    LocalID    string
    Completion *GenerationCompletion
}
func (cap *GenerationCleanupCapability) Execute(ctx context.Context)
```

**New adapter interface** (PA3 addition to `internal/mux/adapter.go`):

```go
// SessionCreatorWithIdentity creates an adapter session and returns
// the created (localID, Session, error) atomically. The returned
// Session MUST be the exact adapter session identity — callers must
// not re-resolve it via ListSessions.
// Implemented by: controlledPTYAdapter.
type SessionCreatorWithIdentity interface {
    CreateSessionAndCapture(ctx context.Context, opts CreateOptions) (localID string, session Session, err error)
}
```

**New/modified functions**:

```go
// RetireIfGeneration — retire only if gen matches (new on TerminalTransport)
func (t *TerminalTransport) RetireIfGeneration(gen int64)

// StartRecorderUnconditional — always new Recorder (new on recorder.go)
func StartRecorderUnconditional(sessionID string, stream ptyStream, activity *ActivityBuffer) *Recorder

// Existing, unchanged:
func DeleteRecorderIfSame(sessionID string, rec *Recorder) // instance-guarded
func (s *Service) ReplaceTranscript(sessionID string) int64
func (s *Service) SetTranscriptGeneration(sessionID string, gen int64)
```

#### 6.1.9 Focused acceptance tests

1. **Pre-install barrier**: Old capability claimed before replacement
   CreateSession. Replacement blocks on `<-oldCap.Completion.Done()`.

2. **Cleanup-wins**: `CompareAndTerminate` removes only captured Session.
   Stale Session returns `ErrStaleSessionIdentity`.

3. **Recorder ABA**: `DeleteRecorderIfSame(oldRec)` cannot remove `recNew`.

4. **Transport ABA**: `RetireIfGeneration(oldGen)` retires only oldTransport.

5. **GenerationCompletion**: success, rollback, cancellation,
   supersession, Stop, Kill, natural-exit — all call `Complete()`.
   `sync.Once` guarantees single close. Zero panic. Every waiter wakes.

6. **Cleanup panic/error**: `Complete()` still runs (defer in Execute).

7. **Rollback**: `CreateSession` success then handle/Recorder-start
   failure → captured conditional cleanup, zero ID-addressed termination.

8. **Stop/Kill barrier**: Captured process + captured Recorder awaited,
   not `GetRecorder(id)`.

9. **Blocking I/O**: Admission and lifecycle locks FREE during
   `CompareAndTerminate`, Recorder stop, transport retirement,
   completion wait.

10. **Static rejection**: grep confirms no `terminateAdapterSession`,
    `TerminateSession`, `DeleteRecorder` (by ID), `GetRecorder`,
    Registry/recorder/transport lookup in post-capture cleanup paths.

### 6.2 Does mobile need generation awareness?

**Yes — minimally, for Transcript reset detection.** Mobile needs to
detect when the Transcript has been cleared due to session replacement.

**Mechanism**: `TranscriptResponse` gains a new additive field:

```json
{
  "sessionId": "...",
  "generation": 3,
  "semantic": [...],
  ...
}
```

`generation` is an `int64` that is the current Transcript generation of the
session. It is sourced from the `transcript.Service` per-session generation
counter — NOT from `LookupLaunch` (which is managed-launch-only and returns
0 for non-managed sessions). The counter is incremented on every
`ReplaceTranscript` call, which happens at session replacement. For a
first-time session creation, the generation is set to 1 via
`SetTranscriptGeneration(sessionID, 1)` after the Recorder is started.

**Mobile behavior**:
1. On each Transcript poll, mobile records `lastSeenGeneration` per session.
2. If the new response's `generation` != `lastSeenGeneration` (and
   `lastSeenGeneration` != 0, i.e. not the first poll), the Transcript
   has been reset — discard all cached segments and re-render from scratch.
3. If `generation` == `lastSeenGeneration`, apply incremental diff using
   `?after=<lastKnownSeq>`.

Mobile never constructs or asserts a generation value — it only observes
changes. A generation of 0 (no launch binding) means the session has no
generation-tracking; mobile treats each poll independently (no incremental
optimization, full re-render is safe).

**Why generation instead of a reset boolean**: A monotonic counter survives
message reordering. A boolean `reset: true` could be missed if the mobile
polls between reset and first content, or if a stale cached response with
`reset: false` arrives after the real reset. A counter increments on every
replacement so mobile detects the change regardless of timing.

## 7. Reconnect and stale-event behavior

### 7.1 Mobile reconnect data path

On reconnect (app foreground, network restore, WS reconnect):

1. Mobile calls `GET /api/sessions` → gets current session list with
   lifecycle states, agent activity, approvals.
2. For the active session, mobile calls `GET /api/sessions/{id}/transcript`
   (or with `?after=<lastKnownSeq>` for incremental poll).
3. Mobile compares `response.generation` to `lastSeenGeneration`:
   - Same generation → safe incremental diff.
   - Different/non-zero generation → discard cache, full re-render.
4. Mobile reconnects WebSocket via ticket for live terminal bytes.

The daemon guarantees:
- Transcript segments are monotonic per-session (`Seq` field).
- A cleared Transcript (generation replacement) starts from `Seq=0` and
  the `generation` field in `TranscriptResponse` increments.
- Mobile detects the generation change and re-renders from scratch.
- `TranscriptResponse.primarySource` tells mobile whether the content
  is agent-event-sourced or byte-stream fallback.

### 7.2 Stale-event rejection

Stale events from replaced generations are rejected at multiple layers:

| Layer | Mechanism | Behavior |
| --- | --- | --- |
| Transcript store | `Service.ReplaceTranscript(sessionID)` before new Recorder starts (old capability already executed) | Old segments cleared; old generation's Transcript retired |
| Agent-activity store | `LaunchGen` gate in `AgentStatusStore.Update` | Writes with `LaunchGen < current` rejected |
| Approval store | `SupersedeRuntime` on replacement | Prior pending approvals invalidated |
| Recorder | `cap.Execute()` → `DeleteRecorderIfSame(cap.CanonicalID, cap.Recorder)` — instance-guarded via capability | Stops ONLY captured Recorder; stale is no-op |
| TerminalTransport | `cap.Execute()` → `RetireIfGeneration(cap.Generation)` — instance-guarded via capability | Retires ONLY captured transport at captured gen |
| TranscriptResponse | `generation` field change | Mobile detects reset and discards stale cache |

Mobile cannot receive stale events because the old capability is claimed
and executed (CompareAndTerminate + DeleteRecorderIfSame +
RetireIfGeneration) BEFORE the replacement CreateSession and Recorder
start. Completion.Done() is waited before any new data enters Transcript.

## 8. Ordering and exactly-once guarantees

### 8.1 Transcript ordering

- `TranscriptSegment.Seq` is a monotonic per-session integer assigned at
  append time under the `Store.mu` lock. Within a generation, ordering is
  strictly monotonic.
- Across generations: a Clear resets Seq to 0. The `generation` field on
  `TranscriptResponse` disambiguates which generation the segments belong to.
  There is no cross-generation ordering — the old generation's segments are
  deleted.
- Source arbitration (AgentEvent vs byte-stream) is enforced at append time:
  AgentEvent-sourced segments and byte-stream segments are never merged,
  deduplicated, or correlated by content.

### 8.2 Activity ordering (legacy — deleted)

The legacy `ActivityBuffer.Seq` is deleted with the ActivityBuffer. No
replacement is needed because Transcript provides the authoritative
ordering.

### 8.3 Telemetry consistency

- `SessionTelemetry` snapshot is read under `TelemetryService.mu` for
  per-session state copies + `Registry.Sessions()` for live sessions.
- The snapshot is always the latest poll cycle's data. No partial update:
  all fields for a session come from the same poll cycle.
- LifecycleState merge (`mergeLifecycleState`) reads from the
  `OwnedPTYRuntime` catalog under its own lock and annotates the
  already-snapshotted telemetry rows.
- Managed catalog append (`appendCatalogRows`) reads from
  `ManagedRuntimeCatalog` and adds non-duplicate rows.

### 8.4 Exactly-once Transcript append

Transcript exactly-once is achieved through **two complementary mechanisms**
because the two feed paths have different idempotency properties:

**Byte-stream path (Recorder → FeedBytes)**:

Each PTY chunk is a unique, irreproducible byte sequence. The chunk queue
has a single ordered worker. If the worker processes a chunk, it is
projected exactly once (the queue does not retry). If the queue overflows
before the worker processes a chunk, a single coalesced `projection_gap`
degraded marker is emitted — the lost chunks are explicitly marked, not
silently dropped. The same byte range is never re-read from the PTY
(because PTY output is a stream, not a seekable file).

**Agent-event path (accepted adapter → ProjectAgentEvents)**:

Idempotency is **cursor-based**, not event-ID based. The accepted adapter
(`contract.AgentAdapter.ReadEvents`) receives an opaque cursor from the
previous poll and returns a new cursor. The Transcript projection layer
does NOT track individual event IDs for dedup. Instead:

1. `TelemetryService.processSession` passes the cursor through to
   `callAcceptedAdapter`.
2. The accepted adapter's `ReadEvents` contract guarantees that the same
   cursor range produces the same events (cursor-based idempotency).
3. `Service.ProjectAgentEvents` is called with the full batch from one
   `ReadEvents` call. If the same cursor range is replayed (e.g. daemon
   restart, poll retry), the same events are projected again.

**Duplicate tolerance at the store layer**: `Store.Append` generates segment
IDs from `SHA256(sessionID + kind + source + text + agentEventRef + seq)`.
If the same agent event is projected twice (same cursor replayed), it
produces two distinct segments with different `Seq` values but the same
`AgentEventRef` field.

**Mobile rendering behavior (pre-PA3)**: The mobile `TranscriptRenderer`
(classifier in `transcriptClassify.ts`) classifies segments by `kind`/
`source`/`eventType` without deduplicating by `AgentEventRef`. Re-projected
duplicate agent events render as separate sequential segments — the user
sees the same content twice. This is the current production behavior; PA3
does not change it.

**PA3 Step 1 adds client-side dedup**: During the mobile cutover step,
`transcriptClassify.ts` gains a `Set<string>` of recently-seen
`AgentEventRef` values (bounded to 128 entries, per-generation, cleared on
generation change). Adjacent same-`AgentEventRef` spans are collapsed to
the first occurrence. Non-adjacent duplicates (interleaved with other
events) are rendered as-is — interleaved duplicates indicate reordered
delivery, not replay, and collapsing them would hide intervening content.
This is not a correctness guarantee; it is a best-effort display optimization
for the common cursor-replay case.

**What is explicitly NOT guaranteed**: The Transcript store does NOT
guarantee that a re-projected agent event from a replayed cursor range
produces zero additional segments. It guarantees that the content is
semantically identical (same `AgentEventRef`, same `Text`, same `Kind`,
same `AgentKind`). Consumers that need strict dedup should key on
`AgentEventRef` + `EventType` within a single generation.

## 9. Privacy and secret-handling

### 9.1 What Transcript guarantees

Transcript provides these **structural** guarantees:

- **ANSI escape sequences are stripped** by the `ByteStreamProjector`,
  which maintains ANSI parser state across chunks and commits only
  plain-text lines.
- **Carriage-return repaints are handled**: CR without LF replaces the
  current pending line rather than being stored as content.
- **Alternate-screen / TUI burst regions** are collapsed to a single
  `KindUIOmitted` marker with no textual content.
- **Input echo regions** are replaced with a content-free
  `KindInputBoundary` marker (after `BeginInput` is called). The marker
  carries no typed command content, prompt text, or timing data.
- **All segment `Text` fields are bounded** to `MaxTextBytes` (32768 bytes)
  with a truncation marker.
- **Degraded marker reasons are bounded** to `MaxDegradedReasonBytes`
  (256 bytes).
- **Agent-event Text is allowlist-projected**: only specific fields
  (assistant message text, tool name) are projected; prompts, thinking,
  and tool arguments are excluded by the `AgentEventProjector`.

### 9.2 What Transcript explicitly does NOT guarantee

Transcript cannot and does not guarantee that arbitrary PTY output is
free of sensitive content:

- **ANSI stripping is a lossy text transform, not a security boundary.**
  Plain-text terminal output can and does contain commands, file paths,
  secret values printed by tools, environment variables echoed by shells,
  API responses containing tokens, and git diffs with source code.
- **`boundedText` truncation is a storage bound, not a redaction
  mechanism.** Truncated text may still contain sensitive prefixes.
- **The `AgentEventProjector` allowlist blocks known event fields.**
  Unknown or future event types may carry fields not recognized by the
  allowlist. The projector fails safe (emits unknown or degraded) but
  this is a design-time guarantee, not a runtime content scanner.
- **No runtime secret scanning or pattern matching is performed.**
  Transcript does not regex-scan for `sk-*`, `ghp_*`, bearer tokens,
  email addresses, or IP addresses in byte-stream output.

### 9.3 Design rationale for the gap

The PTY is a raw byte stream. Full content-aware secret redaction from
a byte stream is a hard AI/ML problem (it requires understanding which
substrings are secrets vs. benign lookalikes, handling ANSI-wrapped
secrets, and dealing with tool-specific output formats). PA3 does not
attempt to solve this.

Instead, PA3 relies on structural protections that work at the
projection layer (ANSI stripping, TUI omission, input suppression,
field allowlists) and acknowledges that byte-stream Transcript output
from a PTY that prints secrets will contain those secrets. This is the
same posture as the current `ActivityBuffer` (which also only strips
ANSI) and the pre-PA3 Transcript byte-stream projector.

### 9.4 What must NOT appear in Transcript segments (by design)

The following structural rules are enforced at the projection layer:

- Raw terminal input text (suppressed via `BeginInput`/`EndInput` markers)
- Raw JSONL log content (never fed to Transcript; only parsed agent events)
- Raw agent prompts or thinking content (excluded by `AgentEventProjector`)
- Tool call arguments (excluded by `AgentEventProjector`)

### 9.5 What must NOT appear in AgentActivity

`AgentActivityDTO` carries only `{contractVersion, status, provenance,
confidence, degraded, observedAt, stale}`. It must never carry:

- Raw errors or error messages
- Evidence, adapter records, or internal state
- Prompts, paths, or private metadata

### 9.6 What must NOT appear in SessionTelemetry

`SessionTelemetry` is the public session list. It must never carry:

- Raw terminal screen content (the `LastOutput` field is internal-only, never serialized)
- Log file paths
- Process PIDs, command lines, or CWDs
- Device tokens, pairing data, or raw approval prompts

### 9.7 Approval notification safety

Approval notifications (push) carry only:
- Session display name
- Agent kind
- Bounded summary (`SafeApprovalDTO.Summary` — Pokit-owned, never the raw provider prompt)
- Non-sensitive action hint

They must never carry:
- Raw command, tool name, prompt text, or file paths
- Approval option payloads

### 9.8 Existing redaction gates (unchanged)

PA3 does not modify the existing redaction infrastructure:

- `internal/agent/testdata/` redacted fixtures (A1 inventory)
- `SafeApprovalDTO` bounded summary (B6)
- `TranscriptSegment` bounded text (`MaxTextBytes` = 32768)
- `boundedString` / `boundedText` helpers in transcript package
- `stripANSI` for terminal output cleaning

## 10. Migration sequence

**Ordering principle**: All consumers and producers of a store must be
migrated away BEFORE the store is physically deleted. No store deletion
step may precede the step that removes the last producer/consumer.

### Step 1: Mobile cutover to Transcript read path (no backend changes)

Mobile consumers are migrated first — the furthest-downstream layer.

- Mobile `FeedScreen` and `TranscriptRenderer` consume `getTranscript()`
  instead of `getActivityHistory()` / `getSessionHistory()`.
- Mobile `AgentCard` drops `state` field; uses `AgentActivity.status` +
  `AgentStatus`.
- Mobile `AgentCard` drops `events` field rendering.
- Mobile `TranscriptRenderer` updated to accept `TranscriptResponse` and
  handle the `generation` field for reset detection.
- Functions `getActivityHistory()` and `getSessionHistory()` left in
  `client.ts` but marked `@deprecated`; no production callers remain.
- Mobile `validateTranscriptResponse` updated to accept the new additive
  `generation` field (unknown field rejection relaxed for this key).
- Mobile `transcriptClassify.ts` gains bounded `Set<string>` dedup of
  recently-seen `AgentEventRef` values (128 entries, cleared on generation
  change). Adjacent same-`AgentEventRef` spans collapse to first occurrence
  (best-effort display optimisation for cursor replay). Non-adjacent
  duplicates render as-is.
- Verify: mobile `tsc --noEmit` passes; emulator renders Transcript.

**Gate**: Mobile compiles and renders Transcript without Activity/History
APIs. Backend unchanged — rollback is instant (revert mobile commit).

### Step 2: Simplify TelemetryService (remove legacy producers)

Producers of legacy stores are removed before the stores.

- Remove `sessionStateData` struct and all associated fields
  (`LastOutput`, `LastActivity`, `State`, `Load`, `Cursor`, `Parser`,
  `SamplingFailures`). Retain `Adapter *adapterState` field by moving
  it to a simpler location (a separate `map[string]*adapterState` on
  `TelemetryService`).
- Remove `evaluateState`, `isApprovalPrompt`, `isThinkingFallback`
  screen heuristics.
- Remove legacy parser dispatch from `processSession` (the
  `parser.Parse(line)` loop and EventStore append).
- Remove screen read path from `processSession`
  (`ReadScreen`, `diffSize`, `tailLines`).
- Remove log resolution from `processSession` (the `ResolveAgentLog`
  call, `LogRef`, `LogCursor`).
- Simplify `Snapshot()`: remove `sessionStateData` copy,
  `events` field population, `State`/`Load`/`Runner`/`RunnerColor`
  population. Keep lifecycle merge, agent detection, approval listing,
  agent-activity projection.
- **Retain the accepted adapter path intact**: `callAcceptedAdapter`,
  `acceptedAdapterFor`, `isAcceptedAdapter`, `readRawLines`, and all
  Transcript/Status/Approval feed logic.
- Wire new approval notification: after `ingestApprovals`, check for
  newly pending actionable approvals and call `s.notifier.ApprovalRequired`.

**Gate**: `go build ./...` passes. `go test -race ./internal/term -count=1` passes.

### Step 3: Remove legacy API endpoints (remove HTTP consumers)

- Remove `?activity=` branch from `HandleSessionsV2`.
- Remove `?history=` branch from `HandleSessionsV2`.
- Verify both return 404/400 for the removed query parameters.

**Gate**: HTTP route tests confirm removed endpoints return appropriate
errors. At this point, no HTTP consumer of ActivityBuffer or EventStore
exists.

### Step 4: Update SessionTelemetry DTO (remove fields from API surface)

- Remove `State`, `Load`, `Runner`, `RunnerColor`, `Events` from
  `SessionTelemetry` struct.
- Update `mergeLifecycleState` retained-row construction (remove
  `State: "idle"`, `Runner`, `RunnerColor`; keep `LifecycleState`,
  `Capabilities: ["history"]`).
- Update `HandleSessionsV2` fallback path (`buildSimpleSnapshot`
  and `buildSimpleSnapshotWithDetector`).
- Update golden test fixtures to reflect new DTO shape.

**Gate**: `go build ./...`, `go vet ./...`, `go test -race ./internal/term
./cmd/devremote -count=1` pass.

### Step 5: Mobile DTO update (sync TypeScript types)

- Update `SessionTelemetry` TypeScript interface: remove `state`,
  `load`, `runner`, `runnerColor`, `events`. Add `profileId?`, `name?`.
- Remove `getActivityHistory()` and `getSessionHistory()` from `client.ts`
  (now that no mobile caller references them and backend endpoints are gone).
- Verify `TranscriptRenderer` handles `TranscriptResponse` (including
  `generation` field) correctly.

**Gate**: `npx tsc --noEmit` passes.

### Step 6: Remove ActivityBuffer and EventStore (physical deletion)

**Only after all producers (Step 2) and consumers (Steps 1, 3, 5) are
migrated.**

- Delete `internal/term/activity.go` (ActivityBuffer, ActivityEvent type,
  ActivityType constants).
- Delete `internal/term/eventstore.go` (EventStore interface,
  memoryEventStore).
- Delete related test files: `activity_test.go`, `eventstore_test.go`.
- Remove `ActivityBuffer` parameter from `NewTelemetryService`,
  `StartRecorder`, `EnsureRecorder`, `NewLifecycleService`,
  `NewOwnedPTYRuntime`.
- Remove `Activity` field from `Handlers` struct.
- Remove `activity` field from `App` struct.
- Remove `Events` field from `Handlers` struct.
- Remove `EventStore` wiring from `App.NewAppWithDeps`.
- Remove legacy parser/resolver/tracker files (see deletion gates §11.1).
- **Add `SessionCreatorWithIdentity` to `internal/mux/adapter.go`**:
  new interface with `CreateSessionAndCapture(ctx, opts) (string, Session, error)`.
  Implement on `controlledPTYAdapter` per §6.1.8.
- **Add `GenerationCleanupCapability` + `GenerationCompletion`**:
  new types per §6.1.1. `cap.Execute()` nil-safe, instance-guarded cleanup.
- **Add `RetireIfGeneration` on `TerminalTransport`** per §6.1.8.
- **Add `StartRecorderUnconditional` on `recorder.go`** per §6.1.8.
- **Add `ReplaceTranscript` method on `transcript.Service`**: drains old
  chunk queue, clears store + projectors + arbiters, re-enables queue,
  increments per-session generation, returns new generation. Does NOT call
  `RemoveLaunch`. PRECONDITION: no concurrent feeder (caller must stop old
  Recorder first).
- **Add `SetTranscriptGeneration` method on `transcript.Service`**: records
  the initial generation for a newly created session. Idempotent
  (first-write-wins). Safe to call after Recorder start.
- **Implement GenerationCleanupCapability + GenerationCompletion**:
  Add `GenerationCleanupCapability` struct with all captured fields per
  §6.1.1. Add `GenerationCompletion` with `sync.Once` + `Complete()`.
  Add `RetireIfGeneration` to `TerminalTransport`. Add
  `StartRecorderUnconditional` to `recorder.go`. Restructure
  `OwnedPTYRuntime.Create` with pre-install barrier (§6.1.2):
  claim old capability → Execute → wait Completion.Done() → then
  CreateSession. Construct capability immediately after CreateSession,
  before any fallible step. All rollback paths use cap.Execute()
  (nil-safe, instance-guarded). Zero ID-addressed termination.
- **Add `generation` field to `TranscriptResponse`** envelope and
  populate it from `Service` per-session generation counter (not from
  `LookupLaunch`). For sessions without explicit initialisation (tmux,
  cmux, localpty), first feed lazily initialises generation to 1.
- **Remove `ActivityBuffer.Append` calls from `Recorder.readLoop`**.
  Retain Transcript `FeedBytes`, TUI detection, and cmux sentinel
  detection for Transcript.

**Gate**: `go build ./...` passes. No production code references
`ActivityBuffer`, `ActivityEvent`, `EventStore`, `memoryEventStore`,
`ResolveAgentLog`, legacy parser types.

### Step 7: Final cleanup and static verification

- Remove `models.AgentEvent` type if no production consumers remain
  (check managed Codex/Claude paths — they use `agent.AgentEvent` from
  the accepted contract, not `models.AgentEvent`).
- Remove `EventStore` interface if no consumers remain.
- Remove `CommandBroker` if unused after legacy parser removal
  (verify — `commandbroker.go` may be needed for debug endpoints).
- Static verification: zero production references to deleted types.

**Gate**: All static and dynamic gates pass (see §12).

## 11. Deletion gates

### 11.1 Files deleted

| File | Step | Reason |
| --- | --- | --- |
| `internal/term/activity.go` | Step 6 | Replaced by Transcript byte-stream projection |
| `internal/term/activity_test.go` | Step 6 | No production code remains |
| `internal/term/eventstore.go` | Step 6 | Replaced by Transcript store |
| `internal/term/eventstore_test.go` | Step 6 | No production code remains |
| `internal/term/tracker.go` | Step 6 | Legacy log resolution — callers removed in Step 2 |
| `internal/term/resolver.go` | Step 6 | AgentLogResolver interface — callers removed in Step 2 |
| `internal/term/parser.go` | Step 6 | Legacy AgentLogParser interface — callers removed in Step 2 |
| `internal/term/gemini_resolver.go` | Step 6 | GeminiResolver — callers removed in Step 2 |
| `internal/term/gemini_parser.go` | Step 6 | GeminiParser — callers removed in Step 2 |
| `internal/term/claude_parser.go` | Step 6 | ClaudeParser legacy — callers removed in Step 2 |
| `internal/term/codex_parser.go` | Step 6 | CodexParser legacy — callers removed in Step 2 |
| `internal/term/antigravity_parser.go` | Step 6 | AntigravityParser legacy — callers removed in Step 2 |
| `internal/term/claude_resolver.go` | Step 6 | ClaudeResolver legacy — callers removed in Step 2 |
| `internal/term/codex_resolver.go` | Step 6 | CodexResolver legacy — callers removed in Step 2 |

### 11.2 API endpoints removed

| Endpoint | Step | Reason |
| --- | --- | --- |
| `GET /api/sessions?activity=<id>` | Step 3 | Replaced by `GET /api/sessions/{id}/transcript` |
| `GET /api/sessions?history=<id>` | Step 3 | Replaced by `GET /api/sessions/{id}/transcript` |

### 11.3 DTO fields removed

| DTO | Field | Reason |
| --- | --- | --- |
| `SessionTelemetry` | `state` | Replaced by `AgentActivity.status` + `AgentStatus` |
| `SessionTelemetry` | `load` | No longer computed |
| `SessionTelemetry` | `runner` | Replaced by `AgentKind` |
| `SessionTelemetry` | `runnerColor` | No longer needed |
| `SessionTelemetry` | `events` | Moved to Transcript API |

### 11.4 Struct fields removed

| Struct | Field | Reason |
| --- | --- | --- |
| `Handlers` | `Activity *ActivityBuffer` | ActivityBuffer deleted |
| `Handlers` | `Events EventStore` | EventStore deleted |
| `App` | `activity *ActivityBuffer` | ActivityBuffer deleted |
| `TelemetryService` | `activity *ActivityBuffer` | ActivityBuffer deleted |
| `TelemetryService` | `logResolver` | Legacy log resolution deleted |
| `TelemetryService` | `sessions map[string]*sessionStateData` | Legacy state machine deleted |
| `Recorder` | `activity *ActivityBuffer` | No longer appends to ActivityBuffer |
| `Recorder` | `captureMode string` | Only used for ActivityBuffer append |

### 11.5 DTO fields added

| DTO | Field | Type | Purpose |
| --- | --- | --- | --- |
| `TranscriptResponse` | `generation` | `int64` | Monotonic Transcript generation for mobile reset detection (sourced from Service, not LookupLaunch) |

### 11.6 Production behavior added

| Location | Change | Purpose |
| --- | --- | --- |
| `GenerationCleanupCapability` | New type: Generation, Session, Recorder, Transport, Completion | Immutable generation-bound cleanup; claimed at most once |
| `GenerationCompletion` | New type: sync.Once + chan struct{} | Idempotent completion signal; Complete() on every terminal path |
| `TerminalTransport.RetireIfGeneration` | New method: retire only if gen matches | Instance-guarded transport retirement; stale is no-op |
| `internal/mux/adapter.go` | `SessionCreatorWithIdentity` interface + `CreateSessionAndCapture` | Returns (id, Session, error) atomically; Session never nil in capability |
| `recorder.go` | `StartRecorderUnconditional` | Always new Recorder; never reuses existing |
| `OwnedPTYRuntime.Create` pre-install | Claim old capability under o.mu → Execute → wait Completion.Done() → then CreateSession | Old cleanup completes BEFORE new adapter session installed |
| Rollback paths | `cap.Execute()` (instance-guarded) | Zero ID-addressed termination: no terminateAdapterSession, TerminateSession, DeleteRecorder(id), GetRecorder(id) |
| Stop/Kill finalization | `cap.Execute()` outside locks | CompareAndTerminate + DeleteRecorderIfSame + RetireIfGeneration; Completion.Complete() even on panic |
| `transcript.Service` | ReplaceTranscript, SetTranscriptGeneration | Independent per-session Transcript generation counter |

**Prohibited in all post-capture paths**: `terminateAdapterSession`,
`SessionTerminator.TerminateSession`, `DeleteRecorder` (by ID),
`GetRecorder`, Registry/recorder/transport lookup by ID, raw `close()`
on any completion channel.

## 12. Acceptance gates

### 12.1 Build and static analysis

```sh
cd companion-daemon
go build ./...        # must pass — no compile errors
go vet ./...          # must pass — no vet warnings
gofmt -l <changed .go files>  # must be empty
git diff --check      # must pass — no whitespace errors
```

### 12.2 Unit and regression tests

```sh
cd companion-daemon
# Core packages — full race suite
go test -race ./internal/term ./internal/mux ./internal/transcript ./cmd/devremote -count=1

# Agent package — regression
go test -race ./internal/agent/... -count=1

# Session identity — regression
go test -race ./internal/sessionid -count=1

# Device trust — regression
go test -race ./internal/devicetrust/... -count=1

# Models — regression
go test -race ./internal/models -count=1
```

### 12.3 Mobile TypeScript

```sh
cd mobile
npx tsc --noEmit     # must pass — no type errors
```

### 12.4 Deletion static verification

```sh
# Zero production references to deleted types
rg "ActivityBuffer|ActivityEvent|ActivityType" companion-daemon/internal companion-daemon/cmd \
  --glob '!*_test.go' --glob '!testdata/*'
# Expected: 0 matches (except possibly in migration comments)

rg "EventStore|memoryEventStore|NewMemoryEventStore" companion-daemon/internal companion-daemon/cmd \
  --glob '!*_test.go'
# Expected: 0 matches

rg "ResolveAgentLog|findAgentProcess" companion-daemon/internal companion-daemon/cmd \
  --glob '!*_test.go' --glob '!testdata/*'
# Expected: 0 matches

rg "ClaudeParser|CodexParser|GeminiParser|AntigravityParser" companion-daemon/internal/term \
  --glob '!*_test.go'
# Expected: 0 matches

rg "sessionStateData|evaluateState|isApprovalPrompt|isThinkingFallback" companion-daemon/internal/term \
  --glob '!*_test.go'
# Expected: 0 matches
```

### 12.5 Secret scan

```sh
SECRETS=$(grep -rn "sk-[A-Za-z0-9]\|ghp_\|xox[baprs]-\|Bearer [A-Za-z0-9]" \
  companion-daemon/internal/ companion-daemon/cmd/ companion-daemon/docs/ \
  | grep -v "testdata/\|diagnostic\.go\|approval_test\.go\|auth_test\.go\|fake\|REDACTED\|redact")
[ -n "$SECRETS" ] && echo "SECRET FOUND: $SECRETS" && exit 1 || true
```

### 12.6 Focused tests required

1. **Transcript becomes sole history store**: Create session, write PTY
   output, verify `GET /api/sessions/{id}/transcript` returns segments.
   Verify `GET /api/sessions?activity=` returns 404.
2. **Telemetry snapshot excludes deleted fields**: `GET /api/sessions`
   response has no `state`, `load`, `runner`, `runnerColor`, `events` keys.
3. **Telemetry snapshot includes required fields**: `GET /api/sessions`
   response has `agentKind`, `agentStatus`, `agentConfidence`,
   `agentActivity`, `lifecycleState`, `approvals`.
4. **ActivityBuffer deletion**: zero production references; tests for
   packages that imported ActivityBuffer still compile and pass.
5. **Recorder single-reader invariant**: After ActivityBuffer removal,
   Recorder is still the sole PTY reader. Live terminal output unchanged.
6. **Transcript ordering**: Segments have monotonic Seq. Clear resets Seq.
7. **Reconnect with generation**: Mobile reconnects, calls Transcript API,
   detects generation change, discards stale cache.
8. **Admission boundary acceptance**: See §6.1.9 for the 10 admission-boundary
   tests. These are the authoritative PA3 implementation gate for the
   generation-reset and lifecycle-admission mechanisms.
9. **Mobile rendering**: Unknown agentKind/status → graceful fallback.
   Missing optional fields → no crash.
10. **Privacy structural**: Transcript segments contain no raw input
    (input boundary markers only). Agent-event Text is allowlist-projected
    (no prompts, no thinking, no tool args).
11. **Generation monotonicity**: `TranscriptResponse.generation` is
    monotonic within a canonical session ID. Replacement increments it.
12. **Cursor-based dedup**: Accepted adapter produces same events for
    same cursor range. Duplicate agent events produce distinct segments
    with same `AgentEventRef`.

### 12.7 Race tests

All focused tests run with `-race -count=20`:

```sh
go test -race ./internal/term -run "TestTranscript|TestTelemetry|TestRecorder" -count=20
go test -race ./internal/transcript -count=20
```

### 12.8 Mobile gate

```sh
cd mobile
npx tsc --noEmit
```

Report mobile emulator/physical-device gate as `manual` with checklist.

## 13. Non-goals and later-phase exclusions

The following are explicitly **NOT in PA3**:

- **PB physical deletion of tmux/cmux/localpty adapters** — adapters
  remain registered and functional.
- **Managed Codex/Claude runtime changes** — the managed session
  endpoints, launch, lifecycle, and approval execution paths are frozen.
- **Approval claim, decision, delivery, consumption semantic changes** —
  the A1/B6/B7 approval infrastructure is frozen. `AuthoritativeApprovalStore`
  is not modified; notification is wired in `TelemetryService`, not inside
  the store.
- **Transcript contract version bump** — `t3.1` is retained. The
  `generation` field is additive to the envelope (not to segments).
  No new segment kinds, sources, or envelope restructuring.
- **Canonical Timeline, notifications infrastructure, Grok, or
  Navigator work** — separate phases.
- **New agent adapter addition** — no new parser, detector, or resolver.
- **Adapter Doctor/Repair** — D1 is a separate phase.
- **Daemon-restart persistence for Transcript** — Transcript remains
  in-memory (non-durable). This is a documented limitation carried
  forward from T3.
- **Raw Transcript attachments** — out of MVP scope per the T project plan.
- **Mobile push notification infrastructure** — the `Notifier` interface
  wire point changes (screen heuristics → accepted adapter path) but the
  push delivery mechanism (`registerPush`, APNs/FCM) is unchanged.
- **Native managed Codex/Claude approval notifications** — the managed
  activation paths bypass `TelemetryService.ingestApprovals` and go
  directly to `AuthoritativeApprovalStore.Ingest`. Notification hookup
  for those paths is deferred to the actionable-mapping acceptance
  phase, not retrofitted into PA3.
- **New session profiles or custom command support** — profile list is
  frozen (shell, codex, claude).
- **Per-session ACL or multi-device role UI** — M3 non-goal.
- **Nested-agent-only interrupt** — M3 non-goal.
- **Process restart/resume** — out of scope.
- **Automatic content-aware secret redaction** — Transcript uses
  structural protections (ANSI stripping, TUI omission, input suppression,
  field allowlists) but does not scan for or redact secrets in
  byte-stream terminal output. See §9.2.

## 14. Rollback points

Each migration step is independently revertible:

1. **Step 1 (mobile cutover)**: Revert mobile commits. Backend unchanged.
2. **Step 2 (TelemetryService simplification)**: Revert to prior SHA.
   Full legacy state machine restored. ActivityBuffer and EventStore still intact.
3. **Step 3 (API endpoint removal)**: Revert to prior SHA. Endpoints restored.
4. **Step 4 (DTO update)**: Revert to prior SHA. Old fields restored.
5. **Step 5 (mobile DTO)**: Revert mobile commits.
6. **Step 6 (store deletion)**: Revert to prior SHA. ActivityBuffer,
   EventStore, and legacy parser/resolver files restored.
7. **Step 7 (final cleanup)**: Revert individual file deletions.

The PA2d accepted SHA (`b93c521b7de45f3f577805dbb7de505e16f3172d`) is the
authoritative rollback anchor for the entire PA3 change set.

## 15. Frozen-unaffected boundaries

These accepted paths must not be modified by PA3:

- `internal/mux/` — all adapter files (tmux, cmux, localpty);
  `controlled_pty_adapter.go` — **PA3 carve-out**: may implement
  `SessionCreatorWithIdentity` (§6.1.8). No other adapter file changes.
- `internal/mux/adapter.go` — Adapter interface, capability interfaces;
  **PA3 carve-out**: may add `SessionCreatorWithIdentity` interface
  (`CreateSessionAndCapture(ctx, opts) (string, Session, error)`)
  implemented by `controlledPTYAdapter`. No other adapter interface changes.
- `internal/mux/registry.go` — Registry, snapshot caching
- `internal/sessionid/` — SessionRef, ParseSessionID, Canonical
- `internal/term/terminal_transport.go` — TerminalTransport (PA2d)
- `internal/term/owned_pty_runtime.go` — OwnedPTYRuntime (PA2c); **allowed
  changes**: add `GenerationCleanupCapability` + `GenerationCompletion`
  per §6.1.1; pre-install barrier per §6.1.2 (claim old capability →
  Execute → wait Completion.Done() → then CreateSession); construct
  capability immediately after CreateSession before any fallible step;
  all rollback/Stop/Kill paths use `cap.Execute()` (nil-safe,
  instance-guarded); zero ID-addressed termination. Add
  `RetireIfGeneration` on `TerminalTransport`. Add
  `StartRecorderUnconditional` on `recorder.go`. Transcript gen counter
  on `transcript.Service`.
 
- `internal/term/lifecycle_service.go` — LifecycleService (PA2c)
- `internal/term/lifecycle_handlers.go` — lifecycle HTTP handlers
- `internal/term/lifecycle.go` — lifecycle state machine
- `internal/term/managed_codex.go` — ManagedCodexService (SP0)
- `internal/term/managed_claude.go` — ManagedClaudeService (C1D)
- `internal/term/managed_catalog.go` — ManagedRuntimeCatalog (PA1)
- `internal/term/managed_api.go` — managed REST handlers
- `internal/term/managed_registry.go` — ManagedSessionRegistry
- `internal/term/approval_*.go` — all approval infrastructure (A1/B6/B7)
- `internal/term/agent_status_store.go` — AgentStatusStore (S1)
- `internal/term/adapter_state.go` — adapterState (accepted adapter ingestion)
- `internal/agent/` — all agent adapter files (T0/T1/T2/A8)
- `internal/transcript/contract.go` — TranscriptSegment, SegmentKind, SegmentSource
- `internal/transcript/arbitration.go` — SourceArbiter, TranscriptResponse;
  **allowed additive change**: `Generation` field on `TranscriptResponse`
- `internal/transcript/projector_agent.go` — AgentEventProjector
- `internal/transcript/projector_bytes.go` — ByteStreamProjector
- `internal/transcript/store.go` — Store
- `internal/transcript/service.go` — Service; **allowed additive methods**:
  `ReplaceTranscript` and `SetTranscriptGeneration` per §6.1.8. Does NOT call `RemoveLaunch`.
- `internal/transcript/api.go` — HandleTranscript, HandleTranscriptStats
- `internal/devicetrust/` — all device trust infrastructure (M2.5)
- `internal/watcher/` — file watcher
- `mobile/src/lib/agentActivity.ts` — AgentActivity derivation (unchanged)
- `mobile/src/lib/agentDisplay.ts` — agent display helpers (unchanged)
- `mobile/src/lib/transcriptClassify.ts` — transcript classification (unchanged)

**Frozen functions in `mobile/src/lib/client.ts`** (retained, unmodified):
- `listSessions()`, `listSessionProfiles()`, `createSession()`
- `stopSession()`, `killSession()`, `deleteSessionHistory()`
- `getTranscript()`, `getManagedStatus()`, `getManagedEvents()`, `postManagedPrompt()`
- `terminalURL()`, `terminalWebSocketURL()`, `terminalWebSocketTicketURL()`
- `resolveApproval()`, `probeDaemon()`, `setBaseURL()`, `setDeviceAuth()`, `hasDeviceAuth()`
- All type exports: `SessionProfile`, `SessionLifecycle`, `LifecycleActionResult`,
  `TranscriptSegment`, `TranscriptResponse`, `SafeApproval`, `SafeOption`,
  `ApprovalActionResult`, `PokitError`, `ConnectivityFailure`, `ReachabilityResult`,
  `DeviceAuth`

**Deleted functions in `mobile/src/lib/client.ts`**:
- `getActivityHistory()` — deleted in Step 5
- `getSessionHistory()` — deleted in Step 5
- `createOrUpdateSession()` — legacy, removed if no callers remain after Step 1
- `deleteSession()` — legacy query-form, removed if no callers remain after Step 1

## 16. Commit and review discipline

1. Each migration step (1–7) is a separate implementation with its own
   contract refinement, implementation commit(s), focused tests, gate,
   and independent review stop.
2. No step may begin before the preceding step is independently accepted.
3. The implementation commit and evidence/report commit must be separated
   when required by the PA1-established evidence protocol.
4. Push fast-forward only; confirm local == remote and clean worktree
   before requesting review.
5. PB (physical adapter deletion) is prohibited until PA3 is independently
   accepted.

## 17. Relationship to PA2 contract

PA2 Section 4 explicitly excluded "PA3 mobile, Transcript, Activity, or
telemetry authority cutover" from PA2 scope. This PA3 contract is the
fulfillment of that exclusion.

PA2 Section 8 (independent review amendments) closed these pre-PA3 blockers:
1. TerminalTransport no longer owns stop/kill/delete (PA2c owns lifecycle).
2. Lifecycle requests are generation-gated (PA2c).
3. Recorder remains sole PTY reader; no raw Read/OpenStream surface (PA2d).
4. Transcript/Activity projection explicitly excluded from PA2d scope
   (deferred to PA3 — this contract).

PA2 established the foundation (generation-bound identity, lifecycle
ownership, TerminalTransport, Recorder single-reader). PA3 cleans up the
projection and presentation layers on that foundation.
