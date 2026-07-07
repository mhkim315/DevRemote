# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

DevRemote (POKIT) is a remote terminal session manager. A Mac daemon exposes tmux/cmux/LocalPTY terminal sessions via HTTP/WebSocket APIs. A React Native mobile app connects to the daemon through a Cloudflare tunnel to view and interact with terminal sessions. The project has two abstraction layers: **Terminal Adapter** (where sessions run) and **Agent Adapter** (what AI agent runs inside them).

## Build & Test

All commands run from `companion-daemon/`:

```sh
# Compile
go build ./...

# Vet
go vet ./...

# Run all tests with race detector
go test -race ./... -count=1

# Run specific package tests
go test -race ./internal/mux -count=1
go test -race ./internal/term -count=1
go test -race ./internal/agent -count=1

# Run targeted tests (20x repeat for stability)
go test ./internal/mux -run "TestFixtureE2E|TestLocalPTY" -count=20 -v

# Mobile TypeScript check
cd ../mobile && npx tsc --noEmit
```

## Build Gate (P3)

Before claiming a phase as complete, run the full verification gate:

```sh
# From project root:
cd companion-daemon

# Backend gate
echo "=== Backend ==="
go build ./... || { echo "BUILD FAILED"; exit 1; }
go vet ./... || { echo "VET FAILED"; exit 1; }
go test -race ./... -count=1 || { echo "TESTS FAILED"; exit 1; }
git diff --check || { echo "FORMAT FAILED"; exit 1; }
echo "Backend: OK"

# Mobile gate
echo "=== Mobile ==="
cd ../mobile
npx tsc --noEmit || { echo "TSC FAILED"; exit 1; }

# Vendor branch scan
echo "=== Invariants ==="
grep -rn "agentKind.*===" src/ && echo "VENDOR BRANCH FOUND" && exit 1 || true
grep -rn "opt\.id === 'approve'\|opt\.id === 'reject'" src/ && echo "ID INFERENCE FOUND" && exit 1 || true

# Secret scan (exclude test fixtures, redaction patterns, test mocks)
echo "=== Security ==="
cd ../companion-daemon
SECRETS=$(grep -rn "sk-[A-Za-z0-9]\|ghp_\|xox[baprs]-\|Bearer [A-Za-z0-9]" internal/ docs/ | grep -v "testdata/\|diagnostic\.go\|approval_test\.go\|auth_test\.go\|fake\|REDACTED\|redact" || true)
[ -n "$SECRETS" ] && echo "SECRET FOUND: $SECRETS" && exit 1 || true

echo "=== ALL GATES PASSED ==="
```

Or run the gate script: `sh scripts/build-gate.sh`

### Environment requirements

Backend:
- Go 1.26+ (`go version`)
- macOS or Linux (darwin/linux)

Mobile (for full gate):
- Node.js 20+ (`node --version`)
- TypeScript 5.8 (`npx tsc --version`)
- Expo SDK 56
- `node_modules/` installed (`cd mobile && npm install`)

If mobile dependencies are unavailable, run the backend gate and report mobile as `not-run` with the reason.

## Architecture

### Terminal Adapter Layer (`internal/mux/`)
- **`adapter.go`** — `Adapter` interface: `Name()`, `ListSessions(ctx)`. Optional capability interfaces: `StreamOpener`, `ScreenReader`, `HistoryReader`, `SessionCreator`, `SessionTerminator`, `ProcessProvider`, `InputWriter`.
- **`registry.go`** — `Registry` manages adapter registration, snapshot caching, parallel refresh with 200ms collection window.
- **`tmux_adapter.go`** — tmux backend.
- **`cmux_adapter.go`** — cmux (custom terminal multiplexer) backend.
- **`localpty_adapter.go`** — LocalPTY backend (child PTY, no external daemon). Feature-flagged behind `EnableLocalPTY`.
- **`fixture_adapter_test.go`** — In-memory test adapter for contract proof.
- **`adapter_contract_test.go`** — Reusable contract harness: `RunAdapterContract`, `RunLiveStreamContract`, etc. 15 required + optional capability suites.
- **`session.go`** — `NativeSession`/`SpawnPTY`: PTY process management via `creack/pty`.

