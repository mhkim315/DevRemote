# PA3 Planning Evidence

Status: **EVIDENCE — read-only code inspection for PA3 contract proposal**
Date: 2026-07-19
Baseline SHA: `b93c521b7de45f3f577805dbb7de505e16f3172d` (PA2d ACCEPTED)

This document records the concrete code inspection findings that inform the
PA3 contract proposal at `docs/PA3_CONTRACT.md`. It does not propose changes;
it catalogs what exists today.

## 1. ActivityBuffer — current authority and consumers

### 1.1 Definition

**File**: `internal/term/activity.go`

```go
type ActivityBuffer struct {
    mu       sync.Mutex
    events   map[string][]ActivityEvent // sessionID → events
    seqs     map[string]uint64          // sessionID → next seq
    capacity int
}
```

`ActivityEvent` DTO:
```go
type ActivityEvent struct {
    ID        string       `json:"id"`
    Seq       uint64       `json:"seq"`
    SessionID string       `json:"sessionId"`
    Type      ActivityType `json:"type"`  // terminal_output, terminal_input, system, status
    Text      string       `json:"text"`
    Bytes     int          `json:"bytes,omitempty"`
    Hash      string       `json:"hash,omitempty"`
    Timestamp time.Time    `json:"timestamp"`
}
```

### 1.2 Producers (who writes to ActivityBuffer)

Only ONE producer exists: `Recorder.readLoop` (line 336-350 of `recorder.go`):

```go
if r.activity != nil {
    text := stripANSI(string(payload))
    text = strings.ReplaceAll(text, "\r", "\n")
    if !isANSIControlOnly(text) && len(text) > 3 {
        if len(text) > 32768 {
            text = text[:32768]
        }
        r.activity.Append(ActivityEvent{
            SessionID: r.sessionID,
            Type:      ActivityTerminalOutput,
            Text:      text,
            Bytes:     n,
        })
    }
}
```

cmux delta frames also write to ActivityBuffer (line 298-306):

```go
if r.activity != nil {
    text := stripANSI(string(payload))
    text = strings.ReplaceAll(text, "\r", "\n")
    if !isANSIControlOnly(text) && len(text) > 3 {
        ...
        r.activity.Append(ActivityEvent{...})
    }
}
```

### 1.3 Consumers (who reads ActivityBuffer)

| Consumer | File:Line | Context |
| --- | --- | --- |
| ActivityBuffer.List | `telemetry.go:303` | `GET /api/sessions?activity=` → `HandleSessionsV2` |
| ActivityBuffer.Clear | `lifecycle_service.go` (via LifecycleService) | Session delete clears activity |
| ActivityBuffer creation | `cmd/devremote/app.go:187` | `NewActivityBuffer(2000)` in NewAppWithDeps |
| ActivityBuffer injection | `app.go` → `Handlers.Activity` | Passed to Handlers |
| ActivityBuffer injection | `app.go` → `NewTelemetryService` | Passed to TelemetryService |
| ActivityBuffer injection | `app.go` → `NewOwnedPTYRuntime` | Passed to OwnedPTYRuntime |
| ActivityBuffer injection | `app.go` → `NewLifecycleService` | Passed to LifecycleService |

### 1.4 Summary

ActivityBuffer is a separate in-memory store that captures **the same PTY
bytes** that Transcript's byte-stream projector also captures. Both paths
run in the same `Recorder.readLoop`. The key differences:
- ActivityBuffer strips ANSI and merges adjacent chunks.
- Transcript byte-stream projector preserves raw bytes and uses ANSI-stateful
  projection with newline-bounded commits.
- ActivityBuffer serves `GET /api/sessions?activity=`; Transcript serves
  `GET /api/sessions/{id}/transcript`.

This is the primary motivation for PA3: eliminate the duplication.

---

## 2. EventStore / memoryEventStore — current authority and consumers

### 2.1 Definition

**File**: `internal/term/eventstore.go`

```go
type EventStore interface {
    Emit(session, eventType, summary, detail string)
    Append(session string, events []models.AgentEvent)
    List(session string) []models.AgentEvent
    Clear(session string)
}
```

In-memory only. Max 500 events/session.

