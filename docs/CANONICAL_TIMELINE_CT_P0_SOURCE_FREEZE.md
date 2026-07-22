# CT-P0 — Canonical Timeline Plan Amendment & Source Freeze

**Status:** IMPLEMENTATION DOCUMENT

**Branch:** `feature/phase10-multi-adapter`

**Freeze baseline:** `ab1884662` (PB_DEVICE_CANDIDATE_SHA)

**Current HEAD:** `316008304`

**Date:** 2026-07-22

## 1. Freeze Verification

### 1.1 Source Tree

```
$ git rev-parse HEAD
316008304b74331fed0eb99f70a6768c7e9ea836

$ git status --porcelain --untracked-files=all
(0 untracked/modified files — clean worktree)

$ git merge-base --is-ancestor ab1884662 HEAD && echo YES
YES

$ git diff --name-status ab1884662..HEAD
A    companion-daemon/docs/ARTIFACT_ID1_EVIDENCE.md
M    companion-daemon/docs/PB_7_EVIDENCE.md
A    docs/CANONICAL_TIMELINE_CT_PRE_EXECUTION_PLAN.md
A    docs/COORDINATOR_HANDOFF.md
M    docs/LOCAL_E2E_EVIDENCE.md
M    docs/PB_DEVICE_GATE_TERMINAL_REGRESSION_REMEDIATION_PLAN.md
M    docs/PB_EXECUTION_PLAN.md
M    docs/PB_LEGACY_REMOVAL_CONTRACT.md
M    docs/POST_PA3_AUTHORITATIVE_ROADMAP.md
M    docs/PRE_DEVICE_QR_INPUT_REMEDIATION_PLAN.md

ALL files are in docs/ — zero Go, TypeScript, mobile, test, build, config,
fixture, or runtime changes from the frozen device candidate.
```

### 1.2 Frozen Artifact SHA-256

```
$ shasum -a 256 /tmp/pokit-pb-device-artifacts/pokit-daemon
5c1470a0d58072564298572aa8184a9c9565a8f57452eba56e2ebda56b10e580
Expected: 5c1470a0d580... ✅ MATCH

$ shasum -a 256 /tmp/pokit-pb-device-artifacts/pokit-app-release.apk
934febb17b8de175816413a1aaa1a17b8d1e8f43c2cbfcb4fe26e20fce433c7d
Expected: 934febb17b... ✅ MATCH
```

## 2. Managed-Native Producer & Consumer Enumeration

After PB legacy removal, all managed terminal sessions flow through exactly
two native managed producer paths. There is no T1/T2 mapper fallback.

### 2.1 Codex — ManagedCodexService

| Attribute | Value |
|-----------|-------|
| **Type** | Native managed (T0 — direct process spawn) |
| **Entry point** | `ManagedCodexService.CreateDetached(cwd)` |
| **Session prefix** | `codex_app_server:` |
| **Runtime binding** | `ManagedRuntimeCatalog` (register/status) |
| **Generation** | Monotonic, owned by `ManagedCodexService` |
| **Input** | `SubmitPrompt` through IPC or REST |
| **Live/fixture** | LIVE — production binary at pinned version `0.144.1` |
| **Feature flag** | `cfg.EnableManagedCodex` |
| **Production callers** | `app.go:NewAppWithDeps`, `create.go:createFromProfile` |
| **IPC entry** | `ipc.go:StartIPCServer` → `handleIPCConnection` (profile `codex`) |

### 2.2 Claude — ManagedClaudeService

| Attribute | Value |
|-----------|-------|
| **Type** | Native managed (T0 — direct process spawn) |
| **Entry point** | `ManagedClaudeService.CreateDetached(cwd)` |
| **Session prefix** | `claude_headless:` |
| **Runtime binding** | `ManagedRuntimeCatalog` |
| **Generation** | Monotonic, owned by `ManagedClaudeService` |
| **Input** | Through approval delivery system |
| **Live/fixture** | LIVE — production binary at pinned version `2.1.202` |
| **Feature flag** | `cfg.EnableManagedClaude` |
| **Production callers** | `app.go:NewAppWithDeps`, `create.go:createFromProfile` |
| **IPC entry** | `ipc.go:handleIPCConnection` (profile `claude`) |

