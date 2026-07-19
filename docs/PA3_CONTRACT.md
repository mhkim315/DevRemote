# PA3 — Mobile, Transcript, Activity, and Telemetry Authority Cutover Contract

Status: **PROPOSED CONTRACT — PENDING INDEPENDENT REVIEW**

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

- `ActivityBuffer` (`internal/term/activity.go`) is **deleted**.
- `EventStore` / `memoryEventStore` (`internal/term/eventstore.go`) is **deleted**.
- `GET /api/sessions?activity=` is **removed** (404).
- `GET /api/sessions?history=` is **removed** (404).
- `SessionTelemetry.Events` field is **removed**.
- Mobile moves to `GET /api/sessions/{id}/transcript` as the sole history/activity read path.

### 3.2 Telemetry simplifies to session listing + agent projection

`TelemetryService` retains:
- Session list snapshot assembly (canonical ID, adapter, capabilities, display fields)
- Lifecycle state merge from `OwnedPTYRuntime` + managed catalog
- Agent detection bridge (agentKind, agentConfidence)
- Agent-activity projection from `AgentStatusStore` (`AgentActivityDTO`)
- Approval listing from `AuthoritativeApprovalStore`

`TelemetryService` drops:
- Legacy state machine (`idle`/`thinking`/`working`/`waiting` from screen parsing)
- Legacy log resolution and parser dispatch (the `processSession` parser path)
- Legacy `Events` field population
- `sessionStateData` (the cached per-session screen/log parsing state)
- `evaluateState`, `isApprovalPrompt`, `isThinkingFallback` screen heuristics
- The `Notifier` call from screen heuristics (approval notifications move to the accepted adapter path)

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
- `AgentCard` drops `events` field usage; Transcript is the separate read path
- `getActivityHistory()` deleted from client
- `getSessionHistory()` deleted from client
- `TranscriptRenderer` becomes the primary history/activity view (already exists, consumes classified events)

### 3.5 Approval notification path moves to accepted adapter

The legacy `Notifier.ApprovalRequired` call in `processSession` (triggered by screen heuristics — `isApprovalPrompt`) is removed. Approval notifications are driven exclusively by the accepted adapter's `DetectApproval` capability, which already feeds the `AuthoritativeApprovalStore`. A new notification hook fires from the store on first detection of a pending actionable approval (bounded, redacted summary only).

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
 └─ accepted adapter → AuthoritativeApprovalStore        (approvals)
     └─ first pending actionable → Notifier               (moved here)

GET /api/sessions
 ├─ SessionTelemetry{ ID, DisplayID, Adapter }
 ├─ SessionTelemetry{ Capabilities, AdapterCapabilities }
 ├─ SessionTelemetry{ LifecycleState }
 ├─ SessionTelemetry{ AgentKind, AgentStatus, AgentConfidence }
 ├─ SessionTelemetry{ AgentActivity }
 ├─ SessionTelemetry{ Approvals }
 └─ SessionTelemetry{ Stale, LastSuccessAt, LastError }

GET /api/sessions/{id}/transcript → TranscriptResponse   (sole history read path)
 ├─ semantic[]   (AgentEvent-sourced when correlated)
 ├─ fallback[]   (byte-stream when AgentEvent primary; snapshot always)
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

### 5.2 Additive changes (backward-compatible)

| Change | Field | Purpose |
| --- | --- | --- |
| Profile name on telemetry | `SessionTelemetry.ProfileID`, `SessionTelemetry.Name` (optional) | Display session profile and user-given name |
| Managed session marker | `SessionTelemetry.Managed` (bool, optional) | Distinguish managed (Codex/Claude) from PTY sessions |

### 5.3 Unchanged DTOs

| DTO | Status |
| --- | --- |
| `TranscriptSegment` (`t3.1`) | **frozen** — no change |
| `TranscriptResponse` (`t3.1`) | **frozen** — no change |
| `AgentActivityDTO` (S1) | **frozen** — no change |
| `SafeApprovalDTO` (B6) | **frozen** — no change |
| `SessionLifecycle` DTO | **frozen** — no change |
| Lifecycle action endpoints (`/stop`, `/kill`, `DELETE /{id}`) | **frozen** — no change |
| `/api/session-profiles` | **frozen** — no change |
| WebSocket `/term/ws` protocol | **frozen** — no change |

## 6. Generation / session identity rules

### 6.1 How PA2d generation-bound identity carries through PA3

The PA2d generation-bound identity model (SessionID + launch generation
from `transcript.RegisterOrReplaceLaunch`) carries through PA3 unchanged:

- **Transcript store**: segments are keyed by canonical `SessionID`. When a
  session is replaced (same canonical ID, new launch generation), the old
  Transcript store is cleared via `Service.ClearTranscript(sessionID)` before
  the new Recorder starts. The new generation starts with an empty Transcript.
  This is the existing production behavior in `LifecycleService` →
  `OwnedPTYRuntime` → `clearTranscript`.

- **Agent-activity store**: `AgentStatusStore` is generation-gated via
  `LaunchGen`. A replacement raises the non-current high-water mark via
  `Invalidate`, and a delayed write from the old generation is rejected.
  This is existing production behavior (S1.1-R3).

- **Approval store**: `AuthoritativeApprovalStore` is generation-gated.
  A replacement calls `SupersedeRuntime`, invalidating prior pending
  authority. This is existing production behavior (A1 R2-C).

- **Telemetry snapshot**: `mergeLifecycleState` already derives the
  authoritative `LifecycleState` from the `OwnedPTYRuntime` catalog,
  which is generation-bound. No change needed.

### 6.2 Does mobile need generation awareness?

**No.** Mobile does not need generation awareness:

- Mobile never constructs canonical IDs (daemon generates them at create).
- Mobile never asserts generation — it sends lifecycle requests by
  canonical ID only; the daemon resolves the current generation.
- Mobile Transcript reads are scoped to the canonical session. If a
  replacement clears the Transcript, the mobile simply sees an empty/fresh
  Transcript on next poll — it does not need to know *why*.
- Stale events from a replaced generation are rejected at the daemon
  (store-level `LaunchGen` gate), never reaching the mobile.

Mobile does need to handle the *absence* of data gracefully: a newly
created session has no Transcript segments yet; a replaced session's
Transcript starts empty.

## 7. Reconnect and stale-event behavior

### 7.1 Mobile reconnect data path

On reconnect (app foreground, network restore, WS reconnect):

1. Mobile calls `GET /api/sessions` → gets current session list with
   lifecycle states, agent activity, approvals.
2. For the active session, mobile calls `GET /api/sessions/{id}/transcript`
   (or with `?after=<lastKnownSeq>` for incremental poll).
3. Mobile reconnects WebSocket via ticket for live terminal bytes.

The daemon guarantees:
- Transcript segments are monotonic per-session (`Seq` field).
- A cleared Transcript (generation replacement) starts from `Seq=0`.
- Mobile detects the cleared Transcript by `Seq` reset and re-renders
  from scratch.
- `TranscriptResponse.primarySource` tells mobile whether the content
  is agent-event-sourced or byte-stream fallback.

### 7.2 Stale-event rejection

Stale events from replaced generations are rejected at multiple layers:

| Layer | Mechanism | Behavior |
| --- | --- | --- |
| Transcript store | `Service.ClearTranscript(sessionID)` on replacement | Old segments deleted before new Recorder starts |
| Agent-activity store | `LaunchGen` gate in `AgentStatusStore.Update` | Writes with `LaunchGen < current` rejected |
| Approval store | `SupersedeRuntime` on replacement | Prior pending approvals invalidated |
| Recorder | `DeleteRecorderIfSame(sessionID, oldRec)` on replacement | Old recorder stopped, subscribers closed |
| TerminalTransport | `Retire()` on replacement | Input/resize to old handle silently discarded |

Mobile cannot receive stale events because the store-level clear runs
before the new generation's Recorder starts feeding bytes.

## 8. Ordering and exactly-once guarantees

### 8.1 Transcript ordering

- `TranscriptSegment.Seq` is a monotonic per-session integer assigned at
  append time under the `Service.mu` lock. Within a generation, ordering is
  strictly monotonic.
- Across generations: a Clear resets Seq to 0. There is no cross-generation
  ordering — the old generation's segments are deleted.
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

- Byte-stream: `Recorder.readLoop` copies each PTY chunk (`make([]byte, n)`)
  and feeds it to `Service.FeedBytes`. The chunk queue has a single ordered
  worker. Overflow produces a `projection_gap` degraded marker, not silent
  loss.
- Agent-event: `TelemetryService.processSession` reads from the accepted
  adapter with an opaque cursor. The cursor advances only on successful
  projection. The adapter's `ReadEvents` is idempotent for the same
  input range. Re-reading the same cursor range produces the same events;
  duplicate event IDs are detected at the Transcript store layer
  (idempotent append by event ID).

## 9. Privacy and secret-handling

### 9.1 What must NOT appear in Transcript segments

The following must never appear in `TranscriptSegment.Text` or any other
Transcript field:

- Raw terminal input text (commands typed by user)
- API keys, tokens, secrets (`sk-*`, `ghp_*`, `xox*`, Bearer tokens)
- Absolute file paths containing home directory or project names
- Email addresses
- Hostnames, IP addresses
- Raw JSONL log content
- Raw agent prompts or thinking/scratchpad content
- Tool call arguments or results containing secrets
- Git diffs containing sensitive code
- Environment variable values

### 9.2 What must NOT appear in AgentActivity

`AgentActivityDTO` carries only `{contractVersion, status, provenance,
confidence, degraded, observedAt, stale}`. It must never carry:

- Raw errors or error messages
- Evidence, adapter records, or internal state
- Prompts, paths, or private metadata

### 9.3 What must NOT appear in SessionTelemetry

`SessionTelemetry` is the public session list. It must never carry:

- Raw terminal screen content (the `LastOutput` field is internal-only,
  never serialized)
- Log file paths
- Process PIDs, command lines, or CWDs
- Device tokens, pairing data, or raw approval prompts

### 9.4 Approval notification safety

Approval notifications (push) carry only:
- Session display name
- Agent kind
- Bounded summary (Pokit-owned, never the raw provider prompt)
- Non-sensitive action hint

They must never carry:
- Raw command, tool name, prompt text, or file paths
- Approval option payloads

### 9.5 Existing redaction gates (unchanged)

PA3 does not modify the existing redaction infrastructure:

- `internal/agent/testdata/` redacted fixtures (A1 inventory)
- `SafeApprovalDTO` bounded summary (B6)
- `TranscriptSegment` bounded text (`MaxTextBytes` = 32768)
- `boundedString` / `boundedText` helpers in transcript package
- `stripANSI` for terminal output cleaning

## 10. Migration sequence

### Step 1: Mobile cutover to Transcript read path (no backend changes)

- Mobile `FeedScreen` and `TranscriptRenderer` consume `getTranscript()`
  instead of `getActivityHistory()` / `getSessionHistory()`.
- Mobile `AgentCard` drops `state` field; uses `AgentActivity.status` +
  `AgentStatus`.
- Mobile `AgentCard` drops `events` field rendering.
- Verify: mobile `tsc --noEmit` passes; emulator renders Transcript.

**Gate**: Mobile compiles and renders Transcript without Activity/History APIs.
Backend unchanged — rollback is instant (revert mobile commit).

### Step 2: Remove ActivityBuffer and legacy EventStore from production

- Delete `internal/term/activity.go` (ActivityBuffer, ActivityEvent type,
  ActivityType constants).
- Delete `internal/term/eventstore.go` (EventStore interface,
  memoryEventStore).
- Delete `internal/term/activity_test.go`, `internal/term/eventstore_test.go`.
- Remove `ActivityBuffer` parameter from `NewTelemetryService`,
  `StartRecorder`, `EnsureRecorder`, `NewLifecycleService`,
  `NewOwnedPTYRuntime`.
- Remove `Activity` field from `Handlers` struct.
- Remove `activity` field from `App` struct.
- Remove `Events` parameter from all Handler constructors that no longer need it.
- Remove `Events` field from `Handlers` struct (replaced by Transcript).

**Gate**: `go build ./...` passes. No production code references
`ActivityBuffer`, `ActivityEvent`, `EventStore`, `memoryEventStore`.

### Step 3: Remove legacy API endpoints

- Remove `?activity=` branch from `HandleSessionsV2`.
- Remove `?history=` branch from `HandleSessionsV2`.
- Verify both return 404/400 for the removed query parameters.

**Gate**: HTTP route tests confirm removed endpoints return appropriate errors.

### Step 4: Simplify TelemetryService

- Remove `sessionStateData` struct and all associated fields
  (`LastOutput`, `LastActivity`, `State`, `Load`, `Cursor`, `Parser`,
  `SamplingFailures`).
- Remove `evaluateState`, `isApprovalPrompt`, `isThinkingFallback`
  screen heuristics.
- Remove legacy parser dispatch from `processSession` (the
  `parser.Parse(line)` loop and EventStore append).
- Remove screen read path from `processSession`
  (`ReadScreen`, `diffSize`, `tailLines`).
- Remove `Notifier` call from `processSession`.
- Remove log resolution from `processSession` (the `ResolveAgentLog`
  call, `LogRef`, `LogCursor` — these were only used by the legacy parser).
- Simplify `Snapshot()`: remove `sessionStateData` copy,
  `events` field population, `State`/`Load`/`Runner`/`RunnerColor`
  population. Keep lifecycle merge, agent detection, approval listing,
  agent-activity projection.