### 2.2 Producers

| Producer | File:Line | Context |
| --- | --- | --- |
| TelemetryService.processSession (legacy parser) | `telemetry_service.go:335-336` | `s.events.Append(id, newEvents)` — from screen-parsed lines |
| Claude/Codex managed events | `managed_events.go` | Managed Codex/Claude events appended to EventStore |

### 2.3 Consumers

| Consumer | File:Line | Context |
| --- | --- | --- |
| SessionTelemetry.Events | `telemetry.go:415` | `events := s.events.List(compoundID)` — in Snapshot() |
| SessionTelemetry.Events | `telemetry.go:408` | `evts := events.List(compoundID)` — in buildSimpleSnapshot |
| HandleSessionsV2 (?history=) | `telemetry.go:315-316` | `events := h.Events.List(historyID)` |
| EventStore.Clear | `telemetry_service.go` | Via TelemetryService.Clear on session cleanup |
| IPC replay path | `ipc.go` | IPC subscriber receives EventStore data |

### 2.4 Summary

EventStore is a legacy store for screen-parsed `AgentEvent` records (idle/
thinking/working/waiting state derived from log file parsing). The accepted
adapter path (T0 contract agents) feeds directly into Transcript and
AgentStatusStore — it does NOT use EventStore as an intermediary.

---

## 3. Transcript Service — current authority

### 3.1 Definition

**Package**: `internal/transcript/`

**Files**:
- `contract.go` — `TranscriptSegment`, `SegmentKind`, `SegmentSource`, bounds
- `arbitration.go` — `SourceArbiter`, `TranscriptResponse`, source separation
- `service.go` — `Service` (store, projectors, arbiters, queues)
- `store.go` — in-memory `Store` with eviction
- `projector_bytes.go` — `ByteStreamProjector` (ANSI-stateful)
- `projector_agent.go` — `AgentEventProjector` (agent event → segments)
- `api.go` — `HandleTranscript`, `HandleTranscriptStats`
- `chunk_queue.go` — bounded non-blocking chunk queue worker
- `launch_binding.go` — generation-bound launch binding

### 3.2 Feed paths

1. **Byte-stream**: `Recorder.readLoop` → `Service.FeedBytes` → `chunkQueue`
   → worker → `ByteStreamProjector.Feed` → `Store.Append`
2. **Agent-event**: `TelemetryService.processSession` → accepted adapter
   `ReadEvents` → `Service.SetCorrelation` → `Service.ProjectAgentEvents`
   → `AgentEventProjector.ProjectBatch` → `Store.Append`
3. **Snapshot (cmux degraded)**: `Recorder.readLoop` → `Service.AddSnapshotSegment`

### 3.3 Read API

`GET /api/sessions/{id}/transcript[?after=<seq>][&limit=<n>]` → `TranscriptResponse`

Mobile client validates response strictly (`validateTranscriptResponse`):
- Validates segment IDs, seq ordering, kind/source cross-validation
- Rejects unknown fields
- Validates `contractVersion === "t3.1"`
- Max 10000 segments per response

### 3.4 Summary

Transcript is already the most complete and well-structured read path.
It supports incremental reads (`?after=`), dual-channel separation,
source arbitration, and bounded storage. PA3 makes it the **sole** read path.

---

## 4. TelemetryService — current complexity

### 4.1 Current responsibilities (all in one type)

| Responsibility | Code location | PA3 disposition |
| --- | --- | --- |
| Background 2s polling loop | `Run()` | Retained |
| Session reconciliation (add/prune) | `reconcileSessions()` | Retained |
| Log file resolution | `resolveLog()` → `ResolveAgentLog()` | **Deleted** |
| Legacy parser dispatch | `processSession` parser loop | **Deleted** |
| Screen reading | `processSession` ReadScreen + tailLines | **Deleted** |
| State machine (idle/thinking/working/waiting) | `evaluateState()` | **Deleted** |
| Approval prompt detection (screen) | `isApprovalPrompt()` → Notifier | **Deleted** (moves to ApprovalStore) |
| Accepted adapter path | `callAcceptedAdapter()` → Transcript + Status + Approval | Retained |
| Agent detection bridge | `Snapshot()` → `DetectAgent()` | Retained |
| Approval listing | `Snapshot()` → `ListSafe()` | Retained |
| Agent activity projection | `Snapshot()` → `AgentStatusStore.Current()` | Retained |
| Launch replacement | `RegisterOrReplaceLaunch()` | Retained |
| Delivery gate sync | `SetDeliveryGate()` → activate/deactivate | Retained |

