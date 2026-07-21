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
