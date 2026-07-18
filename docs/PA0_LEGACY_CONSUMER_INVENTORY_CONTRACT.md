# PA0 — Legacy Consumer Inventory and Ownership Contract

Status: **REVIEW REQUEST**

This document fulfills the Packet A0 requirement from `POST_CLAUDE_MANAGED_ONLY_RESTRUCTURING_PLAN.md`. It inventories all production consumers of legacy constructs, assigns their target migration owners, and defines the frozen call-site requirements for the new managed boundaries.

No production code is modified in this phase.

## 1. Exact Production Consumer Inventory

| Exact File | Exact Symbol / Route / Flag | Path Classification | Target Owner / Resolution | Migration Packet | Deletion Test / Criterion |
| --- | --- | --- | --- | --- | --- |
| `internal/term/runtime.go` | `Handlers.Registry`, `WithRegistry`, `RegistryFromContext` | legacy-only consumer | delete | A1 | `rg "mux\.Registry" companion-daemon/internal/term/runtime.go` -> 0 matches |
| `internal/term/pty.go` | `HandleSessionsAPI`, `HandleSessionCRUD` | legacy-only consumer | delete | A1, A2 | `rg "HandleSessionsAPI|HandleSessionCRUD" companion-daemon/internal/term` -> 0 matches |
| `internal/term/pty.go` | `HandleWS` | retained PTY primitive | TerminalTransport | A2 | Retained as TerminalTransport WebSocket bridge. |
| `internal/term/telemetry.go` | `HandleSessionsV2`, `mergeLifecycleState`, `buildSimpleSnapshot` | mixed public composition | Catalog | A3 | `rg "TelemetryService" companion-daemon/internal/term/telemetry.go` -> deterministic negative |
| `internal/term/diagnostic.go` | `dumpDiagnostic` | legacy-only consumer | delete | A1 | `rg "mux\.Registry" companion-daemon/internal/term/diagnostic.go` -> 0 matches |
| `internal/term/lifecycle_handlers.go` | `HandleStopSession`, `HandleKillSession`, `HandleDeleteSession` | mixed public composition | Runtime / provider | A2 | `rg "HandleStopSession" companion-daemon/internal/term` -> 0 matches |
| `internal/term/recorder.go` | `Recorder`, `startRecorder`, `VT`, replay, subscriber | retained PTY primitive | TerminalTransport | A2 | `rg "\.Adapter\(\)" companion-daemon/internal/term/recorder.go` -> 0 matches |
| `cmd/devremote/client.go` | `attachManagedSession`, `attachLocalTerminal`, `attach` | mixed public composition | provider-specific transport | A2, A3 | `attachManagedSession` never opens raw PTY. |
| `cmd/devremote/main.go` | `--enable-localpty` | legacy-only consumer | delete | Phase B | `rg "enable-localpty" cmd/devremote/main.go` -> 0 matches |
| `cmd/devremote/main.go` | `--enable-managed-codex`, `--enable-managed-claude` | mixed public composition | provider | Phase B | Provider activation remains capability/certification gated. Whether CLI flags are removed is a PA1 composition decision. No unsupported provider becomes active by default. |
| `cmd/devremote/app.go` | `registry` instantiation, `ControlledPTY` registration, `/api/sessions`, `/api/sessions/{id}`, `/api/sessions/{id}/ws` | mixed public composition | Catalog / TerminalTransport | A1, A2 | App struct loses `*mux.Registry` and `LinkStore`. IPC does not inject them. |
| `internal/term/create.go` | `createControlledSession`, `startRecorder`, `managedProcessIdentity` | retained PTY primitive | Runtime / TerminalTransport | A1, A2 | Managed runtimes are created natively without legacy adapter registration. |
| `internal/term/ipc.go` | `StartIPCServer`, `handleIPCConnection`, `attach` | mixed public composition | Catalog / TerminalTransport | A1, A2 | IPC list uses Catalog; raw attach uses TerminalTransport; prompt uses structured transport. |
| `internal/term/lifecycle_service.go` | `NewLifecycleService`, `Stop`, `Kill`, `Delete` | mixed public composition | Runtime / provider | A2 | Stop/Kill operate natively without legacy `Registry`. |
| `internal/term/telemetry_service.go` | `Snapshot` | legacy-only consumer | delete | A3 | `rg "Snapshot\(" companion-daemon/internal/term/telemetry_service.go` -> 0 matches |
| `internal/term/telemetry_service.go` | `RegisterOrReplaceLaunch`, `invalidateForLaunch`, `Clear` | mixed public composition | Catalog / Identity | A3 | Retained/migrated to support accurate accepted identity behavior. |
| `internal/term/linkstore.go` | `LinkStore`, `FileLinkStore`, `NewFileLinkStore`, `NewNopLinkStore`, `Load`, `Save`, `GetLink`, `DeleteLink` | legacy-only consumer | delete | A2 | `rg "LinkStore" companion-daemon/internal/term` -> 0 matches |
| `internal/term/managed_api.go` | `HandleManagedSessions`, `appendManagedRows`, `appendClaudeManagedRows` | managed-native production consumer | Catalog | A1 | Read-only Catalog serves managed lists; never falls through to `mux.Registry`. |
| `internal/term/profiles.go` | `sessionProfile`, `builtinProfiles`, `ResolveProfile`, `ProfileLabel`, `HandleSessionProfiles` | managed-native production consumer | provider | A1 | Profiles map strictly to managed providers, bypassing legacy adapters. |
| `internal/term/activity.go` | `ActivityBuffer`, `RecordActivity`, `GetActivity` | PTY/semantic projection boundary | PTY/semantic projection boundary | A3 | Activity consumption routes do not touch legacy `TelemetryService`. |
| `internal/transcript/api.go` | `HandleTranscript`, `HandleTranscriptStats` | PTY/semantic projection boundary | PTY/semantic projection boundary | A3 | Package preserved and decoupled from legacy telemetry. |
| `internal/transcript/service.go` | `FeedBytes`, `ProjectAgentEvents`, `AddSnapshotSegment`, `BuildResponse` | PTY/semantic projection boundary | PTY/semantic projection boundary | A3 | Package preserved and decoupled from legacy telemetry. |
| `internal/transcript/launch_binding.go` | `bindLaunch`, `validateLaunch` | PTY/semantic projection boundary | PTY/semantic projection boundary | A3 | Package preserved and decoupled from legacy telemetry. |
| `internal/transcript/store.go` | `TranscriptStore`, `StoreEvent` | PTY/semantic projection boundary | PTY/semantic projection boundary | A3 | Package preserved and decoupled from legacy telemetry. |
| `internal/mux/registry.go` | `Register`, `CreateSession`, `TerminateSession`, `Adapter`, `Adapters`, `FindSession`, `InvalidateAdapter`, `Invalidate`, `Refresh`, `Sessions`, `Snapshot`, `FindSessionInCache` | physical deletion target | delete | Phase B | `rg -w "mux\.Registry" companion-daemon/internal/term` -> 0 matches |
| `internal/mux/adapter.go` | `Adapter`, `TerminalStream`, `StreamOpener`, `InputWriter`, `ProcessProvider`, `HistoryReader`, `ScreenReader` | retained PTY primitive | TerminalTransport | A2 | `retained TerminalStream interfaces relocated before session.go deletion` |
| `internal/mux/session.go` | `NativeSession`, `ID`, `AdapterName`, `Read`, `Write`, `Resize`, `GetSize`, `Close` | retained PTY primitive | TerminalTransport | A2 | `retained TerminalStream interfaces relocated before session.go deletion` |
| `internal/mux/id_parser.go` | `SessionRef`, `ParseSessionID`, `Canonical`, `ValidateAdapterName` | mixed public composition | provider-neutral identity | A2 | Canonical identity parser relocated to provider-neutral package; byte-for-byte meaning preserved. |
| `internal/mux/controlled_pty_adapter.go` | `NewControlledPTYAdapter`, `ListSessions`, `CreateSession`, `TerminateSession` | legacy-only consumer | delete | A2 | `rg "NewControlledPTYAdapter" companion-daemon/internal/term` -> 0 matches |
| `internal/mux/controlled_pty_adapter.go` | `TerminateGroup`, `ProcessInfo`, `OpenStream`, `WriteInput`, `controlledPTYSession` | retained PTY primitive | TerminalTransport | A2 | Retained responsibilities migrated to pure TerminalTransport bound to process group before adapter deletion. |
| `internal/mux/adapter_capability.go` | `AdapterCapabilities`, `hasCap`, `ManagedLifecycleProvider`, `AdapterCapability` | physical deletion target | delete | Phase B | `rg "AdapterCapability" companion-daemon/internal/term` -> 0 matches |
| `internal/mux/tracker.go` | `SessionTracker`, `Track`, `Untrack`, `List` | physical deletion target | delete | Phase B | `rg "SessionTracker" companion-daemon/internal/mux` -> 0 matches |
| `internal/mux/transcript_capture.go` | `TranscriptCaptureProvider`, `TranscriptCaptureMode`, `Capture` | physical deletion target | delete | Phase B | `rg "TranscriptCaptureProvider" companion-daemon/internal/mux` -> 0 matches |
| `internal/mux/cmux_delta.go` | `cmuxDeltaSnapshot`, `parseCmuxDelta` | physical deletion target | delete | Phase B | `rg "cmuxDeltaSnapshot" companion-daemon/internal/mux` -> 0 matches |
| `internal/mux/tmux_adapter.go` | `tmux:` ID, adapter | physical deletion target | delete | Phase B | `rg "tmux:" companion-daemon/internal/mux` -> 0 matches |
| `internal/mux/cmux_adapter.go` | `cmux:` ID, adapter | physical deletion target | delete | Phase B | `rg "cmux:" companion-daemon/internal/mux` -> 0 matches |
| `internal/mux/localpty_adapter.go` | `localpty:` ID, adapter | physical deletion target | delete | Phase B | `rg "localpty:" companion-daemon/internal/mux` -> 0 matches |
| `mobile/src/lib/lifecycle.ts` | history/screen fallback branches | legacy-only consumer | delete | A3 | Mobile client does not branch on external/best-effort capability. |
| `mobile/src/screens/FeedScreen.tsx` | cmux warnings / read-only fallback | legacy-only consumer | delete | A3 | UI never renders cmux/best-effort warning states. |
| `scripts/dev-setup.sh` | `--enable-localpty` guidance/flags | legacy-only consumer | delete | Phase B | Build artifacts and scripts require no tmux/cmux executable or flags. |
| `scripts/install.sh` | `--enable-localpty` guidance/flags | legacy-only consumer | delete | Phase B | Build artifacts and scripts require no tmux/cmux executable or flags. |

