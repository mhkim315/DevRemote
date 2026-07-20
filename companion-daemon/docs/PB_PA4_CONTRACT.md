# PB/PA4 Contract — Consumer Inventory, Migration Sequence, and Deletion Gates

**Status:** SUPERSEDED COMBINED DRAFT — historical inventory only
**Branch:** `feature/phase10-multi-adapter`
**Rollback SHA:** `34d55e950012e97ccdcb03fd9abba88088ffd9a7` (PA3 ACCEPTED)
**Prerequisite:** PF accepted-state freeze (POST_CLAUDE_MANAGED_ONLY_RESTRUCTURING_PLAN.md §1)

> **Superseded notice:** This combined PB/PA4 draft is not execution authority.
> PA4 isolation and PB deletion now have separate contracts at
> `docs/PA4_MANAGED_ISOLATION_CONTRACT.md` and
> `docs/PB_LEGACY_REMOVAL_CONTRACT.md`; their ordering and SHA ledger are
> controlled by `docs/POST_PA3_AUTHORITATIVE_ROADMAP.md`. The inventory below
> is retained only as historical discovery input. In particular, its PA3
> rollback cannot be used as PB's prerequisite or rollback.

## Classification Vocabulary

| Class | Symbol | Definition |
|-------|--------|------------|
| **ALREADY REPLACED** | AR | Fully superseded by managed runtime; no production callers remain |
| **REQUIRES MIGRATION** | RM | Has production callers; needs new owner before legacy can be deleted |
| **ACCEPTED-ADAPTER EXCEPTION** | AE | Preserved intentionally per PA3 Closeout D; deferred to later phase |
| **SHARED PLATFORM-NEUTRAL** | SP | Core platform abstraction; must remain permanently |
| **PROVEN DEAD** | PD | Zero production callers; safe to delete immediately in PB |

---

## §1 — Exact Consumer Inventory

### 1.1 tmux_adapter.go (`TmuxAdapter`, `tmuxSession`, `tmuxExecRunner`)

| Consumer | File:Line | Current Owner | Class | Target Owner |
|----------|-----------|---------------|-------|--------------|
| Registration | `cmd/devremote/app.go:144` | `NewAppWithDeps` | **RM** | Deleted in PB |
| `StreamOpener` (PTY attach) | `internal/mux/tmux_adapter.go:43` | `tmuxSession.OpenStream` | **RM** | Deleted in PB |
| `ScreenReader` (capture-pane) | `internal/mux/tmux_adapter.go:69` | `tmuxSession.ReadScreen` | **RM** | Deleted in PB |
| `HistoryReader` (capture-pane -S) | `internal/mux/tmux_adapter.go:73` | `tmuxSession.ReadHistory` | **RM** | Deleted in PB |
| `SessionCreator` (new-session) | `internal/mux/tmux_adapter.go:89` | `tmuxAdapter.CreateSession` | **RM** | Deleted in PB |
| `SessionTerminator` (kill-session) | `internal/mux/tmux_adapter.go:94` | `tmuxAdapter.TerminateSession` | **RM** | Deleted in PB |
| `ProcessProvider` | `internal/mux/tmux_adapter.go:50` | `tmuxSession.ProcessInfo` | **RM** | Deleted in PB |
| `TranscriptCaptureMode` | `internal/mux/tmux_adapter.go:170` | Capability query | **SP** | Retained on `controlled_pty` |
| Session creation (legacy) | `internal/mux/session.go:84-86` | `NewSession` | **RM** | Deleted in PB |
| tmux list-panes (process) | `internal/mux/tracker.go:12-13` | `TrackCmuxPanels` | **RM** | Deleted with cmux |
| Resolver hardcoded tmux | `internal/term/gemini_resolver.go:22` | `GeminiResolver` | **AE** | Deferred (see §5) |
| Capability enum comments | `internal/mux/adapter_capability.go:14,24,30` | Capability mapping | **SP** | Updated to remove tmux |

**Deletion gate:** `grep -rn "tmux" --include="*.go" . | grep -v "_test.go" | grep -v "cmux"` returns zero production matches.

