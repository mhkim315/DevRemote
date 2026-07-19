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

PA3 introduces a shared per-canonical-session admission token used by ALL
operations that mutate the lifecycle catalog: Create, replacement Create
(same canonical ID), Stop, Kill, final cleanup, and invalidation. The
admission token is NOT a mutex held across process/PTY/recorder/adapter/
provider I/O — it serializes same-ID mutations only and releases before
any external I/O begins.

#### 6.1.1 Admission authority

**Admission token (per canonical-session ID, not global)**:

```
admissionGate: map[canonicalID] → per-session mutex
```

The admission gate is acquired BEFORE any adapter or lifecycle state
mutation and released AFTER the catalog entry is published (for Create)
or the lifecycle transition is recorded (for Stop/Kill). It blocks only
operations targeting the SAME canonical session ID. Different sessions
are independent. Reads (list, telemetry, Transcript) are unblocked by
the admission gate — they use `o.mu` (brief catalog lock) or the
Transcript store's own lock.

**Operations that acquire the admission gate**:

| Operation | Hold scope | I/O under gate |
| --- | --- | --- |
| Create (first or replacement) | Phase 0 acquire → Phase 6 publish → release | Recorder stop/start, Transcript replacement |
| Stop | derive current generation → claim handle → record transition → release | Process signal, wait, Recorder closure (outside gate) |
| Kill | derive current generation → claim handle → record transition → release | Process signal (outside gate) |
| Final cleanup (natural exit) | finalizeRecord under o.mu only (brief) | Cleanup function invocation (outside gate) |
| Invalidation (replacement retirement) | invalidateForLaunch under o.mu only (brief) | Status-store write, approval supersede (outside gate) |

**What the admission gate does NOT cover**: Process signaling, PTY
read/write/close, Recorder start/stop readLoop, Transcript chunk queue
drain, adapter CreateSession I/O, or any external process wait. These
all happen outside any daemon lock — the admission gate serializes
catalog mutations only.

#### 6.1.2 Catalog state machine

Every catalog entry (`CatalogEntry.State`) is in exactly one of these
states. All transitions are guarded by the admission gate + `o.mu`.

```
                    ┌──────────────┐
                    │    absent    │
                    └──────┬───────┘
                           │ Create (Phase 2 reserve)
                    ┌──────▼───────┐
                    │   creating   │  ← LifecycleStarting
                    └──────┬───────┘
                           │ Phase 6 publish
                    ┌──────▼───────┐
                    │   running    │  ← LifecycleRunning
                    └──┬────┬──────┘
                       │    │
          Stop/Kill ───┘    └─── natural exit (Recorder EOF)
                       │         │
                ┌──────▼──┐  ┌──▼───────────┐
                │stopping │  │   retiring    │ ← finalizeRecord
                └────┬────┘  └──┬────────────┘
                     │          │ cleanup invoked
                ┌────▼──────────▼────┐
                │     terminal       │ ← LifecycleExited | LifecycleKilled | LifecycleFailed
                └────────┬───────────┘
                         │ Delete
                    ┌────▼───────────┐
                    │    absent      │  (row removed; history cleared)
                    └────────────────┘

   Replacement path (admission gate serializes):
     running → (admission gate) → Stop old Recorder → ReplaceTranscript
     → Start new Recorder → publish → old generation superseded
     → old entry.State unchanged (terminal via natural exit/finalize)
```

**Legal transitions**:

| From | To | Trigger | Guard |
| --- | --- | --- | --- |
| absent | creating | Create reserve (Phase 2) | admission gate + o.mu; gen matches |
| creating | running | Create publish (Phase 6) | admission gate + o.mu; gen still current |
| creating | absent | Create rollback | admission gate + o.mu; gen matches |
| running | stopping | beginStop | admission gate + o.mu; gen matches |
| running | retiring | finalizeRecord (natural exit) | o.mu; gen matches |
| stopping | terminal | finalizeRecord (Stop/Kill) | o.mu; gen matches |
| retiring | terminal | finalizeRecord (cleanup done) | o.mu; gen matches |
| running | superseded | Replacement publish | admission gate + o.mu; old entry's Recorder stopped, transport retired |

