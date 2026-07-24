package main

import (
	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/term"
	"devremote/companion-daemon/internal/transcript"
)

// testMutationAuthorizer is explicit test wiring; production composition
// always supplies the registry-backed authorizer.
type testMutationAuthorizer struct{}

func (testMutationAuthorizer) AuthorizeCommit(string, uint64, devicetrust.MutationIntent) error {
	return nil
}
func (testMutationAuthorizer) AuthorizeAndCommit(_ string, _ uint64, _ devicetrust.MutationIntent, commit func() error) error {
	return commit()
}

func testApprovalStore() *term.AuthoritativeApprovalStore {
	store, err := term.NewApprovalStore(testMutationAuthorizer{})
	if err != nil {
		panic(err)
	}
	return store
}

func testOwnedPTYRuntime(v1 term.ManagedPTYLauncherV1, transcriptSvc *transcript.Service) *term.OwnedPTYRuntime {
	runtime, err := term.NewOwnedPTYRuntime(testMutationAuthorizer{}, v1, transcriptSvc)
	if err != nil {
		panic(err)
	}
	return runtime
}

func testLifecycleService(owned *term.OwnedPTYRuntime, transcriptSvc *transcript.Service) *term.LifecycleService {
	service, err := term.NewLifecycleService(testMutationAuthorizer{}, owned, transcriptSvc)
	if err != nil {
		panic(err)
	}
	return service
}

func testManagedCodexService(cfg term.CodexAppServerEntryConfig, launcher term.ManagedLauncher) *term.ManagedCodexService {
	service, err := term.NewManagedCodexService(testMutationAuthorizer{}, cfg, launcher)
	if err != nil {
		panic(err)
	}
	return service
}

func testManagedClaudeService(cfg term.ClaudeEntryConfig, launcher term.ManagedLauncher, attestor term.ClaudeAttestor) *term.ManagedClaudeService {
	service, err := term.NewManagedClaudeService(testMutationAuthorizer{}, cfg, launcher, attestor)
	if err != nil {
		panic(err)
	}
	return service
}
