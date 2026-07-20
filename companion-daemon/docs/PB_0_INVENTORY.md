# PB.0 Consumer Inventory

**Generated:** 2026-07-21  
**Baseline:** PA4 ACCEPT `74560edd`, IMPL `4aaf3b76a`  
**Status:** READ-ONLY — no deletions performed

## §3.1 Legacy Mux Ownership

### Interfaces and Registry — 7/7 confirmed

| File | Present | Notes |
|------|---------|-------|
| `internal/mux/adapter.go` | YES | `Adapter` interface definition |
| `internal/mux/adapter_capability.go` | YES | Capability interfaces (StreamOpener, ScreenReader, etc.) |
| `internal/mux/session.go` | YES | `NativeSession`, `SpawnPTY`, `SpawnPTYWithDir` |
| `internal/mux/registry.go` | YES | `Registry` struct — adapter registration + snapshot caching |
| `internal/mux/id_parser.go` | YES | `ParseSessionID`, `SessionRef` |
| `internal/mux/tracker.go` | YES | Session tracking for telemetry |
| `internal/mux/transcript_capture.go` | YES | `TranscriptCaptureMode`, `CaptureModeScreenSnapshotDelta` |

**Consumer count:** 7 interface/registry files

### Replaced Local PTY — 2/2 confirmed

| File | Present | Notes |
|------|---------|-------|
| `internal/mux/localpty_adapter.go` | YES | Uses `SpawnPTY` from session.go |
| `internal/mux/localpty_adapter_test.go` | YES | Test file |

**Consumer count:** 2 localpty files

### Shared Spawn Primitive Analysis

**Critical finding:** `localpty_adapter.go` and `controlled_pty_adapter.go` BOTH call `SpawnPTY`/`SpawnPTYWithDir` from `internal/mux/session.go`. These are the SAME shared functions.

| Primitive | File | Used by localpty | Used by controlled_pty |
|-----------|------|:---:|:---:|
| `SpawnPTY()` | `session.go:21` | YES (line 65) | NO |
| `SpawnPTYWithDir()` | `session.go:27` | NO | YES (lines 103, 106, 169, 171) |
| `NativeSession` | `session.go` | YES | YES (both return `*NativeSession`) |

**PB.1 action required:** Before deleting localpty, extract `SpawnPTY`/`SpawnPTYWithDir`/`NativeSession` behind the retained platform-neutral PTY boundary. `controlled_pty_adapter.go` itself is a RETAINED managed component — it shares the `session.go` spawn primitive.

### Controlled PTY (Managed — RETAINED)

| File | Present | Notes |
|------|---------|-------|
| `internal/mux/controlled_pty_adapter.go` | YES | Managed adapter — RETAINED |
| `internal/term/create.go` | YES | Session creation paths — RETAINED |
| `internal/term/owned_pty_runtime.go` | YES | Lifecycle owner — RETAINED |
| `cmd/devremote/app.go` | YES | Application registration — RETAINED |

### tmux — 2/2 core + tests + extras confirmed

| File | Present | Notes |
|------|---------|-------|
| `internal/mux/tmux_adapter.go` | YES | tmux backend |
| `internal/mux/tmux_adapter_test.go` | YES | Tests |

**Additional tmux artifacts:**
- Registration in `cmd/devremote/app.go:144` (`reg.Register(mux.NewTmuxAdapter())`)
- No tmux-specific scripts found in `scripts/` or `companion-daemon/scripts/`
- Capability projections in telemetry

**Consumer count:** 2 core + 1 registration site

### cmux — 2/2 core + 7 test/data files confirmed

| File | Present | Notes |
|------|---------|-------|
| `internal/mux/cmux_adapter.go` | YES | cmux backend |
| `internal/mux/cmux_delta.go` | YES | Delta parsing |
| `internal/mux/cmux_adapter_test.go` | YES | |
| `internal/mux/cmux_tree_parser_test.go` | YES | |
| `internal/mux/cmux_top_test.go` | YES | |
| `internal/mux/cmux_forceflush_diag_test.go` | YES | |
| `internal/mux/cmux_delta_poc_test.go` | YES | |
| `internal/mux/cmux_adapter_mock_test.go` | YES | |
| `internal/mux/testdata/cmux_tree_real.txt` | YES | Screenshot test data |

