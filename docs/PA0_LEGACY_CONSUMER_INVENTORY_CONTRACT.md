# PA0 — Legacy Consumer Inventory and Ownership Contract

Status: **REVIEW REQUEST**

This document fulfills the Packet A0 requirement from `POST_CLAUDE_MANAGED_ONLY_RESTRUCTURING_PLAN.md`. It inventories all production consumers of legacy constructs, assigns their target migration owners, and defines the frozen call-site requirements for the new managed boundaries.

No production code is modified in this phase.

## 1. Exact Production Consumer Inventory

| Exact File | Exact Symbol / Route / Flag | Used in Managed Path? | Target Owner / Resolution | Migration Packet | Deletion Test / Criterion |
| --- | --- | --- | --- | --- | --- |
| `internal/term/runtime.go` | `Handlers.Registry`, `WithRegistry`, `RegistryFromContext` | Yes (legacy fallback) | `ManagedSessionRegistry` | A1 | `Handlers` struct drops `*mux.Registry`; contexts never carry legacy registry. |
| `internal/term/pty.go` | `HandleSessionsAPI`, `HandleSessionCRUD`, `HandleWS` | Yes | `ManagedSessionRegistry` | A1, A2 | `HandleManagedSessions` never invokes `mux.Registry` or legacy CRUD routes. |
| `internal/term/telemetry.go` | `HandleSessionsV2`, `mergeLifecycleState`, `buildSimpleSnapshot*` | Yes | `ManagedSessionRegistry` (Managed DTOs) | A3 | `managed status` ignores `TelemetryService` and never reads legacy snapshots. |
| `internal/term/diagnostic.go` | Registry/Telemetry snapshot | No | `ManagedSessionRegistry` | A1 | Diagnostic dumps do not require or read `*mux.Registry`. |
| `internal/term/lifecycle_handlers.go` | REST lifecycle routes | Yes | `ManagedSessionRegistry` | A2 | Managed session lifecycle REST routes bypass legacy routes. |
| `internal/term/recorder.go` | Recorder/VT/replay/subscriber | Yes | Owned PTY Terminal Transport | A2 | Recorder remains single reader without legacy `adapter` interface wrapping. |
| `cmd/devremote/client.go` | `attachManagedSession`, `attachLocalTerminal`, linker CLI | Yes | Structured Provider Transport | A2, A3 | `attachManagedSession` never opens raw PTY. Linker CLI fails/removed. |
| `cmd/devremote/main.go` | `--enable-localpty`, `--managed-provider` flags | Yes | Hardcoded/Removed | Phase B | CLI flags deleted; managed providers always active; no localpty flag. |
| `cmd/devremote/app.go` | Registry creation, `ControlledPTY`/`LocalPTY` registration, IPC, routes, shutdown | Yes | `ManagedSessionRegistry` | A1, A2 | App struct loses `*mux.Registry` and `LinkStore`. IPC does not inject them. |
| `internal/term/agent_activity_api.go` | Activity REST handlers | Yes | Managed DTOs / Events | A3 | Activity consumption routes do not touch legacy `TelemetryService`. |
| `internal/term/linker.go` | `LoadLinks`, `LinkSession`, `UnlinkSession` | No | **DELETED** | A2 | `/api/v2/links` route is unregistered. |
| `scripts/build-gate.sh` / docs | installation scripts, fixtures, active flags | No | **DELETED** | Phase B | Build artifacts and scripts require no tmux/cmux executable or flags. |
| `internal/mux/tmux_adapter.go` | `tmux:` ID, adapter | No | **DELETED** | Phase B | File and symbols deleted. |
| `internal/mux/cmux_adapter.go` | `cmux:` ID, adapter, snapshot | No | **DELETED** | Phase B | File and symbols deleted. |
| `internal/mux/localpty_adapter.go` | `localpty:` ID, adapter | No | **DELETED** | Phase B | File and symbols deleted. |
| `internal/mux/discovery.go` | `GetSessions`, `FindSession` | No | **DELETED** | Phase B | File and symbols deleted. |
| `mobile/src/lib/client.ts` | best-effort branches | Yes | **DELETED** | A3 | Mobile client does not branch on external/best-effort capability. |
| `mobile/src/screens/FeedScreen.tsx` | cmux warnings / read-only fallback | Yes | **DELETED** | A3 | UI never renders cmux/best-effort warning states. |
| `mobile/src/screens/DashboardScreen.tsx` | cmux warnings | Yes | **DELETED** | A3 | UI never renders cmux/best-effort warning states. |

## 2. Frozen Call-Site Requirements

Rather than inventing new interfaces, the migration relies on the existing accepted `ManagedSessionRegistry` and `ManagedSessionRecord` contracts, and strictly separates raw PTY transport from structured provider transport.

### 2.1 Managed Session Registry

The registry is the singular authority for managed runtime state. Existing call sites require:

- `Register(rec ManagedSessionRecord) error`
- `UpdateNativeStatus(sessionID string, epoch int64, status ManagedNativeStatus) bool`
- `MarkExited(sessionID string, epoch int64) bool`
- `Get(sessionID string) (ManagedSessionRecord, bool)`
- `List() []ManagedSessionRecord`
- `Remove(sessionID string) bool`

**No combined store:** Provider-specific identity/generation data (e.g. Codex vs. Claude) remains correctly scoped within their respective accepted registries.

### 2.2 Transport Separation

1. **Owned PTY TerminalTransport:**
   For raw byte boundary (legacy controlled_pty replacement), the system must preserve the single-reader Recorder, raw VT byte ingestion, resize, and byte-slice Replay/Subscribe.
2. **Structured Provider Transport:**
   Managed Codex and Claude sessions are strictly structured; they have **no PTY**. They must not implement or be wrapped in raw Write/Resize/Replay bytes.
3. **Provider Lifecycle/Approval Transport:**
   Approval and lifecycle use exact authenticated endpoints (e.g. `HandleManagedSessionPrompt`, `HandleManagedClaudeSessionKill`), completely decoupled from generic terminal transport.

## 3. Actionability Security Contract

All product sessions are managed; unsupported capabilities remain explicitly non-actionable and fail closed.

Actionability is dynamically resolved based on current runtime state, provider certification, exact action mapping, and consumption evidence. A session being "managed" does not automatically make it actionable.

## 4. Exit Criteria for PA0
- This document is independently accepted.
- No production code has been modified.
- PA1 may commence upon ACCEPT.
