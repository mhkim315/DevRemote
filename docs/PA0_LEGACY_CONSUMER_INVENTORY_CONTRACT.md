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
| `internal/term/diagnostic.go` | `Handlers.HandleDiagnostic` | legacy-only consumer | delete | A1 | `rg "mux\.Registry" companion-daemon/internal/term/diagnostic.go` -> 0 matches |
| `internal/term/lifecycle_handlers.go` | `HandleStopSession`, `HandleKillSession`, `HandleDeleteSession` | mixed public composition | Runtime / provider | A2 | `cd companion-daemon && go test ./internal/term -run '^TestManagedLifecycleREST_PositiveRouting$'` -> PASS (This test must be added in A2 to verify REST routes strictly hit managed services) |
| `internal/term/recorder.go` | `Recorder`, `VT`, `StartRecorder`, `EnsureRecorder`, `SubscribeWithBootstrap`, `WriteInput`, `Resize`, `GetSize`, `Bootstrap`, `Stop` | retained PTY primitive | TerminalTransport | A2 | `rg "\.Adapter\(\)" companion-daemon/internal/term/recorder.go` -> 0 matches |
| `cmd/devremote/client.go` | `attachManagedSession`, `attachLocalTerminal`, `attach` | mixed public composition | provider-specific transport | A2, A3 | `attachManagedSession` never opens raw PTY. |
| `cmd/devremote/main.go` | `--enable-localpty` | legacy-only consumer | delete | Phase B | `rg -- "--enable-localpty" cmd/devremote/main.go` -> 0 matches |
| `cmd/devremote/main.go` | `--enable-managed-codex`, `--enable-managed-claude` | mixed public composition | provider | Phase B | Provider activation remains capability/certification gated. Whether CLI flags are removed is a PA1 composition decision. No unsupported provider becomes active by default. |
| `cmd/devremote/app.go` | `registry` instantiation, `ControlledPTY` registration, `Handlers.Registry`, `LifecycleService and TelemetryService composition`, `/api/sessions`, `/api/v2/links`, `/term/ws`, `StartIPC` `*mux.Registry` injection | mixed public composition | Catalog / TerminalTransport | A1, A2 | App struct loses `*mux.Registry` and `LinkStore`. IPC does not inject them. |
| `internal/term/create.go` | `createControlledSession`, `startRecorder`, `managedProcessIdentity` | retained PTY primitive | Runtime / TerminalTransport | A1, A2 | Managed runtimes are created natively without legacy adapter registration. |
| `internal/term/ipc.go` | `StartIPCServer`, `handleIPCConnection`, `attach` | mixed public composition | Catalog / TerminalTransport | A1, A2 | IPC list uses Catalog; raw attach uses TerminalTransport; prompt uses structured transport. |
| `internal/term/linker.go` | `LoadLinks`, `LinkSession`, `UnlinkSession`, `GetLink`, `GetAllLinks`, `HandleLinksAPI`, `/api/v2/links` GET/POST/DELETE routes | legacy-only consumer | delete | A2 | `rg "HandleLinksAPI" companion-daemon/internal/term` -> 0 matches |
| `internal/term/lifecycle_service.go` | `NewLifecycleService`, `Stop`, `Kill`, `Delete` | mixed public composition | Runtime / provider | A2 | Stop/Kill operate natively without legacy `Registry`. |
| `internal/term/telemetry_service.go` | `Snapshot` | legacy-only consumer | delete | A3 | `rg "Snapshot\(" companion-daemon/internal/term/telemetry_service.go` -> 0 matches |
| `internal/term/telemetry_service.go` | `RegisterOrReplaceLaunch`, `invalidateForLaunch`, `Clear` | mixed public composition | Catalog / Identity | A3 | Retained/migrated to support accurate accepted identity behavior. |
| `internal/term/linkstore.go` | `LinkStore`, `NewNopLinkStore`, `fileLinkStore`, `NewFileLinkStoreAt`, `Load`, `Get`, `List`, `Put`, `Delete` | legacy-only consumer | delete | A2 | `rg "LinkStore" companion-daemon/internal/term` -> 0 matches |
| `internal/term/managed_api.go` | `HandleManagedSessions`, `appendManagedRows`, `appendClaudeManagedRows` | managed-native production consumer | Catalog | A1 | Read-only Catalog serves managed lists; never falls through to `mux.Registry`. |
| `internal/term/profiles.go` | `sessionProfile`, `builtinProfiles`, `ResolveProfile`, `ProfileLabel`, `HandleSessionProfiles` | managed-native production consumer | provider | A1 | Profiles map strictly to managed providers, bypassing legacy adapters. |
| `internal/term/activity.go` | `ActivityBuffer`, `ActivityBuffer.Append`, `ActivityBuffer.List`, `ActivityBuffer.Clear` | PTY/semantic projection boundary | PTY/semantic projection boundary | A3 | Activity consumption routes do not touch legacy `TelemetryService`. |
| `internal/transcript/api.go` | `HandleTranscript`, `HandleTranscriptStats` | PTY/semantic projection boundary | PTY/semantic projection boundary | A3 | Package preserved and decoupled from legacy telemetry. |
| `internal/transcript/service.go` | `FeedBytes`, `ProjectAgentEvents`, `AddSnapshotSegment`, `BuildResponse` | PTY/semantic projection boundary | PTY/semantic projection boundary | A3 | Package preserved and decoupled from legacy telemetry. |
| `internal/transcript/launch_binding.go` | `LaunchRegistry.RegisterOrReplace`, `RegisterOrReplaceLaunch`, `LookupLaunch`, `RemoveLaunch`, `LaunchCorrelation` | PTY/semantic projection boundary | PTY/semantic projection boundary | A3 | Package preserved and decoupled from legacy telemetry. |
| `internal/transcript/store.go` | `Store`, `Append`, `List`, `ListAfter`, `Clear`, `Stats` | PTY/semantic projection boundary | PTY/semantic projection boundary | A3 | Package preserved and decoupled from legacy telemetry. |
| `internal/mux/registry.go` | `Register`, `CreateSession`, `TerminateSession`, `Adapter`, `Adapters`, `FindSession`, `InvalidateAdapter`, `Invalidate`, `Refresh`, `Sessions`, `Snapshot`, `FindSessionInCache`, `MigrateLegacyID` | physical deletion target | delete | Phase B | `rg -w "mux\.Registry" companion-daemon/internal/term` -> 0 matches |
| `internal/mux/adapter.go` | `Adapter`, `TerminalStream`, `StreamOpener`, `InputWriter`, `ProcessProvider`, `HistoryReader`, `ScreenReader`, `SessionCreator`, `SessionTerminator` | retained PTY primitive | TerminalTransport | A2 | `retained TerminalStream interfaces relocated before session.go deletion` |
| `internal/mux/session.go` | `NativeSession`, `ID`, `AdapterName`, `Read`, `Write`, `Resize`, `GetSize`, `Close` | retained PTY primitive | TerminalTransport | A2 | `retained TerminalStream interfaces relocated before session.go deletion` |
| `internal/mux/id_parser.go` | `SessionRef`, `ParseSessionID`, `Canonical`, `ValidateAdapterName` | mixed public composition | provider-neutral identity | A2 | Canonical identity parser relocated to provider-neutral package; byte-for-byte meaning preserved. |
| `internal/term/approval_delivery.go` | `validSessionID`, `validAdapterID` | mixed public composition | provider-neutral identity | A2 | Preserved and migrated to provider-neutral identity package. |
| `internal/term/managed_claude_activation.go` | `NewCombinedRuntimeResolver` | mixed public composition | provider-neutral identity | A2 | Preserved and migrated to provider-neutral identity package. |
| `internal/mux/controlled_pty_adapter.go` | `NewControlledPTYAdapter`, `ListSessions`, `CreateSession`, `TerminateSession` | legacy-only consumer | delete | A2 | `rg "NewControlledPTYAdapter" companion-daemon/internal/term` -> 0 matches |
| `internal/mux/controlled_pty_adapter.go` | `TerminateGroup`, `ProcessInfo`, `OpenStream`, `WriteInput`, `controlledPTYSession` | retained PTY primitive | TerminalTransport | A2 | Retained responsibilities migrated to pure TerminalTransport bound to process group before adapter deletion. |
| `internal/mux/adapter_capability.go` | `AdapterCapabilities`, `hasCap`, `ManagedLifecycleProvider`, `AdapterCapability` | physical deletion target | delete | Phase B | `rg "AdapterCapability" companion-daemon/internal/term` -> 0 matches |
| `internal/mux/tracker.go` | `TrackCmuxPanels` | physical deletion target | delete | Phase B | `rg "TrackCmuxPanels" companion-daemon/internal/mux` -> 0 matches |
| `internal/mux/transcript_capture.go` | `TranscriptCaptureMode()` | physical deletion target | delete | Phase B | `rg "TranscriptCaptureMode" companion-daemon/internal/mux` -> 0 matches |
| `internal/mux/cmux_delta.go` | `cmuxDeltaSnapshot`, `parseCmuxDelta` | physical deletion target | delete | Phase B | `rg "cmuxDeltaSnapshot" companion-daemon/internal/mux` -> 0 matches |
| `internal/mux/tmux_adapter.go` | `NewTmuxAdapter`, `tmuxAdapter`, `tmuxSession`, `tmuxExecRunner`, `parseTmuxListSessionLine`, `resolveTmuxTarget` | physical deletion target | delete | Phase B | `rg "tmuxAdapter" companion-daemon/internal/mux` -> 0 matches |
| `internal/mux/cmux_adapter.go` | `NewCmuxAdapter`, `cmuxAdapter`, `CmuxSession`, `serialCommandRunner`, `execCommandRunner`, `logCmuxError`, `parseCmuxTop`, `mapAgentProcesses` | physical deletion target | delete | Phase B | `rg "cmuxAdapter" companion-daemon/internal/mux` -> 0 matches |
| `internal/mux/localpty_adapter.go` | `NewLocalPTYAdapter`, `localptyAdapter`, `localptySession` | physical deletion target | delete | Phase B | `rg "localptyAdapter" companion-daemon/internal/mux` -> 0 matches |
| `mobile/src/lib/lifecycle.ts` | history/screen fallback branches | legacy-only consumer | delete | A3 | Mobile client does not branch on external/best-effort capability. |
| `mobile/src/screens/FeedScreen.tsx` | cmux warnings / read-only fallback | legacy-only consumer | delete | A3 | UI never renders cmux/best-effort warning states. |
| `scripts/dev-setup.sh` | `--enable-localpty` | legacy-only consumer | delete | Phase B | `rg -- "--enable-localpty" scripts/dev-setup.sh scripts/install.sh` -> 0 matches |
| `scripts/install.sh` | `--enable-localpty` | legacy-only consumer | delete | Phase B | `rg -- "--enable-localpty" scripts/dev-setup.sh scripts/install.sh` -> 0 matches |
| `tests and fixtures` | `tmux_adapter_test.go`, `cmux_adapter_test.go`, `cmux_tree_parser_test.go`, `cmux_adapter_mock_test.go`, `cmux_delta_poc_test.go`, `cmux_forceflush_diag_test.go`, `cmux_top_test.go`, `localpty_adapter_test.go`, `localpty_e2e_test.go`, `testdata/cmux`, `phase1_test.go`, `registry_test.go`, `adapter_capability_test.go`, `adapter_contract_test.go`, `integration_test.go`, `linkstore_test.go`, `fixture_e2e_test.go`, `telemetry_test.go`, `capability_boundary_test.go` | legacy-only consumer | delete | Phase B | These tests explicitly removed as they assert legacy provider/store features. |
| `tests and fixtures` | `controlled_pty_test.go`, `pty_ws_test.go`, `process_session_test.go` | retained PTY primitive | migrate | Phase B | Migrated to test pure TerminalTransport boundaries. |
| `tests and fixtures` | `lifecycle_blocker_test.go`, `s1e_correctness_test.go`, `api_golden_test.go`, `approval_store_gen_test.go`, `phase7_golden_test.go`, `runtime_test.go`, `production_bridge_test.go` | mixed public composition | rewrite | Phase B | Rewritten to use managed providers. |
| `docs/...` | Any active documentation mentioning tmux/cmux/localpty | legacy-only consumer | delete | Phase B | `find docs/ -name "*.md" \| grep -v -F -f docs/.historical_allowlist \| xargs rg -l "tmux\|cmux\|localpty"` -> 0 matches |