**Illegal transitions** (rejected with deterministic error):

- creating → stopping (lifecycle cannot target unpublished session)
- creating → terminal (natural exit before publish → Recorder not started yet)
- absent → stopping/kill (not found)
- terminal → running/stopping (cannot restart)
- running → running with different generation (stale generation)

#### 6.1.3 Linearization points

Every authority-bearing transition has exactly one linearization point
under `o.mu` (the catalog lock). The admission gate serializes same-ID
operations so they reach the lock in order.

**Create linearization points**:

| Point | Phase | What is decided |
| --- | --- | --- |
| LP1 — generation allocation | Phase 2 | `o.nextGen++` under `o.mu`; lifecycle generation assigned |
| LP2 — catalog reservation | Phase 2 | `o.entries[id] = &CatalogEntry{State: LifecycleStarting, Generation: gen}` under `o.mu` |
| LP3 — catalog publication | Phase 6 | `entry.State = LifecycleRunning; entry.handle = handle; entry.transport = transport` under `o.mu`; gen verified still current |
| LP4 — Transcript generation init | Phase 4 | `transcript.SetTranscriptGeneration(id, 1)` (first create) or `transcript.ReplaceTranscript(id)` (replacement); independent counter |
| LP5 — Recorder instance binding | Phase 5 | `StartRecorderUnconditional(id, stream)` creates new Recorder; old Recorder already stopped in Phase 3 |

**Lifecycle linearization points** (unchanged from PA2c):

| Point | Method | What is decided |
| --- | --- | --- |
| LP-L1 — stop claim | `beginStop` | Under `o.mu`: gen check, state → stopping, handle claimed |
| LP-L2 — kill claim | `requestKill` | Under `o.mu`: gen check, killRequested = true, handle claimed |
| LP-L3 — terminal finalization | `finalizeRecord` | Under `o.mu`: gen check, state → terminal, cleanup capability claimed at most once |
| LP-L4 — cleanup invocation | Outside `o.mu` | The claimed cleanup function runs (adapter termination + Recorder deletion) |

**Transcript generation linearization** (new in PA3):

| Point | Method | What is decided |
| --- | --- | --- |
| LP-T1 — generation initialisation | `SetTranscriptGeneration` | Under Service internal lock: gen set to 1 (first-write-wins) |
| LP-T2 — generation replacement | `ReplaceTranscript` | Under Service internal lock: queue drained, store cleared, gen bumped |

#### 6.1.4 Guarantees

**G1 — No conflicting identity authority**: Create and lifecycle cannot
independently authorize conflicting identities for the same canonical ID.
The admission gate serializes all mutations; `o.mu` checks generation
currency at every transition. A lifecycle action (Stop/Kill) cannot claim
a handle from a replaced generation — the generation check in `beginStop`/
`requestKill` rejects it.

**G2 — Adapter session identity isolation**: A new adapter session is never
visible while the catalog authorizes an incompatible old generation. The
admission gate ensures the old Recorder is stopped (Phase 3) AND the old
catalog entry is superseded (Phase 6) BEFORE the new entry is published.
Between reserve (Phase 2) and publish (Phase 6), reads see `LifecycleStarting`
— they know the session is not yet running.

**G3 — No stale lifecycle targeting**: A stale Stop/Kill/cleanup cannot
target or delete a replacement. `beginStop`, `requestKill`, and
`finalizeRecord` all check `e.Generation != gen` under `o.mu` and fail
closed. Stale operations receive `stale_generation` and cannot affect the
current generation's process.

