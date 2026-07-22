# CT-P0 — Canonical Timeline Plan Amendment & Source Freeze

**Status:** IMPLEMENTATION DOCUMENT

**IMPL SHA:** `7b7f23d0a`
**Branch:** `feature/phase10-multi-adapter`
**Freeze baseline:** `ab1884662` (PB_DEVICE_CANDIDATE_SHA)
**Date:** 2026-07-22

## 1. Freeze Verification

### 1.1 Source Tree

```
$ git rev-parse --short=9 7b7f23d0a
7b7f23d0a

$ git rev-parse --short=9 ab1884662
ab1884662

$ git status --porcelain --untracked-files=all
(0 files — clean worktree)

$ git merge-base --is-ancestor ab1884662 HEAD && echo YES
YES

$ git diff --name-status ab1884662..HEAD
(all files in docs/ only — zero Go/TS/mobile/test/build/config changes)
```

### 1.2 Frozen Artifact SHA-256

```
$ shasum -a 256 /tmp/pokit-pb-device-artifacts/pokit-daemon
5c1470a0d58072564298572aa8184a9c9565a8f57452eba56e2ebda56b10e580
Expected: 5c1470a0d58072564298572aa8184a9c9565a8f57452eba56e2ebda56b10e580 ✅ MATCH

$ shasum -a 256 /tmp/pokit-pb-device-artifacts/pokit-app-release.apk
934febb17b8de175816413a1aaa1a17b8d1e8f43c2cbfcb4fe26e20fce433c7d
Expected: 934febb17b8de175816413a1aaa1a17b8d1e8f43c2cbfcb4fe26e20fce433c7d ✅ MATCH
```

## 2. Managed-Native Producer & Consumer Enumeration

After PB legacy removal, all managed terminal sessions flow through exactly
three native managed producer paths. No T1/T2 mapper fallback exists.

### 2.1 Controlled PTY — OwnedPTYRuntime

| Attribute | Value |
|-----------|-------|
| **Type** | Native managed (V1 NativePTYLauncher — direct PTY spawn) |
| **Entry point** | `OwnedPTYRuntime.Create(ctx, SpawnConfig, profileID, name)` |
| **Session prefix** | `controlled_pty:` |
| **Runtime binding** | `OwnedPTYRuntime` catalog entries |
| **Generation** | Monotonic, owned by `OwnedPTYRuntime` |
| **Transport** | `TerminalTransport` (generation-gated WriteInput, Resize, Geom) |
| **Recorder** | Per-session, generation-bound (`StartRecorderUnconditional`) |
| **Input** | `TerminalTransport.WriteInput` via acknowledged Input-B protocol |
| **Live/fixture** | LIVE — daemon-owned PTY via `creack/pty` |
| **Feature flag** | Always enabled (default adapter) |

### 2.2 Codex — ManagedCodexService

| Attribute | Value |
|-----------|-------|
| **Type** | Native managed (direct process spawn, no agent adapter) |
| **Entry point** | `ManagedCodexService.CreateDetached(cwd)` |
| **Session prefix** | `codex_app_server:` |
| **Runtime binding** | `ManagedRuntimeCatalog` (register/status) |
| **Generation** | Monotonic, owned by `ManagedCodexService` |
| **Input** | `SubmitPrompt` through IPC or REST |
| **Live/fixture** | LIVE — production binary at pinned version `0.144.1` |
| **Feature flag** | `cfg.EnableManagedCodex` |
| **Production callers** | `app.go:NewAppWithDeps`, `create.go:createFromProfile`, `ipc.go:handleIPCConnection` |

### 2.3 Claude — ManagedClaudeService

| Attribute | Value |
|-----------|-------|
| **Type** | Native managed (direct process spawn, no agent adapter) |
| **Entry point** | `ManagedClaudeService.CreateDetached(cwd)` |
| **Session prefix** | `claude_headless:` |
| **Runtime binding** | `ManagedRuntimeCatalog` |
| **Generation** | Monotonic, owned by `ManagedClaudeService` |
| **Input** | Through approval delivery system |
| **Live/fixture** | LIVE — production binary at pinned version `2.1.202` |
| **Feature flag** | `cfg.EnableManagedClaude` |
| **Production callers** | `app.go:NewAppWithDeps`, `create.go:createFromProfile`, `ipc.go:handleIPCConnection` |

## 3. Agent Adapter Layer — Post-PB Classification

### 3.1 No live adapter producer or consumer

`telemetry_service.go` imports the version-pinned Codex and Claude packages only
to construct values in `acceptedAdapterFor`. That import/compilation fact is not
a live call path. The code-level caller inventory is decisive: every
`callAcceptedAdapter` invocation is in `production_bridge_test.go`, while
`ingestApprovals` has no invocation at all. Consequently neither function
provides a production telemetry consumer, approval consumer, session producer,
process spawner, or input route.

**Classification: NO LIVE PRODUCER OR CONSUMER.** T1 and T2 are fixture-only
parser assets. Managed-native lifecycle is directly owned by the managed
services; it does not pass through the adapter layer.

### 3.2 Contract/Test-Only

| Item | Path | Classification |
|------|------|---------------|
| `RunParserContract` | `agent/parser_contract_test.go` | TEST-ONLY — reusable test helper |
| `RunDetectorContract` | `agent/detector_contract_test.go` | TEST-ONLY — reusable test helper |
| `TestFixtureParser` | `agent/parser_contract_test.go` | TEST-ONLY — contract harness fixture |
| `TestFixtureDetector` | `agent/detector_contract_test.go` | TEST-ONLY — contract harness fixture |

