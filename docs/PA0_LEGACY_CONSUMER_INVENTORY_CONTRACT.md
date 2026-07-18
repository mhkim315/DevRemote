# PA0 — Legacy Consumer Inventory and Ownership Contract

Status: **REVIEW REQUEST**

This document fulfills the Packet A0 requirement from `POST_CLAUDE_MANAGED_ONLY_RESTRUCTURING_PLAN.md`. It inventories all production consumers of legacy constructs, assigns their target migration owners, and defines the frozen call-site requirements for the new managed boundaries.

No production code is modified in this phase.

## 1. Exact Production Consumer Inventory

| Exact File | Exact Symbol / Route / Flag | Path Classification | Target Owner / Resolution | Migration Packet | Deletion Test / Criterion |
| --- | --- | --- | --- | --- | --- |
| `internal/term/runtime.go` | `Handlers.Registry`, `WithRegistry`, `RegistryFromContext` | legacy-only consumer | delete | A1 | Contexts never carry legacy registry; struct drops `*mux.Registry`. |
| `internal/term/pty.go` | `HandleSessionsAPI`, `HandleSessionCRUD`, `HandleWS` | legacy-only consumer | delete | A1, A2 | Legacy CRUD/WS routes removed; managed routes never invoke `mux.Registry`. |
| `internal/term/telemetry.go` | `HandleSessionsV2`, `mergeLifecycleState`, `buildSimpleSnapshot*` | mixed public composition | Catalog | A3 | `managed status` ignores `TelemetryService` and never reads legacy snapshots. |
| `internal/term/diagnostic.go` | Registry/Telemetry snapshot | legacy-only consumer | delete | A1 | Diagnostic dumps do not require or read `*mux.Registry`. |
| `internal/term/lifecycle_handlers.go` | REST lifecycle routes | mixed public composition | Runtime / provider | A2 | Managed session lifecycle REST routes bypass legacy routes. |
| `internal/term/recorder.go` | Recorder/VT/replay/subscriber | retained PTY primitive | TerminalTransport | A2 | Recorder remains single reader without legacy `adapter` interface wrapping. |
| `cmd/devremote/client.go` | `attachManagedSession`, `attachLocalTerminal`, linker CLI | mixed public composition | provider-specific transport | A2, A3 | `attachManagedSession` never opens raw PTY. Linker CLI fails/removed. |
| `cmd/devremote/main.go` | `--enable-localpty` | legacy-only consumer | delete | Phase B | CLI flag deleted. |
| `cmd/devremote/main.go` | `--enable-managed-codex`, `--enable-managed-claude` | managed-native production consumer | provider | Phase B | Always active natively; no legacy fallback needed. |
| `cmd/devremote/app.go` | `registry` / `links` instantiation, `ControlledPTY`/`LocalPTY` registration, IPC, routes, shutdown | mixed public composition | Catalog | A1, A2 | App struct loses `*mux.Registry` and `LinkStore`. IPC does not inject them. |
| `internal/term/create.go` | `createControlledSession`, `startRecorder`, `managedProcessIdentity` | retained PTY primitive | Runtime / TerminalTransport | A1, A2 | Managed runtimes are created natively without legacy adapter registration. |
| `internal/term/ipc.go` | `StartIPCServer`, `handleIPCConnection`, subscriber/attach routing | mixed public composition | Catalog / TerminalTransport | A1, A2 | IPC list uses Catalog; raw attach uses TerminalTransport; prompt uses structured transport. |
| `internal/term/lifecycle_service.go` | `NewLifecycleService`, `Stop`, `Kill`, `Delete` | mixed public composition | Runtime / provider | A2 | Stop/Kill operate natively without legacy `Registry`. |
| `internal/term/telemetry_service.go` | `NewTelemetryService`, `Snapshot`, launch binding/invalidation | legacy-only consumer | delete | A3 | Observer telemetry is completely deleted from managed path. |
| `internal/term/linkstore.go` | all symbols | legacy-only consumer | delete | A2 | `LinkStore` interface deleted; `/api/v2/links` route is unregistered. |
| `internal/term/managed_api.go` | managed read handlers, `/api/sessions` legacy/managed row composition | mixed public composition | Catalog | A1 | Read-only Catalog serves managed lists; never falls through to `mux.Registry`. |
| `internal/term/profiles.go` | `AgentProfile` validation | managed-native production consumer | provider | A1 | Profiles map strictly to managed providers, bypassing legacy adapters. |
| `internal/term/activity.go` | Activity buffers | mixed public composition | provider | A3 | Activity consumption routes do not touch legacy `TelemetryService`. |
| `internal/term/linker.go` | `LoadLinks`, `LinkSession`, `UnlinkSession` | legacy-only consumer | delete | A2 | File deleted. |
| `internal/mux/registry.go` | `mux.Registry` | physical deletion target | delete | Phase B | Repository-wide forbidden-reference gate passes. |
| `internal/mux/adapter.go` | `SessionAdapter` | physical deletion target | delete | Phase B | Repository-wide forbidden-reference gate passes. |
| `internal/mux/adapter_capability.go` | `Capabilities()`, `HasCapability()` | physical deletion target | delete | Phase B | Repository-wide forbidden-reference gate passes. |
| `internal/mux/session.go` | `Session` | physical deletion target | delete | Phase B | Repository-wide forbidden-reference gate passes. |
| `internal/mux/tracker.go` | all symbols | physical deletion target | delete | Phase B | Repository-wide forbidden-reference gate passes. |
| `internal/mux/transcript_capture.go` | all symbols | physical deletion target | delete | Phase B | Repository-wide forbidden-reference gate passes. |
| `internal/mux/cmux_delta.go` | all symbols | physical deletion target | delete | Phase B | Repository-wide forbidden-reference gate passes. |
| `internal/mux/id_parser.go` | all symbols | physical deletion target | delete | Phase B | Repository-wide forbidden-reference gate passes. |
| `internal/mux/tmux_adapter.go` | `tmux:` ID, adapter | physical deletion target | delete | Phase B | Repository-wide forbidden-reference gate passes. |
| `internal/mux/cmux_adapter.go` | `cmux:` ID, adapter | physical deletion target | delete | Phase B | Repository-wide forbidden-reference gate passes. |
| `internal/mux/localpty_adapter.go` | `localpty:` ID, adapter | physical deletion target | delete | Phase B | Repository-wide forbidden-reference gate passes. |
| `mobile/src/lib/lifecycle.ts` | legacy branch logic | mixed public composition | provider | A3 | Mobile client does not branch on external/best-effort capability. |
| `mobile/src/screens/FeedScreen.tsx` | cmux warnings / read-only fallback | mixed public composition | delete | A3 | UI never renders cmux/best-effort warning states. |
| `scripts/build-gate.sh` / docs | installation scripts, active flags, fixtures | legacy-only consumer | delete | Phase B | Build artifacts and scripts require no tmux/cmux executable or flags. |