### 4.2 sessionStateData (target for deletion)

```go
type sessionStateData struct {
    LastOutput   []byte
    LastActivity time.Time
    State        string       // "idle"/"thinking"/"working"/"waiting"
    Load         int          // 0-100
    Runner       string
    RunnerColor  string
    Cursor       *LogCursor
    Parser       AgentLogParser
    Adapter      *adapterState  // RETAINED — accepted adapter ingestion state
    SamplingFailures int
}
```

Everything except `Adapter` is deleted in PA3. The `Adapter` field (accepted
adapter ingestion state) is retained and may move to a simpler location.

### 4.3 Process flow to remove

The entire section `telemetry_service.go` lines 155-350 implements:

```
processSession
  → resolveLog (ps, ProcessInfo)
  → rawLines = readRawLines(cursor)
  → accepted adapter path (RETAINED: Transcript.ProjectAgentEvents etc.)
  → legacy parser: for each line { parser.Parse(line); append to EventStore }
  → screen read: ReadScreen → tailLines → isApprovalPrompt → Notifier
  → state machine: evaluateState, preserveTransientSamplingFailure
```

After PA3, only the accepted adapter path remains. The legacy parser,
screen read, and state machine are removed.

---

## 5. Mobile consumption paths

### 5.1 Current API calls

| API | Client function | Component consumer |
| --- | --- | --- |
| `GET /api/sessions` | `listSessions()` | Dashboard (AgentCard list), FeedScreen |
| `GET /api/sessions?activity=` | `getActivityHistory()` | FeedScreen Activity tab |
| `GET /api/sessions?history=` | `getSessionHistory()` | FeedScreen History tab |
| `GET /api/sessions/{id}/transcript` | `getTranscript()` | Not yet wired as primary UI |
| `GET /api/session-profiles` | `listSessionProfiles()` | NewSessionModal |
| `POST /api/sessions` | `createSession()` | NewSessionModal |
| `POST /api/sessions/{id}/stop` | `stopSession()` | AgentCard (lifecycle) |
| `POST /api/sessions/{id}/kill` | `killSession()` | AgentCard (lifecycle) |
| `DELETE /api/sessions/{id}` | `deleteSessionHistory()` | AgentCard (lifecycle) |

### 5.2 SessionTelemetry TypeScript interface (mobile/src/components/AgentCard.tsx)

Current fields consumed:
```typescript
export interface SessionTelemetry {
  id: string;
  displayId?: string;
  state: 'idle' | 'thinking' | 'working' | 'waiting';  // DELETED in PA3
  lifecycleState?: string;
  load: number;                                          // DELETED in PA3
  runner?: string;                                       // DELETED in PA3
  runnerColor?: string;                                  // DELETED in PA3
  adapter?: string;
  capabilities?: string[];
  adapterCapabilities?: string[];
  isAddBtn?: boolean;
  events?: AgentEvent[];                                 // DELETED in PA3
  agentKind?: string;
  agentStatus?: string;
  agentConfidence?: number;
  approvals?: SafeApproval[];
  agentActivity?: AgentActivity;
}
```

### 5.3 Transcript API already has strict mobile validation

The mobile client (`client.ts` lines 446-561) already has a complete,
strict `validateTranscriptResponse` function that validates the Transcript
response envelope, segment ordering, field bounds, kind/source cross-validation,
and contract version. This is ready to become the primary read path.

---

## 6. Recorder — current activity coupling

### 6.1 Activity append locations in readLoop

1. **Byte-stream output** (line 336-350): after ANSI strip → `r.activity.Append(ActivityTerminalOutput)`
2. **cmux delta frames** (line 298-306): after delta marker strip → `r.activity.Append(ActivityTerminalOutput)`
3. **cmux snapshot exclusion**: snapshots are NOT appended to ActivityBuffer (line 323-331, broadcast only)