### 1.2 cmux_adapter.go (`CmuxAdapter`, `CmuxSession`, sentinel detection)

| Consumer | File:Line | Current Owner | Class | Target Owner |
|----------|-----------|---------------|-------|--------------|
| Registration | `cmd/devremote/app.go:137-142` | `NewAppWithDeps` | **RM** | Deleted in PB |
| Invalidation sender | `internal/mux/cmux_adapter.go:142` | `NewCmuxAdapter(reg)` | **RM** | Deleted in PB |
| `SessionCreator` (new-surface) | `internal/mux/cmux_adapter.go:470` | `cmuxAdapter.CreateSession` | **RM** | Deleted in PB |
| `SessionTerminator` (kill-surface) | `internal/mux/cmux_adapter.go:497` | `cmuxAdapter.TerminateSession` | **RM** | Deleted in PB |
| `ProcessSnapshot` (top) | `internal/mux/cmux_adapter.go:721` | `cmuxAdapter.ProcessSnapshot` | **RM** | Deleted in PB |
| `ScreenReader` (read-screen poll) | `internal/mux/cmux_adapter.go:244+` | `CmuxSession` | **RM** | Deleted in PB |
| Delta extraction | `internal/mux/cmux_delta.go` (entire file) | cmux delta | **RM** | Deleted in PB |
| WS rejection message | `internal/term/pty.go:239` | `HandleWS` | **RM** | Removed (cmux sessions gone) |
| Delta marker detection | `internal/term/recorder.go:295-313` | Recorder readLoop | **RM** | Removed after cmux deleted |
| Snapshot end-marker detection | `internal/term/recorder.go:397-401` | Recorder | **RM** | Removed after cmux deleted |
| Screen snapshot drain | `internal/term/recorder.go:319-331, 429-469` | Recorder | **RM** | Removed after cmux deleted |
| `isClearScreenSnapshot` | `internal/term/recorder.go:612-624` | Recorder | **RM** | Removed after cmux deleted |
| `TranscriptCaptureMode` | `internal/mux/cmux_adapter.go:151` | Capability query | **RM** | Deleted |
| `MigrateLegacyID` (cmux:NN → cmux:surface:NN) | `internal/mux/registry.go:376-381` | Registry | **RM** | Deleted after no cmux sessions |
| Capability enum comments | `internal/mux/adapter_capability.go:25,34,42` | Capability mapping | **SP** | Updated to remove cmux |
| `TrackCmuxPanels` comment | `internal/mux/tracker.go:9-11` | tracker | **RM** | Removed |

**Deletion gate:** `grep -rn "cmux\|Cmux" --include="*.go" . | grep -v "_test.go"` returns zero production matches (after recorder.go sentinel cleanup).

### 1.3 localpty_adapter.go (`LocalPTYAdapter`, `localptySession`)

| Consumer | File:Line | Current Owner | Class | Target Owner |
|----------|-----------|---------------|-------|--------------|
| Registration (feature-flagged) | `cmd/devremote/app.go:147-149` | `NewAppWithDeps` | **AR** | Deleted in PB |
| CLI flag | `cmd/devremote/main.go:63,79` | `main()` | **AR** | Deleted in PB |
| App config field | `cmd/devremote/app.go:30` | `Config.EnableLocalPTY` | **AR** | Deleted in PB |
| `NewLocalPTYAdapter()` | `internal/mux/localpty_adapter.go:22` | Self | **AR** | Deleted in PB |
| `SpawnPTY` call | `internal/mux/localpty_adapter.go:67` | CreateSession | **AR** | Deleted in PB |
| Capability comments | `internal/mux/adapter_capability.go:24,30` | Capability mapping | **SP** | Updated |

**Deletion gate:** `grep -rn "localpty\|LocalPTY\|EnableLocalPTY" --include="*.go" . | grep -v "_test.go"` returns zero production matches.

### 1.4 controlled_pty_adapter.go (ControlledPTYAdapter)