**Consumer count:** 2 core + 6 test files + 1 test data file

## §3.2 Link, Discovery, and Observer Ownership

### Route/Store/App Composition — 1/1 confirmed

| File | Present | Notes |
|------|---------|-------|
| `cmd/devremote/app.go` | YES | Link/attach handlers, lifecycle wiring |

### Resolver and Observer — 5/5 confirmed

| File | Present | Notes |
|------|---------|-------|
| `internal/term/tracker.go` | YES | Session tracking |
| `internal/term/resolver.go` | YES | Log resolver |
| `internal/term/gemini_resolver.go` | YES | Gemini resolver |
| `internal/term/telemetry_service.go` | YES | Background sampling loop |
| `internal/term/telemetry.go` | YES | Session telemetry schema |

### Process Discovery — 1/1 confirmed

| File | Present | Notes |
|------|---------|-------|
| `internal/agent/detector.go` | YES | `AgentDetector` interface + detection evidence types |

### Managed Codex/Claude — PRESERVED (parser-only, not discovery)

The following files use `internal/agent/` for PARSING only (provider-native JSONL), not process discovery:
- `internal/agent/claude_adapter.go` — Claude parser
- `internal/agent/codex_adapter.go` — Codex parser
- `internal/agent/bridge.go` — Composite detector (claude + codex + antigravity)
- `internal/agent/antigravity_adapter.go` — Antigravity detector

**Consumer count:** 7 files (§3.2 total)

## §3.3 Recorder and Terminal Compatibility

### Global ID Lookup — 3/3 confirmed

| File | Present | Notes |
|------|---------|-------|
| `internal/term/recorder.go` | YES | `GetRecorder()` (legacy-only), `StartRecorder`, `DeleteRecorder`, snapshot markers |
| `internal/term/pty.go` | YES | `HandleWS` (legacy path), `HandleTermSize` (legacy path) |
| `internal/term/ipc.go` | YES | `handleIPCSubscriber` (legacy path) |

### cmux-Only Compatibility — confirmed present

| Item | File | Lines |
|------|------|-------|
| `snapshotEndMarker` | `recorder.go` | 399-402 (`\x1b[9999m`) |
| `deltaMarker` | `recorder.go` | 404-406 (`\x1b[9998m`) |
| Snapshot drain/filter | `recorder.go` | 290-350 (drainSnapshot, broadcast) |
| `CaptureModeScreenSnapshotDelta` | `transcript_capture.go` | 34 |
| Mode gating | `cmux_adapter.go` | 148-152 |

### Retained Behavior — confirmed

| Item | Verified |
|------|----------|
| Byte-stream capture | YES — `Recorder.readLoop()` |
| Transcript projection | YES — `Recorder.transcriptSvc` |
| Atomic bootstrap/fan-out | YES — `TerminalTransport.SubscriberFanOut()` |
| Generation-bound transport | YES — `TerminalTransport` with explicit `retired` flag |
| Resize | YES — `TerminalTransport.Resize()` |
| Input | YES — `TerminalTransport.WriteInput()` |
| Geom | YES — `TerminalTransport.Geom()` |

**Consumer count:** 3 core files with legacy fallback paths

## §3.4 Mobile, Scripts, and Packaging

### Mobile — 5/5 core + 3 screens confirmed

| File | Present | Notes |
|------|---------|-------|
| `mobile/src/components/AgentCard.tsx` | YES | Agent display card |
| `mobile/src/components/NewSessionModal.tsx` | YES | Session creation modal |
| `mobile/src/lib/client.ts` | YES | API client |
| `mobile/src/lib/lifecycle.ts` | YES | Lifecycle state types |
| `mobile/src/lib/agentActivity.ts` | YES | Agent activity types |
| `mobile/src/screens/DashboardScreen.tsx` | YES | Dashboard |
| `mobile/src/screens/FeedScreen.tsx` | YES | Activity feed |
| `mobile/src/screens/GlobalFeedScreen.tsx` | YES | Global feed |
| Jest tests | YES | 451/451 pass, 34 suites |