- Remove `ResolveAgentLog`, `findAgentProcess`, `ClaudeResolver`,
  `CodexResolver`, `GeminiResolver` resolution dispatch from
  `tracker.go` (only the `*Parser` types and screen heuristics used them).
- Remove `ResolveAgentLog` from `resolver.go`.
- Remove `AgentLogResolver` interface if no callers remain.
- Remove legacy `*Parser` types: `ClaudeParser`, `CodexParser`,
  `GeminiParser`, `AntigravityParser` (these are distinct from the
  accepted T0 contract adapters at `internal/agent/adapters/`).

**Gate**: `go build ./...` passes. `go test -race ./internal/term -count=1` passes.

### Step 5: Update SessionTelemetry DTO

- Remove `State`, `Load`, `Runner`, `RunnerColor`, `Events` from
  `SessionTelemetry` struct.
- Remove `sessionStateData` usage from `Snapshot()`.
- Update `mergeLifecycleState` retained-row construction (remove
  `State: "idle"`, `Runner`, `RunnerColor`; keep `LifecycleState`,
  `Capabilities: ["history"]`).
- Update `HandleSessionsV2` fallback path (`buildSimpleSnapshot`
  and `buildSimpleSnapshotWithDetector`).

**Gate**: `go build ./...`, `go vet ./...`, `go test -race ./internal/term
./cmd/devremote -count=1` pass.

### Step 6: Mobile DTO update

- Update `SessionTelemetry` TypeScript interface: remove `state`,
  `load`, `runner`, `runnerColor`, `events`. Add `profileId?`, `name?`.
- Update `AgentCard` to use `AgentActivity.status` for the activity
  indicator (replaces `state`).
- Update `FeedScreen` Activity tab to use Transcript exclusively.
- Remove `getActivityHistory()` and `getSessionHistory()` from client.
- Update `TranscriptRenderer` to render from `TranscriptResponse`.

**Gate**: `npx tsc --noEmit` passes.

### Step 7: Move approval notification to accepted adapter path

- Add notification hook to `AuthoritativeApprovalStore`: on first
  detection of a pending actionable approval, emit to `Notifier` with
  bounded summary (not raw screen output).
- Remove `isApprovalPrompt` and screen-based Notifier call from
  `processSession` (already removed in Step 4).

**Gate**: Approval notification contract test passes. No raw screen
content in notification payload.

### Step 8: Final cleanup and static verification

- Remove `models.AgentEvent` type if no production consumers remain
  (check managed Codex/Claude paths — they use `agent.AgentEvent` from
  the accepted contract, not `models.AgentEvent`).
- Remove `EventStore` interface if no consumers remain.
- Remove `CommandBroker` if unused after legacy parser removal
  (verify — `commandbroker.go` may be needed for debug endpoints).
- Static verification: zero production references to deleted types.

**Gate**: All static and dynamic gates pass (see §11).

## 11. Deletion gates

### 11.1 Files deleted

| File | Reason | Gate |
| --- | --- | --- |
| `internal/term/activity.go` | Replaced by Transcript byte-stream projection | Step 2 |
| `internal/term/activity_test.go` | No production code remains | Step 2 |
| `internal/term/eventstore.go` | Replaced by Transcript store | Step 2 |
| `internal/term/eventstore_test.go` | No production code remains | Step 2 |
| `internal/term/tracker.go` | Legacy log resolution (ResolveAgentLog, findAgentProcess) — no callers remain after Step 4 | Step 4 |
| `internal/term/resolver.go` | AgentLogResolver interface + ClaudeResolver, CodexResolver, GeminiResolver — no callers remain | Step 4 |
| `internal/term/parser.go` | Legacy AgentLogParser interface + ClaudeParser, CodexParser, GeminiParser — no callers remain | Step 4 |
| `internal/term/gemini_resolver.go` | GeminiResolver — no callers remain | Step 4 |
| `internal/term/gemini_parser.go` | GeminiParser — no callers remain | Step 4 |
| `internal/term/claude_parser.go` | ClaudeParser (legacy, distinct from `agent/adapters/claude`) | Step 4 |
| `internal/term/codex_parser.go` | CodexParser (legacy, distinct from `agent/adapters/codex`) | Step 4 |
| `internal/term/antigravity_parser.go` | AntigravityParser (legacy, distinct from accepted adapter) | Step 4 |
| `internal/term/claude_resolver.go` | ClaudeResolver (legacy) | Step 4 |
| `internal/term/codex_resolver.go` | CodexResolver (legacy) | Step 4 |