| Consumer | File:Line | Current Owner | Class | Target Owner |
|----------|-----------|---------------|-------|--------------|
| Registration | `cmd/devremote/app.go:159-162` | `NewAppWithDeps` | **AR** | Remain as Registry fallback |
| Owned by | `cmd/devremote/app.go:186` | `OwnedPTYRuntime` | **AR** | OwnedPTYRuntime is primary |
| Comment: Registry fallback | `cmd/devremote/app.go:153-158` | N/A | **AR** | Retained until A4 |
| `SessionCreatorWithIdentity` | `internal/mux/controlled_pty_adapter.go:140` | `CreateSessionAndCapture` | **AR** | OwnedPTYRuntime |

**Note:** The adapter remains registered for session-list fallback. PA4 may gate its removal after OwnedPTYRuntime handles all session-list paths.

### 1.5 tracker.go (`ResolveAgentLog`, `findAgentProcess`, `TrackCmuxPanels`)

| Consumer | File:Line | Current Owner | Class | Target Owner |
|----------|-----------|---------------|-------|--------------|
| `ResolveAgentLog` → telemetry | `internal/term/telemetry_service.go:257` | `processSession` | **AE** | Deferred (see §5) |
| `ResolveAgentLog` → test seam | `internal/term/telemetry_service.go:36,241` | `logResolver` field | **AE** | Deferred |
| `findAgentProcess` | `internal/term/tracker.go` (internal) | `ResolveAgentLog` | **AE** | Deferred |
| `TrackCmuxPanels` comment | `internal/mux/tracker.go:9-11` | Dead code hint | **PD** | Delete comment in PB |

**Preserved per PA3 Closeout D:** `ResolveAgentLog`, `findAgentProcess`, and the `AgentLogResolver` chain. Owned by accepted-adapter telemetry path.

### 1.6 resolver.go (`AgentLogResolver` interface, `ClaudeResolver`, `CodexResolver`, `GeminiResolver`)

| Consumer | File:Line | Current Owner | Class | Target Owner |
|----------|-----------|---------------|-------|--------------|
| `AgentLogResolver` interface | `internal/term/resolver.go:11-14` | Definition | **AE** | Deferred |
| `GeminiResolver` → PaneID | `internal/term/gemini_resolver.go:22` | Hardcoded tmux | **RM** | Must be generic or removed |
| `ClaudeResolver` | `internal/term/claude_resolver.go` | Impl | **AE** | Deferred |
| `CodexResolver` | `internal/term/codex_resolver.go` | Impl | **AE** | Deferred |

**Preserved per PA3 Closeout D.** The GeminiResolver hardcoded tmux dependency must be migrated to adapter-agnostic before PB deletes tmux.

### 1.7 reader.go (`ReadRawLines`, `RawLinesResult`)

| Consumer | File:Line | Current Owner | Class | Target Owner |
|----------|-----------|---------------|-------|--------------|
| `ReadRawLines` | `internal/term/telemetry_service.go:441` | `processSession` | **AE** | Deferred |
| `RawLinesResult` | `internal/term/telemetry_service.go` (internal) | `processSession` | **AE** | Deferred |

**Preserved per PA3 Closeout D.** Used exclusively by the accepted-adapter telemetry path.

### 1.8 parser.go (`LogCursor`)

| Consumer | File:Line | Current Owner | Class | Target Owner |
|----------|-----------|---------------|-------|--------------|
| `LogCursor` struct | `internal/term/reader.go:23-25,39` | `ReadRawLines` | **AE** | Deferred |
| `telemetry_service.go` fields | `internal/term/telemetry_service.go` (internal) | `adapterState` | **AE** | Deferred |

**Preserved per PA3 Closeout D.**

### 1.9 registry.go (`mux.Registry`, `Adapter` interface, `Snapshot`, `MigrateLegacyID`)

