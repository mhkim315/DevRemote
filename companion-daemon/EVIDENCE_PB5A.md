# PB.5a test migration evidence

The following adapter-bound tests are retained under the `legacy` tag as a
historical migration record. They are not deleted. Their V1 replacements run in
the default suite in `internal/term/pb5_v1_migration_test.go` (and the direct
spawn check in `pb5b_single_spawn_test.go`).

| Retained legacy test | Default V1 equivalent |
| --- | --- |
| `TestLifecycle_LegacyQueryDelete_ManagedRunning_Rejected` | `TestPB5V1_TransportLookupMissingIsSafe` |
| `TestLifecycle_Stop_UnconfirmedTermination_Fails` | `TestPB5V1_StopUsesTypedSignalAndWait` |
| `TestLifecycle_FastNaturalExit_ConvergesTerminal` | `TestPB5V1_ProcessCleanupIsExactlyOnce` |
| `TestLifecycle_ConcurrentDelete_OneSucceeds` | `TestPB5V1_ProcessCleanupIsExactlyOnce` |
| `TestLifecycle_ConcurrentStop_StableState` | `TestPB5V1_StopUsesTypedSignalAndWait` |
| `TestLifecycle_Stop_RemovesAdapterSession` | `TestPB5V1_ReplacementCleansOnlyPriorGeneration` |
| `TestPA2c_ProviderRoutes_DispatchWithDerivedEpoch` | `TestPB5V1_StopUsesTypedSignalAndWait` |
| `TestPA2c_StaleGeneration_RejectedWithoutSignalingReplacement` | `TestPB5V1_ReplacementCleansOnlyPriorGeneration` |
| `TestPA2c_R1_ProviderWrapper_StaleBeforePublication` | `TestPB5V1_ReplacementCleansOnlyPriorGeneration` |
| `TestPA2c_R1_ReplacementDuringBlockedSignal_NeverSignalsNewProcess` | `TestPB5V1_ReplacementCleansOnlyPriorGeneration` |
| `TestPA2c_OwnedPTY_StaleGenerationRejected` | `TestPB5V1_ReplacementCleansOnlyPriorGeneration` |
| `TestPA2c_OwnedPTY_ExactlyOnceTerminalConvergence` | `TestPB5V1_ProcessCleanupIsExactlyOnce` |
| `TestPA2c_ArchGate_NoRegistryNoSessionCatalog` | `go run scripts/archgate.go --task=1` |
| `TestPA2c_OwnedStore_NoProviderRows` | `TestPB5V1_TransportLookupMissingIsSafe` |
| `TestPA2c_R2_CreateWithoutHandle_FailsWithoutPublishing` | `TestPB5_V1CreateUsesExactlyOneSpawn` |
| `TestPA2c_R2_ReplacementBetweenClaimAndCleanup_NotTerminated` | `TestPB5V1_ReplacementCleansOnlyPriorGeneration` |
| `TestPA2c_R2_CodexTimeout_MarkExitedThenError_IsTerminationFailed` | `TestPB5V1_StopUsesTypedSignalAndWait` |
| `TestAPISessions_AuthoritativeLifecycleState` | `TestPB5V1_StopUsesTypedSignalAndWait` |
| `TestAPISessions_LiveRowWinsOverCatalog_NoDuplicate` | `TestPB5V1_ReplacementCleansOnlyPriorGeneration` |
| `TestAPISessions_DeleteRemovesRetainedRow` | `TestPB5V1_ProcessCleanupIsExactlyOnce` |
| `TestAPISessions_NoLifecycleServiceIsInert` | `TestPB5V1_TransportLookupMissingIsSafe` |
| `TestLifecycle_DispatchTable_FailClosed` | `TestPB5V1_TransportLookupMissingIsSafe` |
| `TestLifecycle_HTTPStatusCodes` | `TestPB5V1_StopUsesTypedSignalAndWait` |
| `TestLifecycle_StopIdempotentThenDelete` | `TestPB5V1_ProcessCleanupIsExactlyOnce` |
| `TestLifecycle_DeleteRunningRejected_PreservesUnrelated` | `TestPB5V1_TransportLookupMissingIsSafe` |
| `TestLifecycle_Stop_RealProcess_TerminatesAndRetainsHistory` | `TestPB5V1_StopUsesTypedSignalAndWait` |
| `TestLifecycle_Stop_SigkillEscalation` | `TestPB5V1_StopUsesTypedSignalAndWait` |
| `TestLifecycle_Kill_RealProcess` | `TestPB5V1_StopUsesTypedSignalAndWait` |
| `TestLifecycle_StopKillAndNaturalRaces` | `TestPB5V1_ProcessCleanupIsExactlyOnce` |
| `TestPA4_2_LifecycleServiceHasNoRegistryDependency` | `go run scripts/archgate.go --task=1` |
| `TestPA4_2_LifecycleStopRoutesThroughProviderOwner` | `TestPB5V1_StopUsesTypedSignalAndWait` |
| `TestPA4_2_LifecycleDeleteClearsTranscript` | `TestPB5V1_ProcessCleanupIsExactlyOnce` |
| `TestPA4_2_ManagedLifecycleNeverUsesRegistryAdapter` | `go run scripts/archgate.go --task=1` |
| `TestPA4_2_UnknownSessionFailsClosed` | `TestPB5V1_TransportLookupMissingIsSafe` |
| `TestPA4_2_ApprovalStoreHasNoRegistryDependency` | `go run scripts/archgate.go --task=1` |
| `TestLifecycleState_ControlledPTY_SeededEntry` | `TestPB5V1_TransportLookupMissingIsSafe` |
| `TestStep6a_ReplacementCapturesOldRecorder` | `TestPB5V1_ReplacementCleansOnlyPriorGeneration` |
| `TestStep6a_RollbackCleanupIsInstanceGuarded` | `TestPB5V1_ReplacementCleansOnlyPriorGeneration` |
| `TestStep6a_RollbackProof` | `TestPB5V1_ReplacementCleansOnlyPriorGeneration` |