**G4 — Replacement preserves required cleanup**: Replacement does not
discard old-generation cleanup. `replaceTranscript` drains the old chunk
queue (processing remaining chunks through the projector) before clearing
the store. The old Recorder's `readLoop` exits naturally via PTY close
(performed by `DeleteRecorder` in Phase 3). The old catalog entry's
`cleanup` function is NOT invoked by replacement — it is invoked only by
`finalizeRecord` when the old Recorder's natural exit goroutine fires
(`watchExit`). Replacement retires the transport but does not terminate
the old adapter session — the old `watchExit` goroutine does that on EOF.

**G5 — Recorder closure is not lifecycle termination**: Recorder closure
(Phase 3 stop of old Recorder) is transport cleanup, not lifecycle
termination authority. The lifecycle catalog entry's state is NOT changed
by Recorder stop. Only `finalizeRecord` (called from `watchExit` or
Stop/Kill) transitions the state to terminal. This preserves PA2c's
invariant: "Recorder closure is NOT substituted for lifecycle-owner
termination authority" (PA2c §5).

**G6 — No authority lock held across external I/O**: `o.mu` is held only
for brief catalog reads/writes (Phase 2, Phase 6, `beginStop`,
`requestKill`, `finalizeRecord`). The admission gate is held across
Recorder stop/start (Phase 3-5) but it is per-ID — it blocks only
same-session operations, not all sessions. Process signaling, PTY I/O,
and Recorder readLoop happen entirely outside any daemon lock. This
preserves PA2d §5: "No lifecycle/catalog lock is held across process
wait, signal delivery, or other external I/O."

**G7 — Failed/cancelled creation cannot publish**: If any phase between
reserve (Phase 2) and publish (Phase 6) fails, the catalog entry is
rolled back: `delete(o.entries[canonicalID])` under `o.mu` ONLY IF
`entry.Generation == lifecycleGen` AND `entry.State == LifecycleStarting`.
A replacement that already superseded this entry is not affected (the
generation check fails, so the delete is a no-op). The adapter session
is terminated via `terminateAdapterSession`. The Recorder (if started)
is stopped.

**G8 — Bounded generation-specific completion signals**: Every `watchExit`
goroutine waits on `rec.Done()` which closes exactly once when that
Recorder's `readLoop` exits. The `cleanupDone` channel on the catalog
entry is closed exactly once by `finalizeRecord` and is waited on by the
lifecycle Stop/Kill path. All waiters use bounded, generation-specific
channels — no polling of mutable IDs.

#### 6.1.5 Canonical phase model (six phases, Phase 0–6)

This is the SINGLE authoritative flow description. All other sections
reference this model.

**Phase 0 — Generate identity, acquire admission gate** (BEFORE any
adapter mutation):
- Daemon generates canonical ID from profile + name (deterministic) or
  fresh UUID
- Acquires per-ID admission gate
- No adapter state is mutated before the gate is held

**Phase 1 — Create adapter session** (admission gate held, no catalog lock):
- `CreateSession(ctx, opts)` creates the PTY session
- Adapter I/O runs with no daemon global lock

**Phase 2 — Catalog reservation** (`o.mu`, brief, no I/O):
- Increment `o.nextGen` (lifecycle generation)
- Reserve `CatalogEntry{State: LifecycleStarting, Generation: lifecycleGen}`
- If entry already exists (replacement): record `isReplacement = true`
- Release `o.mu`

**Phase 3 — Stop old Recorder** (no locks — PTY I/O):
- If replacement: `DeleteRecorder(canonicalID)`
- Old Recorder's readLoop exits; subscribers closed; PTY closed

**Phase 4 — Transcript generation** (no locks):
- If replacement: `ReplaceTranscript(canonicalID)` (drain + clear + bump)
- If first create: `SetTranscriptGeneration(canonicalID, 1)` (idempotent)
- Independent Transcript generation counter — NOT `o.nextGen`

**Phase 5 — Start new Recorder** (no locks — PTY I/O):
- `StartRecorderUnconditional(canonicalID, stream, activity)`
- Never reuses an existing Recorder