## 2. Frozen-Unaffected Boundaries

The following accepted paths are explicitly NOT targets for legacy migration and remain unchanged. They have no `mux` dependency and must not be modified by PA1:

| Unaffected Core System | Exact File / Symbol | Path Classification | Target Owner / Resolution | Migration Packet | Deletion Test / Criterion |
| --- | --- | --- | --- | --- | --- |
| `Approval RuntimeOf` | `internal/term/approval_store.go: RuntimeOf` | frozen-unaffected | frozen-unaffected | none | unchanged/frozen, no mux dependency |
| `Codex/Claude delivery dispatch` | `internal/term/managed_codex.go`, `internal/term/claude_boundary.go` | frozen-unaffected | frozen-unaffected | none | unchanged/frozen, no mux dependency |
| `claim/receipt/commit Store` | `internal/term/authoritative_approval_store.go`, `internal/term/approval_delivery.go` | frozen-unaffected | frozen-unaffected | none | unchanged/frozen, no mux dependency |
| `provider-native consumption witness` | `internal/term/approval_delivery.go: verifyConsumption` | frozen-unaffected | frozen-unaffected | none | unchanged/frozen, no mux dependency |

## 3. Frozen Call-Site Requirements

Rather than inventing new interfaces, the migration relies on the existing accepted `ManagedSessionRegistry` and `ManagedSessionRecord` contracts, and strictly separates raw PTY transport from structured provider transport.