### 2.3 Controlled PTY — OwnedPTYRuntime

| Attribute | Value |
|-----------|-------|
| **Type** | Native managed (T0 — V1 NativePTYLauncher) |
| **Entry point** | `OwnedPTYRuntime.Create(ctx, SpawnConfig, profileID, name)` |
| **Session prefix** | `controlled_pty:` |
| **Runtime binding** | `OwnedPTYRuntime` catalog entries |
| **Generation** | Monotonic, owned by `OwnedPTYRuntime` |
| **Input** | `TerminalTransport.WriteInput` (generation-gated) |
| **Live/fixture** | LIVE — daemon-owned PTY via `creack/pty` |
| **Feature flag** | Always enabled (default adapter) |

## 3. Agent Adapter Layer — Post-PB Classification

The agent adapter layer (`internal/agent/`) is post-PB inventory.
Every adapter is classified as live, test-fixture, or deleted.

### 3.1 Live (Production)

| Adapter | Path | Session prefix | Status |
|---------|------|---------------|--------|
| Claude v2.1.202 | `agent/adapters/claude/v2_1_202/` | `claude_headless:` | LIVE — production parser + detector |
| Codex v0.144.1 | `agent/adapters/codex/v0_144_1/` | `codex_app_server:` | LIVE — production parser + detector |
| Contract harness | `agent/contract/` | — | LIVE — reusable parser/detector contract tests |
| Doctor | `agent/doctor/` | — | LIVE — secret scanner, no runtime dependency |

### 3.2 Test-Only (no production path)

| Item | Path | Classification |
|------|------|---------------|
| `TestFixtureParser` | `agent/parser_contract_test.go` | TEST-ONLY — contract harness fixture |
| `TestFixtureDetector` | `agent/detector_contract_test.go` | TEST-ONLY — contract harness fixture |
| `RunParserContract` | `agent/parser_contract_test.go` | TEST-ONLY — reusable test helper |
| `RunDetectorContract` | `agent/detector_contract_test.go` | TEST-ONLY — reusable test helper |

### 3.3 Deleted (PB physical removal)

| Item | Original path | Removal wave |
|------|--------------|--------------|
| tmux adapter | `mux/tmux_adapter.go` | PB.3 |
| cmux adapter | `mux/cmux_adapter.go` | PB.4 |
| localpty adapter | `mux/localpty_adapter.go` | PB.1 |
| Legacy mux.Registry | `mux/registry.go` | PB.5b |
| Manual link | `agent/models.go:SourceManualLink` | PB.2a |
| Legacy JSONL observation | External file watching | PB.2b |
| Link resolver | `agent/detector.go:ResolveLink` | PB.2a |
| Snapshot drain/delta | `recorder.go` (legacy capture) | PB.4 |

## 4. T0/T1/T2/T3 Reuse & Legacy Matrices

### 4.1 Terminal Producer Matrix

| Layer | Current | Post-PB Path | Legacy Equivalent |
|-------|---------|-------------|-------------------|
| T0 (direct spawn) | `NativePTYLauncher.Spawn`, `ManagedCodexService`, `ManagedClaudeService` | Direct V1/managed-native | — |
| T1 (broker) | — | Not implemented post-PB | `CommandBroker` (deleted) |
| T2 (mux mapper) | — | Not implemented post-PB | `mux.Registry` (deleted) |
| T3 (remote) | — | Not implemented post-PB | `cloudflared tunnel` (app-level) |

### 4.2 Agent Producer Matrix