**Phase 6 — Publish** (`o.mu`, brief, no I/O):
- If `entry.Generation != lifecycleGen`: another create superseded us;
  stop our Recorder, terminate adapter session, return error
- Otherwise: set `entry.State = LifecycleRunning`, bind handle, transport,
  cleanup; start `watchExit`
- Release `o.mu` and admission gate
- Return canonical ID

**Replacement semantics**: The admission gate serializes same-ID creates.
A second create blocks until the first returns. The second sees the
first's published entry and takes the replacement path (Phases 3-5 run
as replacement). No concurrent same-ID race exists.

#### 6.1.6 Failure/cancellation matrix

| Phase | Failure mode | Catalog state | Adapter session | Recorder | Transcript | Outcome |
| --- | --- | --- | --- | --- | --- | --- |
| 0 | ID generation fails | absent | not created | none | untouched | Error returned; admission gate not acquired |
| 0 | Admission gate deadlock | absent | not created | none | untouched | Timeout after 30s; error returned |
| 1 | CreateSession fails | (reserved in Phase 2 not yet reached) | not created | none | untouched | Error returned; admission gate released; no catalog mutation |
| 2 | o.mu contention (transient) | absent | created (Phase 1) | none | untouched | Retry lock; if timeout, rollback Phase 1 (terminate adapter session) |
| 3 | DeleteRecorder fails (old Recorder hung) | reserved (LifecycleStarting) | created (new) | old hung, new not started | untouched | Force-close PTY FD; `recorderRegistry.terminated[id] = true`; proceed |
| 4 | ReplaceTranscript fails | reserved (LifecycleStarting) | created (new) | old stopped, new not started | old gen cleared, new gen NOT allocated | Rollback: `delete(o.entries[id])` under o.mu if gen matches; terminate adapter session; return error |
| 5 | OpenStream fails | reserved (LifecycleStarting) | created (new) | old stopped (if replacement), new not started | if replacement: old gen replaced (committed); if first: untouched | Rollback: `delete(o.entries[id])` under o.mu if gen matches; terminate adapter session; return error. Transcript replacement committed — old data gone, no resurrection |
| 5 | StartRecorderUnconditional fails | reserved (LifecycleStarting) | created (new) | none | same as above | Same rollback as OpenStream failure |
| 6 | Generation superseded (another create published first) | reserved by us, published by other | created (new) | our Recorder started | other's generation active | Stop our Recorder; terminate our adapter session; return `"session replaced"` error |
| 6 | handle/transport/cleanup creation fails | reserved (LifecycleStarting) | created (new) | started (Phase 5) | generation active | Rollback: `delete(o.entries[id])` if gen matches; stop Recorder; terminate adapter session; return error |

**Key rollback invariant**: `o.nextGen` is NEVER decremented on failure.
If this was a replacement, the old Transcript data is already cleared
(ReplaceTranscript committed in Phase 4). The caller may retry — the
retry is a new Create that sees the rolled-back catalog entry (absent)
and takes the first-create path, allocating a new Transcript generation.

#### 6.1.7 Non-OwnedPTYRuntime sessions (tmux, cmux, localpty)

For sessions NOT created through `OwnedPTYRuntime.Create`, generation is
initialised lazily: the first call to `FeedBytes`, `AddSnapshotSegment`,
or `ProjectAgentEvents` implicitly sets generation to 1 if not already
set. `TranscriptResponse.generation` returns 1. No replacement path
exists — the generation never changes, so mobile never detects a reset.

#### 6.1.8 New method contracts

**`ReplaceTranscript`**:
```go
// ReplaceTranscript atomically drains+clears a session's Transcript and
// allocates a new generation. Returns the new generation number.
// PRECONDITION: no goroutine is actively feeding bytes for this session.
// The caller must stop all Recorders before calling this method.
func (s *Service) ReplaceTranscript(sessionID string) int64
```
Steps: close old chunk queue (drain) → clear store → reset projector +
arbiter → bump per-session generation counter → re-enable queue → return
generation. Does NOT call `RemoveLaunch`.