| Consumer | File:Line | Current Owner | Class | Target Owner |
|----------|-----------|---------------|-------|--------------|
| `NewRegistry()` | `cmd/devremote/app.go:133` | App startup | **SP** | Retained |
| `.Register(adapter)` | `cmd/devremote/app.go:141-162` | App startup | **SP** | Only `controlled_pty` remains post-PB |
| `.Snapshot()` → telemetry | `internal/term/telemetry_service.go` (internal loop) | Session list | **SP** | Retained |
| `.CreateSession()` | `internal/term/create.go:295` | `createControlledSession` | **SP** | Retained |
| `.Adapter(name)` → capabilities | `internal/term/telemetry.go:126` | `adapterCapabilityStrings` | **SP** | Retained |
| `MigrateLegacyID()` | `internal/mux/registry.go:378` (internal) | Session ID canonicalization | **RM** | Delete after cmux removal |
| `.GetSession()` → session lookup | `internal/term/create.go:319` | `startRecorder` | **SP** | Retained |
| `InvalidationSender` interface | `internal/mux/adapter.go:142-143` | Adapter invalidation | **SP** | Retained |

**Classification: SHARED PLATFORM-NEUTRAL.** Registry is the session-discovery hub. Post-PB, it contains only `controlled_pty` adapter.

### 1.10 Recorder (`recorder.go`)

| Consumer | File:Line | Current Owner | Class | Target Owner |
|----------|-----------|---------------|-------|--------------|
| `StartRecorder` | `internal/term/create.go:303` (via `startRecorder`) | Session create | **SP** | Retained |
| `EnsureRecorder` | `internal/term/pty.go:250` | `HandleWS` | **SP** | Retained |
| `EnsureRecorder` | `internal/term/telemetry_service.go:122` | `processSession` | **SP** | Retained |
| `GetRecorder` | `internal/term/pty.go:216,795` | `HandleWS`, resize | **SP** | Retained |
| `GetRecorder` | `internal/term/terminal_transport.go:95` | TerminalTransport | **SP** | Retained |
| `DeleteRecorder` | `internal/term/pty.go:142` | DELETE handler | **SP** | Retained |
| `DeleteRecorder` / `DeleteRecorderIfSame` | `internal/term/create.go:303` | `createControlledSession` | **SP** | Retained |
| `recorderRegistry` (package var) | `internal/term/recorder.go:58-62` | Recorder tracking | **SP** | Retained |
| cmux sentinel detection | `internal/term/recorder.go:295-331, 397-469, 612-624` | readLoop | **RM** | Removed after cmux deleted |
| `resolveCaptureMode` | `internal/term/recorder.go:410-411` | Capture routing | **SP** | Simplified to byte_stream only |

**Classification: SHARED PLATFORM-NEUTRAL.** Recorder is the single PTY reader invariant (ROADMAP_AFTER_E10B.md §"Recorder is the single PTY reader"). cmux sentinel detection is REMOVED after cmux adapter deletion.

### 1.11 pty.go Handlers (`HandleWS`, `HandleSessionsAPI`, `HandleSessionCRUD`)

| Consumer | File:Line | Current Owner | Class | Target Owner |
|----------|-----------|---------------|-------|--------------|
| `HandleWS` route | `cmd/devremote/app.go:414` | HTTP mux | **SP** | Retained |
| `HandleSessionsAPI` route | `cmd/devremote/app.go:398` | HTTP mux | **SP** | Retained |
| `HandleSessionsV2` route | `cmd/devremote/app.go:429` | HTTP mux | **SP** | Retained |
| `HandleSessionCRUD` route | `cmd/devremote/app.go:431,433` | HTTP mux | **SP** | Retained |
| cmux WS rejection | `internal/term/pty.go:239` | `HandleWS` | **RM** | Removed after cmux deleted |
| `EnsureRecorder` (WS + create) | `internal/term/pty.go:250` | `HandleWS` | **SP** | Retained |
| `GetRecorder` (WS + resize) | `internal/term/pty.go:216,795` | `HandleWS` | **SP** | Retained |
| `DeleteRecorder` (API) | `internal/term/pty.go:142` | `HandleSessionCRUD` | **SP** | Retained |

**Classification: SHARED PLATFORM-NEUTRAL.** WebSocket terminal, session list/CRUD APIs are core platform.

### 1.12 telemetry_service.go (accepted adapter path)