### Term Handler Layer (`internal/term/`)
- **`pty.go`** — HTTP handlers: `HandleSessionsAPI` (GET/POST/DELETE), `HandleWS` (WebSocket). Session CRUD via canonical ID `<adapter>:<local-id>`.
- **`runtime.go`** — `Handlers` struct holding `*mux.Registry`, `EventStore`, `TelemetryService`.
- **`telemetry.go`** — `SessionTelemetry` JSON schema, `sessionCapabilities()` maps Go interfaces to string arrays.
- **`telemetry_service.go`** — Background telemetry sampling loop.

### Agent Adapter Layer (`internal/agent/`)
- **`models.go`** — Common types: `AgentIdentity`, `AgentStatus`(10), `AgentEventType`(13), `AgentEventSource`(5), `AgentApproval`.
- **`parser.go`** — `AgentParser` interface + `ParseResult` struct (Events, Cursor, Status, Approvals, Degraded, Diagnostics).
- **`parser_contract_test.go`** — Reusable parser harness: `RunParserContract` (12 tests per agent).
- **`detector.go`** — `AgentDetector` + `LogResolver` interfaces. `DetectionEvidence`, `ManualEvidence`. Confidence < 0.5 → Kind="unknown".
- **`detector_contract_test.go`** — Detector harness: `RunDetectorContract` (10 tests).
- **`claude_adapter.go`** — First production agent: `ClaudeParser`, `ClaudeDetector`, `ClaudeLogResolver`.
- **`testdata/`** — Redacted A1 fixtures for Claude, Codex, Antigravity.

### Command Layer (`cmd/devremote/`)
- **`app.go`** — Production composition root. `NewAppWithDeps(cfg, deps)` registers tmux + cmux (+ localpty if enabled). Route wiring: `/api/sessions`, `/term/ws`, `/term/`.
- **`main.go`** — CLI flags: `--enable-localpty`, `--insecure-local-only`.

### Mobile (`mobile/`)
- **`src/screens/FeedScreen.tsx`** — WebView terminal + activity feed. History tab gated by `capabilities?.includes('history')`.
- **`src/components/AgentCard.tsx`** — `adapter !== 'native'` display filter (harmless).
- **`src/lib/client.ts`** — API client: `listSessions`, `createOrUpdateSession`, `deleteSession`, `getSessionHistory`.

## Key Patterns

### Feature Flags
New adapters register behind `cfg.EnableXxx` boolean. Default `false`. Registration in `app.go` after tmux/cmux.

### Contract Testing
Every adapter layer has a reusable contract harness:
- **Terminal**: `RunAdapterContract(t, name, factory)` in `adapter_contract_test.go`
- **Agent Parser**: `RunParserContract(t, agentName, factory)` in `parser_contract_test.go`
- **Agent Detector**: `RunDetectorContract(t, name, df, rf)` in `detector_contract_test.go`

### Canonical Session ID
Format: `<adapter>:<local-id>`. Parsed by `mux.ParseSessionID()`. First `:` splits adapter from local ID. Local IDs may contain `:` and Unicode.

### Session Lifecycle
- POST `/api/sessions` with `{"id":"adapter:name"}` → creates via `SessionCreator`
- DELETE `/api/sessions?id=adapter:name` → terminates via `SessionTerminator`
- `Registry.InvalidateAdapter(name)` forces refresh on next access

### Phase Plan Documents
`docs/ADAPTER_EXPANSION_PLAN.md` — Terminal Adapter Phase 0-7 plan.
`docs/AGENT_ADAPTER_LAYER_PLAN.md` — Agent Adapter Phase A0-A10 plan.