### 3.1 Managed Session Registry (Catalog)

The independent registries are the singular authority for managed runtime state. Existing call sites require:

- `Register(rec ManagedSessionRecord) error`
- `UpdateNativeStatus(sessionID string, epoch int64, status ManagedNativeStatus) bool`
- `MarkExited(sessionID string, epoch int64) bool`
- `Get(sessionID string) (ManagedSessionRecord, bool)`
- `List() []ManagedSessionRecord`
- `Remove(sessionID string) bool`

**No combined store:** Provider-specific identity/generation data (e.g. Codex vs. Claude) remains correctly scoped within their respective accepted registries. The federated read-only `Catalog` simply queries both existing managed registries for `/api/sessions` lists without creating a new combined store.

### 3.2 Transport and Authority Separation

1. **Owned PTY TerminalTransport:**
   For raw byte boundary (legacy controlled_pty replacement), the system must preserve the single-reader Recorder, raw VT byte ingestion, resize, and byte-slice Replay/Subscribe.
2. **Structured Provider Transport:**
   Managed Codex and Claude sessions are strictly structured; they have **no PTY**. They must not implement or be wrapped in raw Write/Resize/Replay bytes.
3. **Provider Lifecycle Transport:**
   Lifecycle routes (Stop/Kill/Delete) defer directly to the provider-specific managed service (`ManagedRuntime`).
4. **ApprovalAuthority:**
   The existing `ApprovalAuthority` remains the singular source for approvals.

## 4. Actionability Security Contract

All product sessions are managed; unsupported capabilities remain explicitly non-actionable and fail closed.

Actionability is dynamically resolved based on current runtime state, provider certification, exact action mapping, and consumption evidence. A session being "managed" does not automatically make it actionable.

## 5. Exit Criteria for PA0
- This document is independently accepted.
- No production code has been modified.
- PA1 may commence upon ACCEPT.
