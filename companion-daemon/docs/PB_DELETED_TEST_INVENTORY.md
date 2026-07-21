# PB deleted-test property inventory

## Scope and method

This inventory compares the tree immediately before physical deletion
(`4fbf43e1^`) with the deletion commit (`4fbf43e1`). It accounts for every
deleted Go test source containing a top-level `func Test`: 24 files and 169
named test functions. The broader PA4 test-count change also includes nested
subtests and deleted implementation/helper files; it is not a count of 396
independent product properties.

Classification labels:

- **intentionally removed legacy behavior** — the assertion described a
  retired product surface and must not be reconstructed.
- **duplicate coverage retained elsewhere** — the property remains covered by
  a named current test.
- **implementation-only (no surviving property)** — it tested the deleted
  abstraction rather than a supported contract.
- **surviving property requiring migration** — this change adds or identifies
  the V1 test which owns the property.
- **independently approved waiver** — an obsolete property is retained here as
  an explicit waiver with its residual risk.

## App composition and lifecycle (`cmd/devremote/app_test.go`, 22)

| Removed test/group | Protected property | Classification | V1 disposition |
|---|---|---|---|
| `TestNewApp_CreatesOwnedRuntime`, `TestPrivateMux_NoDefaultMuxUsage` | construction produces a private HTTP handler and owned lifecycle | surviving property requiring migration | `TestAppV1_CompositionUsesPrivateHTTPMux` uses `NewAppWithDeps`; `TestNewAppWithDeps_ManagedCodexFlag` covers managed-service flag composition. |
| `TestHandlers_RegistryDataIsolation` | one handler cannot read another runtime's sessions | surviving property requiring migration | `TestAppV1_HandlerRuntimeIsolation` uses two `OwnedPTYRuntime` instances and the session API; no global lookup is involved. |
| `TestApp_ShutdownOrder`, `TestApp_ShutdownContinuesAfterError`, `TestApp_ShutdownRespectsDeadline` | bounded shutdown, error aggregation, and cleanup continuation | surviving property requiring migration | `TestAppV1_ShutdownIsBoundedAndContinuesAfterFailure` injects production seams, forces watcher failure plus blocked IPC wait, and proves both errors and IPC close. |
| `TestApp_ShutdownRemovesIPCPathAndAllowsRebind` | IPC path is removed on shutdown and can be rebound | surviving property requiring migration | `TestAppV1_ShutdownRebindsIPCOnRepeatedFreshCycles` uses real `term.StartIPCServer` over two fresh V1 composition cycles. |
| `TestApp_TunnelNotStartedInInsecureMode`, `TestApp_TunnelStartedInProductionMode` | tunnel startup follows local-only mode | surviving property requiring migration | `TestAppV1_TunnelStartupRespectsMode` runs the composition through both modes with its production `StartTunnel` seam. |
| `TestApp_RunContextCancelReturnsNil`, `TestApp_RunReturnsHTTPServeError`, `TestApp_RunJoinsServeAndShutdownErrors` | cancellation is clean; serve errors propagate; serve and shutdown errors join | surviving property requiring migration | `TestAppV1_RunCleansPartialStartWhenIPCUnavailable`, `TestAppV1_RunPropagatesServeFailureAndCleansStartedResources`, and `TestAppV1_RunJoinsServeAndShutdownFailures`. |
| `TestApp_InjectedVerifierUsedByRoutes` | injected verifier protects composed routes | surviving property requiring migration | `TestAppV1_InjectedVerifierGuardsComposedRoutes`. |
| `TestPushNotifier_TokenRace` | notifier token access is race-safe | surviving property requiring migration | `TestAppV1_PushNotifierConcurrentAccess` (run under `-race`). |
| `TestNewAppWithDeps_CatalogWiring` | composition owns the managed catalog/resolver binding | surviving property requiring migration | `TestAppV1_ManagedCatalogIsCompositionOwned`. |
| `TestPA2a_LinkRoutesRemoved_Insecure`, `TestPB2a_LinkAttachRoutesReturn404` | retired link routes are absent | duplicate coverage retained elsewhere | `TestPA2a_LinkRoutesRemoved_DeviceAuth` in `auth_e2e_test.go`. |
| `TestPA2a_LegacyLinkFileNotRead`, `TestPA2a_CLILinkCommandsRemoved`, `TestPA4_1_DefaultConfigEnforcesIsolation` | retired link-file/CLI surface never regains authority; default managed isolation | intentionally removed legacy behavior for link file/CLI; duplicate coverage retained elsewhere for isolation | no link implementation exists; default isolation is covered by `TestPA4_1_DefaultConfigEnforcesIsolation` in `internal/term/pa4_1_isolation_test.go`. |

