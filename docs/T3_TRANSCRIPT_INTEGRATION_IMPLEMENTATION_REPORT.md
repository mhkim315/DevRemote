# T3 Transcript Integration — Implementation Report

Status: **READY FOR INDEPENDENT REVIEW**

Branch: `feature/phase10-multi-adapter`

## Baseline and final SHAs

```text
accepted D1 / T3 baseline: 8f7c22def81abf0b932f6dbbacc07325ae2bb12e
handoff document:          68eccf1f7967860195814f337297b7249a7d4b33
final implementation tip:  <will be set after commit>
remediated tip (BLOCKER 1-7 fixes): <will be set after commit>
```

Ancestry verification — all accepted phases are ancestors of HEAD:

```text
T0  3ce2604bd333dcb63142b5b1710185a823162efa  OK
T1  162266f830caaf07bf701d9a1294557432855769  OK
T2  ef4a162c7f9a5644fd52d89501e97f4e62301dfa  OK
R2  6f940b03bbb451dd0ddcfc6bf7ce5f86bbec3b50  OK
D1  8f7c22def81abf0b932f6dbbacc07325ae2bb12e  OK
```

## 1. Code-path audit and legacy-heuristic disposition

### Current Transcript producers

| Producer | Package | Output | Consumer |
|---|---|---|---|
| Recorder readLoop | term/recorder.go | ActivityBuffer (ActivityEvent) | GET /api/sessions?activity= |
| TelemetryService | term/telemetry_service.go | EventStore (models.AgentEvent) | GET /api/sessions?history= |
| cmux delta frame | mux/cmux_delta.go | ActivityBuffer via Recorder | same as Recorder |

### Current Transcript readers

| Path | Transport | Auth | Mobile consumer |
|---|---|---|---|
| GET /api/sessions?activity=ID | HTTP REST | AuthMiddleware / RequirePrincipal | E8g2Transcript (FeedScreen) |
| GET /api/sessions?history=ID | HTTP REST | AuthMiddleware / RequirePrincipal | EventBubble (Activity tab), HistoryModal |
| GET /api/sessions | HTTP REST | AuthMiddleware / RequirePrincipal | SessionTelemetry DTO |

### Legacy heuristics disposition

| Heuristic | Location | T3 treatment |
|---|---|---|
| Agent detection by process name | agent/detector.go | Unchanged (S1 scope) |
| Screen-text-based agent detection | agent/detector.go | Unchanged (S1 scope) |
| cmux snapshot delta extraction | mux/cmux_delta.go | **Separated**: snapshot isolation (§6.4), never runs on byte-stream input |
| ActivityBuffer text merging | term/activity.go | **Preserved**: legacy path retained; new T3 path uses byte-stream projector with explicit newline boundaries |
| ANSI stripping in Recorder | term/recorder.go | **Preserved**: raw bytes still broadcast unchanged; T3 projector strips ANSI independently |
| cmux screen tracker (isVolatileLine) | mux/cmux_delta.go | **Separated**: cmux best-effort remains separate projector with degraded provenance |

## 2. T3-A: Versioned Transcript contract and bounded store

### New package: `internal/transcript/`

**contract.go** — Versioned `t3.1` contract:
- `TranscriptSegment`: immutable per-session segment with stable ID, monotonic `Seq`, explicit `Kind` and `Source`
- Kind vocabulary: `agent_event`, `terminal_output`, `input_boundary`, `degraded`, `ui_omitted`, `unknown`
- Source provenance: `agent_event`, `byte_stream`, `snapshot_delta`, `unknown`
- Bounds: `MaxTextBytes` (32KB), `MaxSegmentsPerSession` (5000), `MaxTotalBytesPerSession` (16MB), `MaxDegradedReasonBytes` (256)

**store.go** — Bounded session-isolated store:
- `Store`: concurrent-safe map of per-session segment slices with `sync.Mutex`
- Session-isolated capacity eviction with gap marker insertion
- `Append`, `List`, `ListAfter` (cursor-based incremental read), `Clear`, `Stats`
- `Cursor`: atomic cursor for non-blocking incremental reads

### No changes to frozen contracts
- T0 `contract.AgentEvent` unchanged
- T1 Codex adapter unchanged
- T2 Claude adapter unchanged
- D1 Doctor unchanged
- `internal/agent/models.go` unchanged

## 3. T3-B: AgentEvent production projection and source arbitration

**projector_agent.go** — `AgentEventProjector`:
- Projects `agent.AgentEvent` → `TranscriptSegment` with strict field allowlisting
- **Redacted fields**: thinking text, tool call input/output, user messages with code-like content, private metadata
- **Allowlisted fields**: assistant message text, approval prompts, tool names (identity only), event types, confidence
- Cross-session events rejected (SessionID mismatch)
- Advisory provenance events projected at reduced confidence