### 11.2 API endpoints removed

| Endpoint | Reason | Gate |
| --- | --- | --- |
| `GET /api/sessions?activity=<id>` | Replaced by `GET /api/sessions/{id}/transcript` | Step 3 |
| `GET /api/sessions?history=<id>` | Replaced by `GET /api/sessions/{id}/transcript` | Step 3 |

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
| `TelemetryService` | `notifier Notifier` | If moved to ApprovalStore; otherwise retained for approval notifications |

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
7. **Reconnect**: Mobile reconnects, calls Transcript API, gets current
   state. No stale events from prior generation.
8. **Generation replacement**: Replace session → old Transcript cleared →
   new Transcript starts fresh → mobile sees reset.
9. **Mobile rendering**: Unknown agentKind/status → graceful fallback.
   Missing optional fields → no crash.
10. **Privacy**: Transcript segments contain no raw input, secrets, paths,
    or prompts (spot-check with known-sensitive PTY output).

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
  the A1/B6/B7 approval infrastructure is frozen.
- **Transcript contract version bump** — `t3.1` is frozen. No new segment
  kinds, sources, or envelope changes.
- **Canonical Timeline, notifications infrastructure, Grok, or
  Navigator work** — separate phases.
- **New agent adapter addition** — no new parser, detector, or resolver.
- **Adapter Doctor/Repair** — D1 is a separate phase.
- **Daemon-restart persistence for Transcript** — Transcript remains
  in-memory (non-durable). This is a documented limitation carried
  forward from T3.
- **Raw Transcript attachments** — out of MVP scope per the T project plan.
- **Mobile push notification infrastructure** — the Notifier interface
  may move but the push delivery mechanism is unchanged.
- **New session profiles or custom command support** — profile list is
  frozen (shell, codex, claude).
- **Per-session ACL or multi-device role UI** — M3 non-goal.
- **Nested-agent-only interrupt** — M3 non-goal.
- **Process restart/resume** — out of scope.

## 14. Rollback points

Each migration step is independently revertible:

1. **Step 1 (mobile cutover)**: Revert mobile commits. Backend unchanged.
2. **Step 2 (ActivityBuffer/EventStore deletion)**: Revert to prior SHA.
   No data migration — all stores are in-memory.
3. **Step 3 (API endpoint removal)**: Revert to prior SHA. Endpoints restored.
4. **Step 4 (TelemetryService simplification)**: Revert to prior SHA.
   Full legacy state machine restored.
5. **Step 5 (DTO update)**: Revert to prior SHA. Old fields restored.
6. **Step 6 (mobile DTO)**: Revert mobile commits.
7. **Step 7 (notification path)**: Revert to prior SHA.
8. **Step 8 (final cleanup)**: Revert individual file deletions.

The PA2d accepted SHA (`b93c521b7de45f3f577805dbb7de505e16f3172d`) is the
authoritative rollback anchor for the entire PA3 change set.

## 15. Frozen-unaffected boundaries

These accepted paths must not be modified by PA3:

- `internal/mux/` — all adapter files (tmux, cmux, localpty, controlled_pty)
- `internal/mux/adapter.go` — Adapter interface, capability interfaces
- `internal/mux/registry.go` — Registry, snapshot caching
- `internal/sessionid/` — SessionRef, ParseSessionID, Canonical
- `internal/term/terminal_transport.go` — TerminalTransport (PA2d)
- `internal/term/owned_pty_runtime.go` — OwnedPTYRuntime (PA2c)
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
- `internal/transcript/arbitration.go` — SourceArbiter, TranscriptResponse
- `internal/transcript/projector_agent.go` — AgentEventProjector
- `internal/transcript/projector_bytes.go` — ByteStreamProjector
- `internal/transcript/store.go` — Store
- `internal/transcript/service.go` — Service (may gain notification hook; core API unchanged)
- `internal/transcript/api.go` — HandleTranscript, HandleTranscriptStats
- `internal/devicetrust/` — all device trust infrastructure (M2.5)
- `internal/watcher/` — file watcher
- `mobile/src/lib/client.ts` — Transcript API functions (unchanged);
  lifecycle functions (unchanged); device auth (unchanged)
- `mobile/src/lib/agentActivity.ts` — AgentActivity derivation (unchanged)
- `mobile/src/lib/agentDisplay.ts` — agent display helpers (unchanged)
- `mobile/src/lib/transcriptClassify.ts` — transcript classification (unchanged)

## 16. Commit and review discipline

1. Each migration step (1–8) is a separate implementation with its own
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