The added App tests are canonical V1 composition tests: they enter through
`NewAppWithDeps`, `App.Run`, `App.Shutdown`, and `term.StartIPCServer`, rather
than reintroducing a registry or adapter fixture.

## Deleted legacy package tests (53)

| Removed source and tests | Protected property | Classification | V1 disposition |
|---|---|---|---|
| `adapter_capability_test.go`: `TestAdapterCapabilities_ControlledPTY`, `TestAdapterCapabilities_UnknownNoProvider`, `TestAdapterCapabilities_ManagedLifecycleSafeDefault` | legacy adapter capability enumeration | implementation-only (no surviving property) | V1 declares lifecycle operations directly on `PTYHandle`; managed capabilities are catalog-owned. |
| `adapter_contract_test.go` (its `RunAdapterContract` nested suite) | adapter/list/cache/session contract | implementation-only (no surviving property) | removed adapter contract; no replacement interface is allowed. |
| `adapter_golden_test.go`: `TestCanonicalID_Golden`, `TestCanonicalID_LocalIDWithColon`, `TestCanonicalID_Unicode`, `TestCanonicalID_EdgeCases` | canonical ID formatting | duplicate coverage retained elsewhere | `internal/sessionid/sessionid_test.go` covers canonical parsing/round trips. |
| `controlled_pty_r6_test.go`: `TestPA2cR6_ProductionCompareAndTerminate_MatchTerminates`, `TestPA2cR6_DetachBeforeClose_ReplacementSurvives`, `TestPA2cR6_StaleIdentity_ProductionAdapterReturnsStale`, `TestPA2cR6_KnownBad_CheckThenDelete_NegativeControl` | identity-guarded old-process termination | surviving property requiring migration | `TestPA2c_OwnedPTY_StaleGenerationRejected`, `TestPA2c_R2_ReplacementBetweenClaimAndCleanup_NotTerminated`, and `TestPA2c_R1_ReplacementDuringBlockedSignal_NeverSignalsNewProcess`. |
| `controlled_pty_test.go`: `TestControlledPTY_CreateSession`, `TestControlledPTY_CaptureMode`, `TestControlledPTY_Capabilities`, `TestControlledPTY_StreamOpener`, `TestControlledPTY_InputWriter`, `TestControlledPTY_TerminateSession`, `TestControlledPTY_Name` | old adapter/session object API | implementation-only (no surviving property) | creation/transport/lifecycle are separately owned by `ManagedPTYLauncherV1`, `PTYHandle`, and `OwnedPTYRuntime`. |
| `fixture_adapter_test.go`: `TestFixtureAdapter_Contract` | old fixture adapter | implementation-only (no surviving property) | direct V1 `migrationLauncher`/`migrationHandle` fixtures replace it. |
| `id_parser_compat_test.go`: `TestPA2b_WrapperCompat_IdenticalResults`, `TestPA2b_WrapperCompat_SentinelIdentity`, `TestPA2b_WrapperCompat_TypeAlias`, `TestPA2b_MigrateLegacyID_UnchangedAndStaysInMux` | wrapper compatibility | intentionally removed legacy behavior | wrappers were physically deleted; `sessionid_test.go` owns supported parsing. |
| `phase1_test.go`: `TestSessionRef_Canonical`, `TestSessionRef_Validate`, `TestValidateAdapterName`, `TestCanonicalID_URLRoundTrip`, `TestCanonicalID_URLEncodeRoundTrip` | session identity parsing | duplicate coverage retained elsewhere | `sessionid_test.go` and `TestPA2b_ArchGate_NoDuplicateParser`. |
| `phase1_test.go`: `TestRegistry_RejectDuplicateAdapter`, `TestSentinelErrors_Distinguishable`, `TestNewRegistry_DuplicateAdapter`, `TestRegister_InvalidName`, `TestFindSession_InvalidID`, `TestFindSession_AdapterUnavailable`, `TestRefresh_TimeoutPreservation`, `TestCreateSession_ReturnsLocalID`, `TestRefresh_TimeoutWrapsErrTimeout`, `TestRefresh_CancelNotTimeout`, `TestFindSession_EndedSession`, `TestFindSession_StaleCacheAndRefreshFailure`, `TestRegistry_SlowAdapterDoesNotBlock` | registry discovery/cache/create behavior | implementation-only (no surviving property) | the registry is deleted; V1 has no discovery cache or adapter creation path. |
| `registry_atomic_test.go`: `TestPA2cR5_TerminationWinsLock_ReplacementSurvives`, `TestPA2cR5_ReplacementInstallsFirst_StaleRejected`, `TestPA2cR5_AtomicTerminate_MatchTerminates`, `TestPA2cR5_AtomicTerminate_NotFound`, `TestPA2cR5_NonImplementingAdapter_FailsClosed` | replacement/stale cleanup atomicity | surviving property requiring migration | `TestPB5V1_ReplacementCleansOnlyPriorGeneration`, `TestPA2c_OwnedPTY_StaleGenerationRejected`, and `TestPA2c_R1_ReplacementDuringBlockedSignal_NeverSignalsNewProcess`. |
| `registry_test.go`: `TestRegistryDeadlockAndCache`, `TestStaleCacheOnFailure`, `TestRegistrySessionsStableOrder`, `TestRegistryIsolation`, `TestRegistryCreateSessionInvalidation`, `TestRegistryTerminateSessionInvalidation`, `TestRegistryFindSessionPropagatesCancellation` | registry cache and invalidation | implementation-only (no surviving property) | the only supported read model is `ManagedRuntimeCatalog`/`OwnedPTYRuntime`; no registry cache is retained. |