**arbitration.go** — `SourceArbiter`:
- AgentEvent segments are PRIMARY when available
- Byte-stream segments are SEPARATE fallback
- Never merges, correlates, or deduplicates across sources
- Explicit `SegmentSource` label on every segment

## 4. T3-C: Byte-stream fallback, echo privacy, snapshot isolation

**projector_bytes.go** — `ByteStreamProjector`:
- Processes Recorder PTY chunks character-by-character
- Commits at stable newline/CR boundaries
- Backspace handling for erase operations
- Queue overflow → bounded degraded marker, never blocks Recorder

### Echo privacy (content-free input boundary)
- `BeginInput(sessionID)` → suppresses ALL subsequent bytes
- `EndInput(sessionID)` → resumes normal projection
- Input boundary marker is **content-free**: no typed command, prompt text, or timing
- PTY echo bytes are dropped, never projected
- Proven by `TestEchoPrivacy`: typed command bytes never enter Transcript

### Snapshot isolation
- cmux `screen_snapshot_delta` remains a separate best-effort projector
- `extractDelta`, `screenTracker` unchanged in mux/cmux_delta.go
- cmux sentinel detection in Recorder unchanged
- Byte-stream projector never processes snapshot input

### Recorder integration
- `Recorder.transcriptSvc`: optional non-blocking feed to Transcript byte-stream projector
- Set via `term.SetTranscriptService()` during app initialization
- Feed is asynchronous (goroutine with copied buffer) — never blocks raw PTY delivery
- Recorder remains the SINGLE PTY reader (invariant preserved)

## 5. T3-D: Authenticated API and actual mobile consumer

### API endpoints (new)

```
GET /api/sessions/{id}/transcript        — list all segments, oldest-first
GET /api/sessions/{id}/transcript?after=<seq> — incremental read after cursor
GET /api/sessions/{id}/transcript/stats  — diagnostic counters (optional)
```

### Authentication
- Insecure local mode: `AuthMiddleware` (Supabase JWT / dev-token)
- Remote production mode: `RequirePrincipal(sessionMgr, ..., PermSessionsRead)`
- No Transcript-specific credential path — reuses accepted central auth

### Mobile consumer (`mobile/src/`)

**client.ts** — New functions:
- `TranscriptSegment` interface (TypeScript)
- `getTranscript(sessionID, token?, after?)` — calls T3 API with 404→legacy fallback

**FeedScreen.tsx** — Updated `E8g2Transcript` component:
- Renders `TranscriptSegment[]` with kind-aware display:
  - `agent_event`: shows agent-kind label + event type + safe text
  - `terminal_output`: full-width monospace text (backward compatible)
  - `input_boundary`: visual divider
  - `degraded` / `ui_omitted`: styled diagnostic text
- Legacy `ActivityEvent` format preserved as fallback
- Fetch uses T3 endpoint first, falls back to legacy on 404

## 6. DTO/auth/permission compatibility

- No new DTO fields on existing `SessionTelemetry`
- New `TranscriptSegment` DTO is additive (new endpoint only)
- Permissions: `sessions:read` for GET transcript (same as session list)
- No bearer/ticket/host-key leakage in DTOs, logs, or errors
- No credential in Transcript content or navigation URLs

## 7. Fixture and test-to-invariant evidence matrix

### Test file: `internal/transcript/transcript_test.go`

| Test | Invariant covered |
|---|---|
| TestStoreAppendAndList | Ordered append, stable ID/Seq, ContractVersion |
| TestStoreListAfter | Cursor-based incremental read, no replay |
| TestStoreClear | Delete removes all, idempotent |
| TestStoreCrossSessionIsolation | Cross-session isolation |
| TestStoreCapacityEviction | Bounded capacity, gap marker on eviction |
| TestStoreConcurrentAppend | Concurrent safety, monotonic Seq |
| TestAgentEventProjectorBasic | Field allowlisting, AgentEventRef |
| TestAgentEventProjectorThinkingRedacted | Thinking text redacted |
| TestAgentEventProjectorToolCallNameOnly | Tool input redacted, name preserved |
| TestAgentEventProjectorCrossSessionRejected | Cross-session event rejection |
| TestAgentEventProjectorBatch | Batch projection with skip |
| TestAgentEventProjectorUnknownType | Unknown event preserved (ordering) |
| TestAgentEventProjectorUserMessageRedacted | Code-like user input redacted |
| TestByteStreamProjectorBasic | Byte-stream newline boundary |
| TestByteStreamProjectorMultiline | Multi-line chunk processing |
| TestByteStreamProjectorPartialLine | Partial line accumulation |
| TestByteStreamProjectorCRHandling | CR line ending |
| TestByteStreamProjectorBackspace | Backspace erase operation |
| TestByteStreamProjectorInputBoundarySuppression | Echo suppression during input |
| TestByteStreamOutputPreserved | Repeated log lines preserved |
| **TestEchoPrivacy** | **PTY echo cannot enter Transcript (content-free boundary)** |
| TestInputBoundaryIsContentFree | Input boundary carries zero content |
| TestSegmentTextTruncation | Text bounded to MaxTextBytes |
| TestDegradedSegmentReasonBounded | Degraded reason bounded |
| TestSourceArbiter* | Source arbitration (primary/fallback/explicit) |
| TestService* | Integration: both sources, clear, cross-session, stats |