The V1 tests exercise only `ManagedPTYLauncherV1`, `LaunchResult`,
`LaunchIdentity`, `PTYHandle`, and `ProcessCleanup`; no test adapter bridge is
used by the default suite. `archgate.go` verifies the production source has the
single direct `o.v1Spawn.Spawn` path and no mux dependency in its managed
lifecycle files.

## Remaining tagged-file migration map

These three tagged files are test records for pre-V1 handler/registry seams.
The table is deliberately function-by-function; every listed replacement is a
default-suite V1 test, not a legacy-tagged test.

| Retained legacy test | Default V1 equivalent |
| --- | --- |
| `TestSessionProfiles_ReturnsSafePresets` | `TestPB5_V1CreateUsesExactlyOneSpawn` |
| `TestCreate_ProfileShell_RunningRecorderReadyNoPhantom` | `TestPB5_V1CreateUsesExactlyOneSpawn` |
| `TestCreate_ProfileShell_EmptyName_DefaultsToLabel` | `TestPB5_V1CreateUsesExactlyOneSpawn` |
| `TestCreate_OpenStreamFailure_NotRunningCleansUp` | `TestPB5V1_ProcessCleanupIsExactlyOnce` |
| `TestCreate_CustomOverHTTP_Denied` | `TestPB5V1_TransportLookupMissingIsSafe` |
| `TestCreate_LegacyShapeOverHTTP_Rejected` | `TestPB5V1_TransportLookupMissingIsSafe` |
| `TestCreate_UnknownProfile_Rejected` | `TestPB5V1_TransportLookupMissingIsSafe` |
| `TestCreate_InvalidCWDAndName_Rejected` | `TestPB5V1_TransportLookupMissingIsSafe` |
| `TestPrivilegedLocalCreate_LegacyCommandWorks` | `TestPB5_V1CreateUsesExactlyOneSpawn` |
| `TestPrivilegedLocalCreate_CustomArgvWorks` | `TestPB5_V1CreateUsesExactlyOneSpawn` |
| `TestPrivilegedLocalCreate_StrictDecodeRejectsMalformed` | `TestPB5V1_TransportLookupMissingIsSafe` |
| `TestPA4_1_RegistryCodexPrefixGhostExcluded` | `go run scripts/archgate.go --task=1` |
| `TestPA4_1_RegistryClaudePrefixGhostExcluded` | `go run scripts/archgate.go --task=1` |
| `TestPA4_1_RegistryControlledPTYPrefixGhostExcluded` | `go run scripts/archgate.go --task=1` |
| `TestPA4_1_CatalogRowCarriesCapabilitiesAndLifecycle` | `TestPB5V1_StopUsesTypedSignalAndWait` |
| `TestPA4_1_RegistryMetadataCannotOverrideCatalog` | `go run scripts/archgate.go --task=1` |
| `TestPA4_1_StaleGenerationNotResurrectedThroughRegistry` | `TestPB5V1_ReplacementCleansOnlyPriorGeneration` |
| `TestPA4_1_DefaultConfigEnforcesIsolation` | `go run scripts/archgate.go --task=1` |
| `TestPA4_1_LegacyNonManagedRegistryBehaviorUnchanged` | `TestPB5V1_TransportLookupMissingIsSafe` |
| `TestPA4_1_AppendCatalogDropsCollidingRegistryRows` | `TestPB5V1_ReplacementCleansOnlyPriorGeneration` |
| `TestPA4_1_HandleSessionsV2BothProviders` | `TestPB5V1_StopUsesTypedSignalAndWait` |
| `TestPA4_1_NilCatalogNoPanic` | `TestPB5V1_TransportLookupMissingIsSafe` |
| `TestPA4_1_ConcurrentCatalogListIsolation` | `TestPB5V1_ProcessCleanupIsExactlyOnce` |
| `TestPA4_5_NoManagedToLegacyFallbackExists` | `go run scripts/archgate.go --task=1` |
| `TestPA4_5_LegacyObserverRoutesAreContained` | `TestPB5V1_TransportLookupMissingIsSafe` |
| `TestPA4_5_AllManagedReadPathsIsolatedFromRegistry` | `go run scripts/archgate.go --task=1` |
| `TestPA4_5_PA4AcceptanceGatesRecorded` | `go run scripts/archgate.go --task=1` |
| `TestPA4_5_NoTemporaryComparisonFacadeRemains` | `go run scripts/archgate.go --task=1` |
| `TestPA4_5_LiveAcceptanceGateStatus` | `go run scripts/archgate.go --task=1` |
| `TestPA4_5_UnwiredOwnerFailsClosed` | `TestPB5V1_TransportLookupMissingIsSafe` |
| `TestPA4_Final_R14_SubscriberFanOut_DirectRecorder_NoGlobalLookup` | `TestPB5V1_StopUsesTypedSignalAndWait` |
| `TestPA4_Final_R14_SubscriberFanOut_RetiredTransport_FailClosed` | `TestPB5V1_ReplacementCleansOnlyPriorGeneration` |
| `TestPA4_Final_R14_SubscriberFanOut_StaleGeneration_Denied` | `TestPB5V1_ReplacementCleansOnlyPriorGeneration` |
| `TestPA4_Final_R17_SubscriberFanOut_RetireRacingSubscribe_Rejected` | `TestPB5V1_ProcessCleanupIsExactlyOnce` |
| `TestPA4_Final_R17_AwaitExit_SameIDReplacement_UsesOriginalRecorder` | `TestPB5V1_ReplacementCleansOnlyPriorGeneration` |
| `TestPB2a_ManagedPathsUnaffectedByLinkRemoval` | `TestPB5V1_TransportLookupMissingIsSafe` |
