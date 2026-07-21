# PB.5b Task 3 — Physical Deletion

This change physically removes the obsolete lifecycle implementation and its
implementation-bound tests. No compatibility package, lookup map, wrapper, or
bridge remains. Tests which exercise current behaviour use the V1 launcher,
`PTYHandle`, `LaunchIdentity`, `ProcessCleanup`, `OwnedPTYRuntime`, and direct
generation-owned `Recorder` references.

## Required V1 properties

| Property | Non-vacuous V1 evidence |
|---|---|
| Same-ID replacement isolation | `TestPB5V1_ReplacementCleansOnlyPriorGeneration` launches two results for one ID and asserts only the first cleanup runs; `TestStep6a_ReplacementCapturesOldRecorder` asserts the replacement has a distinct identity and recorder. |
| Failed-create rollback | `TestPA2c_R2_CreateWithoutHandle_FailsWithoutPublishing` supplies an incomplete launch result, checks cleanup is invoked once, and checks the catalog remains empty. |
| A stale finalizer cannot affect a replacement | `TestPA2c_OwnedPTY_StaleGenerationRejected` finalizes the old generation after replacement and asserts the new generation stays running; `TestPA2c_R2_ReplacementBetweenClaimAndCleanup_NotTerminated` exercises the claim/cleanup race. |
| Cleanup and terminal completion occur exactly once | `TestPB5V1_ProcessCleanupIsExactlyOnce` concurrently invokes cleanup; `TestPA2c_OwnedPTY_ExactlyOnceTerminalConvergence` compares the original and repeated terminal transition timestamps. |
| No lock spans external lifecycle I/O | `TestPA2c_R1_ReplacementDuringBlockedSignal_NeverSignalsNewProcess` blocks the old handle's signal while registering a replacement and requires registration to complete before the signal is released. |
| Old-generation cleanup cannot delete or terminate a new generation | `TestPA2c_R1_ReplacementDuringBlockedSignal_NeverSignalsNewProcess`, `TestStep6a_RollbackCleanupIsInstanceGuarded`, and `TestPB5V1_ReplacementCleansOnlyPriorGeneration` each assert the new handle/cleanup remains untouched. |

## Deleted-test migration map

The following test files were tied to the removed implementation. The map is
by source file because the listed tests shared the same removed construction
surface; it records the replacement V1 tests that execute the corresponding
live contract. A `removed implementation` entry has no semantic successor:
it tested a type or wrapper that no longer exists, rather than a supported
contract.

| Removed source | Former test scope | V1 successor or disposition |
|---|---|---|
| `cmd/devremote/app_test.go` | app composition, shutdown, tunnel and route wiring | `cmd/devremote/auth_e2e_test.go`, `cmd/devremote/app_lifecycle_v1_test.go`, and `cmd/devremote/test_helpers_test.go` construct the V1 dependency graph; shutdown/tunnel tests were implementation-bound to the removed construction surface. |
| `internal/mux/adapter.go`, `adapter_capability_test.go`, `adapter_contract_test.go`, `adapter_golden_test.go` | adapter interface and capability contract | removed implementation; V1 equivalents are `PTYHandle`, `ManagedPTYLauncherV1`, and `TestPB5V1_StopUsesTypedSignalAndWait`. |
| `internal/mux/controlled_pty_adapter.go`, `controlled_pty_test.go`, `controlled_pty_r6_test.go`, `pty_spawn.go`, `transcript_capture.go` | old controlled-PTY spawn, capture, and identity termination | `TestPB5V1_ReplacementCleansOnlyPriorGeneration`, `TestStep6a_ReplacementCapturesOldRecorder`, `TestPA2c_R2_ReplacementBetweenClaimAndCleanup_NotTerminated`, and `TestLifecycle_Stop_RealProcess_TerminatesAndRetainsHistory`. |
| `internal/mux/fixture_adapter_test.go` | old fixture adapter | removed implementation; `migrationLauncher` and `migrationHandle` are direct V1 fixtures used by the successor tests above. |
| `internal/mux/id_parser.go`, `id_parser_compat_test.go`, `phase1_test.go` identity cases | compatibility parser/wrapper and registry discovery | `internal/sessionid/sessionid_test.go` and `TestPA2b_ArchGate_NoDuplicateParser`; registry-only cases are removed implementation. |
| `internal/mux/registry.go`, `registry_test.go`, `registry_atomic_test.go` | registry caching, replacement, and atomic termination | `TestPA2c_R1_ReplacementDuringBlockedSignal_NeverSignalsNewProcess`, `TestPA2c_OwnedPTY_StaleGenerationRejected`, `TestPB5V1_ReplacementCleansOnlyPriorGeneration`, and `TestPB5V1_ProcessCleanupIsExactlyOnce`. |
| `internal/term/launcher_test_helper.go` | old-launcher test helper | replaced by direct V1 fixtures in `pb5_v1_migration_test.go`, `step6a_test.go`, and `legacy_fixture_support_test.go`. |
| `internal/term/agent_agnostic_test.go` | tolerant managed-session DTO decoding | `internal/term/managed_io_test.go` and `internal/term/pa4_1_isolation_test.go`. |
| `internal/term/api_golden_test.go`, `capability_boundary_test.go` | session API status and capability projection | `create_test.go`, `lifecycle_state_api_test.go`, `pa4_1_isolation_test.go`, and `managed_io_test.go`. |
| `internal/term/controlled_pty_e2e_test.go`, `fixture_e2e_test.go` | controlled PTY creation, output, resize, deletion, and telemetry | `create_test.go`, `lifecycle_test.go`, `pa4_5_isolation_test.go`, and `managed_io_test.go`. |
| `internal/term/managed_api_test.go`, `managed_catalog_test.go`, `managed_remediation_test.go` | managed catalog/provider isolation and bounded lifecycle behaviour | `managed_io_test.go`, `lifecycle_pa2c_test.go`, `pa4_1_isolation_test.go`, `pa4_2_isolation_test.go`, and `pa4_5_isolation_test.go`. |
| `internal/term/phase7_golden_test.go` | status, diagnostics, and mobile DTO fields | `managed_io_test.go`, `telemetry_service_test.go`, and `pa4_1_isolation_test.go`. |
| `internal/term/pty_framing_test.go`, `pty_ws_test.go` | terminal frame/control and websocket close handling | `pa4_5_isolation_test.go` direct `TerminalTransport` fan-out tests and `managed_io_test.go` IPC boundary tests. |
| `internal/term/recorder_test.go` | byte stream, subscriber fan-out, snapshots, and stale closeout | `pa4_5_isolation_test.go` (`TestPA4_Final_R14_SubscriberFanOut_DirectRecorder_NoGlobalLookup`, stale-generation and retired-transport tests), `step6a_test.go`, and `lifecycle_test.go`. |
| `internal/term/step3_fixture_test.go` | activity/history API retirement | `lifecycle_state_api_test.go` and `managed_io_test.go`. |

The replacement tests are intentionally separate fixtures: each creates its
own V1 launcher/handle and asserts its own property; no shared compatibility
registry is introduced.