### 6.2 Transcript feed locations in readLoop

1. **Byte-stream feed** (line 363): `r.feedTranscript(payload)` — raw bytes, non-blocking
2. **TUI boundary detection** (lines 354-359): `BeginTUIBurst` / `EndTUIBurst`
3. **cmux delta** (lines 310-319): `AddSnapshotSegment` (degraded snapshot path)
4. **Queue management**: `EnableQueue` on start, `CloseSessionQueue` on stop

### 6.3 Both paths feed from the same raw payload

ActivityBuffer and Transcript byte-stream both receive the same PTY chunks.
ActivityBuffer strips ANSI first; Transcript processes ANSI statefully.
This duplication is eliminated in PA3 by removing the ActivityBuffer feed
and keeping only the Transcript feed.

---

## 7. API route inventory (current)

### 7.1 Authenticated routes (from `app.go` lines 406-431)

```
GET    /api/sessions                         → HandleSessionsV2
POST   /api/sessions                         → HandleSessionsAPI
POST   /api/sessions/{id}/stop               → HandleSessionStop
POST   /api/sessions/{id}/kill               → HandleSessionKill
DELETE /api/sessions/{id}                    → HandleSessionDelete
GET    /api/session-profiles                 → HandleSessionProfiles
POST   /api/sessions/{id}/approvals/{id}     → HandleApprovalAction
GET    /api/sessions/{id}/native-status      → HandleManagedNativeStatus
GET    /api/managed-sessions                 → HandleManagedSessions
GET    /api/managed-sessions/{id}/events     → HandleManagedSessionEvents
POST   /api/managed-sessions/{id}/prompt     → HandleManagedSessionPrompt
POST   /api/managed-sessions/{id}/stop       → HandleManagedSessionStop
POST   /api/managed-sessions/{id}/kill       → HandleManagedSessionKill
DELETE /api/managed-sessions/{id}            → HandleManagedSessionDelete
POST   /api/managed-claude-sessions/{id}/stop → HandleManagedClaudeSessionStop
POST   /api/managed-claude-sessions/{id}/kill → HandleManagedClaudeSessionKill
DELETE /api/managed-claude-sessions/{id}     → HandleManagedClaudeSessionDelete
GET    /term/ws                              → HandleWS
POST   /term/size                            → HandleTermSize
GET    /term/                                → HandleHTML
POST   /push/register                        → registerPush
GET    /debug/dump                           → HandleDump
POST   /debug/cmd                            → HandleCmd
GET    /debug/diag                           → HandleDiagnostic
GET    /debug/e8diag                         → HandleE8Diag
GET    /api/sessions/{id}/transcript         → HandleTranscript
GET    /api/sessions/{id}/transcript/stats   → HandleTranscriptStats
```

### 7.2 Query-parameter sub-routes within HandleSessionsV2

The `GET /api/sessions` handler (`HandleSessionsV2`, `telemetry.go:296`)
dispatches three different behaviors based on query parameters:

| Query | Behavior | PA3 disposition |
| --- | --- | --- |
| `?activity=<id>` | `ActivityBuffer.List(id)` → JSON | **Deleted** |
| `?history=<id>` | `EventStore.List(id)` with `ScreenReader` fallback | **Deleted** |
| (none) | Full `SessionTelemetry` snapshot | **Retained** (fields trimmed) |

---

## 8. Deletion impact analysis

### 8.1 Files with confirmed ActivityBuffer dependency

```
internal/term/activity.go           — definition (DELETE)
internal/term/activity_test.go      — tests (DELETE)
internal/term/recorder.go           — Append calls in readLoop (MODIFY: remove Append)
internal/term/telemetry.go          — HandleSessionsV2 ?activity= branch (MODIFY: remove)
internal/term/telemetry_service.go  — activity field in struct (MODIFY: remove field)
internal/term/runtime.go            — Activity field in Handlers (MODIFY: remove field)
internal/term/lifecycle_service.go  — ActivityBuffer.Clear on delete (MODIFY: use Transcript)
internal/term/lifecycle.go          — ActivityBuffer parameter (MODIFY: remove parameter)
internal/term/owned_pty_runtime.go  — ActivityBuffer parameter (MODIFY: remove parameter)
internal/term/create.go             — ActivityBuffer parameter (MODIFY: remove parameter)
cmd/devremote/app.go                — ActivityBuffer creation + wiring (MODIFY: remove)
```