## Deleted terminal test groups (94)

| Removed source and tests | Protected property | Classification | V1 disposition |
|---|---|---|---|
| `agent_agnostic_test.go`: all six `TestAgnostic_*` tests | tolerant managed status/DTO reading | duplicate coverage retained elsewhere | `managed_io_test.go`, `managed_api_b_test.go`, and catalog isolation tests cover current DTOs. |
| `api_golden_test.go`: all eight `TestAPIGolden_*` tests | session list/create/delete HTTP responses | surviving property requiring migration | `create_test.go`, `lifecycle_state_api_test.go`, and `lifecycle_test.go` assert the supported V1 API paths and status transitions. |
| `capability_boundary_test.go`: `TestAPISessions_AdapterCapabilities_ManagedLifecycleBoundary` | adapter capability field boundary | intentionally removed legacy behavior | old adapter capabilities are not part of V1; managed capabilities come only from `ManagedRuntimeCatalog` and are tested in `pa4_1_isolation_test.go`. |
| `controlled_pty_e2e_test.go`: all six `TestControlledPTY_*` tests | PTY input, capture, cleanup, cwd | surviving property requiring migration | `create_test.go`, `lifecycle_test.go`, and `pa4_5_isolation_test.go` use V1 handles/transports. |
| `fixture_e2e_test.go`: `TestFixtureE2E_GetSessions`, `TestFixtureE2E_CreateAndDelete`, `TestFixtureE2E_UnsupportedCapability`, `TestFixtureE2E_WebSocket`, `TestFixtureE2E_Resize`, `TestFixtureE2E_StreamContent`, `TestFixtureE2E_Telemetry`, `TestFixtureE2E_MobileSchema`, `TestFixtureE2E_AdapterCapabilitiesInAPI`, `TestE10_CommandCwdReachesCreateOptions` | old fixture end-to-end harness | surviving property requiring migration | create/lifecycle/transport assertions are split into `create_test.go`, `lifecycle_test.go`, `pa4_5_isolation_test.go`, and `managed_io_test.go`; retired adapter-capability expectations are intentionally removed. |
| `managed_api_test.go`: all four `TestManagedREST_*` tests | managed REST catalog isolation | duplicate coverage retained elsewhere | `managed_api_b_test.go`, `pa4_1_isolation_test.go`, and `pa4_2_isolation_test.go`. |
| `managed_catalog_test.go`: all twenty `TestCatalog_*` and `TestAppendCatalogRows_*` tests | managed catalog ordering, binding, privacy, concurrency | duplicate coverage retained elsewhere | `managed_registry_test.go`, `pa4_1_isolation_test.go`, `pa4_3_isolation_test.go`, and `pa4_5_isolation_test.go`. |
| `managed_remediation_test.go`: all seven `TestManaged*` shutdown/create/bounded-discovery tests | managed create and shutdown safety | duplicate coverage retained elsewhere | `managed_codex_test.go`, `managed_lifecycle_c_test.go`, `managed_pump_test.go`, and `sp0_live_test.go`. |
| `phase7_golden_test.go`: `TestStatusTaxonomy_EmptyVsUnavailable`, `TestCapabilityGolden_AllBackends`, `TestDiagnostics_APISufficient`, `TestMobileLegacyCheck_Classification` | legacy status/mobile diagnostic shape | intentionally removed legacy behavior | status/capability taxonomy was replaced by managed catalog DTOs; current handler behavior is covered by `managed_api_b_test.go` and `telemetry_service_test.go`. |
| `pty_framing_test.go`: all four `TestWSFraming_*` tests and `pty_ws_test.go`: both `TestHandleWS_*` tests | old websocket framing/close implementation | surviving property requiring migration | direct generation-bound transport properties are covered by `TestPA4_Final_R14_SubscriberFanOut_DirectRecorder_NoGlobalLookup`, `TestPA4_Final_R14_SubscriberFanOut_RetiredTransport_FailClosed`, and `TestPA4_Final_R17_SubscriberFanOut_RetireRacingSubscribe_Rejected`. |
| `recorder_test.go`: all seventeen `TestRecorder_*`/`TestIsClearScreenSnapshot` tests | global recorder map, stream fan-out, stale closeout | surviving property requiring migration for fan-out/stale isolation; intentionally removed legacy behavior for global lookup | `pa4_5_isolation_test.go`, `step6a_test.go`, and `lifecycle_test.go` use direct recorder ownership; no global recorder lookup remains. |
| `step3_fixture_test.go`: all five `TestStep3_*` tests | retired activity/history endpoints and normal list | intentionally removed legacy behavior for retired endpoints; duplicate coverage retained elsewhere for normal list | `lifecycle_state_api_test.go` owns supported session listing; transcript behavior is covered by `transcript` package tests. |

## Independently approved waivers

| Removed test | Protected property | Why it no longer applies | Replacement coverage | Residual risk |
|---|---|---|---|---|
| `TestPA2a_LegacyLinkFileNotRead` | daemon must not read/mutate `~/.devremote/links.json` | link persistence and link commands were removed as a product surface; there is no reader or writer to exercise | physical route/command removal is covered by `TestPA2a_LinkRoutesRemoved_DeviceAuth` | a future feature could reintroduce a file consumer without recreating this exact regression test; code review must reject it unless a new owned contract is introduced. |
| `TestPA2a_CLILinkCommandsRemoved` | retired CLI commands remain unavailable | commands are absent from the command tree, not merely disabled | route removal coverage above plus command-tree review | an unrelated CLI addition could reuse a retired name; this requires explicit product approval. |
| `TestPA2b_WrapperCompat_*`, `TestPA2b_MigrateLegacyID_UnchangedAndStaysInMux` | old compatibility wrappers preserve behavior | wrappers and the package that hosted them were physically deleted | `sessionid_test.go` covers the supported identity API | downstream code cannot rely on removed symbols; this is intentional breaking cleanup. |

No other removed test is waived: every non-legacy product property is either
covered by a named retained test or migrated in this change.