## 2. Frozen-Unaffected Boundaries

The following accepted paths are explicitly NOT targets for legacy migration and remain unchanged. They have no `mux` dependency and must not be modified by PA1:

| Unaffected Core System | Exact File / Symbol | Path Classification | Target Owner / Resolution | Migration Packet | Deletion Test / Criterion |
| --- | --- | --- | --- | --- | --- |
| `Approval RuntimeOf` | `internal/term/approval_execution.go`: `RuntimeRef`<br>`internal/term/managed_approval_activation.go`: `RuntimeOf` (Codex)<br>`internal/term/managed_claude_activation.go`: `RuntimeOf` (Claude) | frozen-unaffected | frozen-unaffected | none | unchanged/frozen, no mux dependency |
| `Codex/Claude delivery dispatch` | `internal/term/managed_approval_delivery.go`<br>`internal/term/claude_approval_delivery.go` | frozen-unaffected | frozen-unaffected | none | unchanged/frozen, no mux dependency |
| `claim/receipt/commit Store` | `internal/term/approval_store_gen.go`: `ClaimForExecution`, `RecordDelivery` | frozen-unaffected | frozen-unaffected | none | unchanged/frozen, no mux dependency |
| `provider-native consumption witness` | `internal/term/claude_resume_coordinator.go`: `MarkWitnessed`, `WitnessKind`<br>`internal/term/managed_approval_delivery.go`: `CodexManagedApprovalDelivery.Deliver`, `deliverResponse` | frozen-unaffected | frozen-unaffected | none | unchanged/frozen, no mux dependency |

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
