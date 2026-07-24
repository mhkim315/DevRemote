package term

import (
	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/transcript"
)

// testMutationAuthorizer is an explicit dependency for legacy unit fixtures.
// Production code never constructs this implementation.
type testMutationAuthorizer struct{}

func (testMutationAuthorizer) AuthorizeCommit(string, uint64, devicetrust.MutationIntent) error {
	return nil
}
func (testMutationAuthorizer) AuthorizeAndCommit(_ string, _ uint64, _ devicetrust.MutationIntent, commit func() error) error {
	return commit()
}

func testApprovalStore() *AuthoritativeApprovalStore {
	store, err := NewApprovalStore(testMutationAuthorizer{})
	if err != nil {
		panic(err)
	}
	return store
}

func testOwnedPTYRuntime(v1 ManagedPTYLauncherV1, transcriptSvc *transcript.Service) *OwnedPTYRuntime {
	runtime, err := NewOwnedPTYRuntime(testMutationAuthorizer{}, v1, transcriptSvc)
	if err != nil {
		panic(err)
	}
	return runtime
}

func testLifecycleService(owned *OwnedPTYRuntime, transcriptSvc *transcript.Service) *LifecycleService {
	service, err := NewLifecycleService(testMutationAuthorizer{}, owned, transcriptSvc)
	if err != nil {
		panic(err)
	}
	return service
}

func testClaudeCoordinator() *claudeResumeCoordinator {
	coordinator, err := NewClaudeResumeCoordinator(testMutationAuthorizer{})
	if err != nil {
		panic(err)
	}
	return coordinator
}

func testReserveEntry(c *claudeResumeCoordinator, claimToken string, binding ApprovalExecutionBinding) (ResumeHandle, bool) {
	return c.ReserveEntry(claimToken, binding, "test-device", 0)
}

func testDeliveryGate() *RuntimeDeliveryGate {
	gate, err := NewRuntimeDeliveryGate(testMutationAuthorizer{})
	if err != nil {
		panic(err)
	}
	return gate
}

func testManagedCodexService(cfg CodexAppServerEntryConfig, launcher ManagedLauncher) *ManagedCodexService {
	service, err := NewManagedCodexService(testMutationAuthorizer{}, cfg, launcher)
	if err != nil {
		panic(err)
	}
	return service
}

func testManagedClaudeService(cfg ClaudeEntryConfig, launcher ManagedLauncher, attestor ClaudeAttestor) *ManagedClaudeService {
	service, err := NewManagedClaudeService(testMutationAuthorizer{}, cfg, launcher, attestor)
	if err != nil {
		panic(err)
	}
	return service
}