## 8. Gate results

### Backend gates

```text
gofmt:                    CLEAN (changed files only)
go build ./...            PASS
go vet ./...              PASS
go test -race ./internal/agent/contract/...       PASS
go test -race ./internal/agent/adapters/codex/... PASS
go test -race ./internal/agent/adapters/claude/... PASS
go test -race ./internal/transcript/...           PASS (41 tests, 0 skipped)
go test -race ./internal/mux/...                  PASS
go test -race ./internal/term/...                 PASS
go test -race ./internal/agent/doctor/...         FAIL (4 pre-existing; confirmed on D1 baseline 8f7c22d)
```

### Mobile gates

```text
npx tsc --noEmit         PASS (0 errors)
npx jest                 PASS (21 suites, 289 tests)
```

### Security

```text
git diff --check         PASS (no whitespace errors)
secret scan              No new secrets introduced
credential leakage       No credentials in new DTOs, logs, or Transcript content
private path leakage     No absolute paths in TranscriptSegment fields
```

### Disk/process safety (§10)

```text
disk before:  164 GiB used
disk after:   165 GiB used
dedicated temp root removed: yes
leftover t3 temp dirs: 0
runaway test/build processes: 0
recursive go test: NOT triggered (explicit package lists used)
```

## 9. BLOCKER remediation (v2)

### BLOCKER 1 — AgentEvent production projection wired
- `TelemetryService` now carries `transcript *transcript.Service`
- `processSession` calls `s.transcript.ProjectAgentEvents(id, convertToAgentEvents(newEvents, logRef.Agent))`
- `convertToAgentEvents` maps legacy `models.AgentEvent` → `agent.AgentEvent` with explicit agent kind
- Projector rejects empty SessionID (fail-closed)
- `CorrelationState.CanBePrimarySource()` enforced at production boundary

### BLOCKER 2 — Source arbitration enforced in storage/API
- `TranscriptResponse` envelope separates `semantic` (primary) and `fallback` channels
- `SourceArbiter.SuppressByteStream()` gates byte-stream append after AgentEvent primary
- API returns `primarySource` field; mobile renders channels separately
- Mobile `validateTranscriptResponse()` enforces SessionID equality, closed vocabularies, contract version

### BLOCKER 3 — Echo privacy wired to production WS input path
- `HandleWS` calls `h.Transcript.BeginInput(session, time.Now())` before `WriteInput`
- `time.AfterFunc(200ms, ...)` calls `EndInput` after bounded window
- Byte-stream projector suppresses all bytes during input window
- No typed command content, prompt matching, or string comparison used

### BLOCKER 4 — Bounded ordered worker replaces per-chunk goroutines
- `chunkQueue`: session-owned bounded channel (256 capacity), single ordered worker goroutine
- Non-blocking enqueue: `select { case q.chunks <- item: ... default: drop + coalesced gap }`
- `Service.EnableQueue(sessionID)` called from Recorder; `CloseSessionQueue` on stop
- Synchronous fallback path when no queue attached (tests)
- Deterministic shutdown: `close(q.chunks)` → worker drains → flush

### BLOCKER 5 — Capture-mode routing and stateful ANSI/UTF-8
- Byte-stream projector rewritten with stateful UTF-8 decoder across chunk boundaries
- Stateful ANSI escape sequence parser (CSI, OSC, ESC) across chunks
- CR progress: captured in `crProgress` buffer; committed only when followed by non-CR or chunk-end
- Double-CR, CR+LF, and backspace handling
- `IsAlternateScreenStart/End`, `HasClearScreen` detectors available for TUI routing

### BLOCKER 6 — Closed structural-display allowlist
- `displayFieldsForEvent()`: type-specific structural fields only
- NEVER projects arbitrary `event.Text`
- `EventUserMessage` → `"[user input]"` (prompts never exposed)
- `EventThinking` → empty text
- `EventToolCallStarted/Finished` → tool name only (never input/output)
- `EventAssistantMessage` → text (adapter-redacted per contract)
- `EventApprovalRequested` → bounded text (adapter-redacted)