### Scripts — confirmed

| Directory | Contents |
|-----------|----------|
| `scripts/` | `android-native-gate.sh`, `build-gate.sh`, `cp0`, `dev-setup.sh`, `install.sh`, `ios-native-gate.sh`, `package.sh`, `uninstall.sh` |
| `companion-daemon/scripts/` | `install-launchagent.sh`, `uninstall-launchagent.sh` |
| `cmd/devremote/main.go` | YES — CLI entry point |

**Consumer count:** 8 mobile files + 10 script files + 1 main.go

## Surprise Inventory

### Files NOT in plan that reference mux legacy types

These files reference `mux.Registry`, `mux.Adapter`, or `mux.Session` but are MANAGED/RETAINED paths (not legacy deletion targets):

| File | Why it references mux types | Classification |
|------|----------------------------|----------------|
| `internal/term/cleanup_capability.go` | `GenerationCleanupCapability` holds `mux.Session` | RETAINED — PA3 frozen |
| `internal/term/create.go` | Session creation via mux primitives | RETAINED — listed in plan §3.1 |
| `internal/term/ipc.go` | IPC needs Registry for legacy sessions | RETAINED — legacy path only |
| `internal/term/lifecycle_service.go` | Dispatches to `OwnedPTYRuntime` | RETAINED — managed lifecycle |
| `internal/term/managed_api.go` | Managed API handlers | RETAINED — not legacy |
| `internal/term/managed_catalog.go` | `ManagedRuntimeCatalog` | RETAINED — core managed |
| `internal/term/managed_codex.go` | Managed Codex service | RETAINED |
| `internal/term/managed_registry.go` | `ManagedSessionRegistry` | RETAINED — not mux |
| `internal/term/owned_pty_runtime.go` | Uses mux for spawn/terminate seam | RETAINED — listed in plan |
| `internal/term/pty.go` | Legacy WS/size paths | CONTAINED — guarded by adapter check |
| `internal/term/runtime.go` | `Handlers` struct holds `*mux.Registry` | CONTAINED — for legacy adapters |
| `internal/term/telemetry_service.go` | Observer snapshot loop | LEGACY — listed in plan §3.2 |
| `internal/term/telemetry.go` | Telemetry schema | LEGACY — listed in plan §3.2 |

**No unplanned files reference legacy types as primary authority.**

### Snapshot marker consumers

The `snapshotEndMarker` and `deltaMarker` are referenced ONLY in:
- `internal/term/recorder.go` (definition + drain/delta handling)
- Tests in `internal/term/recorder_test.go`

No managed Codex/Claude paths consume snapshot markers.

## Summary

| Component | Files Confirmed | Status |
|-----------|:---:|--------|
| §3.1 — Legacy mux interfaces | 7/7 | ALL PRESENT |
| §3.1 — localpty | 2/2 | ALL PRESENT |
| §3.1 — controlled_pty (retained) | 4/4 | ALL PRESENT |
| §3.1 — tmux | 2+1 | ALL PRESENT |
| §3.1 — cmux | 2+6+1 | ALL PRESENT |
| §3.2 — Link/discovery/observer | 7/7 | ALL PRESENT |
| §3.3 — Recorder/terminal | 3/3 | ALL PRESENT |
| §3.4 — Mobile | 8/8 | ALL PRESENT |
| §3.4 — Scripts/packaging | 10+1 | ALL PRESENT |

**Total legacy consumers:** 54 files confirmed  
**Shared spawn primitive:** `SpawnPTY`/`SpawnPTYWithDir` in `session.go` — shared by localpty + controlled_pty  
**Surprises:** 0 — all mux-referencing files are either in-plan, retained managed paths, or contained legacy paths  
**No plan omissions found.**