### 8.2 Files with confirmed EventStore dependency

```
internal/term/eventstore.go         — definition (DELETE)
internal/term/eventstore_test.go    — tests (DELETE)
internal/term/telemetry.go          — HandleSessionsV2 ?history= branch, Snapshot events (MODIFY)
internal/term/telemetry_service.go  — events field + legacy parser append (MODIFY)
internal/term/runtime.go            — Events field in Handlers (MODIFY: remove field)
internal/term/ipc.go                — EventStore usage in IPC (MODIFY — verify)
internal/term/managed_events.go     — EventStore usage for managed events (MODIFY — verify)
cmd/devremote/app.go                — EventStore creation + wiring (MODIFY: remove)
```

### 8.3 Files with confirmed legacy parser/resolver dependency

```
internal/term/tracker.go            — ResolveAgentLog, findAgentProcess (DELETE)
internal/term/resolver.go           — AgentLogResolver interface (DELETE)
internal/term/parser.go             — AgentLogParser interface, ClaudeParser, CodexParser (DELETE)
internal/term/gemini_resolver.go    — GeminiResolver (DELETE)
internal/term/gemini_parser.go      — GeminiParser (DELETE)
internal/term/claude_parser.go      — ClaudeParser legacy (DELETE)
internal/term/codex_parser.go       — CodexParser legacy (DELETE)
internal/term/antigravity_parser.go — AntigravityParser legacy (DELETE)
internal/term/claude_resolver.go    — ClaudeResolver legacy (DELETE)
internal/term/codex_resolver.go     — CodexResolver legacy (DELETE)
internal/term/resolver_test.go      — resolver tests (DELETE)
internal/term/parser_test.go        — parser tests (DELETE)
internal/term/telemetry_service.go  — resolveLog, readRawLines, parser loop (MODIFY)
```

---

## 9. Type inventory to delete

### 9.1 Go types

| Type | Package | Reason |
| --- | --- | --- |
| `ActivityBuffer` | `internal/term` | Duplicate of Transcript byte-stream |
| `ActivityEvent` | `internal/term` | Replaced by TranscriptSegment |
| `ActivityType` | `internal/term` | Enum only used by ActivityEvent |
| `EventStore` (interface) | `internal/term` | Replaced by Transcript store |
| `memoryEventStore` | `internal/term` | Implementation of EventStore |
| `sessionStateData` | `internal/term` | Legacy state machine state |
| `Notifier` (interface) | `internal/term` | May move to approval package |
| `AgentLogParser` (interface) | `internal/term` | Legacy parser interface |
| `AgentLogResolver` (interface) | `internal/term` | Legacy resolver interface |
| `LogRef` | `internal/term` | Only used by legacy path |
| `LogCursor` | `internal/term` | Only used by legacy path |
| `ClaudeParser` | `internal/term` | Legacy screen/log parser |
| `CodexParser` | `internal/term` | Legacy screen/log parser |
| `GeminiParser` | `internal/term` | Legacy screen/log parser |
| `AntigravityParser` | `internal/term` | Legacy screen/log parser |
| `ClaudeResolver` | `internal/term` | Legacy log resolver |
| `CodexResolver` | `internal/term` | Legacy log resolver |
| `GeminiResolver` | `internal/term` | Legacy log resolver |

### 9.2 TypeScript types

| Type | File | Reason |
| --- | --- | --- |
| `SessionTelemetry.state` | `AgentCard.tsx` | Replaced by AgentActivity.status |
| `SessionTelemetry.load` | `AgentCard.tsx` | No longer computed |
| `SessionTelemetry.runner` | `AgentCard.tsx` | Replaced by AgentKind |
| `SessionTelemetry.runnerColor` | `AgentCard.tsx` | No longer needed |
| `SessionTelemetry.events` | `AgentCard.tsx` | Moved to Transcript API |
| `AgentEvent` (in AgentCard.tsx) | `AgentCard.tsx` | Only used by events[] |