| Source | Description | Live/Deleted |
|--------|-------------|-------------|
| `SourceProcess` | Direct process observation (Codex/Claude stdout) | LIVE |
| `SourceScreen` | Terminal screen parse (generic fallback) | LIVE (contract only) |
| `SourceJSONL` | Structured JSONL ingestion | LIVE (Claude v2.1.202) |
| `SourceLogFile` | Log file tailing | LIVE (Codex v0.144.1) |
| `SourceManualLink` | User-provided external log path | DELETED (PB.2a) |

### 4.3 Forbidden Legacy Sources

After PB, no production code may:
- Import `mux.Registry`, `mux.Adapter`, `mux.Session`
- Reference `tmux`, `cmux`, `localpty`
- Call `GetRecorder`, `EnsureRecorder`, `RegistryFromContext`
- Use `v1Bridge`, `NewV1FromOld`
- Read `ManualLink` or `ResolveAgentLog`
- Use `snapshotEndMarker`, `deltaMarker`, `drainSnapshot`
- Reference `observedAdapter` or `observerAdapter`

All above: ZERO production hits at freeze baseline (verified PB.6 scans).

### 4.4 Agent Adapter Import Map

The ONLY production path from `term/` into `agent/adapters/` is:

```
internal/term/telemetry_service.go:
  imports claude v2_1_202  (telemetry sampling — reads managed agent status)
  imports codex v0_144_1   (telemetry sampling — reads managed agent status)
```

This is a read-only telemetry consumer. It does not create sessions, spawn
processes, or route input. All managed-native lifecycle is owned by the
`ManagedCodexService` / `ManagedClaudeService` in `term/`.

## 5. Session/Runtime/Generation Binding

### 5.1 Controlled PTY

```
Session ID:  controlled_pty:<local-id>
Generation:  monotonic int64, owned by OwnedPTYRuntime
Transport:   TerminalTransport (writer, resizer, recorder reference)
Recorder:    per-session, generation-bound (StartRecorderUnconditional)
Lifecycle:   Create → LifecycleRunning → Stop/Kill → LifecycleExited/Killed → Delete
```

### 5.2 Managed Codex

```
Session ID:  codex_app_server:<pid>-<nanos>
Runtime:     ManagedRuntimeCatalog → status, approval, process info
Generation:  per-runtime monotonic, owned by ManagedCodexService
Identity:    Pinned binary 0.144.1 (verified SHA-256 at spawn)
```

### 5.3 Managed Claude

```
Session ID:  claude_headless:claude-<nanos>
Runtime:     ManagedRuntimeCatalog → status, approval, process info
Generation:  per-runtime monotonic, owned by ManagedClaudeService
Identity:    Pinned binary 2.1.202 (verified SHA-256 at spawn)
```

## 6. Legacy Symbol Classification (existing code only)

The following symbols exist in the codebase but have NO production callers
from `term/` or `cmd/`:

| Symbol | File | Classification |
|--------|------|---------------|
| `SourceManualLink` | `agent/models.go:93` | Enum value — deleted source, retained for contract completeness |
| `SourceScreen` fallback | `agent/contract/validate.go:173` | Contract test fallback — never used as primary production source |

These are structural definitions in the agent contract layer, not production
entry points. They were retained during PB because they do not create legacy
discovery paths.

## 7. Gate Results (at freeze baseline ab1884662)

```
go build ./...                          PASS (exit 0)
go vet ./...                            PASS (exit 0)
gofmt -l .                              CLEAN (0 files)
git diff --check                        CLEAN
go test -race ./... -count=1            PASS (12 ok, 3 no-test)
cd mobile && npx tsc --noEmit           PASS (clean)
Secret scan                             CLEAN
Timeline scan (cmd/term/mobile)         ZERO
Legacy symbol scan                      ZERO production callers
Non-doc changes ab1884662..HEAD         ZERO
Frozen artifact SHA-256 verify          BOTH MATCH
```