## 2. Frozen Call-Site Requirements

Rather than inventing new interfaces, the migration relies on the existing accepted `ManagedSessionRegistry` and `ManagedSessionRecord` contracts, and strictly separates raw PTY transport from structured provider transport.

### 2.1 Managed Session Registry (Catalog)

The independent registries are the singular authority for managed runtime state. Existing call sites require:

- `Register(rec ManagedSessionRecord) error`
- `UpdateNativeStatus(sessionID string, epoch int64, status ManagedNativeStatus) bool`
- `MarkExited(sessionID string, epoch int64) bool`
- `Get(sessionID string) (ManagedSessionRecord, bool)`
- `List() []ManagedSessionRecord`
- `Remove(sessionID string) bool`

**No combined store:** Provider-specific identity/generation data (e.g. Codex vs. Claude) remains correctly scoped within their respective accepted registries. The federated read-only `Catalog` simply queries both existing managed registries for `/api/sessions` lists without creating a new combined store.

### 2.2 Transport and Authority Separation

1. **Owned PTY TerminalTransport:**
   For raw byte boundary (legacy controlled_pty replacement), the system must preserve the single-reader Recorder, raw VT byte ingestion, resize, and byte-slice Replay/Subscribe.
2. **Structured Provider Transport:**
   Managed Codex and Claude sessions are strictly structured; they have **no PTY**. They must not implement or be wrapped in raw Write/Resize/Replay bytes.
3. **Provider Lifecycle Transport:**
   Lifecycle routes (Stop/Kill/Delete) defer directly to the provider-specific managed service (`ManagedRuntime`).
4. **ApprovalAuthority:**
   The existing `ApprovalAuthority` remains the singular source for approvals.

## 3. Actionability Security Contract

All product sessions are managed; unsupported capabilities remain explicitly non-actionable and fail closed.

Actionability is dynamically resolved based on current runtime state, provider certification, exact action mapping, and consumption evidence. A session being "managed" does not automatically make it actionable.

## 4. Exit Criteria for PA0
- This document is independently accepted.
- No production code has been modified.
- PA1 may commence upon ACCEPT.