| Consumer | File:Line | Current Owner | Class | Target Owner |
|----------|-----------|---------------|-------|--------------|
| Telemetry loop | `internal/term/telemetry_service.go:65` (internal ticker) | `Run()` | **SP** | Retained |
| `processSession` | `internal/term/telemetry_service.go` (internal) | Telemetry loop | **AE** | Deferred |
| `EnsureRecorder` call | `internal/term/telemetry_service.go:122` | `processSession` | **SP** | Retained |
| `ReadRawLines` call | `internal/term/telemetry_service.go:441` | `processSession` | **AE** | Deferred |
| `ResolveAgentLog` call | `internal/term/telemetry_service.go:257` | `processSession` | **AE** | Deferred |
| Instantiation | `cmd/devremote/app.go:198-200` | `NewAppWithDeps` | **SP** | Retained |

**Classification: ACCEPTED-ADAPTER EXCEPTION for the discovery chain; SHARED PLATFORM-NEUTRAL for the telemetry loop and Recorder integration.**

### 1.13 app.go:133-160 adapter registration block

| Adapter | Line | Class | PB Action |
|---------|------|-------|-----------|
| cmux | 137-142 | **RM** | Remove registration + `NewCmuxAdapter` call |
| tmux | 144-145 | **RM** | Remove registration + `NewTmuxAdapter` call |
| localpty | 147-149 | **AR** | Remove registration + `EnableLocalPTY` flag + Config field + CLI flag |
| controlled_pty | 159-162 | **AR** | Retain (OwnedPTYRuntime primary; Registry is fallback) |

### 1.14 mobile/src/ TypeScript DTOs

| Consumer | Class | PB Action |
|----------|-------|-----------|
| `adapter` field in session DTO | **RM** | Remove `tmux`/`cmux`/`localpty` from adapter union types |
| `capabilities` field | **RM** | Remove `screen` capability (cmux-only) |
| `agentKind ===` vendor branches | **SP** | Already scanned by build gate; preserve |
| WebView terminal | **SP** | Unchanged (Recorder subscriber) |

### 1.15 Additional PROVEN DEAD symbols

| Symbol | File:Line | Evidence |
|--------|-----------|----------|
| Legacy parser files (deleted in PA3-D) | N/A | Already removed |
| `ReadNewEvents` (deleted in PA3-D) | N/A | Already removed |
| `AgentLogParser` (deleted in PA3-D) | N/A | Already removed |
| `normalizeEventType` (deleted in PA3-D) | N/A | Already removed |
| `SpawnPTY` (used by localpty only) | `internal/mux/session.go:86` | Only tmux call; becomes dead after tmux removed |
| `TrackCmuxPanels` comment | `internal/mux/tracker.go:9` | Dead comment; remove in PB |
| cmux delta file | `internal/mux/cmux_delta.go` (entire file) | Only included by cmux_adapter.go |
| `NewSession` (legacy) | `internal/mux/session.go:82-86` | Dead after tmux removal |

---

## §2 — Ordered PA4 Authority-Isolation Migration Sequence

PA4 gates managed paths so no legacy adapter path remains reachable before Phase B deletion.

### A4.1 — Adapter Registration Gate
1. Add `EnableLegacyAdapters bool` config flag (default `true`)
2. Gate all legacy adapter registrations behind `cfg.EnableLegacyAdapters`
3. Verify with `EnableLegacyAdapters=false`: only `controlled_pty` adapter registered
4. Prove no startup path requires tmux/cmux/localpty when flag is `false`

### A4.2 — Session List Isolation
1. Verify `Registry.Snapshot()` returns only `controlled_pty` sessions when legacy flag is `false`
2. Prove mobile session list renders correctly with only managed sessions
3. Gate: `grep -rn "tmux\|cmux\|localpty" internal/term/telemetry.go` returns zero matches when checking non-managed adapter types

### A4.3 — WebSocket Terminal Isolation
1. Remove cmux WS rejection in `pty.go:239` (no cmux sessions exist)
2. Verify `HandleWS` only serves `controlled_pty` sessions
3. Prove `TerminalTransport` path is the exclusive controlled_pty terminal path