### 3.3 Deleted (PB physical removal)

| Item | Original path | Removal wave |
|------|--------------|-------------|
| tmux adapter | `mux/tmux_adapter.go` | PB.3 |
| cmux adapter | `mux/cmux_adapter.go` | PB.4 |
| localpty adapter | `mux/localpty_adapter.go` | PB.1 |
| Legacy mux.Registry | `mux/registry.go` | PB.5b |
| Manual link (SourceManualLink) | `agent/detector.go` | PB.2a — DELETED; only comment references remain |
| Link resolver | `agent/detector.go` | PB.2a |
| Snapshot drain/delta | `recorder.go` (legacy capture) | PB.4 |

## 4. T0/T1/T2/T3 Matrix (per contract definitions)

### 4.1 Contract Layer Definitions

Per `docs/CANONICAL_TIMELINE_CT_PRE_EXECUTION_PLAN.md` §2 and
`internal/agent/contract/contract.go`:

| Layer | Contract Definition | Current State |
|-------|-------------------|---------------|
| T0 | AgentEvent/contract — the common event model, parser/detector contracts, reusable test harnesses | LIVE — `agent/models.go`, `agent/contract/` |
| T1 | Codex fixtures — version-pinned parser + detector for `v0_144_1` | FIXTURE-ONLY — `agent/adapters/codex/v0_144_1/`; read-only telemetry parsing only, never a live session producer |
| T2 | Claude fixtures — version-pinned parser + detector for `v2_1_202` | FIXTURE-ONLY — `agent/adapters/claude/v2_1_202/`; read-only telemetry parsing only, never a live session producer |
| T3 | Transcript — the canonical capture/ingest/replay authority | LIVE — `internal/transcript/` |

### 4.2 Reuse Matrix

| Layer | Reused By | Reuse Mechanism |
|-------|----------|----------------|
| T0 (contract) | T1, T2, contract tests, doctor | `AgentEvent`, `AgentParser`, `AgentDetector` interfaces |
| T1 (Codex fixtures) | No live caller | Fixture-only parser asset |
| T2 (Claude fixtures) | No live caller | Fixture-only parser asset |
| T3 (Transcript) | `term/`, `Recorder`, `OwnedPTYRuntime` | Byte-stream projection, generation-bound queue |

### 4.3 Forbidden Legacy Sources

After PB, no production code may:
- Import `mux.Registry`, `mux.Adapter`, `mux.Session`
- Reference `tmux`, `cmux`, `localpty`
- Call `GetRecorder`, `EnsureRecorder`, `RegistryFromContext`
- Use `v1Bridge`, `NewV1FromOld`
- Call `ResolveAgentLog` or use `ManualLink` (SourceManualLink deleted PB.2a)
- Use `snapshotEndMarker`, `deltaMarker`, `drainSnapshot`

All above: ZERO production hits at freeze baseline (verified PB.6 scans with `grep -E`).

## 5. Session/Runtime/Generation Binding

### 5.1 Controlled PTY

```
Session ID:  controlled_pty:<local-id>
Generation:  monotonic int64, owned by OwnedPTYRuntime
Transport:   TerminalTransport (writer, resizer, recorder reference)
Recorder:    per-session, generation-bound (StartRecorderUnconditional)
Lifecycle:   Create → Running → Stop/Kill → Exited/Killed → Delete
```

### 5.2 Managed Codex

```
Session ID:  codex_app_server:<pid>-<nanos>
Runtime:     ManagedRuntimeCatalog → status, approval, process info
Generation:  per-runtime monotonic, owned by ManagedCodexService
```

### 5.3 Managed Claude

```
Session ID:  claude_headless:claude-<nanos>
Runtime:     ManagedRuntimeCatalog → status, approval, process info
Generation:  per-runtime monotonic, owned by ManagedClaudeService
```

## 6. Legacy Symbol Classification

Verified against actual file content at freeze baseline:

| Symbol | Location | Actual Content |
|--------|----------|---------------|
| `manual_link` | `models.go:93` | Comment-only — `// mechanical origin (jsonl/log_file/screen/process/manual_link)` in JSON tag description. NOT a constant. |
| `SourceScreen` | `validate.go:173` | Contract test default — `e.Source = agent.SourceScreen // never manual_link by default` |
| `manual_link` | `contract.go:68` | Documentation comment — legacy source classification reference |

**SourceManualLink is DELETED (PB.2a).** The string `manual_link` survives only in
comments and JSON tag descriptions. It has zero runtime effect. The four remaining
`AgentEventSource` constants are: `SourceJSONL`, `SourceLogFile`, `SourceScreen`,
`SourceProcess` — all four are live.

## 7. Gate Results (at freeze baseline ab1884662)

```
go build ./...                          PASS (exit 0)
go vet ./...                            PASS (exit 0)
gofmt -l .                              CLEAN (0 files)
git diff --check                        CLEAN
go test -race ./... -count=1            PASS (12 ok, 3 no-test)
cd mobile && npx tsc --noEmit           PASS (clean)
cd mobile && npx jest --runInBand       PASS (537/537, 35 suites)
Secret scan (grep -E)                   CLEAN
Timeline scan (grep -E, cmd/term/mobile) ZERO
Legacy symbol scan (grep -E)            ZERO production callers (comments only)
Forbidden symbol scan (grep -E)         ZERO
Non-doc changes ab1884662..HEAD         ZERO
Frozen artifact SHA-256 verify          BOTH MATCH
```