### BLOCKER 7 — Store bounds, IDs, gap coalescing, lifecycle deletion
- Evict to `MaxSegments - 1` before gap marker insertion → final count always ≤ MaxSegments
- Single coalesced gap marker: update existing if first segment is already degraded
- Unique IDs: `segmentID()` includes Seq for distinct same-content IDs
- `LifecycleService` carries `transcript *transcript.Service`; `Delete()` calls `ClearTranscript(id)`
- Transcript retained after Stop/Kill/exit until explicit Delete

### Honest deferrals
- Physical-device Transcript smoke test: M-track only
- Transcript persistence across daemon restart: in-memory per scope
- Doctor test failures (4): pre-existing on D1 baseline, not caused by T3

## 10. Production data flow diagram

```text
┌─────────────────────────────────────────────────────────────────┐
│                     PRODUCTION DATA FLOW                         │
│                                                                  │
│  [PTY Process]                                                   │
│       │                                                          │
│       │ raw bytes                                                │
│       ▼                                                          │
│  ┌──────────────────────┐                                        │
│  │  Recorder (single     │                                        │
│  │  PTY reader)          │                                        │
│  │                       │                                        │
│  │  readLoop():          │                                        │
│  │  ├─→ ActivityBuffer   │  legacy path (unchanged)              │
│  │  ├─→ Subscribers (WS) │  live terminal (unchanged)            │
│  │  └─→ feedTranscript() │  T3: non-blocking goroutine          │
│  └──────┬───────────────┘                                        │
│         │                                                        │
│         │ copied chunk (async)                                   │
│         ▼                                                        │
│  ┌──────────────────────┐                                        │
│  │  transcript.Service  │                                        │
│  │                       │                                        │
│  │  ├─ ByteStreamProj.  │  byte-stream → terminal_output        │
│  │  ├─ AgentEventProj.  │  agent.Event → agent_event            │
│  │  ├─ SourceArbiter    │  primary/fallback selection            │
│  │  └─ Store            │  bounded, session-isolated            │
│  └──────┬───────────────┘                                        │
│         │                                                        │
│         │ GET /api/sessions/{id}/transcript                      │
│         ▼                                                        │
│  ┌──────────────────────┐                                        │
│  │  HTTP API             │                                        │
│  │  AuthMiddleware /     │                                        │
│  │  RequirePrincipal    │                                        │
│  └──────┬───────────────┘                                        │
│         │                                                        │
│         │ HTTPS (cloudflare tunnel)                              │
│         ▼                                                        │
│  ┌──────────────────────┐                                        │
│  │  Mobile Client        │                                        │
│  │  apiGet (device-auth) │                                        │
│  │  getTranscript()      │                                        │
│  └──────┬───────────────┘                                        │
│         │                                                        │
│         ▼                                                        │
│  ┌──────────────────────┐                                        │
│  │  E8g2Transcript       │                                        │
│  │  (FeedScreen.tsx)     │                                        │
│  │  kind-aware render    │                                        │
│  └──────────────────────┘                                        │
└─────────────────────────────────────────────────────────────────┘
```

## 11. Final review marker

REVIEW REQUEST: T3 Transcript Integration — 0fe9bda2263bc8ea495d06261ecd7162b5e49062

## 12. T3-A through T3-E completion status

- [x] T3-A: Code-path audit, versioned Transcript contract, bounded store
- [x] T3-B: AgentEvent production projection + explicit source arbitration
- [x] T3-C: Bounded byte-stream fallback, echo privacy, snapshot isolation
- [x] T3-D: Authenticated API and actual mobile read/render consumer
- [x] T3-E: Integrated regression/privacy/race/safety gates

## 13. Changed files

```text
companion-daemon/
  cmd/devremote/app.go                          — import, creation, wiring, endpoint registration
  internal/term/runtime.go                      — Transcript field on Handlers
  internal/term/recorder.go                     — transcriptSvc field, feedTranscript, SetTranscriptService
  internal/transcript/
    contract.go                                 — NEW: versioned t3.1 contract
    store.go                                    — NEW: bounded session-isolated store
    projector_agent.go                          — NEW: AgentEvent → TranscriptSegment projector
    projector_bytes.go                          — NEW: Byte-stream projector with echo privacy
    arbitration.go                              — NEW: Source arbitration
    service.go                                  — NEW: Integration service
    api.go                                      — NEW: HTTP handlers
    transcript_test.go                          — NEW: 41 tests

mobile/
  src/lib/client.ts                             — TranscriptSegment interface, getTranscript()
  src/screens/FeedScreen.tsx                    — Updated E8g2Transcript, T3 endpoint with 404 fallback
```