---

## 10. Test impact

### 10.1 Tests that must be rewritten (not just deleted)

- `activity_test.go` → DELETE (tests for deleted type)
- `eventstore_test.go` → DELETE (tests for deleted type)
- `telemetry_test.go` → MODIFY (remove state machine assertions)
- `telemetry_service_test.go` → MODIFY (remove parser/resolver assertions)
- `recorder_test.go` → MODIFY (remove ActivityBuffer assertions)
- `fixture_e2e_test.go` → MODIFY (update DTO assertions)
- `api_golden_test.go` → MODIFY (update golden fixtures for new DTO)
- `phase7_golden_test.go` → MODIFY (update golden fixtures)
- `lifecycle_test.go` → MODIFY (remove ActivityBuffer assertions)
- `managed_api_test.go` → MODIFY (if EventStore-dependent)
- `managed_api_b_test.go` → MODIFY (if EventStore-dependent)
- `parser_test.go` → DELETE (tests for deleted types)
- `resolver_test.go` → DELETE (tests for deleted types) (?if separate)
- `agent_activity_api_test.go` → VERIFY (may reference removed fields)
- `agent_agnostic_test.go` → VERIFY (may reference removed fields)

### 10.2 Tests that must be added

1. Transcript becomes sole history store (no activity/history fallback)
2. Telemetry snapshot excludes deleted fields
3. Telemetry snapshot includes required fields
4. ActivityBuffer zero-production-reference static check
5. EventStore zero-production-reference static check
6. Recorder single-reader invariant preserved
7. Transcript ordering across generation replacement
8. Mobile Transcript integration (strict validation path)
9. Mobile graceful handling of unknown agentKind/status
10. Privacy: Transcript segments contain no secrets

---

## 11. Frozen files (must not be modified)

See PA3_CONTRACT.md §15 for the complete list. Key categories:

- All `internal/mux/` adapter files
- All `internal/agent/` files
- All `internal/transcript/contract.go`, `arbitration.go`, `store.go`, `api.go`
- All `internal/term/managed_*.go`, `approval_*.go`, `agent_status_store.go`
- All `internal/devicetrust/` files
- `internal/term/terminal_transport.go`, `owned_pty_runtime.go`, `lifecycle_*.go`
- `mobile/src/lib/client.ts` lifecycle + Transcript functions, `agentActivity.ts`, `agentDisplay.ts`, `transcriptClassify.ts`

---

## 12. PA3 contract proposal gate results (R4 baseline)

Baseline SHA: `52c71ae055fe584be27f563413b8af62fa4000e1`
Gate run date: 2026-07-19
No production code changed; supervisor directory already removed at R1.

### 12.1 go build (unfiltered, all packages)

```sh
$ cd companion-daemon && go build ./...
(no output — success)
```

### 12.2 go vet (unfiltered, all packages)

```sh
$ cd companion-daemon && go vet ./...
(no output — success)
```

### 12.3 PA2 regression tests (unfiltered, race detector)

```sh
$ cd companion-daemon && go test -race ./internal/term ./internal/mux ./cmd/devremote -count=1
ok  	devremote/companion-daemon/internal/term	25.701s
ok  	devremote/companion-daemon/internal/mux	6.708s
ok  	devremote/companion-daemon/cmd/devremote	33.967s
```

All three packages pass with race detector enabled. No test regressions from PA2d baseline.

### 12.4 git diff --check

```sh
$ git diff --check
(no output — clean)
```

### 12.5 Secret scan (unfiltered, all changed files)

```sh
$ grep -rn "sk-[A-Za-z0-9]\{20,\}\|ghp_[A-Za-z0-9]\{20,\}\|xox[baprs]-[A-Za-z0-9]\{20,\}" \
  docs/PA3_CONTRACT.md companion-daemon/docs/PA3_PLANNING_EVIDENCE.md
(no output — clean)
```

No actual secrets in any changed file.

### 12.6 gofmt

No `.go` files were modified — not applicable (all changes are `.md` only).

### 12.7 Mobile gate

Not run — no mobile source changes exist. Report as `not-run`.