### A4.4 — Process Discovery Isolation
1. Gate `ProcessProvider` calls to `controlled_pty` only
2. Remove tmux hardcoded in `gemini_resolver.go:22` (or make adapter-agnostic)
3. Prove `ResolveAgentLog` never reaches tmux/cmux process info

### A4.5 — Authority Gate Verification
1. Set `EnableLegacyAdapters=false`
2. Run full gate: build, vet, `go test -race ./... -count=1`
3. Verify zero legacy adapter production references
4. Run canonical secret scan
5. Mobile TypeScript compiles with legacy adapter types removed

**Exit:** A4 ACCEPT SHA with `EnableLegacyAdapters=false` passing full gate.

---

## §3 — Ordered PB Physical Deletion Sequence

### PB.1 — localpty Adapter Removal (ALREADY REPLACED)
1. Delete `internal/mux/localpty_adapter.go`
2. Remove `EnableLocalPTY` from `cmd/devremote/app.go:30,147-149`
3. Remove `--enable-localpty` CLI flag from `cmd/devremote/main.go:63,79`
4. Gate: `grep -rn "localpty\|LocalPTY\|EnableLocalPTY" --include="*.go" . | grep -v "_test.go"` returns empty

### PB.2 — tmux Adapter Removal (REQUIRES MIGRATION)
1. Delete `internal/mux/tmux_adapter.go`
2. Delete `internal/mux/session.go` (`NewSession`, `SpawnPTY`)
3. Remove registration from `cmd/devremote/app.go:144-145`
4. Remove tmux capability comments from `internal/mux/adapter_capability.go`
5. Gate: `grep -rn "tmux\|TmuxAdapter\|tmuxSession" --include="*.go" . | grep -v "_test.go"` returns empty (excluding cmux references)

### PB.3 — cmux Adapter Removal (REQUIRES MIGRATION)
1. Delete `internal/mux/cmux_adapter.go`
2. Delete `internal/mux/cmux_delta.go`
3. Remove registration from `cmd/devremote/app.go:137-142`
4. Remove `MigrateLegacyID` from `internal/mux/registry.go:376-381`
5. Remove cmux capability comments from `internal/mux/adapter_capability.go`
6. Remove cmux WS rejection from `internal/term/pty.go:239`
7. Remove cmux sentinel detection from `internal/term/recorder.go`:
   - `deltaMarker` var + `isDeltaMarker()` + delta processing (lines 295-313)
   - `snapshotEndMarker` var (line 401)
   - `isClearScreenSnapshot()` (lines 612-624)
   - `drainSnapshot()` (lines 429-469)
   - Snapshot detection in readLoop (lines 319-331)
8. Remove `TrackCmuxPanels` comment from `internal/mux/tracker.go:9`
9. Gate: `grep -rn "cmux\|Cmux" --include="*.go" . | grep -v "_test.go"` returns empty

### PB.4 — Recorder Cleanup
1. `resolveCaptureMode` simplified to always return `"byte_stream"`
2. Remove `captureMode` field from Recorder struct
3. Remove `SourceSnapshot` references from `internal/transcript/contract.go:67`
4. Gate: Recorder tests pass with simplified capture mode

### PB.5 — Mobile TypeScript Cleanup
1. Remove `tmux`, `cmux`, `localpty` from adapter union types
2. Remove `screen` capability (cmux-only)
3. Verify `npx tsc --noEmit` passes
4. Gate: mobile build passes

### PB.6 — Capability Enum Cleanup
1. Remove cmux-only capabilities from `internal/mux/adapter_capability.go`
2. Remove `screen_snapshot_delta` transcript capture mode
3. Gate: capability contract matches controlled_pty-only adapters

---

## §4 — Deletion Gates (Static Zero-Reference Checks)

Each PB sub-phase must pass its deletion gate before proceeding:

```
# PB.1 localpty gate
grep -rn "localpty\|LocalPTY\|EnableLocalPTY" --include="*.go" . | grep -v "_test.go"
→ (must be empty)

# PB.2 tmux gate
grep -rn "tmux\|TmuxAdapter\|tmuxSession\|NewTmuxAdapter" --include="*.go" . | grep -v "_test.go"
→ (must be empty)

# PB.3 cmux gate
grep -rn "cmux\|CmuxAdapter\|CmuxSession\|NewCmuxAdapter\|cmux_delta" --include="*.go" . | grep -v "_test.go"
→ (must be empty)

# PB.4 Recorder gate
grep -rn "captureMode\|CaptureMode\|screen_snapshot_delta\|SnapshotEndMarker\|deltaMarker\|drainSnapshot\|isClearScreenSnapshot" --include="*.go" . | grep -v "_test.go"
→ (must be empty)

# PB.5 Mobile gate
cd mobile && npx tsc --noEmit
→ (must pass)

# PB.6 Capability gate
grep -rn "CapScreen\|AdapterCapScreen\|screen_snapshot" --include="*.go" . | grep -v "_test.go"
→ (must be empty)
```

---

## §5 — Accepted-Adapter Exception Boundary

The following symbols are PRESERVED across PA4 and PB, with ownership deferred to a later phase (C0/C1/N1):

| Symbol | File | Reason |
|--------|------|--------|
| `ResolveAgentLog` | `internal/term/tracker.go:16` | Accepted-adapter process→log resolution |
| `findAgentProcess` | `internal/term/tracker.go` (internal) | Process discovery helper |
| `AgentLogResolver` interface | `internal/term/resolver.go:11` | Resolver contract |
| `ClaudeResolver` | `internal/term/claude_resolver.go` | Claude log path resolution |
| `CodexResolver` | `internal/term/codex_resolver.go` | Codex log path resolution |
| `GeminiResolver` | `internal/term/gemini_resolver.go` | Gemini log path; **must become adapter-agnostic** before PB deletes tmux |
| `LogCursor` | `internal/term/parser.go` | File offset tracking |
| `ReadRawLines` | `internal/term/reader.go` | Byte-stream log reading |
| `processSession` | `internal/term/telemetry_service.go` | Accepted-adapter ingestion loop |
| `managedEventStore` | `internal/term/managed_events.go` | Separate type, preserved |

**Boundary rule:** No PA4 or PB change may remove, rename, or alter the signature of any accepted-adapter exception symbol.

---

## §6 — Retained Platform Abstractions

These symbols are SHARED PLATFORM-NEUTRAL and must remain permanently:

| Symbol | Rationale |
|--------|-----------|
| `mux.Registry` | Session discovery hub; must support at least one adapter |
| `Recorder` + `recorderRegistry` | Single PTY reader invariant |
| `HandleWS` | WebSocket terminal access |
| `HandleSessionsAPI` / `HandleSessionsV2` | Session list/CRUD API |
| `HandleSessionCRUD` | Session create/delete API |
| `TerminalTransport` | controlled_pty transport handle |
| `TelemetryService.Run()` | Background telemetry loop |
| `Transcript.Service` | Canonical transcript capture |
| `ApprovalStore` | Approval authority |
| `LifecycleService` | Managed session lifecycle |
| `OwnedPTYRuntime` | controlled_pty lifecycle owner |

---

## §7 — Prerequisites and Rollback

- **Rollback SHA:** `34d55e950012e97ccdcb03fd9abba88088ffd9a7` (PA3 Final ACCEPTED)
- **Prerequisite:** PF accepted-state freeze ledger (POST_CLAUDE_MANAGED_ONLY_RESTRUCTURING_PLAN.md §1)
- **Gate:** PF must be independently ACCEPTED before A4 begins
- **Gate:** A4 must be independently ACCEPTED before PB begins
- **PB rollback:** Each PB sub-phase commits independently; rollback to previous sub-phase SHA
- **Structural rollback:** Phase B ACCEPT SHA (POST_CLAUDE_MANAGED_ONLY_RESTRUCTURING_PLAN.md line 410)

---

## §8 — Deferred: AcceptedRecordSource Ownership

Per POST_CLAUDE_MANAGED_ONLY_RESTRUCTURING_PLAN.md §"AcceptedRecordSource arrives after C1", the `AcceptedRecordSource` canonical event ownership model is deferred to Phase C1. No PA4 or PB change may introduce `AcceptedRecordSource` or modify `managedEventStore`.