**`SetTranscriptGeneration`**:
```go
// SetTranscriptGeneration records the initial generation for a session.
// Idempotent (first-write-wins). Safe to call after Recorder start.
func (s *Service) SetTranscriptGeneration(sessionID string, gen int64)
```

**`StartRecorderUnconditional`** (new on `recorder.go`):
```go
// StartRecorderUnconditional always creates a new Recorder for the session.
// Unlike StartRecorder/EnsureRecorder, it never returns an existing live
// Recorder. The caller guarantees any old Recorder is already stopped.
func StartRecorderUnconditional(sessionID string, stream ptyStream, activity *ActivityBuffer) *Recorder
```

#### 6.1.9 Focused acceptance tests for the admission boundary

1. **Create publishes atomically**: Create → catalog entry visible with
   `State=LifecycleRunning`, bound handle, transport, Recorder, cleanup.
   No intermediate `LifecycleStarting` visible after publish.

2. **Replacement clears old Transcript**: Create-A → feed bytes →
   Create-B (same ID) → Create-B's Transcript is empty (generation 2).
   Create-A's bytes are gone. `TranscriptResponse.generation` is 2.

3. **Admission gate serializes same-ID creates**: Three goroutines create
   same ID. Each blocks until prior returns. Transcript generations are
   1, 2, 3. Lifecycle generations are N, N+1, N+2. No leaked Recorders.

4. **Different-ID creates are parallel**: Two goroutines create different
   IDs. Neither blocks on the other. Both publish. Both Transcript
   generations are 1.

5. **Stale Stop rejected**: Create-A → Create-B (replacement) → Stop
   with Create-A's generation → rejected (`stale_generation`). Create-B
   unaffected.

6. **Stale cleanup does not target replacement**: Finalize with old
   generation → `finalizeRecord` returns nil cleanup. Replacement's
   catalog entry unchanged.

7. **Failed Phase 5 rolls back**: Create → Phase 5 OpenStream fails →
   catalog entry deleted (if gen still current). Adapter session
   terminated. No phantom `LifecycleStarting` entry.

8. **Phase 6 superseded returns error**: Two creates admitted
   sequentially. First publishes. Second's Phase 6 sees gen mismatch →
   stops its Recorder, terminates adapter session, returns error.
   First's entry is the published winner.

9. **No o.mu held across I/O**: Instrumentation proves `o.mu` is never
   held during `DeleteRecorder`, `OpenStream`, `StartRecorderUnconditional`,
   `ReplaceTranscript`, process signaling, or `readLoop` execution.

10. **Lifecycle cannot target creating session**: Create reserves
    (LifecycleStarting) → Stop arrives before publish → `beginStop`
    rejects (state != running).

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
| Transcript store | `Service.ReplaceTranscript(sessionID)` in Phase 4 (no locks, old Recorder already stopped) | Old segments cleared before new Recorder starts; new generation allocated |
| Agent-activity store | `LaunchGen` gate in `AgentStatusStore.Update` | Writes with `LaunchGen < current` rejected |
| Approval store | `SupersedeRuntime` on replacement | Prior pending approvals invalidated |
| Recorder | `DeleteRecorderIfSame(sessionID, oldRec)` on replacement | Old recorder stopped, subscribers closed |
| TerminalTransport | `Retire()` on replacement | Input/resize to old handle silently discarded |
| TranscriptResponse | `generation` field change | Mobile detects reset and discards stale cache |

Mobile cannot receive stale events because the store-level clear (Phase 4)
runs after the old Recorder is stopped (Phase 3) and before the new
Recorder starts (Phase 5).

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
- **Add `ReplaceTranscript` method on `transcript.Service`**: drains old
  chunk queue, clears store + projectors + arbiters, re-enables queue,
  increments per-session generation, returns new generation. Does NOT call
  `RemoveLaunch`. PRECONDITION: no concurrent feeder (caller must stop old
  Recorder first).
- **Add `SetTranscriptGeneration` method on `transcript.Service`**: records
  the initial generation for a newly created session. Idempotent
  (first-write-wins). Safe to call after Recorder start.
- **Restructure `OwnedPTYRuntime.Create` into Phase 0–6 admission**:
  Phase 0: generate canonical ID, acquire per-ID admission gate BEFORE
  CreateSession. Phase 1: CreateSession (admission gate held, no catalog
  lock). Phase 2: `o.mu` reserve (brief — allocate lifecycle gen, create
  LifecycleStarting entry). Phase 3: `DeleteRecorder` if replacement
  (no locks — PTY I/O). Phase 4: `ReplaceTranscript` or
  `SetTranscriptGeneration` (no locks — independent per-session generation
  counter). Phase 5: `StartRecorderUnconditional` (no locks — PTY I/O).
  Phase 6: `o.mu` publish (brief — check gen current, bind handle/
  transport/cleanup, transition to LifecycleRunning). Admission gate
  released on return. See §6.1.4 for guarantees and §6.1.6 for failure
  matrix.
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
| `OwnedPTYRuntime` admission gate | New per-canonical-ID mutex map | Serializes same-ID Create, Stop, Kill; never held across PTY/process I/O |
| `OwnedPTYRuntime.Create` Phase 0 | Generate canonical ID, acquire admission gate BEFORE CreateSession | Prevents adapter-session overwrite by concurrent same-ID creates |
| `OwnedPTYRuntime.Create` Phase 2 | Under `o.mu`: allocate lifecycle gen, reserve catalog entry (brief, no I/O) | Catalog reservation; `o.mu` released before any PTY/Recorder I/O |
| `OwnedPTYRuntime.Create` Phase 3 | `DeleteRecorder(canonicalID)` with no lock held | Stop old Recorder (PTY I/O outside any lock — PA2d preserved) |
| `OwnedPTYRuntime.Create` Phase 4 | `ReplaceTranscript` or `SetTranscriptGeneration` (no locks) | Independent per-session Transcript generation (not `o.nextGen`) |
| `OwnedPTYRuntime.Create` Phase 5 | `StartRecorderUnconditional` with no lock held | New Recorder from new adapter session's stream; never reuses old Recorder |
| `OwnedPTYRuntime.Create` Phase 6 | Under `o.mu`: check gen current, publish or rollback (brief, no I/O) | Atomic publication; failed creation cannot publish running generation |
| `transcript.Service` | Independent per-session generation counter | Strictly monotonic per session; not derived from lifecycle or launch gen |
| `recorder.go` | `StartRecorderUnconditional` new function | Always creates new Recorder; skips existing-live check |
| `TelemetryService.processSession` | `Notifier.ApprovalRequired` from accepted adapter path | Dormant until `provenActionMapping` is proven; no screen heuristics |


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

- `internal/mux/` — all adapter files (tmux, cmux, localpty, controlled_pty)
- `internal/mux/adapter.go` — Adapter interface, capability interfaces
- `internal/mux/registry.go` — Registry, snapshot caching
- `internal/sessionid/` — SessionRef, ParseSessionID, Canonical
- `internal/term/terminal_transport.go` — TerminalTransport (PA2d)
- `internal/term/owned_pty_runtime.go` — OwnedPTYRuntime (PA2c); **allowed
  changes**: add per-canonical-ID admission gate; restructure `Create` into
  Phase 0–6 per §6.1.5; add `StartRecorderUnconditional` to `recorder.go`;
  independent Transcript generation counter on `transcript.Service`. See
  §6.1.4 for guarantees (G1–G8), §6.1.6 for failure matrix
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
  `ReplaceTranscript(sessionID string) int64` (drain + clear + re-enable +
  bump generation) and `SetTranscriptGeneration(sessionID string, gen int64)`
  (idempotent first-write-wins). Does NOT call `RemoveLaunch`.
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
